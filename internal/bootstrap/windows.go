//go:build windows

// Package bootstrap wires production dependencies without starting a scan.
package bootstrap

import (
	"os"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/detect"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/scan"
)

func NewWindowsScanner() *scan.Service {
	runner := platformwindows.NewExecRunner()
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return scan.NewService(
		platformwindows.NewSystemProbe(),
		platformwindows.NewPathProbe(),
		windowsComponentProbes(runner, home),
		time.Now,
	)
}

func windowsComponentProbes(runner platform.CommandRunner, home string) []scan.ComponentProbe {
	commands := detect.CoreCLISpecs()
	desktops := detect.WindowsDesktopSpecs()
	components := make([]scan.ComponentProbe, 0, len(commands)+len(desktops))
	for _, spec := range commands {
		components = append(components, detect.CommandComponentProbe{Detector: detect.CommandDetector{Runner: runner}, Spec: spec})
	}
	for _, spec := range desktops {
		components = append(components, detect.WindowsDesktopComponentProbe{
			Spec: spec, Packages: detect.RegistryPackageQuery{}, Home: home,
		})
	}
	return components
}
