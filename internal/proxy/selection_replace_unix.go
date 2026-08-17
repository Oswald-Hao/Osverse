//go:build !windows

package proxy

import "os"

func replaceSelectionFile(source, destination string) error {
	return os.Rename(source, destination)
}
