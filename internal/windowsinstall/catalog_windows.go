//go:build windows

// Package windowsinstall provides the allowlisted Windows CLI installer.
package windowsinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const (
	maxArtifactBytes = 180_000_000
	maxExpandedBytes = 700_000_000
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.-]+)?$`)
)

type artifact struct {
	ID                 string
	Name               string
	Command            string
	Version            string
	Architecture       string
	URL                string
	SHA256             string
	DownloadBytes      int64
	ExpandedBytesLimit int64
	BinaryPath         string
	VersionArgs        []string
}

func builtInCatalog() (map[string]artifact, error) {
	items := []artifact{
		{
			ID: "claude-code", Name: "Claude Code", Command: "claude", Version: "2.1.232", Architecture: "amd64",
			URL:    "https://registry.npmjs.org/@anthropic-ai/claude-code-win32-x64/-/claude-code-win32-x64-2.1.232.tgz",
			SHA256: "e4c89e799cf0f258f6f138804d8f284e71129db0c38b054e9eab2e634d861008", DownloadBytes: 99_311_921,
			ExpandedBytesLimit: 320_000_000, BinaryPath: "package/claude.exe", VersionArgs: []string{"--version"},
		},
		{
			ID: "codex-cli", Name: "Codex CLI", Command: "codex", Version: "0.147.0", Architecture: "amd64",
			URL:    "https://registry.npmjs.org/@openai/codex/-/codex-0.147.0-win32-x64.tgz",
			SHA256: "299d8603750caaffc24f218789d989f77cf157070bd42451d352f5578a800766", DownloadBytes: 131_782_993,
			ExpandedBytesLimit: 600_000_000, BinaryPath: "package/vendor/x86_64-pc-windows-msvc/bin/codex.exe", VersionArgs: []string{"--version"},
		},
		{
			ID: "opencode-cli", Name: "OpenCode CLI", Command: "opencode", Version: "1.18.18", Architecture: "amd64",
			URL:    "https://registry.npmjs.org/opencode-windows-x64/-/opencode-windows-x64-1.18.18.tgz",
			SHA256: "2c89e0aafd029cebdd98b0dc3465b429a8649b4e1aa7460827d47ba969d36156", DownloadBytes: 59_951_631,
			ExpandedBytesLimit: 250_000_000, BinaryPath: "package/bin/opencode.exe", VersionArgs: []string{"--version"},
		},
	}
	catalog := make(map[string]artifact, len(items))
	for _, item := range items {
		if err := validateArtifact(item); err != nil {
			return nil, err
		}
		if _, exists := catalog[item.ID]; exists {
			return nil, errors.New("duplicate Windows artifact")
		}
		catalog[item.ID] = item
	}
	return catalog, nil
}

func validateArtifact(item artifact) error {
	if !identifierPattern.MatchString(item.ID) || !identifierPattern.MatchString(item.Command) ||
		!versionPattern.MatchString(item.Version) || item.Architecture != "amd64" {
		return errors.New("invalid Windows artifact identity")
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "registry.npmjs.org" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Windows artifact URL is not allowlisted")
	}
	if decoded, err := hex.DecodeString(item.SHA256); err != nil || len(decoded) != sha256.Size ||
		strings.ToLower(item.SHA256) != item.SHA256 ||
		item.DownloadBytes <= 0 || item.DownloadBytes > maxArtifactBytes ||
		item.ExpandedBytesLimit <= 0 || item.ExpandedBytesLimit > maxExpandedBytes {
		return errors.New("invalid Windows artifact bounds")
	}
	cleanBinary := path.Clean(item.BinaryPath)
	if cleanBinary != item.BinaryPath || !strings.HasPrefix(cleanBinary, "package/") ||
		!strings.HasSuffix(strings.ToLower(cleanBinary), ".exe") || strings.Contains(cleanBinary, `\`) {
		return errors.New("invalid Windows artifact binary path")
	}
	if len(item.VersionArgs) == 0 || len(item.VersionArgs) > 4 {
		return errors.New("invalid Windows artifact version arguments")
	}
	for _, argument := range item.VersionArgs {
		if argument == "" || len(argument) > 64 || strings.ContainsAny(argument, "\x00\r\n") {
			return errors.New("invalid Windows artifact version argument")
		}
	}
	return nil
}
