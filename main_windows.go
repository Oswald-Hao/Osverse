//go:build windows

package main

import (
	"os"

	"github.com/Oswald-Hao/Osverse/internal/bootstrap"
)

func platformScanner() Scanner { return bootstrap.NewWindowsScanner() }

func privilegedInvocation() (int, bool) { return 0, false }

func platformExit(code int) { os.Exit(code) }
