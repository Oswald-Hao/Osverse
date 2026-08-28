//go:build windows

package detect

func platformExecutableNames(base string) []string {
	result := []string{base + ".exe", base + ".cmd"}
	if base == "codex" {
		result = append(result, "codex-x86_64-pc-windows-msvc.exe")
	}
	return result
}

func platformMinimumOS() string { return "Windows 10 1809" }

func platformGeminiMinimumOS() string { return "Windows 11 24H2" }
