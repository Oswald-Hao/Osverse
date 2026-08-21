//go:build windows

package windows

import (
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

func TestLaunchInvocationCallsCommandScriptsInTerminal(t *testing.T) {
	path := `C:\Users\Alice\.local\bin\dsh.cmd`
	executable, args, _, err := launchInvocation(platform.LaunchRequest{Path: path, Args: []string{"--profile", "web"}, Terminal: true})
	if err != nil {
		t.Fatal(err)
	}
	if executable == "" || len(args) < 3 {
		t.Fatalf("launchInvocation() = executable %q args %#v", executable, args)
	}
	line := args[len(args)-1]
	if !strings.HasPrefix(line, `call "`+path+`"`) || !strings.Contains(line, `"--profile" "web"`) {
		t.Fatalf("terminal command line = %q", line)
	}
}
