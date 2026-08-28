//go:build windows

package managedcommand

import (
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/windowsinstall"
)

func Inspect(home, componentID, command string, _ Paths) error {
	if err := windowsinstall.ValidateManagedCommandSlot(home, componentID, command); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return ErrExternalCommand
		}
		return err
	}
	return nil
}

func Activate(home, componentID, command, _ string, paths Paths) error {
	if err := windowsinstall.ActivateManagedCommand(home, componentID, command, paths.WrapperPath); err != nil {
		if errors.Is(err, windowsinstall.ErrExternalCommand) {
			return ErrExternalCommand
		}
		return err
	}
	return nil
}
