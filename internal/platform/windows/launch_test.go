//go:build windows

package windows

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	xwindows "golang.org/x/sys/windows"
)

func TestNormalizeFinalPath(t *testing.T) {
	if got := normalizeFinalPath(`\\?\C:\Users\Alice\tool.exe`); got != `C:\Users\Alice\tool.exe` {
		t.Fatalf("normalizeFinalPath drive = %q", got)
	}
	if got := normalizeFinalPath(`\\?\UNC\server\share\tool.exe`); got != `\\server\share\tool.exe` {
		t.Fatalf("normalizeFinalPath UNC = %q", got)
	}
}

func TestLocalWebLaunchAllocatesLoopbackPortAndWaitsForHarness(t *testing.T) {
	request, endpoint, err := prepareLocalWebLaunch(platform.LaunchRequest{
		Path: `C:\Users\Alice\.local\bin\dsh.cmd`, Args: []string{"--profile", "web"}, Terminal: true, LocalWeb: true,
	})
	if err != nil || !strings.HasPrefix(endpoint, "http://127.0.0.1:") || len(request.Args) != 4 ||
		request.Args[0] != "--profile" || request.Args[1] != "web" || request.Args[2] != "--port" || request.Args[3] == "0" {
		t.Fatalf("prepareLocalWebLaunch() = (%#v, %q, %v)", request, endpoint, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("<title>DeepSeek Harness</title>"))
	}))
	defer server.Close()
	if err := waitForLocalWeb(server.URL, time.Second); err != nil {
		t.Fatalf("waitForLocalWeb() error = %v", err)
	}
}

func TestLocalWebBrowserLaunchRunsAfterReadinessWithoutBlockingCaller(t *testing.T) {
	waited, opened, dispatched := false, false, false
	starter := detachedStarter{
		waitLocalWeb: func(endpoint string, timeout time.Duration) error {
			waited = endpoint == "http://127.0.0.1:3080" && timeout == localWebReadyTimeout
			return nil
		},
		openLocalWeb: func(endpoint string) error {
			opened = endpoint == "http://127.0.0.1:3080"
			return nil
		},
		runAsync: func(task func()) {
			dispatched = true
			task()
		},
	}
	starter.launchLocalWeb("http://127.0.0.1:3080")
	if !dispatched || !waited || !opened {
		t.Fatalf("local web launch = dispatched %v waited %v opened %v", dispatched, waited, opened)
	}
}

func TestLocalWebEndpointRejectsNonLoopbackOrInjectedURLs(t *testing.T) {
	for _, endpoint := range []string{
		"https://127.0.0.1:3080", "http://localhost:3080", "http://127.0.0.1:0",
		"http://127.0.0.1:70000", "http://127.0.0.1:3080/path", "http://127.0.0.1:3080?x=1",
		"http://127.0.0.1:3080\r\nexample",
	} {
		if validLocalWebEndpoint(endpoint) {
			t.Errorf("accepted unsafe endpoint %q", endpoint)
		}
	}
	if !validLocalWebEndpoint("http://127.0.0.1:3080") {
		t.Fatal("rejected fixed loopback endpoint")
	}
}

func TestLockedIdentityDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.exe")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := openLockedIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	defer xwindows.CloseHandle(first.handle)
	if err := os.WriteFile(path+".next", []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err == nil {
		t.Fatal("replacement succeeded while target identity was locked")
	}
}

