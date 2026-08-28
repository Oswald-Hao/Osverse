//go:build windows

package windows

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	xwindows "golang.org/x/sys/windows"
)

const localWebReadyTimeout = 90 * time.Second

type detachedStarter struct {
	waitLocalWeb func(string, time.Duration) error
	openLocalWeb func(string) error
	runAsync     func(func())
}

func NewDetachedStarter() platform.ProcessStarter {
	return detachedStarter{
		waitLocalWeb: waitForLocalWeb,
		openLocalWeb: openLocalWeb,
		runAsync:     func(task func()) { go task() },
	}
}

func (starter detachedStarter) Start(request platform.LaunchRequest) error {
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
	localWebURL := ""
	if request.LocalWeb {
		var err error
		request, localWebURL, err = prepareLocalWebLaunch(request)
		if err != nil {
			return err
		}
	}

	path, args, flags, err := launchInvocation(request)
	if err != nil {
		return err
	}
	var startErr error
	if request.Terminal {
		raw, err := terminalCommandLine(path, args)
		if err != nil {
			return err
		}
		// Go's os/exec connects nil streams to NUL. That is correct for a
		// background process but leaves a newly created console without usable
		// input or output, causing interactive CLIs such as Claude Code to exit
		// or render an empty window. CreateProcess without
		// STARTF_USESTDHANDLES lets Windows attach the three standard handles to
		// the new console instead.
		startErr = startTerminalConsole(path, raw, flags, commandEnvironment(nil), terminalWorkingDirectory())
	} else {
		command := exec.Command(path, args...)
		command.Env = commandEnvironment(nil)
		command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err := command.Start(); err != nil {
			return err
		}
		startErr = command.Process.Release()
	}
	if startErr != nil {
		return startErr
	}
	after, err := openLockedIdentity(request.Path)
	if err != nil {
		return errors.New("launch target changed")
	}
	defer xwindows.CloseHandle(after.handle)
	if !locked.same(after) || !sameWindowsPath(after.finalPath, request.ExpectedResolvedPath) {
		return errors.New("launch target changed")
	}
	if localWebURL != "" {
		starter.launchLocalWeb(localWebURL)
	}
	return nil
}

func (starter detachedStarter) launchLocalWeb(endpoint string) {
	wait := starter.waitLocalWeb
	if wait == nil {
		wait = waitForLocalWeb
	}
	open := starter.openLocalWeb
	if open == nil {
		open = openLocalWeb
	}
	run := starter.runAsync
	if run == nil {
		run = func(task func()) { go task() }
	}
	run(func() {
		if wait(endpoint, localWebReadyTimeout) != nil {
			return
		}
		for attempt := 0; attempt < 3; attempt++ {
			if open(endpoint) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	})
}

func startTerminalConsole(application, commandLine string, flags uint32, environment []string, directory string) error {
	applicationUTF16, err := xwindows.UTF16PtrFromString(application)
	if err != nil {
		return err
	}
	commandLineUTF16, err := xwindows.UTF16PtrFromString(commandLine)
	if err != nil {
		return err
	}
	environmentBlock, err := environmentUTF16Block(environment)
	if err != nil {
		return err
	}
	var environmentPointer *uint16
	if len(environmentBlock) > 0 {
		environmentPointer = &environmentBlock[0]
	}
	var directoryPointer *uint16
	if directory != "" {
		directoryPointer, err = xwindows.UTF16PtrFromString(directory)
		if err != nil {
			return err
		}
	}
	startup := xwindows.StartupInfo{Cb: uint32(unsafe.Sizeof(xwindows.StartupInfo{}))}
	var process xwindows.ProcessInformation
	err = xwindows.CreateProcess(
		applicationUTF16, commandLineUTF16, nil, nil, false,
		flags|xwindows.CREATE_UNICODE_ENVIRONMENT, environmentPointer, directoryPointer, &startup, &process,
	)
	if err != nil {
		return err
	}
	threadErr := xwindows.CloseHandle(process.Thread)
	processErr := xwindows.CloseHandle(process.Process)
	return errors.Join(threadErr, processErr)
}

func environmentUTF16Block(environment []string) ([]uint16, error) {
	entries := append([]string(nil), environment...)
	sort.SliceStable(entries, func(i, j int) bool { return strings.ToUpper(entries[i]) < strings.ToUpper(entries[j]) })
	block := make([]uint16, 0, len(entries)*32)
	for _, entry := range entries {
		if entry == "" || strings.ContainsRune(entry, 0) {
			return nil, errors.New("invalid Windows environment entry")
		}
		encoded, err := xwindows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, encoded...)
	}
	// Every encoded entry already carries one NUL. One additional NUL closes
	// the Unicode environment block; an empty block still needs two NULs.
	if len(block) == 0 {
		return []uint16{0, 0}, nil
	}
	return append(block, 0), nil
}

