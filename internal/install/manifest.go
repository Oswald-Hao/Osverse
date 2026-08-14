//go:build linux

// Package install provides the allowlisted, transactional CLI installer.
package install

import (
	"bytes"
	"crypto/ed25519"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	manifestSchemaVersion = 1
	maxArtifactBytes      = 160_000_000
	maxExpandedBytes      = 650_000_000
)

var (
	//go:embed manifest.json
	manifestFiles     embed.FS
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}(?:-[0-9A-Za-z.-]+)?$`)
)

type manifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	Sequence      uint64     `json:"sequence"`
	Components    []artifact `json:"components"`
}

type artifact struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Command            string   `json:"command"`
	Version            string   `json:"version"`
	Architecture       string   `json:"architecture"`
	URL                string   `json:"url"`
	SHA256             string   `json:"sha256"`
	DownloadBytes      int64    `json:"downloadBytes"`
	ExpandedBytesLimit int64    `json:"expandedBytesLimit"`
	BinaryPath         string   `json:"binaryPath"`
	VersionArgs        []string `json:"versionArgs"`
}

func builtInManifest() (manifest, error) {
	raw, err := manifestFiles.ReadFile("manifest.json")
	if err != nil {
		return manifest{}, err
	}
	return decodeManifest(raw)
}

// verifySignedManifest verifies the exact received bytes before strict JSON
// parsing. It is retained for a future remote catalog; production currently
// uses only the compile-time trusted manifest above.
func verifySignedManifest(raw, signature []byte, publicKey ed25519.PublicKey) (manifest, error) {
	if len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return manifest{}, errors.New("invalid manifest signature material")
	}
	if !ed25519.Verify(publicKey, raw, signature) {
		return manifest{}, errors.New("manifest signature verification failed")
	}
	return decodeManifest(raw)
}

func decodeManifest(raw []byte) (manifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value manifest
	if err := decoder.Decode(&value); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return manifest{}, err
	}
	if err := validateManifest(value); err != nil {
		return manifest{}, err
	}
	return value, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("manifest contains trailing JSON")
		}
		return fmt.Errorf("manifest trailing data: %w", err)
	}
	return nil
}

func validateManifest(value manifest) error {
	if value.SchemaVersion != manifestSchemaVersion || value.Sequence == 0 {
		return errors.New("unsupported manifest metadata")
	}
	if len(value.Components) == 0 || len(value.Components) > 32 {
		return errors.New("invalid manifest component count")
	}
	seen := make(map[string]struct{}, len(value.Components))
	for _, item := range value.Components {
		if err := validateArtifact(item); err != nil {
			return fmt.Errorf("component %q: %w", item.ID, err)
		}
		if _, exists := seen[item.ID]; exists {
			return fmt.Errorf("duplicate component %q", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	return nil
}

func validateArtifact(item artifact) error {
	if !identifierPattern.MatchString(item.ID) || !identifierPattern.MatchString(item.Command) {
		return errors.New("invalid identifier")
	}
	if strings.TrimSpace(item.Name) == "" || len(item.Name) > 100 {
		return errors.New("invalid display name")
	}
	if !versionPattern.MatchString(item.Version) || item.Architecture != "amd64" {
		return errors.New("invalid version or architecture")
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "registry.npmjs.org" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path == "" {
		return errors.New("artifact URL is not allowlisted")
	}
	if _, err := hex.DecodeString(item.SHA256); err != nil || len(item.SHA256) != 64 || item.SHA256 != strings.ToLower(item.SHA256) {
		return errors.New("invalid artifact hash")
	}
	if item.DownloadBytes <= 0 || item.DownloadBytes > maxArtifactBytes ||
		item.ExpandedBytesLimit <= 0 || item.ExpandedBytesLimit > maxExpandedBytes {
		return errors.New("invalid artifact size limits")
	}
	cleanBinary := path.Clean(item.BinaryPath)
	if cleanBinary != item.BinaryPath || !strings.HasPrefix(cleanBinary, "package/") || strings.Contains(cleanBinary, `\`) {
		return errors.New("invalid archive binary path")
	}
	if len(item.VersionArgs) == 0 || len(item.VersionArgs) > 4 {
		return errors.New("invalid version arguments")
	}
	for _, argument := range item.VersionArgs {
		if argument == "" || len(argument) > 64 || strings.ContainsRune(argument, 0) {
			return errors.New("invalid version argument")
		}
	}
	return nil
}

func artifactCatalog(value manifest) map[string]artifact {
	catalog := make(map[string]artifact, len(value.Components))
	for _, item := range value.Components {
		item.VersionArgs = append([]string(nil), item.VersionArgs...)
		catalog[item.ID] = item
	}
	return catalog
}

func sortedArtifacts(catalog map[string]artifact) []artifact {
	items := make([]artifact, 0, len(catalog))
	for _, item := range catalog {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
