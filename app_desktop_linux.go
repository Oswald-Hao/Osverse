//go:build linux

package main

func isUserDesktopComponent(id string) bool {
	switch id {
	case "opencode-desktop", "cc-switch", "cockpit-tools":
		return true
	default:
		return false
	}
}