func terminalWorkingDirectory() string {
	home := os.Getenv("USERPROFILE")
	if !safeExecutablePath(home) {
		return ""
	}
	info, err := os.Stat(home)
	if err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Clean(home)
}

func prepareLocalWebLaunch(request platform.LaunchRequest) (platform.LaunchRequest, string, error) {
	if !request.LocalWeb || !request.Terminal {
		return platform.LaunchRequest{}, "", errors.New("invalid local web launch")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return platform.LaunchRequest{}, "", err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return platform.LaunchRequest{}, "", err
	}
	request.Args = append(append([]string(nil), request.Args...), "--port", strconv.Itoa(port))
	return request, "http://127.0.0.1:" + strconv.Itoa(port), nil
}

func waitForLocalWeb(endpoint string, timeout time.Duration) error {
	if !validLocalWebEndpoint(endpoint) || timeout <= 0 {
		return errors.New("invalid local web endpoint")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode == http.StatusOK && bytes.Contains(body, []byte("DeepSeek Harness")) {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("DeepSeek Harness web profile did not become ready")
}

func openLocalWeb(endpoint string) error {
	if !validLocalWebEndpoint(endpoint) {
		return errors.New("invalid local web endpoint")
	}
	// Starting Explorer with an HTTP URL asks the interactive Windows shell to
	// dispatch the user's default browser. Unlike an in-process ShellExecute
	// call from a short-lived background goroutine, the separate shell process
	// remains alive long enough to complete first-browser startup and DDE
	// handoff when no browser window already exists.
	if root := os.Getenv("SystemRoot"); safeCommandProcessorRoot(root) {
		explorer := filepath.Join(filepath.Clean(root), "explorer.exe")
		if info, err := os.Stat(explorer); err == nil && !info.IsDir() {
			command := exec.Command(explorer, endpoint)
			command.Env = commandEnvironment(nil)
			command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
			command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: xwindows.CREATE_NEW_PROCESS_GROUP | xwindows.DETACHED_PROCESS}
			if err := command.Start(); err == nil {
				if err := command.Process.Release(); err == nil {
					return nil
				}
			}
		}
	}
	verb, err := xwindows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := xwindows.UTF16PtrFromString(endpoint)
	if err != nil {
		return err
	}
	return xwindows.ShellExecute(0, verb, target, nil, nil, xwindows.SW_SHOWNORMAL)
}

func validLocalWebEndpoint(endpoint string) bool {
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") || strings.ContainsAny(endpoint, "\x00\r\n") {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	port, err := strconv.Atoi(parsed.Port())
	return err == nil && port > 0 && port <= 65535
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
	if launchesBatchScript(request.Path) {
		line = "call " + line
	}
	for _, argument := range request.Args {
		line += " " + quoteCMD(argument)
	}
	// Always ask the system command processor to create the console itself.
	// Windows Terminal's app-execution alias can be a regular file while the
	// packaged application behind it is unavailable to this process. In that
	// state Start succeeds inconsistently (or wt exits immediately) and the
	// managed .cmd/.bat entry is never executed.
	return shell, []string{"/d", "/k", line}, xwindows.CREATE_NEW_PROCESS_GROUP | xwindows.CREATE_NEW_CONSOLE, nil
}

func launchesBatchScript(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return extension == ".cmd" || extension == ".bat"
}

func terminalCommandLine(shell string, args []string) (string, error) {
	if !safeExecutablePath(shell) || len(args) != 3 || args[0] != "/d" || args[1] != "/k" ||
		args[2] == "" || strings.ContainsAny(args[2], "\x00\r\n") {
		return "", errors.New("invalid Windows terminal command line")
	}
	return quoteCMD(shell) + " /d /k " + args[2], nil
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
