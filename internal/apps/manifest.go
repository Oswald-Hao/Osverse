// Package apps installs and launches the fixed Linux desktop-tool catalog.
package apps

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type artifact struct {
	ID            string
	Name          string
	Command       string
	DesktopFile   string
	Version       string
	Architecture  string
	URL           string
	SHA256        string
	DownloadBytes int64
}

var (
	identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	version    = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){2,3}$`)
)

func builtInCatalog() (map[string]artifact, error) {
	items := []artifact{
		{
			ID: "opencode-desktop", Name: "OpenCode Desktop", Command: "opencode-desktop",
			DesktopFile: "opencode-desktop.desktop", Version: "1.18.18", Architecture: "amd64",
			URL:    "https://github.com/anomalyco/opencode/releases/download/v1.18.18/opencode-desktop-linux-x86_64.AppImage",
			SHA256: "ed493bdbeaec8c5942df32938dc20eb83ef7929ac6a94516667bb51c0217797c", DownloadBytes: 158804382,
		},
		{
			ID: "cc-switch", Name: "CC Switch", Command: "cc-switch",
			DesktopFile: "cc-switch.desktop", Version: "3.19.2", Architecture: "amd64",
			URL:    "https://github.com/farion1231/cc-switch/releases/download/v3.19.2/CC-Switch-v3.19.2-Linux-x86_64.AppImage",
			SHA256: "de19d047df983fa6f05d6faddfbf0b387ddff5a575c9e80e0a37c9f9737f1175", DownloadBytes: 91851256,
		},
		{
			ID: "cockpit-tools", Name: "Cockpit Tools", Command: "cockpit-tools",
			DesktopFile: "cockpit-tools.desktop", Version: "1.3.17", Architecture: "amd64",
			URL:    "https://github.com/jlcodes99/cockpit-tools/releases/download/v1.3.17/Cockpit.Tools_1.3.17_amd64.AppImage",
			SHA256: "eccf0b1a3680a05c6d1ec2a31aa720ff2cc045af5b297111761711815edba471", DownloadBytes: 117549560,
		},
	}
	catalog := make(map[string]artifact, len(items))
	for _, item := range items {
		if err := validateArtifact(item); err != nil {
			return nil, err
		}
		if _, exists := catalog[item.ID]; exists {
			return nil, errors.New("duplicate desktop artifact")
		}
		catalog[item.ID] = item
	}
	return catalog, nil
}

// LatestVersion exposes immutable catalog metadata for update detection.
func LatestVersion(componentID string) (string, bool) {
	catalog, err := builtInCatalog()
	if err != nil {
		return "", false
	}
	item, ok := catalog[componentID]
	return item.Version, ok
}

func validateArtifact(item artifact) error {
	if !identifier.MatchString(item.ID) || !identifier.MatchString(item.Command) ||
		item.DesktopFile != item.ID+".desktop" || !version.MatchString(item.Version) ||
		item.Architecture != "amd64" || item.DownloadBytes <= 0 || item.DownloadBytes > 250_000_000 ||
		len(item.SHA256) != 64 || strings.Trim(item.SHA256, "0123456789abcdef") != "" {
		return errors.New("invalid desktop artifact metadata")
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "github.com" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !strings.HasPrefix(parsed.EscapedPath(), "/") {
		return errors.New("invalid desktop artifact URL")
	}
	return nil
}
