//go:build !windows

package harnessinstall

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCommitHarnessPayloadRejectsDamagedExistingRuntime(t *testing.T) {
	for _, tc := range []struct {
		name        string
		relative    string
		existing    []byte
		replacement []byte
	}{
		{name: "runtime", relative: "runtime/bin/node", existing: []byte("damaged"), replacement: []byte("verified")},
		{name: "entrypoint", relative: "app/node_modules/@deepseek-ai/dsh/lib/bin.js", existing: []byte("damaged"), replacement: []byte("verified")},
		{name: "wrapper", relative: "bin/dsh", existing: []byte("damaged"), replacement: []byte("verified")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			payload := filepath.Join(root, "payload")
			destination := filepath.Join(root, "installed")
			writeHarnessFixture(t, payload, "linux", []byte("verified"))
			writeHarnessFixture(t, destination, "linux", []byte("verified"))
			if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(tc.relative)), tc.existing, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(payload, filepath.FromSlash(tc.relative)), tc.replacement, 0o700); err != nil {
				t.Fatal(err)
			}

			if err := commitHarnessPayload(payload, destination, "linux"); !errors.Is(err, errVersion) {
				t.Fatalf("commitHarnessPayload() error = %v, want %v", err, errVersion)
			}
		})
	}
}

func TestCommitHarnessPayloadRejectsSymlinkedExistingRuntime(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	destination := filepath.Join(root, "installed")
	writeHarnessFixture(t, payload, "linux", []byte("verified"))
	writeHarnessFixture(t, destination, "linux", []byte("verified"))
	installedNode := filepath.Join(destination, "runtime", "bin", "node")
	outsideNode := filepath.Join(root, "outside-node")
	if err := os.WriteFile(outsideNode, []byte("verified"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(installedNode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideNode, installedNode); err != nil {
		t.Fatal(err)
	}

	if err := commitHarnessPayload(payload, destination, "linux"); !errors.Is(err, errVersion) {
		t.Fatalf("commitHarnessPayload() error = %v, want %v", err, errVersion)
	}
}

func TestAtomicSymlinkUsesCollisionFreeStaging(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "dsh")
	targets := []string{"one", "two", "three", "four"}
	var group sync.WaitGroup
	errorsCh := make(chan error, len(targets))
	for _, target := range targets {
		target := target
		group.Add(1)
		go func() {
			defer group.Done()
			errorsCh <- atomicSymlink(destination, target)
		}()
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatalf("atomicSymlink() error = %v", err)
		}
	}
	resolved, err := os.Readlink(destination)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range targets {
		found = found || resolved == target
	}
	if !found {
		t.Fatalf("final symlink target = %q", resolved)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "dsh" {
			t.Fatalf("staging artifact remains: %s", entry.Name())
		}
	}
}

func TestUpdateProfilePATHRejectsConcurrentChanges(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".profile")
	original := []byte("export EDITOR=vim\n")
	if err := os.WriteFile(profile, original, 0o600); err != nil {
		t.Fatal(err)
	}
	states, err := captureProfiles(home)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte("export EDITOR=nano\n")
	if err := os.WriteFile(profile, changed, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateProfilePATH(states[0]); err == nil {
		t.Fatal("concurrently changed shell profile was overwritten")
	}
	got, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, changed) {
		t.Fatalf("profile = %q, want concurrent content %q", got, changed)
	}
}

func TestUpdateProfilePATHRejectsMalformedManagedBlock(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".profile")
	malformed := []byte("# >>> Osverse user commands >>>\nexport PATH=/broken\n")
	if err := os.WriteFile(profile, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	states, err := captureProfiles(home)
	if err != nil {
		t.Fatal(err)
	}

	if err := updateProfilePATH(states[0]); err == nil {
		t.Fatal("malformed managed profile block was accepted")
	}
	got, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, malformed) {
		t.Fatalf("malformed profile changed to %q", got)
	}
}

func writeHarnessFixture(t *testing.T, root, goos string, content []byte) {
	t.Helper()
	paths := []string{
		".osverse-harness-runtime",
		"runtime/bin/node",
		"app/node_modules/@deepseek-ai/dsh/lib/bin.js",
		"bin/dsh",
	}
	if goos == "windows" {
		paths[1], paths[3] = "runtime/node.exe", "bin/dsh.cmd"
	}
	for _, relative := range paths {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		value := content
		if relative == ".osverse-harness-runtime" {
			value = []byte("fixed manifest")
		}
		if err := os.WriteFile(path, value, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
