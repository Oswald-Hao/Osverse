//go:build windows

package main

func isUserDesktopComponent(id string) bool {
	switch id {
	case "claude-desktop", "chatgpt-desktop", "codex-desktop", "opencode-desktop", "cc-switch", "cockpit-tools":
		return true
	default:
		return false
	}
}
