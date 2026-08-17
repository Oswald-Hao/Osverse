//go:build windows

package windowsinstall

import (
	"errors"
	"path/filepath"
)

// ErrExternalCommand reports that a command alias is not owned by Osverse.
var ErrExternalCommand = errExternalShim

// ValidateManagedCommandSlot checks the exact dsh alias without changing disk.
func ValidateManagedCommandSlot(home, componentID, command string) error {
	root, err := ensureManagedDirectories(home, "AppData", "Local", "Osverse")
	if err != nil {
		return err
	}
	binRoot, err := ensureManagedDirectories(home, ".local", "bin")
	if err != nil {
		return err
	}
	if err := rejectConflictingAliases(binRoot, command); err != nil {
		return err
	}
	_, _, err = inspectShim(filepath.Join(binRoot, command+".cmd"), componentID, filepath.Join(root, "tools"))
	return err
}

// ActivateManagedCommand writes the standard removal-verifiable Osverse shim
// and adds its directory to the current user's PATH.
func ActivateManagedCommand(home, componentID, command, target string) error {
	root, err := ensureManagedDirectories(home, "AppData", "Local", "Osverse")
	if err != nil {
		return err
	}
	managedRoot := filepath.Join(root, "tools")
	if !filepath.IsAbs(target) || !within(managedRoot, filepath.Clean(target)) {
		return errors.New("Harness command target is outside the managed root")
	}
	binRoot, err := ensureManagedDirectories(home, ".local", "bin")
	if err != nil {
		return err
	}
	if err := rejectConflictingAliases(binRoot, command); err != nil {
		return err
	}
	shimPath := filepath.Join(binRoot, command+".cmd")
	if _, _, err := inspectShim(shimPath, componentID, managedRoot); err != nil {
		return err
	}
	if _, _, _, err := ensureUserPath(binRoot); err != nil {
		return err
	}
	if err := atomicReplace(shimPath, managedShim(componentID, target)); err != nil {
		return err
	}
	broadcastEnvironmentChange()
	return nil
}
