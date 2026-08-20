//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	executable, args, _, err := launchInvocation(platform.LaunchRequest{Path: path, Args: []string{"web"}, Terminal: true})
	if err != nil {
		t.Fatal(err)
	}
	if executable == "" || len(args) < 3 {
		t.Fatalf("launchInvocation() = executable %q args %#v", executable, args)
	}
	line := args[len(args)-1]
	if !strings.HasPrefix(line, `call "`+path+`"`) || !strings.Contains(line, `"web"`) {
		t.Fatalf("terminal command line = %q", line)
	}
}
