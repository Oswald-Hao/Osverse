//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"strings"
)

func comspec() string {
	for _, value := range []string{os.Getenv("ComSpec"), os.Getenv("COMSPEC")} {
		if safeCommandProcessor(value) {
			return filepath.Clean(value)
		}
	}
	if root := os.Getenv("SystemRoot"); safeCommandProcessorRoot(root) {
		return filepath.Join(filepath.Clean(root), "System32", "cmd.exe")
	}
	return ""
}

func safeCommandProcessor(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n") && strings.EqualFold(filepath.Base(value), "cmd.exe")
}

func safeCommandProcessorRoot(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
