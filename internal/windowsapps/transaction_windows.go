//go:build windows

package windowsapps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/detect"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
	"github.com/Oswald-Hao/Osverse/internal/releaseasset"
)

const (
	installerTimeout = 20 * time.Minute
	installerOutput  = 256 * 1024
)

var (
	errDownload       = errors.New("Windows desktop download failed")
	errHashMismatch   = errors.New("Windows desktop hash mismatch")
	errInvalidPackage = errors.New("invalid Windows desktop installer")
	errInstaller      = errors.New("Windows desktop installer failed")
	errInstallMissing = errors.New("Windows desktop install evidence missing")
)

func (manager *Manager) execute(ctx context.Context, item artifact, protocol proxyservice.Protocol, port int, progress func(progressUpdate)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if item.Kind == "store" {
		return manager.installStore(ctx, item, progress)
	}
	root, err := ensureManagedDirectories(manager.home, "AppData", "Local", "Osverse", "staging")
	if err != nil {
		return err
	}
	staging, err := os.MkdirTemp(root, item.ID+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	extension := ".exe"
	if item.Kind == "msi" {
		extension = ".msi"
	}
	packagePath := filepath.Join(staging, "installer"+extension)
	progress(progressUpdate{phase: "downloading", progress: 5, message: "正在下载官方 Windows 安装包"})
	if err := manager.download(ctx, item, protocol, port, packagePath, progress); err != nil {
		return err
	}
	progress(progressUpdate{phase: "verifying", progress: 75, message: "正在核验安装包哈希和格式"})
	if err := validateInstaller(packagePath, item.Kind); err != nil {
		return err
	}
	evidence, err := platformwindows.OpenExecutableEvidence(packagePath)
	if err != nil || !within(staging, evidence.ResolvedPath()) {
		if evidence != nil {
			_ = evidence.Close()
		}
		return errInvalidPackage
	}
	defer evidence.Close()
	progress(progressUpdate{phase: "installing", progress: 85, message: "正在静默安装到当前 Windows 用户"})
	path, args, err := installerCommand(packagePath, item)
	if err != nil {
		return err
	}
	result, runErr := manager.runner.Run(ctx, platform.CommandRequest{Path: path, Args: args, Timeout: installerTimeout, OutputLimit: installerOutput})
	if !evidence.Unchanged(packagePath) || result.TimedOut || result.Truncated || (result.ExitCode != 0 && result.ExitCode != 3010) {
		return errInstaller
	}
	if runErr != nil && result.ExitCode != 3010 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errInstaller
	}
	progress(progressUpdate{phase: "verifying", progress: 96, message: "正在确认应用注册结果"})
	return waitForInstallEvidence(ctx, manager.home, item.ID, item.ExpectedPaths, manager.packages)
}

func (manager *Manager) installStore(ctx context.Context, item artifact, progress func(progressUpdate)) error {
	winget := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "winget.exe")
	if !filepath.IsAbs(winget) || strings.ContainsAny(winget, "\x00\r\n") {
		return errInstaller
	}
	evidence, err := platformwindows.OpenExecutableEvidence(winget)
	if err != nil {
		return errInstaller
	}
	defer evidence.Close()
	progress(progressUpdate{phase: "installing", progress: 20, message: "正在通过精确 Microsoft Store 产品 ID 安装"})
	args := []string{"install", "--exact", "--id", item.StoreID, "--source", "msstore", "--accept-source-agreements", "--accept-package-agreements", "--disable-interactivity"}
	result, runErr := manager.runner.Run(ctx, platform.CommandRequest{Path: winget, Args: args, Timeout: installerTimeout, OutputLimit: installerOutput})
	if !evidence.Unchanged(winget) || result.TimedOut || result.Truncated || result.ExitCode != 0 || runErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errInstaller
	}
	progress(progressUpdate{phase: "verifying", progress: 99, message: "Microsoft Store 已确认安装"})
	return nil
}

