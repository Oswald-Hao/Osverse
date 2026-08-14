//go:build windows

package windows

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverPathsIncludesEffectiveAndKnownUserLocations(t *testing.T) {
	inputs := PathInputs{
		ProcessPath: `C:\Tools;C:\Windows\System32`,
		UserPath:    `%USERPROFILE%\.local\bin;%APPDATA%\npm;relative;%UNKNOWN%\bin`,
		Home:        `C:\Users\Alice`, AppData: `C:\Users\Alice\AppData\Roaming`,
		LocalAppData: `C:\Users\Alice\AppData\Local`,
	}
	want := []string{
		`C:\Tools`, `C:\Windows\System32`, `C:\Users\Alice\.local\bin`,
		`C:\Users\Alice\AppData\Roaming\npm`, `C:\Users\Alice\.bun\bin`,
		`C:\Users\Alice\.opencode\bin`, `C:\Users\Alice\bin`,
		`C:\Users\Alice\AppData\Local\Microsoft\WindowsApps`,
	}
	if got := DiscoverPaths(inputs); !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverPaths() = %#v, want %#v; separator=%q", got, want, filepath.ListSeparator)
	}
}

func TestDiscoverPathsDeduplicatesCaseInsensitively(t *testing.T) {
	got := DiscoverPaths(PathInputs{ProcessPath: `C:\Tools;c:\tools`, Home: `C:\Users\Alice`})
	if len(got) != 5 || got[0] != `C:\Tools` {
		t.Fatalf("DiscoverPaths() = %#v", got)
	}
}
