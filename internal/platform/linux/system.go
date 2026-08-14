//go:build linux

package linux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

type systemProbe struct{}

// NewSystemProbe returns the production Linux system probe.
func NewSystemProbe() platform.SystemProbe {
	return systemProbe{}
}

func (systemProbe) Probe(ctx context.Context) (domain.SystemInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}

	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}
	if err := ctx.Err(); err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}

	return ProbeSystem(osRelease, runtime.GOARCH, os.Getenv("SHELL")), nil
}

// ProbeSystem parses Linux release data and applies the Phase 1 support policy.
func ProbeSystem(osRelease []byte, goarch, shell string) domain.SystemInfo {
	values := parseOSRelease(osRelease)
	distribution := values["PRETTY_NAME"]
	if distribution == "" {
		distribution = "Unknown Linux"
	}
	version := values["VERSION_ID"]
	if version == "" {
		version = "unknown"
	}
	architecture := goarch
	if architecture == "amd64" {
		architecture = "x86_64"
	}
	shellName := filepath.Base(shell)
	if shell == "" || shellName == "." || shellName == string(filepath.Separator) {
		shellName = "unknown"
	}

	info := domain.SystemInfo{
		Distribution: distribution,
		Version:      version,
		Architecture: architecture,
		Shell:        shellName,
	}

	switch {
	case values["ID"] != "ubuntu":
		info.UnsupportedReason = fmt.Sprintf("distribution %q is not supported; Ubuntu is required", distribution)
	case version != "20.04" && version != "22.04":
		info.UnsupportedReason = fmt.Sprintf("Ubuntu version %q is not supported; 20.04 or 22.04 is required", version)
	case goarch != "amd64":
		info.UnsupportedReason = fmt.Sprintf("architecture %q is not supported; x86_64 is required", architecture)
	default:
		info.Supported = true
	}

	return info
}

func parseOSRelease(data []byte) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "ID" && key != "VERSION_ID" && key != "PRETTY_NAME" {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values
}
