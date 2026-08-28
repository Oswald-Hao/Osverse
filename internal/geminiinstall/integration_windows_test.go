//go:build windows

package geminiinstall

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func init() {
	if os.Getenv("OSVERSE_GEMINI_FAKE_NODE") == "1" {
		fmt.Println(geminiVersion)
		os.Exit(0)
	}
}

func integrationRuntimeArchive(t *testing.T) ([]byte, runtimeArtifact) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "node.zip")
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	entry, err := writer.Create("node-v22.23.2-win-x64/node.exe")
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(entry, input); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	t.Setenv("OSVERSE_GEMINI_FAKE_NODE", "1")
	return raw, runtimeArtifact{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Format: "zip", Size: int64(len(raw)),
		URL: "https://nodejs.org/dist/v22.23.2/node-v22.23.2-win-x64.zip", SHA256: hex.EncodeToString(hash[:]),
		ArchiveRoot: "node-v22.23.2-win-x64", ArchivePath: "node.exe",
	}
}

func runInstalledCommand(home string) (string, []byte, error) {
	path := filepath.Join(home, ".local", "bin", commandName+".cmd")
	shell := os.Getenv("ComSpec")
	if !filepath.IsAbs(shell) || strings.ContainsAny(shell+path, "\x00\r\n&|<>^%!") {
		return path, nil, fmt.Errorf("unsafe Windows integration command")
	}
	// Supplying a complete command line avoids os/exec's generic Windows
	// argument quoting, which is not cmd.exe syntax and can turn the quotes
	// around a batch path into literal filename characters. This matches the
	// raw CreateProcess command line used by the production launcher.
	command := exec.Command(shell)
	command.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + shell + `" /d /c call "` + path + `" --version`,
	}
	output, err := command.CombinedOutput()
	return path, output, err
}
