package harnessinstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxPackageDownload = 64 * 1024 * 1024
	maxPackageExpanded = 192 * 1024 * 1024
	maxTotalDownload   = 384 * 1024 * 1024
	maxNodeExpanded    = 160 * 1024 * 1024
	packageWorkers     = 8
)

var (
	errDownload        = errors.New("Harness download failed")
	errHashMismatch    = errors.New("Harness artifact hash mismatch")
	errVersion         = errors.New("Harness version verification failed")
	errExternalCommand = errors.New("Harness command is owned by another program")
	errRollback        = errors.New("Harness activation rollback failed")
)

//go:embed assets/linux-x64/pty.node
var linuxX64PTY []byte

const linuxX64PTYSHA256 = "0b8f2422678f8c02f5d6034ad6b5a20e5333c48be025869389e89f22d7ece0b1"

type progressFunc func(done, total int, message string)

func buildPayload(
	ctx context.Context,
	client *http.Client,
	goos, goarch, staging, payload string,
	progress progressFunc,
) error {
	if client == nil {
		return fmt.Errorf("%w: client unavailable", errDownload)
	}
	lock, err := builtInLock()
	if err != nil {
		return err
	}
	packages, err := packagesForTarget(lock, goos, goarch)
	if err != nil {
		return err
	}
	runtimeItem, err := runtimeForTarget(goos, goarch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(payload, 0o700); err != nil {
		return err
	}
	progress(0, len(packages)+1, "正在下载 Node.js 运行时")
	archivePath := filepath.Join(staging, "node-runtime.archive")
	if err := downloadNode(ctx, client, runtimeItem, archivePath); err != nil {
		return err
	}
	if err := extractNode(ctx, runtimeItem, archivePath, filepath.Join(payload, filepath.FromSlash(runtimeItem.Executable))); err != nil {
		return err
	}
	progress(1, len(packages)+1, "正在下载并校验 Harness 依赖")
	if err := downloadAndExtractPackages(ctx, client, packages, staging, payload, progress); err != nil {
		return err
	}
	if err := finalizeNativeAssets(goos, goarch, payload); err != nil {
		return err
	}
	return writeRuntimeManifest(payload, goos, goarch, len(packages))
}

func hardenedClient(client *http.Client) *http.Client {
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("Harness artifact redirects are disabled")
	}
	return &copy
}

func downloadNode(ctx context.Context, client *http.Client, item runtimeArtifact, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return fmt.Errorf("%w: Node request", errDownload)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Harness-Installer")
	response, err := hardenedClient(client).Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: Node transport", errDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != item.Size) {
		return fmt.Errorf("%w: unexpected Node response", errDownload)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(output, hash), io.LimitReader(response.Body, item.Size+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != item.Size || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	return nil
}

func extractNode(ctx context.Context, item runtimeArtifact, archivePath, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	wanted := item.ArchiveRoot + "/" + item.ArchivePath
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if item.GOOS == "windows" {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		return extractNodeZip(ctx, file, info.Size(), wanted, destination, maxNodeExpanded)
	}
	return extractNodeTar(ctx, file, wanted, destination, maxNodeExpanded)
}

func downloadAndExtractPackages(
	ctx context.Context,
	client *http.Client,
	packages []lockedPackage,
	staging, payload string,
	progress progressFunc,
) error {
	if len(packages) == 0 {
		return errors.New("Harness package set is empty")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type job struct {
		index int
		item  lockedPackage
	}
	jobs := make(chan job)
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	var completedMu sync.Mutex
	completed := 0
	var downloadedMu sync.Mutex
	var downloaded int64
	worker := func() {
		defer workers.Done()
		for current := range jobs {
			if ctx.Err() != nil {
				return
			}
			archive := filepath.Join(staging, fmt.Sprintf("package-%04d.tgz", current.index))
			size, err := downloadPackage(ctx, client, current.item, archive)
			if err == nil {
				downloadedMu.Lock()
				downloaded += size
				total := downloaded
				downloadedMu.Unlock()
				if total > maxTotalDownload {
					err = fmt.Errorf("%w: package set too large", errDownload)
				}
			}
			if err == nil {
				destination := filepath.Join(payload, "app", filepath.FromSlash(current.item.Path))
				if !pathWithin(filepath.Join(payload, "app"), destination) {
					err = errUnsafeArchive
				} else if mkdirErr := os.MkdirAll(destination, 0o700); mkdirErr != nil {
					err = mkdirErr
				} else {
					file, openErr := os.Open(archive)
					if openErr != nil {
						err = openErr
					} else {
						err = extractNPMPackage(ctx, file, destination, maxPackageExpanded)
						_ = file.Close()
					}
				}
			}
			_ = os.Remove(archive)
			if err != nil {
				err = fmt.Errorf("package %s: %w", current.item.Path, err)
				select {
				case errCh <- err:
					cancel()
				default:
				}
				return
			}
			completedMu.Lock()
			completed++
			done := completed + 1
			completedMu.Unlock()
			progress(done, len(packages)+1, "正在下载并校验 Harness 依赖")
		}
	}
	count := packageWorkers
	if len(packages) < count {
		count = len(packages)
	}
	workers.Add(count)
	for index := 0; index < count; index++ {
		go worker()
	}
	for index, item := range packages {
		select {
		case jobs <- job{index: index, item: item}:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	select {
	case err := <-errCh:
		return err
	default:
	}
	return ctx.Err()
}

func downloadPackage(ctx context.Context, client *http.Client, item lockedPackage, destination string) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return 0, fmt.Errorf("%w: package request", errDownload)
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Harness-Installer")
	response, err := hardenedClient(client).Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("%w: package transport", errDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maxPackageDownload {
		return 0, fmt.Errorf("%w: unexpected package response", errDownload)
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	hash := sha512.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(output, hash), io.LimitReader(response.Body, maxPackageDownload+1))
	closeErr := output.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written == 0 || written > maxPackageDownload || !bytes.Equal(hash.Sum(nil), item.Integrity) {
		return written, errHashMismatch
	}
	return written, nil
}

func finalizeNativeAssets(goos, goarch, payload string) error {
	switch goos + "/" + goarch {
	case "linux/amd64":
		digest := sha256.Sum256(linuxX64PTY)
		if hex.EncodeToString(digest[:]) != linuxX64PTYSHA256 {
			return errHashMismatch
		}
		destination := filepath.Join(payload, "app", "node_modules", "node-pty", "build", "Release", "pty.node")
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, linuxX64PTY, 0o755); err != nil {
			return err
		}
	case "darwin/amd64", "darwin/arm64":
		npmArch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
		helper := filepath.Join(payload, "app", "node_modules", "node-pty", "prebuilds", "darwin-"+npmArch, "spawn-helper")
		info, err := os.Lstat(helper)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errUnsafeArchive
		}
		if err := os.Chmod(helper, 0o755); err != nil {
			return err
		}
	case "windows/amd64":
	default:
		return errors.New("unsupported Harness native target")
	}
	return nil
}

func writeRuntimeManifest(payload, goos, goarch string, packageCount int) error {
	content := fmt.Sprintf("component=%s\nharness=%s\nnode=%s\ntarget=%s/%s\npackages=%d\n", componentID, harnessVer, nodeVersion, goos, goarch, packageCount)
	return os.WriteFile(filepath.Join(payload, ".osverse-harness-runtime"), []byte(content), 0o600)
}
