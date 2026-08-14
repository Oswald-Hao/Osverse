package install

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const (
	maxArchiveEntries = 2048
	versionTimeout    = 15 * time.Second
	versionOutput     = 64 * 1024
)

var (
	errDownload            = errors.New("artifact download failed")
	errHashMismatch        = errors.New("artifact hash mismatch")
	errUnsafeArchive       = errors.New("unsafe artifact archive")
	errVersionVerification = errors.New("installed version verification failed")
	errExternalCommand     = errors.New("external command entry exists")
)

type linkState struct {
	exists bool
	target string
}

type profileState struct {
	path    string
	exists  bool
	mode    os.FileMode
	content []byte
	changed bool
}

func (manager *Manager) execute(
	ctx context.Context,
	stored storedPlan,
	protocol proxyservice.Protocol,
	port int,
	progress func(progressUpdate),
) (returnErr error) {
	item := stored.artifact
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := manager.ensureStateRoot()
	if err != nil {
		return err
	}
	toolRoot, err := ensureDirectories(root, 0o700, "tools", item.ID)
	if err != nil {
		return err
	}
	binRoot, err := ensureDirectories(manager.home, 0o755, ".local", "bin")
	if err != nil {
		return err
	}
	currentPath := filepath.Join(toolRoot, "current")
	shimPath := filepath.Join(binRoot, item.Command)
	currentBefore, err := inspectOwnedLink(currentPath, toolRoot)
	if err != nil {
		return err
	}
	shimBefore, err := inspectOwnedLink(shimPath, toolRoot)
	if err != nil {
		return errExternalCommand
	}
	profileBefore := make([]profileState, 0, len(manager.profiles))
	for _, profilePath := range manager.profiles {
		state, err := inspectProfileState(profilePath)
		if err != nil {
			return err
		}
		profileBefore = append(profileBefore, state)
	}

	stagingRoot, err := ensureDirectories(root, 0o700, "staging")
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(stagingRoot, item.ID+"-")
	if err != nil {
		return err
	}
	if err := os.Chmod(staging, 0o700); err != nil {
		return err
	}
	defer func() { _ = removeStaging(stagingRoot, staging) }()

	progress(progressUpdate{phase: "downloading", progress: 5, message: "正在下载官方安装包"})
	archivePath := filepath.Join(staging, "artifact.tgz")
	if err := manager.download(ctx, item, protocol, port, archivePath, progress); err != nil {
		return err
	}

	progress(progressUpdate{phase: "verifying", progress: 72, message: "正在校验并展开安装包"})
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		return err
	}
	if err := extractArchive(ctx, archivePath, payload, item.ExpandedBytesLimit); err != nil {
		return err
	}
	binaryPath := filepath.Join(payload, filepath.FromSlash(item.BinaryPath))
	if err := validateExtractedBinary(payload, binaryPath); err != nil {
		return err
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return err
	}
	if err := manager.verifyVersion(ctx, binaryPath, item); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(payload, ".osverse-artifact-sha256"), []byte(item.SHA256+"\n"), 0o600); err != nil {
		return err
	}

	progress(progressUpdate{phase: "committing", progress: 92, message: "正在原子切换命令版本"})
	finalPath := filepath.Join(toolRoot, item.Version)
	if err := commitVersion(payload, finalPath, item); err != nil {
		return err
	}
	backupRoot, err := ensureDirectories(root, 0o700, "state", "profile-backups")
	if err != nil {
		return err
	}
	journalPath, err := manager.writeTransactionJournal(root, stored, currentBefore, shimBefore, profileBefore)
	if err != nil {
		return err
	}
	defer func() {
		if returnErr == nil {
			return
		}
		restored := true
		for index := len(profileBefore) - 1; index >= 0; index-- {
			state := profileBefore[index]
			state.changed = true
			if err := restoreProfile(state); err != nil {
				restored = false
			}
		}
		if err := restoreLink(shimPath, shimBefore); err != nil {
			restored = false
		}
		if err := restoreLink(currentPath, currentBefore); err != nil {
			restored = false
		}
		if restored {
			_ = removeJournal(journalPath)
		}
	}()
	if err := manager.replaceLink(currentPath, item.Version); err != nil {
		return err
	}
	shimTarget := filepath.Join(toolRoot, "current", filepath.FromSlash(item.BinaryPath))
	if err := manager.replaceLink(shimPath, shimTarget); err != nil {
		return err
	}
	for _, state := range profileBefore {
		backupPath := filepath.Join(backupRoot, strings.TrimPrefix(filepath.Base(state.path), ".")+".before-osverse")
		_, err := ensureProfilePATHFromState(state, backupPath)
		if err != nil {
			return err
		}
	}
	if err := removeJournal(journalPath); err != nil {
		return err
	}
	progress(progressUpdate{phase: "committing", progress: 99, message: "命令入口已更新"})
	return nil
}

func (manager *Manager) ensureStateRoot() (string, error) {
	share, err := ensureDirectories(manager.home, 0o755, ".local", "share")
	if err != nil {
		return "", err
	}
	return ensureDirectories(share, 0o700, "osverse")
}

func ensureDirectories(base string, finalMode os.FileMode, components ...string) (string, error) {
	current := filepath.Clean(base)
	createdFinal := false
	for index, component := range components {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return "", errors.New("invalid managed path component")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, finalMode); err != nil {
				return "", err
			}
			createdFinal = index == len(components)-1
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return "", errors.New("managed directory path is not a directory")
		}
	}
	if createdFinal {
		if err := os.Chmod(current, finalMode); err != nil {
			return "", err
		}
	}
	return current, nil
}

