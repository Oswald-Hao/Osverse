package linux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/domain"
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
