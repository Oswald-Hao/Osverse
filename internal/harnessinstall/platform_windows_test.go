//go:build windows

package harnessinstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestCommitHarnessRenameDoesNotReplaceDestination(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "destination")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "source.txt"), []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "destination.txt"), []byte("destination"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := commitHarnessRename(source, destination); err == nil {
		t.Fatal("commitHarnessRename() replaced an existing destination")
	}
	for path, want := range map[string]string{
		filepath.Join(source, "source.txt"):           "source",
		filepath.Join(destination, "destination.txt"): "destination",
	} {
		raw, err := os.ReadFile(path)
		if err != nil || string(raw) != want {
			t.Fatalf("preserved file %s = (%q, %v), want %q", path, raw, err, want)
		}
	}
}

func TestRetryWindowsHarnessRenameHandlesTransientFileLocks(t *testing.T) {
	for _, retryable := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION,
	} {
		t.Run(retryable.Error(), func(t *testing.T) {
			calls := 0
			var delays []time.Duration
			err := retryWindowsHarnessRename(func() error {
				calls++
				if calls < 3 {
					return &os.LinkError{Op: "rename", Old: "payload", New: "final", Err: retryable}
				}
				return nil
			}, func() bool { return true }, func(delay time.Duration) { delays = append(delays, delay) })
			if err != nil || calls != 3 || len(delays) != 2 {
				t.Fatalf("retryWindowsHarnessRename() = %v, calls=%d delays=%v", err, calls, delays)
			}
			for _, delay := range delays {
				if delay != windowsHarnessRenameDelay {
					t.Fatalf("retry delay = %v, want %v", delay, windowsHarnessRenameDelay)
				}
			}
		})
	}
}

func TestRetryWindowsHarnessRenameStopsAtBound(t *testing.T) {
	calls, sleeps := 0, 0
	err := retryWindowsHarnessRename(func() error {
		calls++
		return windows.ERROR_SHARING_VIOLATION
	}, func() bool { return true }, func(time.Duration) { sleeps++ })
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || calls != windowsHarnessRenameAttempts || sleeps != windowsHarnessRenameAttempts-1 {
		t.Fatalf("bounded retry = %v, calls=%d sleeps=%d", err, calls, sleeps)
	}
}

func TestRetryWindowsHarnessRenameRejectsPermanentErrorImmediately(t *testing.T) {
	permanent := errors.New("permanent rename failure")
	calls, sleeps := 0, 0
	err := retryWindowsHarnessRename(func() error {
		calls++
		return permanent
	}, func() bool { return true }, func(time.Duration) { sleeps++ })
	if !errors.Is(err, permanent) || calls != 1 || sleeps != 0 {
		t.Fatalf("permanent failure = %v, calls=%d sleeps=%d", err, calls, sleeps)
	}
}

func TestRetryWindowsHarnessRenameDoesNotRetryAfterDestinationAppears(t *testing.T) {
	calls, sleeps := 0, 0
	err := retryWindowsHarnessRename(func() error {
		calls++
		return windows.ERROR_ACCESS_DENIED
	}, func() bool { return false }, func(time.Duration) { sleeps++ })
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || calls != 1 || sleeps != 0 {
		t.Fatalf("destination race = %v, calls=%d sleeps=%d", err, calls, sleeps)
	}
}