func TestLaunchInvocationCallsEveryManagedCommandScriptInSystemConsole(t *testing.T) {
	for _, test := range []struct {
		command string
		args    []string
	}{
		{command: "claude"}, {command: "codex"}, {command: "opencode"},
		{command: "dsh", args: []string{"--profile", "web"}},
		{command: "qwen"}, {command: "kimi"}, {command: "copilot"},
	} {
		t.Run(test.command, func(t *testing.T) {
			path := `C:\Users\Alice\.local\bin\` + test.command + `.cmd`
			executable, args, flags, err := launchInvocation(platform.LaunchRequest{Path: path, Args: test.args, Terminal: true})
			if err != nil {
				t.Fatal(err)
			}
			if executable != comspec() || len(args) != 3 || args[0] != "/d" || args[1] != "/k" ||
				flags&xwindows.CREATE_NEW_CONSOLE == 0 {
				t.Fatalf("launchInvocation() = executable %q args %#v", executable, args)
			}
			line := args[len(args)-1]
			if !strings.HasPrefix(line, `call "`+path+`"`) {
				t.Fatalf("terminal command line = %q", line)
			}
			for _, argument := range test.args {
				if !strings.Contains(line, quoteCMD(argument)) {
					t.Fatalf("terminal command line %q omitted %q", line, argument)
				}
			}
			raw, err := terminalCommandLine(executable, args)
			if err != nil || !strings.HasPrefix(raw, quoteCMD(executable)+" /d /k call ") || !strings.HasSuffix(raw, line) {
				t.Fatalf("terminalCommandLine() = (%q, %v)", raw, err)
			}
		})
	}
}

func TestDetachedStarterExecutesBatchWithoutWindowsTerminal(t *testing.T) {
	root := t.TempDir()
	localAppData := filepath.Join(root, "Local App Data")
	t.Setenv("LOCALAPPDATA", localAppData)
	terminalAlias := filepath.Join(localAppData, "Microsoft", "WindowsApps", "wt.exe")
	if err := os.MkdirAll(filepath.Dir(terminalAlias), 0o700); err != nil {
		t.Fatal(err)
	}
	// A regular but non-executable alias reproduces machines where the Windows
	// Terminal app-execution alias exists but cannot be started by this process.
	if err := os.WriteFile(terminalAlias, []byte("unavailable app alias"), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "batch-started.txt")
	script := filepath.Join(root, "managed command.cmd")
	content := "@echo off\r\nif not \"%~1\"==\"expected\" exit /b 17\r\n>\"" + marker + "\" echo started\r\nexit\r\n"
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewDetachedStarter().Start(platform.LaunchRequest{
		Path: script, ExpectedResolvedPath: resolved, Args: []string{"expected"}, Terminal: true,
	}); err != nil {
		t.Fatalf("Start() through cmd.exe = %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(marker)
		if err == nil && strings.TrimSpace(string(raw)) == "started" {
			break
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("managed batch entry was not executed")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDetachedStarterProvidesRealInteractiveConsoleHandles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", root)
	marker := filepath.Join(root, "console-handles.txt")
	script := filepath.Join(root, "interactive command.cmd")
	content := "@echo off\r\n\"%~1\" -test.run=^TestWindowsLaunchConsoleHelper$ -- \"%~2\"\r\nexit\r\n"
	if err := os.WriteFile(script, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(script)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewDetachedStarter().Start(platform.LaunchRequest{
		Path: script, ExpectedResolvedPath: resolved, Args: []string{os.Args[0], marker}, Terminal: true,
	}); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(marker)
		if err == nil {
			text := strings.ReplaceAll(string(raw), "\r\n", "\n")
			if !strings.Contains(text, "stdin=console\n") || !strings.Contains(text, "stdout=console\n") ||
				!strings.Contains(strings.ToLower(text), "cwd="+strings.ToLower(root)+"\n") {
				t.Fatalf("console helper = %q", text)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("interactive console helper did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWindowsLaunchConsoleHelper(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		t.Skip("helper process only")
	}
	marker := os.Args[separator+1]
	var inputMode, outputMode uint32
	inputErr := xwindows.GetConsoleMode(xwindows.Stdin, &inputMode)
	outputErr := xwindows.GetConsoleMode(xwindows.Stdout, &outputMode)
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatal(cwdErr)
	}
	content := fmt.Sprintf("stdin=%s\nstdout=%s\ncwd=%s\n", consoleResult(inputErr), consoleResult(outputErr), cwd)
	if err := os.WriteFile(marker, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func consoleResult(err error) string {
	if err == nil {
		return "console"
	}
	return "unavailable"
}

func TestEnvironmentUTF16BlockIsSortedAndDoubleTerminated(t *testing.T) {
	block, err := environmentUTF16Block([]string{"Path=C:\\Tools", "APPDATA=C:\\Data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(block) < 2 || block[len(block)-1] != 0 || block[len(block)-2] != 0 {
		t.Fatalf("environment block is not double terminated: %#v", block)
	}
	entries := make([]string, 0, 2)
	start := 0
	for index, value := range block {
		if value == 0 && index > start {
			entries = append(entries, xwindows.UTF16ToString(block[start:index]))
			start = index + 1
		}
	}
	if len(entries) != 2 || entries[0] != "APPDATA=C:\\Data" || entries[1] != "Path=C:\\Tools" {
		t.Fatalf("environment entries = %#v", entries)
	}
}
