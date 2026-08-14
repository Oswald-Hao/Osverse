//go:build linux

package detect

func platformExecutableNames(base string) []string { return []string{base} }

func platformMinimumOS() string { return "Ubuntu 20.04" }
