//go:build linux

package selfupdate

import (
	"os"
	"strings"
)

func linuxArtifactPreference() (string, string, bool) {
	target := linuxTarget()
	if target == "" {
		return "", "", false
	}
	if appImage := os.Getenv("APPIMAGE"); strings.HasPrefix(appImage, "/") {
		return "appimage", target, true
	}
	executable, err := os.Executable()
	if err == nil && executable == "/usr/bin/osverse" {
		return "deb", target, true
	}
	return "tar.gz", target, true
}

func linuxTarget() string {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	text := string(content)
	for _, line := range strings.Split(text, "\n") {
		if line == "VERSION_ID=\"20.04\"" || line == "VERSION_ID=20.04" {
			return "ubuntu20.04"
		}
		if line == "VERSION_ID=\"22.04\"" || line == "VERSION_ID=22.04" {
			return "ubuntu22.04"
		}
	}
	return ""
}
