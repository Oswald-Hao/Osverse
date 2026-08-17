package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const maxSelectionBytes = 1024

// Selection is the only persisted network preference. It contains no
// credentials and can address only a loopback port through a fixed protocol.
type Selection struct {
	Protocol Protocol `json:"protocol"`
	Port     int      `json:"port"`
}

func (selection Selection) valid() bool {
	if selection.Port < 1 || selection.Port > 65535 {
		return false
	}
	switch selection.Protocol {
	case ProtocolHTTP, ProtocolHTTPSConnect, ProtocolSOCKS5:
		return true
	default:
		return false
	}
}

// SelectionStore persists a validated current-user proxy preference without
// reading environment or system proxy settings.
type SelectionStore struct {
	home       string
	components []string
	directory  string
	path       string
}

func NewSelectionStore(home string) (*SelectionStore, error) {
	return newSelectionStore(home, runtime.GOOS)
}

func newSelectionStore(home, goos string) (*SelectionStore, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) {
		return nil, errors.New("invalid proxy selection home")
	}
	resolved = filepath.Clean(resolved)
	if resolved == filepath.VolumeName(resolved)+string(filepath.Separator) {
		return nil, errors.New("invalid proxy selection home")
	}
	components := []string{".config", "osverse"}
	switch goos {
	case "windows":
		components = []string{"AppData", "Local", "Osverse"}
	case "darwin":
		components = []string{"Library", "Application Support", "Osverse"}
	}
	directory := filepath.Join(append([]string{resolved}, components...)...)
	return &SelectionStore{
		home: resolved, components: components, directory: directory,
		path: filepath.Join(directory, "network.json"),
	}, nil
}

func (store *SelectionStore) Load() (Selection, error) {
	if store == nil {
		return Selection{}, errors.New("proxy selection store unavailable")
	}
	exists, err := store.ensureDirectory(false)
	if err != nil || !exists {
		return Selection{}, err
	}
	pathInfo, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return Selection{}, nil
	}
	if err != nil || !pathInfo.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || pathInfo.Size() > maxSelectionBytes {
		return Selection{}, errors.New("unsafe proxy selection file")
	}
	file, err := os.Open(store.path)
	if err != nil {
		return Selection{}, err
	}
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(pathInfo, openedInfo) {
		_ = file.Close()
		return Selection{}, errors.New("proxy selection identity changed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxSelectionBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) > maxSelectionBytes {
		return Selection{}, errors.New("read proxy selection")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var selection Selection
	if err := decoder.Decode(&selection); err != nil {
		return Selection{}, errors.New("invalid proxy selection")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || !selection.valid() {
		return Selection{}, errors.New("invalid proxy selection")
	}
	return selection, nil
}

func (store *SelectionStore) Save(selection Selection) error {
	if store == nil || !selection.valid() {
		return errors.New("invalid proxy selection")
	}
	if _, err := store.ensureDirectory(true); err != nil {
		return err
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := os.CreateTemp(store.directory, ".network-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replaceSelectionFile(temporary, store.path)
}

func (store *SelectionStore) Clear() error {
	if store == nil {
		return errors.New("proxy selection store unavailable")
	}
	exists, err := store.ensureDirectory(false)
	if err != nil || !exists {
		return err
	}
	info, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe proxy selection file")
	}
	return os.Remove(store.path)
}

func (store *SelectionStore) ensureDirectory(create bool) (bool, error) {
	current := store.home
	for _, component := range store.components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if !create {
				return false, nil
			}
			if err := os.Mkdir(current, 0o700); err != nil {
				return false, err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("unsafe proxy selection directory")
		}
	}
	return true, nil
}
