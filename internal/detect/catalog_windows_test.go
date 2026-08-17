//go:build windows

package detect

import (
	"path/filepath"
	"testing"
)

func TestWindowsCoreCLICatalogUsesExecutableAndShimNames(t *testing.T) {
	specs := CoreCLISpecs()
	if len(specs) != 4 {
		t.Fatalf("spec count = %d", len(specs))
	}
	for _, spec := range specs {
		if spec.MinimumOS != "Windows 10 1809" || len(spec.ExecutableNames) < 2 {
			t.Fatalf("Windows spec = %#v", spec)
		}
		if filepath.Ext(spec.ExecutableNames[0]) != ".exe" || filepath.Ext(spec.ExecutableNames[1]) != ".cmd" {
			t.Fatalf("Windows executables = %#v", spec.ExecutableNames)
		}
	}
}
