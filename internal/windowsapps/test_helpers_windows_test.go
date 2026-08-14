//go:build windows

package windowsapps

import "os"

func writeTestFile(path string, content []byte) error {
	return os.WriteFile(path, content, 0o600)
}
