// Package copilotinstall installs checksum-pinned official GitHub Copilot CLI
// standalone releases without depending on a system Node.js installation.
package copilotinstall

import (
	"errors"
	"fmt"
)

const (
	componentID    = "github-copilot-cli"
	copilotName    = "GitHub Copilot CLI"
	copilotVersion = "1.0.80"
)

type artifact struct {
	GOOS, GOARCH string
	Version      string
	URL          string
	SHA256       string
	Size         int64
	Format       string
}

var artifacts = map[string]artifact{
	"linux/amd64": {
		GOOS: "linux", GOARCH: "amd64", Version: copilotVersion, Format: "tar.gz", Size: 110_502_497,
		URL:    "https://github.com/github/copilot-cli/releases/download/v1.0.80/copilot-linux-x64.tar.gz",
		SHA256: "039933c9247686131c4406abb1d439bdbf68103edc1ff585bd70d5b0dc940f72",
	},
	"linux/arm64": {
		GOOS: "linux", GOARCH: "arm64", Version: copilotVersion, Format: "tar.gz", Size: 111_169_100,
		URL:    "https://github.com/github/copilot-cli/releases/download/v1.0.80/copilot-linux-arm64.tar.gz",
		SHA256: "3ed85e711955e13be523bf492bc6c93b40b69925bcb7f817c9d08abf4839cf89",
	},
	"windows/amd64": {
		GOOS: "windows", GOARCH: "amd64", Version: copilotVersion, Format: "zip", Size: 100_680_021,
		URL:    "https://github.com/github/copilot-cli/releases/download/v1.0.80/copilot-win32-x64.zip",
		SHA256: "e9ea2063913faa8a9f1cf374529c5fea075da0545a894d7469026166f854c541",
	},
	"darwin/amd64": {
		GOOS: "darwin", GOARCH: "amd64", Version: copilotVersion, Format: "tar.gz", Size: 110_675_243,
		URL:    "https://github.com/github/copilot-cli/releases/download/v1.0.80/copilot-darwin-x64.tar.gz",
		SHA256: "a1a9c1f25740f9a27b34eb14b70b5d3175794dc8bb410875531aa198b3abc18f",
	},
	"darwin/arm64": {
		GOOS: "darwin", GOARCH: "arm64", Version: copilotVersion, Format: "tar.gz", Size: 99_168_802,
		URL:    "https://github.com/github/copilot-cli/releases/download/v1.0.80/copilot-darwin-arm64.tar.gz",
		SHA256: "2346bb691981c2997d65c1c5bc3cef1aeddc9edd37dcb2f970b911aa597e59f6",
	},
}

func artifactForTarget(target string) (artifact, error) {
	item, ok := artifacts[target]
	if !ok || item.Version != copilotVersion || item.Size <= 0 || len(item.SHA256) != 64 {
		return artifact{}, errors.New("unsupported GitHub Copilot CLI target")
	}
	if item.URL != fmt.Sprintf("https://github.com/github/copilot-cli/releases/download/v%s/%s", copilotVersion, archiveName(item)) {
		return artifact{}, errors.New("invalid GitHub Copilot CLI artifact catalog")
	}
	return item, nil
}

func archiveName(item artifact) string {
	osName := item.GOOS
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[item.GOARCH]
	if item.GOOS == "windows" {
		osName = "win32"
	}
	return "copilot-" + osName + "-" + arch + "." + item.Format
}
