//go:build windows

package geminiinstall

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

const (
	windowsRenameAttempts = 26
	windowsRenameDelay    = 200 * time.Millisecond
)

func managedPathsFor(home, _ string, version string) managedPaths {
	root := filepath.Join(home, "AppData", "Local", "Osverse")
	toolRoot := filepath.Join(root, "tools", componentID)
	finalRoot := filepath.Join(toolRoot, version)
	return managedPaths{
		root: root, stagingRoot: filepath.Join(root, "staging"), toolRoot: toolRoot,
		finalRoot: finalRoot, binRoot: filepath.Join(home, ".local", "bin"),
		shimPath:    filepath.Join(home, ".local", "bin", commandName+".cmd"),
		wrapperPath: filepath.Join(finalRoot, "bin", commandName+".cmd"),
	}
}

func commitRename(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < windowsRenameAttempts; attempt++ {
		lastErr = windows.MoveFile(from, to)
		if lastErr == nil {
			return nil
		}
		if !retryableRename(lastErr) || attempt == windowsRenameAttempts-1 {
			return lastErr
		}
		if _, statErr := os.Lstat(destination); !errors.Is(statErr, os.ErrNotExist) {
			return lastErr
		}
		time.Sleep(windowsRenameDelay)
	}
	return lastErr
}

func retryableRename(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
