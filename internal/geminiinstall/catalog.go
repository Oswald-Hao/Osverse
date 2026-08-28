// Package geminiinstall installs the official Gemini CLI bundle with a
// checksum-pinned Node.js runtime. It never executes npm lifecycle scripts or
// trusts mutable registry metadata at install time.
package geminiinstall

import (
	"errors"
	"fmt"
)

const (
	componentID   = "gemini-cli"
	geminiName    = "Gemini CLI"
	commandName   = "gemini"
	geminiVersion = "0.57.0"
	nodeVersion   = "22.23.2"
)

type runtimeArtifact struct {
	GOOS, GOARCH             string
	URL, SHA256, Format      string
	Size                     int64
	ArchiveRoot, ArchivePath string
}

type packageArtifact struct {
	URL, SHA256 string
	Size        int64
}

var geminiPackage = packageArtifact{
	URL:    "https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-0.57.0.tgz",
	SHA256: "60f3d49767f414a90c6425c4a33295720af38766cd60dec6ca6f2321e8b304eb",
	Size:   20_718_256,
}

var runtimes = map[string]runtimeArtifact{
	"linux/amd64": {
		GOOS: "linux", GOARCH: "amd64", Format: "tar.gz", Size: 56_851_233,
		URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-linux-x64.tar.gz",
		SHA256:      "b294a556e639d64338823920e5866c21c02741742d2e1529ee1a225c1ec9252a",
		ArchiveRoot: "node-v22.23.2-linux-x64", ArchivePath: "bin/node",
	},
	"windows/amd64": {
		GOOS: "windows", GOARCH: "amd64", Format: "zip", Size: 35_683_585,
		URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-win-x64.zip",
		SHA256:      "1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97",
		ArchiveRoot: "node-v22.23.2-win-x64", ArchivePath: "node.exe",
	},
}

func artifactsForTarget(goos, goarch string) (runtimeArtifact, packageArtifact, error) {
	runtimeItem, ok := runtimes[goos+"/"+goarch]
	if !ok {
		return runtimeArtifact{}, packageArtifact{}, errors.New("unsupported Gemini CLI target")
	}
	expectedNodeURL := fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-%s", nodeVersion, nodeVersion, nodeArchiveName(runtimeItem))
	if runtimeItem.URL != expectedNodeURL || len(runtimeItem.SHA256) != 64 || runtimeItem.Size <= 0 {
		return runtimeArtifact{}, packageArtifact{}, errors.New("invalid Gemini Node.js catalog")
	}
	expectedPackageURL := fmt.Sprintf("https://registry.npmjs.org/@google/gemini-cli/-/gemini-cli-%s.tgz", geminiVersion)
	if geminiPackage.URL != expectedPackageURL || len(geminiPackage.SHA256) != 64 || geminiPackage.Size <= 0 {
		return runtimeArtifact{}, packageArtifact{}, errors.New("invalid Gemini CLI package catalog")
	}
	return runtimeItem, geminiPackage, nil
}

func nodeArchiveName(item runtimeArtifact) string {
	if item.GOOS == "windows" {
		return "win-x64.zip"
	}
	return "linux-x64.tar.gz"
}
