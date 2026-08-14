//go:build windows

package windows

import "testing"

func TestProbeSystemSupportsWindows10And11X64(t *testing.T) {
	for _, test := range []struct {
		product, display, build string
	}{
		{"Windows 10 Pro", "22H2", "19045"},
		{"Windows 11 Pro", "24H2", "26100"},
	} {
		info := ProbeSystem(test.product, test.display, test.build, "amd64", "cmd.exe")
		if !info.Supported || info.Distribution != test.product || info.Architecture != "x86_64" || info.Shell != "cmd.exe" {
			t.Fatalf("ProbeSystem(%q) = %#v", test.product, info)
		}
	}
}

func TestProbeSystemRejectsOldBuildAndArchitecture(t *testing.T) {
	if info := ProbeSystem("Windows 10", "1803", "17134", "amd64", "cmd.exe"); info.Supported || info.UnsupportedReason == "" {
		t.Fatalf("old Windows = %#v", info)
	}
	if info := ProbeSystem("Windows 11", "24H2", "26100", "arm64", "pwsh.exe"); info.Supported || info.Architecture != "arm64" {
		t.Fatalf("arm64 Windows = %#v", info)
	}
}
