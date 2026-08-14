//go:build linux

package detect

import (
	"path/filepath"
	"testing"
)

func TestManagedDesktopVersionRequiresExactOwnedLayout(t *testing.T) {
	home := "/home/tester"
	valid := filepath.Join(home, ".local", "share", "osverse", "apps", "cc-switch", "3.19.2", "application.AppImage")
	if version, ok := managedDesktopVersion(home, "cc-switch", valid); !ok || version != "3.19.2" {
		t.Fatalf("managedDesktopVersion() = (%q, %v)", version, ok)
	}
	for _, invalid := range []string{
		filepath.Join(home, ".local", "share", "osverse", "apps", "cc-switch-old", "3.19.2", "application.AppImage"),
		filepath.Join(home, ".local", "share", "osverse", "apps", "cc-switch", "current", "other"),
		"/opt/cc-switch/application.AppImage",
	} {
		if version, ok := managedDesktopVersion(home, "cc-switch", invalid); ok || version != "" {
			t.Fatalf("invalid %q accepted as %q", invalid, version)
		}
	}
}

func TestManagedDesktopUpdateAvailableNeverDowngrades(t *testing.T) {
	if !managedDesktopUpdateAvailable("cc-switch", "3.19.1") {
		t.Fatal("older managed version was not offered an update")
	}
	if managedDesktopUpdateAvailable("cc-switch", "3.19.2") || managedDesktopUpdateAvailable("cc-switch", "4.0.0") {
		t.Fatal("current or newer version was offered a downgrade")
	}
	if managedDesktopUpdateAvailable("unknown", "1.0.0") || managedDesktopUpdateAvailable("cc-switch", "broken") {
		t.Fatal("unknown or malformed version was updateable")
	}
}
