// Package harnessinstall installs the official DeepSeek Harness without
// executing npm lifecycle scripts or trusting mutable package metadata.
package harnessinstall

import (
	"crypto/sha512"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
)

const (
	componentID  = "deepseek-harness"
	harnessName  = "DeepSeek Harness"
	harnessVer   = "0.1.0-rc.6"
	nodeVersion  = "22.23.2"
	registryHost = "registry.npmjs.org"
)

//go:embed assets/package-lock.json
var packageLockJSON []byte

type rawLock struct {
	LockfileVersion int                         `json:"lockfileVersion"`
	Packages        map[string]rawLockedPackage `json:"packages"`
}

type rawLockedPackage struct {
	Version      string            `json:"version"`
	Resolved     string            `json:"resolved"`
	Integrity    string            `json:"integrity"`
	OS           []string          `json:"os"`
	CPU          []string          `json:"cpu"`
	Optional     bool              `json:"optional"`
	Dependencies map[string]string `json:"dependencies"`
}

type lockedPackage struct {
	Path      string
	Version   string
	URL       string
	Integrity []byte
	OS        []string
	CPU       []string
	Optional  bool
}

type packageLock struct {
	HarnessVersion string
	Packages       []lockedPackage
}

type runtimeArtifact struct {
	GOOS        string
	GOARCH      string
	URL         string
	SHA256      string
	Size        int64
	ArchiveRoot string
	ArchivePath string
	Executable  string
}

func builtInLock() (packageLock, error) {
	var raw rawLock
	if err := json.Unmarshal(packageLockJSON, &raw); err != nil {
		return packageLock{}, fmt.Errorf("decode embedded package lock: %w", err)
	}
	if raw.LockfileVersion != 3 || len(raw.Packages) < 2 || len(raw.Packages) > 1000 {
		return packageLock{}, errors.New("invalid embedded package lock")
	}
	root, ok := raw.Packages[""]
	if !ok || len(root.Dependencies) != 1 || root.Dependencies["@deepseek-ai/dsh"] != harnessVer {
		return packageLock{}, errors.New("embedded Harness dependency is not pinned")
	}
	result := packageLock{HarnessVersion: harnessVer, Packages: make([]lockedPackage, 0, len(raw.Packages)-1)}
	for packagePath, item := range raw.Packages {
		if packagePath == "" {
			continue
		}
		if !validPackagePath(packagePath) || strings.TrimSpace(item.Version) == "" {
			return packageLock{}, fmt.Errorf("invalid locked package path %q", packagePath)
		}
		parsed, err := url.Parse(item.Resolved)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() != registryHost || parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return packageLock{}, fmt.Errorf("untrusted package URL for %q", packagePath)
		}
		const prefix = "sha512-"
		if !strings.HasPrefix(item.Integrity, prefix) {
			return packageLock{}, fmt.Errorf("unsupported integrity for %q", packagePath)
		}
		digest, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(item.Integrity, prefix))
		if err != nil || len(digest) != sha512.Size {
			return packageLock{}, fmt.Errorf("invalid integrity for %q", packagePath)
		}
		result.Packages = append(result.Packages, lockedPackage{
			Path: packagePath, Version: item.Version, URL: item.Resolved,
			Integrity: digest, OS: append([]string(nil), item.OS...),
			CPU: append([]string(nil), item.CPU...), Optional: item.Optional,
		})
	}
	return result, nil
}

func validPackagePath(value string) bool {
	return strings.HasPrefix(value, "node_modules/") && value == path.Clean(value) &&
		!strings.Contains(value, `\`) && !strings.Contains(value, "\x00") &&
		!strings.HasPrefix(value, "/") && !strings.Contains(value, "/../")
}

func packagesForTarget(lock packageLock, goos, goarch string) ([]lockedPackage, error) {
	npmOS, npmArch, err := npmTarget(goos, goarch)
	if err != nil {
		return nil, err
	}
	result := make([]lockedPackage, 0, len(lock.Packages))
	for _, item := range lock.Packages {
		if targetAllowed(item.OS, item.CPU, npmOS, npmArch) {
			result = append(result, item)
		}
	}
	return result, nil
}

func npmTarget(goos, goarch string) (string, string, error) {
	osName := map[string]string{"linux": "linux", "windows": "win32", "darwin": "darwin"}[goos]
	archName := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if osName == "" || archName == "" {
		return "", "", errors.New("unsupported Harness target")
	}
	return osName, archName, nil
}

func targetAllowed(osSelectors, cpuSelectors []string, osName, archName string) bool {
	return selectorAllows(osSelectors, osName) && selectorAllows(cpuSelectors, archName)
}

func selectorAllows(selectors []string, value string) bool {
	hasPositive := false
	positiveMatch := false
	for _, selector := range selectors {
		if strings.HasPrefix(selector, "!") {
			if strings.TrimPrefix(selector, "!") == value {
				return false
			}
			continue
		}
		hasPositive = true
		positiveMatch = positiveMatch || selector == value
	}
	return !hasPositive || positiveMatch
}

func runtimeForTarget(goos, goarch string) (runtimeArtifact, error) {
	key := goos + "/" + goarch
	items := map[string]runtimeArtifact{
		"linux/amd64": {
			GOOS: "linux", GOARCH: "amd64", Size: 56851233,
			URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-linux-x64.tar.gz",
			SHA256:      "b294a556e639d64338823920e5866c21c02741742d2e1529ee1a225c1ec9252a",
			ArchiveRoot: "node-v22.23.2-linux-x64", ArchivePath: "bin/node", Executable: "runtime/bin/node",
		},
		"windows/amd64": {
			GOOS: "windows", GOARCH: "amd64", Size: 35683585,
			URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-win-x64.zip",
			SHA256:      "1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97",
			ArchiveRoot: "node-v22.23.2-win-x64", ArchivePath: "node.exe", Executable: "runtime/node.exe",
		},
		"darwin/amd64": {
			GOOS: "darwin", GOARCH: "amd64", Size: 51246936,
			URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-darwin-x64.tar.gz",
			SHA256:      "58e99022c2ff89395576cc7fd4d98cea24bb68081475d5f88b801ee8729fb026",
			ArchiveRoot: "node-v22.23.2-darwin-x64", ArchivePath: "bin/node", Executable: "runtime/bin/node",
		},
		"darwin/arm64": {
			GOOS: "darwin", GOARCH: "arm64", Size: 50068815,
			URL:         "https://nodejs.org/dist/v22.23.2/node-v22.23.2-darwin-arm64.tar.gz",
			SHA256:      "61130f394c1630d211dd50aecc4353d379480f36d3ac913cd85dbba1aed585c6",
			ArchiveRoot: "node-v22.23.2-darwin-arm64", ArchivePath: "bin/node", Executable: "runtime/bin/node",
		},
	}
	item, ok := items[key]
	if !ok {
		return runtimeArtifact{}, errors.New("unsupported Harness target")
	}
	return item, nil
}
