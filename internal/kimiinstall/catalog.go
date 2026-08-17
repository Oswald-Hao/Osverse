// Package kimiinstall installs checksum-pinned official Kimi Code
// standalone releases without depending on a system Node.js installation.
package kimiinstall

import (
	"errors"
	"fmt"
)

const (
	componentID = "kimi-code"
	kimiName    = "Kimi Code"
	kimiVersion = "0.36.1"
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
		GOOS: "linux", GOARCH: "amd64", Version: kimiVersion, Format: "zip", Size: 64_621_491,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-linux-x64.zip",
		SHA256: "c5af089d5ad34c27f2f26d5f93588ba3f656bf771911e5d43c85be95d3e1cbd4",
	},
	"linux/arm64": {
		GOOS: "linux", GOARCH: "arm64", Version: kimiVersion, Format: "zip", Size: 64_574_858,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-linux-arm64.zip",
		SHA256: "345b5ac3354c3d3890e34cf8e50ee1ce81e5f3b719a1db506797e53e520099e6",
	},
	"windows/amd64": {
		GOOS: "windows", GOARCH: "amd64", Version: kimiVersion, Format: "zip", Size: 56_044_751,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-win32-x64.zip",
		SHA256: "eefcd15ef3f35480221b758f60e9568d8166b2776190c24131f162a2f89b6e1b",
	},
	"windows/arm64": {
		GOOS: "windows", GOARCH: "arm64", Version: kimiVersion, Format: "zip", Size: 52_217_799,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-win32-arm64.zip",
		SHA256: "89b684be9eeae8f07106e27f650ddd6880900a99c6c30b5bb06a79cde58f0286",
	},
	"darwin/amd64": {
		GOOS: "darwin", GOARCH: "amd64", Version: kimiVersion, Format: "zip", Size: 62_227_202,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-darwin-x64.zip",
		SHA256: "560dca967a3609b7d46a5b9d95c364a958e35558af231660194f5d77de444b87",
	},
	"darwin/arm64": {
		GOOS: "darwin", GOARCH: "arm64", Version: kimiVersion, Format: "zip", Size: 61_219_833,
		URL:    "https://github.com/MoonshotAI/kimi-code/releases/download/%40moonshot-ai/kimi-code%400.36.1/kimi-code-darwin-arm64.zip",
		SHA256: "14a09fb898742be77eb2bf41fc7fe0d78fdbdc73a4aa8fd3c80b04ebf6bee193",
	},
}

func artifactForTarget(target string) (artifact, error) {
	item, ok := artifacts[target]
	if !ok || item.Version != kimiVersion || item.Size <= 0 || len(item.SHA256) != 64 {
		return artifact{}, errors.New("unsupported Kimi Code target")
	}
	if item.URL != fmt.Sprintf("https://github.com/MoonshotAI/kimi-code/releases/download/%%40moonshot-ai/kimi-code%%40%s/%s", kimiVersion, archiveName(item)) {
		return artifact{}, errors.New("invalid Kimi Code artifact catalog")
	}
	return item, nil
}

func archiveName(item artifact) string {
	osName := item.GOOS
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[item.GOARCH]
	if item.GOOS == "windows" {
		osName = "win32"
	}
	return "kimi-code-" + osName + "-" + arch + "." + item.Format
}
