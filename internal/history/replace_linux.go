//go:build linux

package history

import "os"

func replaceHistoryFile(source, destination string) error {
	return os.Rename(source, destination)
}
