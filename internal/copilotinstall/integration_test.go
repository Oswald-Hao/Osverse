//go:build linux

package copilotinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficialLinuxStandaloneArchiveFromVerifiedCache(t *testing.T) {
	archive := os.Getenv("OSVERSE_COPILOT_LINUX_ARCHIVE")
	if archive == "" {
		t.Skip("set OSVERSE_COPILOT_LINUX_ARCHIVE to the pinned official archive")
	}
	item, err := artifactForTarget("linux/amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		t.Fatalf("archive checksum mismatch: %v", err)
	}
	file, err = os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "copilot")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := extractCopilotTar(ctx, file, destination); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, destination, "--no-auto-update", "--version")
	command.Env = append(os.Environ(), "CI=1", "NO_COLOR=1")
	output, err := command.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(output), "GitHub Copilot CLI "+copilotVersion+".") {
		t.Fatalf("copilot --version = %q, %v", output, err)
	}
}
