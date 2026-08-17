package selfupdate

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeUpdateNamesRejectPathSemantics(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"osverse-update.exe": true,
		"":                   false,
		".":                  false,
		"..":                 false,
		"../update.exe":      false,
		`dir\update.exe`:     false,
		"dir/update.exe":     false,
	}
	for value, want := range tests {
		if got := safeName(value); got != want {
			t.Errorf("safeName(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestAllowedDownloadHostsAreExact(t *testing.T) {
	t.Parallel()

	for _, host := range []string{
		"api.github.com",
		"GITHUB.COM",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
	} {
		if !allowedDownloadHost(host) {
			t.Errorf("allowedDownloadHost(%q) = false, want true", host)
		}
	}
	for _, host := range []string{
		"",
		"github.com.",
		"github.com.evil.example",
		"evil-github.com",
		"raw.githubusercontent.com",
		"objects.githubusercontent.com.evil.example",
	} {
		if allowedDownloadHost(host) {
			t.Errorf("allowedDownloadHost(%q) = true, want false", host)
		}
	}
}

func TestReleaseURLValidationRejectsAmbiguity(t *testing.T) {
	t.Parallel()

	filename := "osverse-1.2.3-windows-amd64-setup.exe"
	valid := "https://github.com/" + repository + "/releases/download/v1.2.3/" + filename
	if !validReleaseURL(valid, filename) {
		t.Fatal("canonical release URL was rejected")
	}
	for _, candidate := range []string{
		"http://github.com/" + repository + "/releases/download/v1.2.3/" + filename,
		"https://user@github.com/" + repository + "/releases/download/v1.2.3/" + filename,
		"https://github.com.evil.example/" + repository + "/releases/download/v1.2.3/" + filename,
		valid + "?download=1",
		valid + "#fragment",
		"https://github.com/other/repository/releases/download/v1.2.3/" + filename,
		"https://github.com/" + repository + "/releases/download/v1.2.3/other.exe",
	} {
		if validReleaseURL(candidate, filename) {
			t.Errorf("ambiguous release URL accepted: %q", candidate)
		}
	}
}

func TestArtifactValidationRejectsBoundaryViolations(t *testing.T) {
	t.Parallel()

	version := "1.2.3"
	filename := expectedArtifactFilename("nsis", version, "windows10+")
	valid := Artifact{
		Architecture: "amd64",
		Filename:     filename,
		Format:       "nsis",
		Platform:     "windows",
		SHA256:       strings.Repeat("a", 64),
		Size:         1024,
		Target:       "windows10+",
		URL:          "https://github.com/" + repository + "/releases/download/v" + version + "/" + filename,
	}
	if err := validateArtifact(valid, version, valid.Target); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}

	tests := map[string]func(*Artifact){
		"empty file":         func(value *Artifact) { value.Size = 0 },
		"oversized file":     func(value *Artifact) { value.Size = maxArtifactBytes + 1 },
		"short digest":       func(value *Artifact) { value.SHA256 = strings.Repeat("a", 63) },
		"uppercase digest":   func(value *Artifact) { value.SHA256 = strings.Repeat("A", 64) },
		"non-hex digest":     func(value *Artifact) { value.SHA256 = strings.Repeat("z", 64) },
		"filename traversal": func(value *Artifact) { value.Filename = "../" + filename },
		"unexpected filename": func(value *Artifact) {
			value.Filename = "other.exe"
			value.URL = "https://github.com/" + repository + "/releases/download/v" + version + "/other.exe"
		},
		"query parameter": func(value *Artifact) { value.URL += "?download=1" },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := valid
			mutate(&candidate)
			if err := validateArtifact(candidate, version, valid.Target); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
}

func TestUpdateDownloadRootRejectsUnsafeHome(t *testing.T) {
	t.Parallel()

	if _, err := updateDownloadRoot("relative/home"); err == nil {
		t.Fatal("relative home accepted")
	}
	root := filepath.VolumeName(string(filepath.Separator)) + string(filepath.Separator)
	if _, err := updateDownloadRoot(root); err == nil {
		t.Fatal("filesystem root accepted as home")
	}
	home := t.TempDir()
	got, err := updateDownloadRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(append([]string{home}, updatePathComponents()...)...)
	if got != want {
		t.Fatalf("update root = %q, want %q", got, want)
	}
}
