//go:build linux

package profiles

import "os"

func replaceProfileFile(source, destination string) error {
	return os.Rename(source, destination)
}
