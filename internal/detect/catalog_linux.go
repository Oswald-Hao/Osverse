//go:build linux

package detect

func platformExecutableNames(base string) []string { return []string{base} }

func platformMinimumOS() string { return "Ubuntu 20.04" }

func platformGeminiMinimumOS() string { return "Ubuntu 20.04" }
