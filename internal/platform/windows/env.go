//go:build windows

package windows

import "os"

func comspec() string {
	if value := os.Getenv("ComSpec"); value != "" {
		return value
	}
	return os.Getenv("COMSPEC")
}
