//go:build linux

package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"golang.org/x/sys/unix"
)

func TestDiscoverPathsCollectsStableCandidatesFromKnownProfiles(t *testing.T) {
	// Removing profile parsing or PATH de-duplication must make this test fail.
	got := DiscoverPaths(PathInputs{
		ProcessPath: "/usr/local/bin/:/usr/bin:/usr/local/bin:relative",
		Home:        "/home/alice",
		Shell:       "/bin/zsh",
		ProfileFiles: map[string][]byte{
			".profile":      []byte("export PATH=\"$HOME/.cargo/bin:$PATH:/opt/profile/bin\"\n"),
			".bash_profile": []byte("PATH=${HOME}/go/bin:${PATH}\n"),
			".bashrc":       []byte("export PATH=/usr/bin:/opt/bash/bin:$PATH\n"),
			".zprofile":     []byte("PATH=/opt/zsh/bin:$HOME/bin\n"),
			".zshrc":        []byte("export PATH=${PATH}:/opt/zsh/bin\n"),
		},
	})

	want := []string{
		"/usr/local/bin",
		"/usr/bin",
		"/home/alice/.local/bin",
		"/home/alice/.cargo/bin",
		"/opt/profile/bin",
		"/home/alice/go/bin",
		"/opt/bash/bin",
		"/opt/zsh/bin",
		"/home/alice/bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverPaths() = %#v, want %#v", got, want)
	}
}

func TestDiscoverPathsRejectsRelativeAndUnsafeAssignments(t *testing.T) {
	// Accepting any shell evaluation syntax or a relative directory is a security bug.
	got := DiscoverPaths(PathInputs{
		ProcessPath: "/usr/bin",
		Home:        "/home/alice",
		ProfileFiles: map[string][]byte{
			".profile":      []byte("PATH=relative:/safe/relative\nexport PATH=/unsafe/whitespace still-not-a-path\n"),
			".bash_profile": []byte("PATH=$(id):/unsafe/command-substitution\n"),
			".bashrc":       []byte("PATH=`id`:/unsafe/backticks\n"),
			".zprofile":     []byte("PATH=/unsafe/redirect>/tmp/out\n"),
			".zshrc":        []byte("PATH=/unsafe/separator:/safe;id\nexport PATH=/unsafe/glob/*:$PATH\n"),
		},
	})

	want := []string{"/usr/bin", "/home/alice/.local/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverPaths() = %#v, want %#v", got, want)
	}
}

func TestPathProbeReadsOnlyKnownProfileFiles(t *testing.T) {
	// Reading arbitrary dotfiles instead of the five allowlisted profiles must fail this test.
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte("PATH=/profile/bin:$PATH\n"), 0o600); err != nil {
		t.Fatalf("write .profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ignored"), []byte("PATH=/ignored/bin:$PATH\n"), 0o600); err != nil {
		t.Fatalf("write .ignored profile: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/process/bin")

	got, err := NewPathProbe().Paths(context.Background())
	if err != nil {
		t.Fatalf("Paths() error = %v", err)
	}
	want := []string{"/process/bin", filepath.Join(home, ".local/bin"), "/profile/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Paths() = %#v, want %#v", got, want)
	}
}

func TestPathProbeRedactsProfileReadFailures(t *testing.T) {
	// Returning the operating-system error directly would disclose the user's home directory.
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".profile"), 0o700); err != nil {
		t.Fatalf("create .profile directory: %v", err)
	}
	t.Setenv("HOME", home)

	_, err := NewPathProbe().Paths(context.Background())
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("Paths() error type = %T, want *domain.PublicError", err)
	}
	if public.Code != domain.ErrScanFailed {
		t.Errorf("error code = %q, want %q", public.Code, domain.ErrScanFailed)
	}
	if err != nil && strings.Contains(err.Error(), home) {
		t.Errorf("Paths() error leaked home %q: %q", home, err)
	}
}

