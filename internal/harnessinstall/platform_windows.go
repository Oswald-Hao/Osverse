//go:build windows

package harnessinstall

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
	"golang.org/x/sys/windows"
)

const (
	windowsHarnessRenameAttempts = 26
	windowsHarnessRenameDelay    = 200 * time.Millisecond
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

func commitHarnessRename(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return retryWindowsHarnessRename(
		func() error { return windows.MoveFile(from, to) },
		func() bool {
			_, statErr := os.Lstat(destination)
			return errors.Is(statErr, os.ErrNotExist)
		},
		time.Sleep,
	)
}

func retryWindowsHarnessRename(rename func() error, destinationAbsent func() bool, sleep func(time.Duration)) error {
	if rename == nil || destinationAbsent == nil || sleep == nil {
		return errors.New("invalid Windows Harness rename retry")
	}
	var lastErr error
	for attempt := 0; attempt < windowsHarnessRenameAttempts; attempt++ {
		lastErr = rename()
		if lastErr == nil {
			return nil
		}
		if !retryableWindowsHarnessRename(lastErr) || attempt == windowsHarnessRenameAttempts-1 || !destinationAbsent() {
			return lastErr
		}
		sleep(windowsHarnessRenameDelay)
	}
	return lastErr
}

func retryableWindowsHarnessRename(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
