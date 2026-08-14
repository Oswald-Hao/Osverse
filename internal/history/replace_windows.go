//go:build windows

package history

import xwindows "golang.org/x/sys/windows"

func replaceHistoryFile(source, destination string) error {
	sourcePath, err := xwindows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPath, err := xwindows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return xwindows.MoveFileEx(
		sourcePath,
		destinationPath,
		xwindows.MOVEFILE_REPLACE_EXISTING|xwindows.MOVEFILE_WRITE_THROUGH,
	)
}
