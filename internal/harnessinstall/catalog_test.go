package harnessinstall

import (
	"runtime"
	"testing"
)

func TestBuiltInLockIsClosedAndPlatformFiltered(t *testing.T) {
	lock, err := builtInLock()
	if err != nil {
		t.Fatal(err)
	}
	if lock.HarnessVersion != "0.1.0-rc.6" {
		t.Fatalf("HarnessVersion = %q", lock.HarnessVersion)
	}
	if len(lock.Packages) < 500 || len(lock.Packages) > 700 {
		t.Fatalf("unexpected package count %d", len(lock.Packages))
	}
	for _, item := range lock.Packages {
		if item.Path == "" || item.URL == "" || len(item.Integrity) != 64 {
			t.Fatalf("invalid locked package: %#v", item)
		}
	}

	filtered, err := packagesForTarget(lock, "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range filtered {
		if !targetAllowed(item.OS, item.CPU, item.LibC, "win32", "x64", "") {
			t.Fatalf("incompatible package survived: %s", item.Path)
		}
	}
}

func TestRuntimeCatalogCoversReleasedTargets(t *testing.T) {
	cases := []struct {
		goos, goarch, executable string
	}{
		{"linux", "amd64", "runtime/bin/node"},
		{"windows", "amd64", "runtime/node.exe"},
		{"darwin", "amd64", "runtime/bin/node"},
		{"darwin", "arm64", "runtime/bin/node"},
	}
	for _, tc := range cases {
		t.Run(tc.goos+"-"+tc.goarch, func(t *testing.T) {
			item, err := runtimeForTarget(tc.goos, tc.goarch)
			if err != nil {
				t.Fatal(err)
			}
			if item.Executable != tc.executable || item.Size <= 0 || len(item.SHA256) != 64 {
				t.Fatalf("invalid runtime: %#v", item)
			}
		})
	}
	if _, err := runtimeForTarget("linux", "arm64"); err == nil {
		t.Fatal("linux arm64 unexpectedly supported")
	}
	_, _ = runtime.GOOS, runtime.GOARCH
}

func TestTargetAllowedHandlesPositiveAndNegativeSelectors(t *testing.T) {
	if !targetAllowed([]string{"linux", "darwin"}, []string{"x64"}, nil, "linux", "x64", "glibc") {
		t.Fatal("positive selector rejected")
	}
	if targetAllowed([]string{"!win32"}, nil, nil, "win32", "x64", "") {
		t.Fatal("negative selector accepted")
	}
	if !targetAllowed([]string{"!win32"}, nil, nil, "linux", "x64", "glibc") {
		t.Fatal("unrelated negative selector rejected")
	}
	if targetAllowed([]string{"linux"}, []string{"x64"}, []string{"musl"}, "linux", "x64", "glibc") {
		t.Fatal("musl package accepted for glibc release")
	}
}
