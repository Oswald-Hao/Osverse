//go:build windows

package kimiinstall

import (
	"errors"
	"path/filepath"

	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
)

func managedPathsFor(home, _ string, version string) managedPaths {
	root := filepath.Join(home, "AppData", "Local", "Osverse")
	toolRoot := filepath.Join(root, "tools", componentID)
	finalRoot := filepath.Join(toolRoot, version)
	return managedPaths{
		root: root, stagingRoot: filepath.Join(root, "staging"), toolRoot: toolRoot,
		finalRoot: finalRoot, binRoot: filepath.Join(home, ".local", "bin"),
		shimPath:    filepath.Join(home, ".local", "bin", "kimi.cmd"),
		wrapperPath: filepath.Join(finalRoot, "bin", "kimi.cmd"),
		binaryPath:  filepath.Join(finalRoot, "bin", "kimi.real.exe"),
	}
}

func inspectCommandEntry(paths managedPaths) error {
	home := filepath.Dir(filepath.Dir(paths.binRoot))
	if err := windowsinstall.ValidateManagedCommandSlot(home, componentID, "kimi"); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return errExternalCommand
		}
		return err
	}
	return nil
}

func activateCommand(home string, paths managedPaths) error {
	if err := windowsinstall.ActivateManagedCommand(home, componentID, "kimi", paths.wrapperPath); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return errExternalCommand
		}
		return err
	}
	return nil
}
