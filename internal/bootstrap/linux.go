//go:build linux

// Package bootstrap wires production dependencies without starting a scan.
package bootstrap

import (
	"io/fs"
	"os"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/detect"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
	"github.com/Oswald-Hao/Osverse/internal/scan"
)

// NewLinuxScanner constructs the fixed Phase-1 Linux scanner.
func NewLinuxScanner() *scan.Service {
	runner := platformlinux.NewExecRunner()
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return scan.NewService(
		platformlinux.NewSystemProbe(),
		platformlinux.NewPathProbe(),
		linuxComponentProbes(runner, os.DirFS("/"), home),
		time.Now,
	)
}

func linuxComponentProbes(runner platform.CommandRunner, filesystem fs.FS, home string) []scan.ComponentProbe {
	commandSpecs := detect.CoreCLISpecs()
	desktopSpecs := detect.DesktopSpecs()
	components := make([]scan.ComponentProbe, 0, len(commandSpecs)+len(desktopSpecs))
	for _, spec := range commandSpecs {
		components = append(components, detect.CommandComponentProbe{
			Detector: detect.CommandDetector{Runner: runner},
			Spec:     spec,
		})
	}
	for _, spec := range desktopSpecs {
		components = append(components, detect.DesktopComponentProbe{
			Spec:     spec,
			Packages: detect.DpkgQuery{Runner: runner},
			FS:       filesystem,
			Home:     home,
		})
	}
	return components
}
