//go:build windows

package windows

import (
	"errors"
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
			if executable != comspec() || len(args) != 4 || args[0] != "/d" || args[1] != "/s" || args[2] != "/k" ||
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
