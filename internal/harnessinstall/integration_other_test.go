//go:build !windows

package harnessinstall

import (
	"context"
	"testing"
)

func assertWindowsProductionRemoval(*testing.T, context.Context, string, managedPaths) {}
