//go:build linux

package linux

import "testing"

func TestProbeSystem(t *testing.T) {
	tests := []struct {
		name          string
		osRelease     string
		goarch        string
		shell         string
		wantDistro    string
		wantVersion   string
		wantArch      string
		wantShell     string
		wantSupported bool
	}{
		{
			name:      "Ubuntu 20.04 amd64",
			osRelease: "ID=ubuntu\nVERSION_ID=\"20.04\"\nPRETTY_NAME=\"Ubuntu 20.04.6 LTS\"\n",
			goarch:    "amd64", shell: "/bin/bash",
			wantDistro: "Ubuntu 20.04.6 LTS", wantVersion: "20.04", wantArch: "x86_64", wantShell: "bash", wantSupported: true,
		},
		{
			name:      "Ubuntu 22.04 amd64",
			osRelease: "PRETTY_NAME='Ubuntu 22.04.5 LTS'\nVERSION_ID=22.04\nID=ubuntu\n",
			goarch:    "amd64", shell: "/usr/bin/zsh",
			wantDistro: "Ubuntu 22.04.5 LTS", wantVersion: "22.04", wantArch: "x86_64", wantShell: "zsh", wantSupported: true,
		},
		{
			name:      "Ubuntu 24.04 unsupported",
			osRelease: "ID=ubuntu\nVERSION_ID=24.04\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n",
			goarch:    "amd64", shell: "/bin/fish",
			wantDistro: "Ubuntu 24.04 LTS", wantVersion: "24.04", wantArch: "x86_64", wantShell: "fish", wantSupported: false,
		},
		{
			name:      "Debian 12 unsupported",
			osRelease: "ID=debian\nVERSION_ID=\"12\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n",
			goarch:    "amd64", shell: "/bin/bash",
			wantDistro: "Debian GNU/Linux 12 (bookworm)", wantVersion: "12", wantArch: "x86_64", wantShell: "bash", wantSupported: false,
		},
		{
			name:      "malformed release unsupported",
			osRelease: "this is not os-release\nVERSION_ID\n",
			goarch:    "amd64", shell: "bash",
			wantDistro: "Unknown Linux", wantVersion: "unknown", wantArch: "x86_64", wantShell: "bash", wantSupported: false,
		},
		{
			name:      "Ubuntu 22.04 arm64 unsupported",
			osRelease: "ID=ubuntu\nVERSION_ID=22.04\nPRETTY_NAME=Ubuntu\n",
			goarch:    "arm64", shell: "/bin/bash",
			wantDistro: "Ubuntu", wantVersion: "22.04", wantArch: "arm64", wantShell: "bash", wantSupported: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProbeSystem([]byte(tt.osRelease), tt.goarch, tt.shell)

			if got.Distribution != tt.wantDistro {
				t.Errorf("Distribution = %q, want %q", got.Distribution, tt.wantDistro)
			}
			if got.Version != tt.wantVersion {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVersion)
			}
			if got.Architecture != tt.wantArch {
				t.Errorf("Architecture = %q, want %q", got.Architecture, tt.wantArch)
			}
			if got.Shell != tt.wantShell {
				t.Errorf("Shell = %q, want %q", got.Shell, tt.wantShell)
			}
			if got.Supported != tt.wantSupported {
				t.Errorf("Supported = %t, want %t", got.Supported, tt.wantSupported)
			}
			if tt.wantSupported && got.UnsupportedReason != "" {
				t.Errorf("UnsupportedReason = %q for supported system, want empty", got.UnsupportedReason)
			}
			if !tt.wantSupported && got.UnsupportedReason == "" {
				t.Error("UnsupportedReason is empty for unsupported system")
			}
		})
	}
}
