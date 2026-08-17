// Package qweninstall installs checksum-pinned official Qwen Code standalone
// releases without depending on a system Node.js installation.
package qweninstall

import (
	"errors"
	"fmt"
)

const (
	componentID = "qwen-code"
	qwenName    = "Qwen Code"
	qwenVersion = "0.21.13"
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
		GOOS: "linux", GOARCH: "amd64", Version: qwenVersion, Format: "tar.gz", Size: 83_551_231,
		URL:    "https://github.com/QwenLM/qwen-code/releases/download/v0.21.13/qwen-code-linux-x64.tar.gz",
		SHA256: "a58e9a99c2f9e706d262c2bcb918e1c62cc29b3af8b96a072d45d2e57edf3ba3",
	},
	"linux/arm64": {
		GOOS: "linux", GOARCH: "arm64", Version: qwenVersion, Format: "tar.gz", Size: 83_365_073,
		URL:    "https://github.com/QwenLM/qwen-code/releases/download/v0.21.13/qwen-code-linux-arm64.tar.gz",
		SHA256: "1aaff5737a86e984cae6079174ca395aa57e5d9c6d29dd94f0114334ebf21504",
	},
	"windows/amd64": {
		GOOS: "windows", GOARCH: "amd64", Version: qwenVersion, Format: "zip", Size: 64_082_209,
		URL:    "https://github.com/QwenLM/qwen-code/releases/download/v0.21.13/qwen-code-win-x64.zip",
		SHA256: "1e2e7db98e5ae52fda85ae8fffb9c58504620ad4b5367cd5e479eff0e23debb1",
	},
	"darwin/amd64": {
		GOOS: "darwin", GOARCH: "amd64", Version: qwenVersion, Format: "tar.gz", Size: 77_773_865,
		URL:    "https://github.com/QwenLM/qwen-code/releases/download/v0.21.13/qwen-code-darwin-x64.tar.gz",
		SHA256: "2e9568d2190e92fe10d2baadda21ffb47c35493fef010a85a2d8ae8b1c7d2cf8",
	},
	"darwin/arm64": {
		GOOS: "darwin", GOARCH: "arm64", Version: qwenVersion, Format: "tar.gz", Size: 76_479_930,
		URL:    "https://github.com/QwenLM/qwen-code/releases/download/v0.21.13/qwen-code-darwin-arm64.tar.gz",
		SHA256: "bf7e0e4c6c4b815a02398b63a12aaacde749f7b11076c75ffd9f3d8592693961",
	},
}

func artifactForTarget(target string) (artifact, error) {
	item, ok := artifacts[target]
	if !ok || item.Version != qwenVersion || item.Size <= 0 || len(item.SHA256) != 64 {
		return artifact{}, errors.New("unsupported Qwen Code target")
	}
	if item.URL != fmt.Sprintf("https://github.com/QwenLM/qwen-code/releases/download/v%s/qwen-code-%s", qwenVersion, archiveName(item)) {
		return artifact{}, errors.New("invalid Qwen Code artifact catalog")
	}
	return item, nil
}

func archiveName(item artifact) string {
	osName := item.GOOS
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[item.GOARCH]
	if item.GOOS == "windows" {
		osName = "win"
	}
	return osName + "-" + arch + "." + item.Format
}