func (manager *Manager) download(
	ctx context.Context,
	item artifact,
	protocol proxyservice.Protocol,
	port int,
	destination string,
	progress func(progressUpdate),
) error {
	if err := validateArtifactRequestURL(item.URL); err != nil {
		return err
	}
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: client unavailable", errDownload)
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		return errors.New("artifact redirects are disabled")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: request", errDownload)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Installer")
	response, err := clientCopy.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: transport", errDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		(response.ContentLength >= 0 && response.ContentLength != item.DownloadBytes) {
		return fmt.Errorf("%w: unexpected response", errDownload)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	limited := io.LimitReader(response.Body, item.DownloadBytes+1)
	written, err := copyWithContext(ctx, io.MultiWriter(file, hash), limited, func(total int64) {
		percent := 5 + int((total*60)/item.DownloadBytes)
		progress(progressUpdate{phase: "downloading", progress: percent, message: "正在下载官方安装包"})
	})
	if err != nil {
		return err
	}
	if written != item.DownloadBytes {
		return fmt.Errorf("%w: unexpected size", errDownload)
	}
	if hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return nil
}

func extractArchive(ctx context.Context, archivePath, destination string, expandedLimit int64) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("%w: gzip", errUnsafeArchive)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var expanded int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: tar", errUnsafeArchive)
		}
		entries++
		if entries > maxArchiveEntries || !safeArchiveName(header.Name) {
			return errUnsafeArchive
		}
		cleanName := path.Clean(header.Name)
		target := filepath.Join(destination, filepath.FromSlash(cleanName))
		if !pathWithin(destination, target) {
			return errUnsafeArchive
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := mkdirArchivePath(destination, target); err != nil {
				return fmt.Errorf("%w: directory", errUnsafeArchive)
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || expanded > expandedLimit-header.Size {
				return errUnsafeArchive
			}
			expanded += header.Size
			if err := mkdirArchivePath(destination, filepath.Dir(target)); err != nil {
				return fmt.Errorf("%w: parent", errUnsafeArchive)
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("%w: file", errUnsafeArchive)
			}
			written, copyErr := copyWithContext(ctx, output, io.LimitReader(tarReader, header.Size), nil)
			closeErr := output.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil || written != header.Size {
				return fmt.Errorf("%w: truncated entry", errUnsafeArchive)
			}
		default:
			return errUnsafeArchive
		}
	}
	return nil
}

func safeArchiveName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") {
		return false
	}
	withoutTrailingSlash := strings.TrimSuffix(name, "/")
	clean := path.Clean(withoutTrailingSlash)
	return clean == withoutTrailingSlash && clean != "." && clean != ".." &&
		(clean == "package" || strings.HasPrefix(clean, "package/"))
}

func mkdirArchivePath(root, target string) error {
	if !pathWithin(root, target) {
		return errUnsafeArchive
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return errUnsafeArchive
		}
	}
	return nil
}

func validateExtractedBinary(root, binary string) error {
	if !pathWithin(root, binary) {
		return errUnsafeArchive
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errUnsafeArchive
	}
	return nil
}

func (manager *Manager) verifyVersion(ctx context.Context, binary string, item artifact) error {
	result, err := manager.runner.Run(ctx, platform.CommandRequest{
		Path: binary, Args: append([]string(nil), item.VersionArgs...),
		Timeout: versionTimeout, OutputLimit: versionOutput,
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut || result.Truncated || ctx.Err() != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return errVersionVerification
	}
	combined := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if !containsVersionToken(combined, item.Version) {
		return errVersionVerification
	}
	return nil
}

func containsVersionToken(output, version string) bool {
	for _, field := range strings.Fields(output) {
		candidate := strings.Trim(field, "vV()[],")
		if candidate == version {
			return true
		}
	}
	return false
}

func commitVersion(payload, finalPath string, item artifact) error {
	info, err := os.Lstat(finalPath)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("managed version path is occupied")
		}
		marker, markerErr := os.ReadFile(filepath.Join(finalPath, ".osverse-artifact-sha256"))
		binary := filepath.Join(finalPath, filepath.FromSlash(item.BinaryPath))
		if markerErr != nil || strings.TrimSpace(string(marker)) != item.SHA256 || validateExtractedBinary(finalPath, binary) != nil {
			return errors.New("managed version directory failed ownership validation")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(payload, finalPath)
}

func inspectOwnedLink(linkPath, ownedRoot string) (linkState, error) {
	info, err := os.Lstat(linkPath)
	if errors.Is(err, os.ErrNotExist) {
		return linkState{}, nil
	}
	if err != nil {
		return linkState{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return linkState{}, errExternalCommand
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		return linkState{}, err
	}
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(linkPath), resolved)
	}
	if !pathWithin(ownedRoot, filepath.Clean(resolved)) {
		return linkState{}, errExternalCommand
	}
	return linkState{exists: true, target: target}, nil
}

func replaceSymlink(linkPath, target string) error {
	temporary := linkPath + ".osverse-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, linkPath); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func restoreLink(linkPath string, state linkState) error {
	if !state.exists {
		info, err := os.Lstat(linkPath)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("managed link recovery path is occupied")
		}
		return os.Remove(linkPath)
	}
	return replaceSymlink(linkPath, state.target)
}

func removeStaging(stagingRoot, staging string) error {
	if !pathWithin(stagingRoot, staging) || filepath.Dir(filepath.Clean(staging)) != filepath.Clean(stagingRoot) {
		return errors.New("refusing unsafe staging cleanup")
	}
	return os.RemoveAll(staging)
}

func pathWithin(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader, progress func(int64)) (int64, error) {
	buffer := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
			if progress != nil {
				progress(total)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func validateArtifactRequestURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "registry.npmjs.org" {
		return errDownload
	}
	return nil
}
