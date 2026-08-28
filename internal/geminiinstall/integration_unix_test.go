//go:build !windows

package geminiinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func integrationRuntimeArchive(t *testing.T) ([]byte, runtimeArtifact) {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "node.tgz")
	const root = "node-v22.23.2-linux-x64"
	writeTarGz(t, archive, map[string][]byte{
		root + "/bin/node": []byte("#!/bin/sh\nif [ \"$2\" = \"--version\" ]; then printf '0.57.0\\n'; exit 0; fi\nexit 1\n"),
	})
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return raw, runtimeArtifact{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Format: "tar.gz", Size: int64(len(raw)),
		URL: "https://nodejs.org/dist/v22.23.2/node-v22.23.2-linux-x64.tar.gz", SHA256: hex.EncodeToString(hash[:]),
		ArchiveRoot: root, ArchivePath: "bin/node",
	}
}

func runInstalledCommand(home string) (string, []byte, error) {
	path := filepath.Join(home, ".local", "bin", commandName)
	output, err := exec.Command(path, "--version").CombinedOutput()
	return path, output, err
}
