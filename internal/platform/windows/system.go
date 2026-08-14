//go:build windows

package windows

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/windows/registry"
)

const minimumWindowsBuild = 17763

type systemProbe struct{}

func NewSystemProbe() platform.SystemProbe { return systemProbe{} }

func (systemProbe) Probe(ctx context.Context) (domain.SystemInfo, error) {
	if err := ctx.Err(); err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}
	defer key.Close()
	product, _, productErr := key.GetStringValue("ProductName")
	display, _, displayErr := key.GetStringValue("DisplayVersion")
	if displayErr != nil {
		display, _, displayErr = key.GetStringValue("ReleaseId")
	}
	build, _, buildErr := key.GetStringValue("CurrentBuildNumber")
	if productErr != nil || displayErr != nil || buildErr != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", fmt.Errorf("incomplete Windows version registry"))
	}
	if err := ctx.Err(); err != nil {
		return domain.SystemInfo{}, domain.NewPublicError(domain.ErrScanFailed, "system probe failed", err)
	}
	return ProbeSystem(product, display, build, runtime.GOARCH, filepath.Base(comspec())), nil
}

func ProbeSystem(product, displayVersion, buildNumber, goarch, shell string) domain.SystemInfo {
	product = strings.TrimSpace(product)
	if product == "" {
		product = "Microsoft Windows"
	}
	displayVersion = strings.TrimSpace(displayVersion)
	if displayVersion == "" {
		displayVersion = "unknown"
	}
	buildNumber = strings.TrimSpace(buildNumber)
	version := displayVersion
	if buildNumber != "" {
		version += " (build " + buildNumber + ")"
	}
	architecture := goarch
	if goarch == "amd64" {
		architecture = "x86_64"
	}
	if shell == "" || shell == "." || shell == string(filepath.Separator) {
		shell = "unknown"
	}
	info := domain.SystemInfo{
		Distribution: product,
		Version:      version,
		Architecture: architecture,
		Shell:        shell,
	}
	build, buildErr := strconv.Atoi(buildNumber)
	switch {
	case buildErr != nil || build < minimumWindowsBuild:
		info.UnsupportedReason = fmt.Sprintf("Windows build %q is not supported; Windows 10 1809 or newer is required", buildNumber)
	case goarch != "amd64":
		info.UnsupportedReason = fmt.Sprintf("architecture %q is not supported; x86_64 is required", architecture)
	default:
		info.Supported = true
	}
	return info
}
