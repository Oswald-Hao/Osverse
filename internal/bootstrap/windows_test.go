//go:build windows

package bootstrap

import (
	"testing"

	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
)

func TestWindowsComponentProbesHaveStableCatalog(t *testing.T) {
	probes := windowsComponentProbes(platformwindows.NewExecRunner(), `C:\Users\Test`)
	if len(probes) != 14 {
		t.Fatalf("probe count = %d, want 14", len(probes))
	}
	seen := map[string]bool{}
	for _, probe := range probes {
		descriptor := probe.Descriptor()
		if descriptor.ID == "" || seen[descriptor.ID] {
			t.Fatalf("invalid duplicate descriptor = %#v", descriptor)
		}
		seen[descriptor.ID] = true
	}
}
