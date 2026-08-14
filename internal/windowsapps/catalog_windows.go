//go:build windows

// Package windowsapps installs the fixed Windows desktop-tool catalog.
package windowsapps

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

type artifact struct {
	ID            string
	Name          string
	Version       string
	Kind          string
	URL           string
	SHA256        string
	DownloadBytes int64
	SilentArgs    []string
	StoreID       string
	ExpectedPaths []string
}

func builtInCatalog() (map[string]artifact, error) {
	items := []artifact{
		{ID: "claude-desktop", Name: "Claude Desktop", Version: "1.30096.1", Kind: "exe",
			URL:    "https://downloads.claude.ai/releases/win32/x64/1.30096.1/Claude-194d93c2558cfbfcd2b8b7a90e02774c489d1875.exe",
			SHA256: "a25593c94242a789e8f97dbc467f3be562c64845d52fa8d2b2ac91126e547fe3", DownloadBytes: 237_459_104,
			SilentArgs: []string{"--silent"}, ExpectedPaths: []string{`AppData\Local\Programs\Claude\Claude.exe`, `AppData\Local\AnthropicClaude\Claude.exe`}},
		{ID: "chatgpt-desktop", Name: "ChatGPT Desktop", Version: "Store latest", Kind: "store", StoreID: "9PLM9XGG6VKS"},
		{ID: "codex-desktop", Name: "Codex Desktop", Version: "Store latest", Kind: "store", StoreID: "9PLM9XGG6VKS"},
		{ID: "opencode-desktop", Name: "OpenCode Desktop", Version: "1.18.18", Kind: "exe",
			URL:    "https://github.com/anomalyco/opencode/releases/download/v1.18.18/opencode-desktop-win-x64.exe",
			SHA256: "f46c9420df889483d64fcb96637adfced89e9b3a1895fb6cc913caa0d6ee1962", DownloadBytes: 126_257_464,
			SilentArgs: []string{"/S"}, ExpectedPaths: []string{`AppData\Local\Programs\OpenCode\OpenCode.exe`, `AppData\Local\Programs\opencode\OpenCode.exe`}},
		{ID: "cc-switch", Name: "CC Switch", Version: "3.19.2", Kind: "msi",
			URL:    "https://github.com/farion1231/cc-switch/releases/download/v3.19.2/CC-Switch-v3.19.2-Windows.msi",
			SHA256: "60ae15c9230240283b7184c6d98624c0fdd26f7b52bde67160f923196813dac6", DownloadBytes: 13_082_624,
			SilentArgs: []string{"/qn", "/norestart", "ALLUSERS=2", "MSIINSTALLPERUSER=1"}, ExpectedPaths: []string{`AppData\Local\Programs\CC Switch\CC Switch.exe`, `AppData\Local\Programs\CC Switch\cc-switch.exe`}},
		{ID: "cockpit-tools", Name: "Cockpit Tools", Version: "1.3.17", Kind: "exe",
			URL:    "https://github.com/jlcodes99/cockpit-tools/releases/download/v1.3.17/Cockpit.Tools_1.3.17_x64-setup.exe",
			SHA256: "c6c8fbf236bd54d0e5caca73dea1678f54e45d76513839a3fb4324314411d7e2", DownloadBytes: 29_170_881,
			SilentArgs: []string{"/S"}, ExpectedPaths: []string{`AppData\Local\Programs\Cockpit Tools\Cockpit Tools.exe`, `AppData\Local\Programs\Cockpit Tools\cockpit-tools.exe`, `AppData\Local\Cockpit Tools\cockpit-tools.exe`}},
	}
	catalog := make(map[string]artifact, len(items))
	for _, item := range items {
		if err := validateArtifact(item); err != nil {
			return nil, err
		}
		if _, exists := catalog[item.ID]; exists {
			return nil, errors.New("duplicate Windows desktop artifact")
		}
		catalog[item.ID] = item
	}
	return catalog, nil
}

func validateArtifact(item artifact) error {
	if item.ID == "" || item.Name == "" || (item.Kind != "exe" && item.Kind != "msi" && item.Kind != "store") {
		return errors.New("invalid Windows desktop artifact identity")
	}
	if item.Kind == "store" {
		if len(item.StoreID) != 12 || item.URL != "" || item.DownloadBytes != 0 || item.SHA256 != "" {
			return errors.New("invalid Microsoft Store artifact")
		}
		for _, character := range item.StoreID {
			if (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
				return errors.New("invalid Microsoft Store product identifier")
			}
		}
		return nil
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Host != "github.com" && parsed.Host != "downloads.claude.ai") {
		return errors.New("desktop artifact URL is not allowlisted")
	}
	digest, err := hex.DecodeString(item.SHA256)
	if err != nil || len(digest) != sha256.Size || item.SHA256 != strings.ToLower(item.SHA256) ||
		item.DownloadBytes <= 0 || item.DownloadBytes > 300_000_000 || len(item.SilentArgs) == 0 || len(item.ExpectedPaths) == 0 {
		return errors.New("invalid desktop artifact bounds")
	}
	for _, path := range item.ExpectedPaths {
		if path == "" || strings.ContainsAny(path, "/:\x00\r\n") || !strings.HasSuffix(strings.ToLower(path), ".exe") {
			return errors.New("invalid expected desktop path")
		}
	}
	return nil
}
