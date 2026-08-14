//go:build linux

package main

import (
	"os"

	"github.com/Oswald-Hao/Osverse/internal/bootstrap"
	"github.com/Oswald-Hao/Osverse/internal/systeminstall"
)

func platformScanner() Scanner {
	return bootstrap.NewLinuxScanner()
}

func privilegedInvocation() (int, bool) {
	if !systeminstall.IsPrivilegedInvocation(os.Args[1:]) {
		return 0, false
	}
	return systeminstall.RunPrivileged(os.Args[1:]), true
}

func platformExit(code int) {
	os.Exit(code)
}
