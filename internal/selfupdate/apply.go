package selfupdate

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

type artifactApplier func(string, Artifact) (ApplyResult, error)

// Apply downloads the artifact bound to planID, verifies its exact length and
// SHA-256 digest, and then delegates to the current platform installer.
func (service *Service) Apply(ctx context.Context, planID string, protocol proxyservice.Protocol, port int) (ApplyResult, error) {
	if service == nil || planID == "" {
		return ApplyResult{}, ErrNoPlan
	}
	service.mu.Lock()
	service.prunePlansLocked()
	candidate, ok := service.plans[planID]
	if ok {
		delete(service.plans, planID)
	}
	service.mu.Unlock()
	if !ok {
		return ApplyResult{}, ErrNoPlan
	}
	client, err := service.secureClient(protocol, port)
	if err != nil {
		return ApplyResult{}, err
	}
	staged, err := service.download(ctx, client, candidate.artifact)
	if err != nil {
		return ApplyResult{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(staged)
		}
	}()
	result, err := service.applier(staged, candidate.artifact)
	if err != nil {
		return ApplyResult{}, err
	}
	// Installers and package managers consume the staged file after this method
	// returns. Atomic replacement strategies move/copy it themselves.
	keep = candidate.artifact.Format == "nsis" || candidate.artifact.Format == "deb" || candidate.artifact.Format == "dmg"
	return result, nil
}

func (service *Service) download(ctx context.Context, client *http.Client, artifact Artifact) (string, error) {
	root, err := updateDownloadRoot(service.home)
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "Osverse/"+service.currentRaw)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update download returned %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != artifact.Size {
		return "", errors.New("update artifact length mismatch")
	}
	suffix := filepath.Ext(artifact.Filename)
	file, err := os.CreateTemp(root, ".osverse-update-*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", err
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, artifact.Size+1))
	if err != nil {
		return "", err
	}
	if written != artifact.Size {
		return "", errors.New("update artifact length mismatch")
	}
	additional := make([]byte, 1)
	if count, readErr := response.Body.Read(additional); count != 0 || (readErr != nil && readErr != io.EOF) {
		return "", errors.New("update artifact exceeded declared length")
	}
	expected, _ := hex.DecodeString(artifact.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), expected) != 1 {
		return "", errors.New("update artifact checksum mismatch")
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	remove = false
	return path, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("update directory is unsafe")
	}
	return os.Chmod(path, 0o700)
}

func updateDownloadRoot(home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", errors.New("home directory is not absolute")
	}
	clean := filepath.Clean(home)
	if clean == filepath.VolumeName(clean)+string(os.PathSeparator) {
		return "", errors.New("home directory is unsafe")
	}
	return filepath.Join(append([]string{clean}, updatePathComponents()...)...), nil
}

func executableFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("update target is not a regular file")
	}
	return info, nil
}

func safeName(value string) bool {
	if value == "." || value == ".." {
		return false
	}
	return value != "" && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}