func TestPathProbeRejectsRelativeHomeWithoutReadingCurrentDirectory(t *testing.T) {
	// Resolving a relative HOME through the current directory would cross the home boundary.
	workingDirectory := t.TempDir()
	if err := os.Mkdir(filepath.Join(workingDirectory, "relative-home"), 0o700); err != nil {
		t.Fatalf("create relative home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workingDirectory, "relative-home", ".profile"), []byte("PATH=/cwd-escape/bin:$PATH\n"), 0o600); err != nil {
		t.Fatalf("write current-directory profile: %v", err)
	}
	t.Chdir(workingDirectory)
	t.Setenv("HOME", "relative-home")

	_, err := NewPathProbe().Paths(context.Background())
	assertPathProbeScanFailed(t, err, "relative-home")
}

func TestValidatePathProbeHomeRejectsEmptyHome(t *testing.T) {
	// Treating an empty home as the current directory would make the profile scope ambiguous.
	if _, err := validatePathProbeHome(""); err == nil {
		t.Fatal("validatePathProbeHome(\"\") error = nil, want rejection")
	}
}

func TestPathProbeRejectsProfileSymlinkOutsideHome(t *testing.T) {
	// Following a named profile symlink would parse data outside the pinned home directory.
	home := t.TempDir()
	outside := t.TempDir()
	outsideProfile := filepath.Join(outside, "profile")
	if err := os.WriteFile(outsideProfile, []byte("PATH=/outside-escape/bin:$PATH\n"), 0o600); err != nil {
		t.Fatalf("write outside profile: %v", err)
	}
	if err := os.Symlink(outsideProfile, filepath.Join(home, ".profile")); err != nil {
		t.Fatalf("create profile symlink: %v", err)
	}
	t.Setenv("HOME", home)

	_, err := NewPathProbe().Paths(context.Background())
	assertPathProbeScanFailed(t, err, outside)
}

func TestPathProbeRejectsFIFOProfileWithoutBlocking(t *testing.T) {
	// Opening a FIFO with a blocking read would let an untrusted profile hang scanning.
	home := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(home, ".profile"), 0o600); err != nil {
		t.Fatalf("create profile FIFO: %v", err)
	}
	t.Setenv("HOME", home)

	done := make(chan error, 1)
	go func() {
		_, err := NewPathProbe().Paths(context.Background())
		done <- err
	}()

	select {
	case err := <-done:
		assertPathProbeScanFailed(t, err, home)
	case <-time.After(time.Second):
		t.Fatal("Paths() blocked while opening a FIFO profile")
	}
}

func TestPathProbeRejectsOversizeProfile(t *testing.T) {
	// Reading a profile beyond the bounded limit would permit unbounded allocation.
	home := t.TempDir()
	profile := make([]byte, maxPathProfileBytes+1)
	copy(profile, "PATH=/oversize/bin:$PATH\n")
	if err := os.WriteFile(filepath.Join(home, ".profile"), profile, 0o600); err != nil {
		t.Fatalf("write oversize profile: %v", err)
	}
	t.Setenv("HOME", home)

	_, err := NewPathProbe().Paths(context.Background())
	assertPathProbeScanFailed(t, err, home)
}

func TestPathProbeRejectsPreCanceledContext(t *testing.T) {
	// Continuing after cancellation would expose the probe to unnecessary filesystem work.
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewPathProbe().Paths(ctx)
	assertPathProbeScanFailed(t, err, home)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Paths() error = %v, want wrapped context cancellation", err)
	}
}

func TestReadBoundedPathProfileStopsAfterContextCancellation(t *testing.T) {
	// Omitting the between-chunk context check would read the second chunk after cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	reader := cancelAfterFirstRead{cancel: cancel, data: []byte("PATH=/first/bin:$PATH\nPATH=/second/bin:$PATH\n")}

	_, err := readBoundedPathProfile(ctx, &reader)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("readBoundedPathProfile() error = %v, want context cancellation", err)
	}
}

func assertPathProbeScanFailed(t *testing.T, err error, forbidden string) {
	t.Helper()
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("Paths() error type = %T, want *domain.PublicError", err)
	}
	if public.Code != domain.ErrScanFailed {
		t.Errorf("error code = %q, want %q", public.Code, domain.ErrScanFailed)
	}
	if err != nil && strings.Contains(err.Error(), forbidden) {
		t.Errorf("Paths() error leaked %q: %q", forbidden, err)
	}
}

type cancelAfterFirstRead struct {
	cancel context.CancelFunc
	data   []byte
	reads  int
}

func (reader *cancelAfterFirstRead) Read(destination []byte) (int, error) {
	if reader.reads > 0 {
		return 0, nil
	}
	reader.reads++
	count := copy(destination, reader.data[:min(len(reader.data), 8)])
	reader.cancel()
	return count, nil
}
