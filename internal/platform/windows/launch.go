//go:build windows

package windows

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	xwindows "golang.org/x/sys/windows"
)

type detachedStarter struct{}

func NewDetachedStarter() platform.ProcessStarter { return detachedStarter{} }

func (detachedStarter) Start(request platform.LaunchRequest) error {
	if !safeExecutablePath(request.Path) || !safeExecutablePath(request.ExpectedResolvedPath) {
		return errors.New("unsafe launch path")
	}
	locked, err := openLockedIdentity(request.Path)
	if err != nil {
		return err
	}
	defer xwindows.CloseHandle(locked.handle)
	if !sameWindowsPath(locked.finalPath, request.ExpectedResolvedPath) {
		return errors.New("launch target changed")
	}

	path, args, flags, err := launchInvocation(request)
	if err != nil {
		return err
	}
	command := exec.Command(path, args...)
	command.Env = commandEnvironment(nil)
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	if err := command.Start(); err != nil {
		return err
	}
	if err := command.Process.Release(); err != nil {
		return err
	}
	after, err := openLockedIdentity(request.Path)
	if err != nil {
		return errors.New("launch target changed")
	}
	defer xwindows.CloseHandle(after.handle)
	if !locked.same(after) || !sameWindowsPath(after.finalPath, request.ExpectedResolvedPath) {
		return errors.New("launch target changed")
	}
	return nil
}

func launchInvocation(request platform.LaunchRequest) (string, []string, uint32, error) {
	if !request.Terminal {
		return request.Path, append([]string(nil), request.Args...), xwindows.CREATE_NEW_PROCESS_GROUP | xwindows.DETACHED_PROCESS, nil
	}
	shell := comspec()
	if !safeExecutablePath(shell) {
		return "", nil, 0, errors.New("Windows command processor unavailable")
	}
	for _, value := range append([]string{request.Path}, request.Args...) {
		if strings.ContainsAny(value, "\x00\r\n&|<>^%!") {
			return "", nil, 0, errors.New("unsafe terminal launch argument")
		}
	}
	line := quoteCMD(request.Path)
	for _, argument := range request.Args {
		line += " " + quoteCMD(argument)
	}
	local := os.Getenv("LOCALAPPDATA")
	terminal := filepath.Join(local, "Microsoft", "WindowsApps", "wt.exe")
	if validRegularFile(terminal) {
		return terminal, []string{"new-tab", "--", shell, "/d", "/k", line}, xwindows.CREATE_NEW_PROCESS_GROUP, nil
	}
	return shell, []string{"/d", "/k", line}, xwindows.CREATE_NEW_PROCESS_GROUP | xwindows.CREATE_NEW_CONSOLE, nil
}

func validRegularFile(path string) bool {
	if !safeExecutablePath(path) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

type windowsFileIdentity struct {
	handle       xwindows.Handle
	finalPath    string
	volumeSerial uint32
	fileIndex    uint64
	fileSize     uint64
	lastWrite    xwindows.Filetime
}

func openLockedIdentity(path string) (windowsFileIdentity, error) {
	name, err := xwindows.UTF16PtrFromString(path)
	if err != nil {
		return windowsFileIdentity{}, err
	}
	handle, err := xwindows.CreateFile(name, xwindows.GENERIC_READ, xwindows.FILE_SHARE_READ, nil,
		xwindows.OPEN_EXISTING, xwindows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return windowsFileIdentity{}, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = xwindows.CloseHandle(handle)
		}
	}()
	var info xwindows.ByHandleFileInformation
	if err := xwindows.GetFileInformationByHandle(handle, &info); err != nil {
		return windowsFileIdentity{}, err
	}
	if info.FileAttributes&xwindows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return windowsFileIdentity{}, errors.New("launch target is not a regular file")
	}
	finalPath, err := finalPathForHandle(handle)
	if err != nil || !safeExecutablePath(finalPath) {
		return windowsFileIdentity{}, errors.New("launch target path unavailable")
	}
	closeOnError = false
	return windowsFileIdentity{
		handle: handle, finalPath: finalPath, volumeSerial: info.VolumeSerialNumber,
		fileIndex: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
		fileSize:  uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow), lastWrite: info.LastWriteTime,
	}, nil
}

func finalPathForHandle(handle xwindows.Handle) (string, error) {
	buffer := make([]uint16, 512)
	for len(buffer) <= 32768 {
		count, err := xwindows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if count < uint32(len(buffer)) {
			return normalizeFinalPath(xwindows.UTF16ToString(buffer[:count])), nil
		}
		buffer = make([]uint16, count+1)
	}
	return "", errors.New("resolved path exceeds limit")
}

func normalizeFinalPath(path string) string {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
	} else {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	return filepath.Clean(path)
}

func sameWindowsPath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func (identity windowsFileIdentity) same(other windowsFileIdentity) bool {
	return identity.volumeSerial == other.volumeSerial && identity.fileIndex == other.fileIndex &&
		identity.fileSize == other.fileSize && identity.lastWrite == other.lastWrite
}

func closeWindowsHandle(handle xwindows.Handle) error { return xwindows.CloseHandle(handle) }