func installerCommand(packagePath string, item artifact) (string, []string, error) {
	if item.Kind == "exe" {
		return packagePath, append([]string(nil), item.SilentArgs...), nil
	}
	if item.Kind != "msi" {
		return "", nil, errInvalidPackage
	}
	systemRoot := os.Getenv("SystemRoot")
	msiexec := filepath.Join(systemRoot, "System32", "msiexec.exe")
	if !filepath.IsAbs(msiexec) || strings.ContainsAny(msiexec, "\x00\r\n") {
		return "", nil, errInstaller
	}
	args := []string{"/i", packagePath}
	args = append(args, item.SilentArgs...)
	return msiexec, args, nil
}

func (manager *Manager) download(ctx context.Context, item artifact, protocol proxyservice.Protocol, port int, destination string, progress func(progressUpdate)) error {
	client, err := manager.client(protocol, port)
	if err != nil || client == nil {
		return fmt.Errorf("%w: client", errDownload)
	}
	copyClient := *client
	copyClient.CheckRedirect = releaseasset.GitHubRedirectPolicy(item.URL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		return errDownload
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Osverse-Windows-Installer")
	response, err := copyClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errDownload
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || (response.ContentLength >= 0 && response.ContentLength != item.DownloadBytes) {
		return errDownload
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(file, hash), io.LimitReader(response.Body, item.DownloadBytes+1), func(total int64) {
		progress(progressUpdate{phase: "downloading", progress: 5 + int(total*62/item.DownloadBytes), message: "正在下载官方 Windows 安装包"})
	})
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != item.DownloadBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errHashMismatch
	}
	return nil
}

func validateInstaller(path, kind string) error {
	file, err := os.Open(path)
	if err != nil {
		return errInvalidPackage
	}
	defer file.Close()
	header := make([]byte, 8)
	if _, err := io.ReadFull(file, header); err != nil {
		return errInvalidPackage
	}
	if kind == "exe" && !bytes.Equal(header[:2], []byte{'M', 'Z'}) {
		return errInvalidPackage
	}
	msiHeader := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	if kind == "msi" && !bytes.Equal(header, msiHeader) {
		return errInvalidPackage
	}
	if kind != "exe" && kind != "msi" {
		return errInvalidPackage
	}
	return nil
}

func waitForInstallEvidence(ctx context.Context, home, componentID string, expected []string, packages detect.WindowsPackageQuery) error {
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	spec, hasSpec := windowsDesktopSpec(componentID)
	var nextPackageCheck time.Time
	check := func() bool {
		for _, relative := range expected {
			path := filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, `/`)))
			evidence, err := platformwindows.OpenExecutableEvidence(path)
			if err == nil {
				_ = evidence.Close()
				return true
			}
		}
		now := time.Now()
		if packages == nil || !hasSpec || now.Before(nextPackageCheck) {
			return false
		}
		nextPackageCheck = now.Add(time.Second)
		evidence, err := packages.Evidence(ctx, spec)
		if err != nil || !evidence.Installed {
			return false
		}
		if evidence.Source == "msix" {
			return true
		}
		for _, candidate := range evidence.ExecutablePaths {
			locked, err := platformwindows.OpenExecutableEvidence(candidate)
			if err == nil {
				_ = locked.Close()
				return true
			}
		}
		return false
	}
	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errInstallMissing
		case <-ticker.C:
		}
	}
}

func windowsDesktopSpec(componentID string) (detect.WindowsDesktopSpec, bool) {
	for _, spec := range detect.WindowsDesktopSpecs() {
		if spec.ID == componentID {
			return spec, true
		}
	}
	return detect.WindowsDesktopSpec{}, false
}

func ensureManagedDirectories(base string, components ...string) (string, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return "", errInvalidPackage
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errInvalidPackage
		}
	}
	return current, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader, report func(int64)) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if report != nil {
				report(total)
			}
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
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

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func publicFailure(err error) string {
	switch {
	case errors.Is(err, errHashMismatch):
		return "安装包校验失败，未运行"
	case errors.Is(err, errInvalidPackage):
		return "安装包格式或路径不安全，已拒绝运行"
	case errors.Is(err, errDownload):
		return "下载安装包失败，请检查网络或代理"
	case errors.Is(err, errInstallMissing):
		return "安装程序结束，但未检测到应用，请在 Windows 设置中检查"
	default:
		return "桌面应用安装失败，未修改 Osverse 配置"
	}
}
