//go:build windows

package harnessinstall

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
		shimPath:    filepath.Join(home, ".local", "bin", "dsh.cmd"),
		wrapperPath: filepath.Join(finalRoot, "bin", "dsh.cmd"),
	}
}

func inspectCommandEntry(paths managedPaths, _ string) error {
	home := filepath.Dir(filepath.Dir(paths.binRoot))
	if err := windowsinstall.ValidateManagedCommandSlot(home, componentID, "dsh"); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return errExternalCommand
		}
		return err
	}
	return nil
}

func activateHarnessCommand(home string, paths managedPaths, _ string) error {
	if err := windowsinstall.ActivateManagedCommand(home, componentID, "dsh", paths.wrapperPath); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return errExternalCommand
		}
		return err
	}
	return nil
}
