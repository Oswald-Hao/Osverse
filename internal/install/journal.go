//go:build linux

package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	transactionJournalVersion = 1
	transactionJournalLimit   = 4 * 1024 * 1024
)

type journalLinkState struct {
	Exists bool   `json:"exists"`
	Target string `json:"target,omitempty"`
}

type journalProfileState struct {
	Name    string      `json:"name"`
	Exists  bool        `json:"exists"`
	Mode    os.FileMode `json:"mode"`
	Content []byte      `json:"content,omitempty"`
}

type transactionJournal struct {
	SchemaVersion int                   `json:"schemaVersion"`
	PlanID        string                `json:"planId"`
	ComponentID   string                `json:"componentId"`
	Current       journalLinkState      `json:"current"`
	Shim          journalLinkState      `json:"shim"`
	Profiles      []journalProfileState `json:"profiles"`
}

func (manager *Manager) writeTransactionJournal(
	root string,
	stored storedPlan,
	current, shim linkState,
	profiles []profileState,
) (string, error) {
	directory, err := ensureDirectories(root, 0o700, "state", "transactions")
	if err != nil {
		return "", err
	}
	journal := transactionJournal{
		SchemaVersion: transactionJournalVersion,
		PlanID:        stored.public.ID,
		ComponentID:   stored.artifact.ID,
		Current:       journalLinkState{Exists: current.exists, Target: current.target},
		Shim:          journalLinkState{Exists: shim.exists, Target: shim.target},
		Profiles:      make([]journalProfileState, 0, len(profiles)),
	}
	for _, state := range profiles {
		journal.Profiles = append(journal.Profiles, journalProfileState{
			Name: filepath.Base(state.path), Exists: state.exists,
			Mode: state.mode.Perm(), Content: append([]byte(nil), state.content...),
		})
	}
	if err := manager.validateTransactionJournal(journal); err != nil {
		return "", err
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		return "", err
	}
	if len(raw) > transactionJournalLimit {
		return "", errors.New("transaction journal exceeds size limit")
	}
	path := filepath.Join(directory, transactionJournalName(journal.PlanID))
	if err := atomicWriteJournal(path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func (manager *Manager) recoverTransactions() error {
	directory, exists, err := existingManagedDirectory(manager.home, ".local", "share", "osverse", "state", "transactions")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return errors.New("transaction journal entry is unsafe")
		}
		journalPath := filepath.Join(directory, entry.Name())
		journal, err := readTransactionJournal(journalPath)
		if err != nil {
			return err
		}
		if entry.Name() != transactionJournalName(journal.PlanID) {
			return errors.New("transaction journal identity mismatch")
		}
		if err := manager.validateTransactionJournal(journal); err != nil {
			return err
		}
		if err := manager.restoreTransaction(journal); err != nil {
			return err
		}
		if err := removeJournal(journalPath); err != nil {
			return err
		}
	}
	return nil
}

func (manager *Manager) validateTransactionJournal(journal transactionJournal) error {
	item, ok := manager.catalog[journal.ComponentID]
	if journal.SchemaVersion != transactionJournalVersion || journal.PlanID == "" || !ok {
		return errors.New("invalid transaction journal metadata")
	}
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	binRoot := filepath.Join(manager.home, ".local", "bin")
	for _, value := range []struct {
		state journalLinkState
		base  string
	}{
		{state: journal.Current, base: toolRoot},
		{state: journal.Shim, base: binRoot},
	} {
		state := value.state
		if !state.Exists && state.Target != "" {
			return errors.New("invalid transaction link state")
		}
		if state.Exists {
			resolved := state.Target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(value.base, resolved)
			}
			if state.Target == "" || !pathWithin(toolRoot, filepath.Clean(resolved)) {
				return errors.New("transaction link target escapes managed root")
			}
		}
	}
	allowedProfiles := map[string]struct{}{
		".profile": {}, ".bashrc": {}, ".zprofile": {}, ".zshrc": {},
	}
	seen := make(map[string]struct{}, len(journal.Profiles))
	for _, state := range journal.Profiles {
		if _, ok := allowedProfiles[state.Name]; !ok || state.Name != filepath.Base(state.Name) {
			return errors.New("transaction profile is not allowlisted")
		}
		if _, duplicate := seen[state.Name]; duplicate {
			return errors.New("duplicate transaction profile")
		}
		seen[state.Name] = struct{}{}
		if state.Mode.Perm() != state.Mode || len(state.Content) > profileLimit || (!state.Exists && len(state.Content) != 0) {
			return errors.New("invalid transaction profile state")
		}
	}
	return nil
}

func (manager *Manager) restoreTransaction(journal transactionJournal) error {
	item := manager.catalog[journal.ComponentID]
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	for index := len(journal.Profiles) - 1; index >= 0; index-- {
		stored := journal.Profiles[index]
		if err := restoreProfile(profileState{
			path: filepath.Join(manager.home, stored.Name), exists: stored.Exists,
			mode: stored.Mode, content: append([]byte(nil), stored.Content...), changed: true,
		}); err != nil {
			return fmt.Errorf("restore profile: %w", err)
		}
	}
	shimPath := filepath.Join(manager.home, ".local", "bin", item.Command)
	if err := restoreLink(shimPath, linkState{exists: journal.Shim.Exists, target: journal.Shim.Target}); err != nil {
		return fmt.Errorf("restore shim: %w", err)
	}
	currentPath := filepath.Join(toolRoot, "current")
	if err := restoreLink(currentPath, linkState{exists: journal.Current.Exists, target: journal.Current.Target}); err != nil {
		return fmt.Errorf("restore current version: %w", err)
	}
	return nil
}

func readTransactionJournal(path string) (transactionJournal, error) {
	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return transactionJournal{}, errors.New("transaction journal file is unsafe")
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return transactionJournal{}, errors.New("transaction journal file is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > transactionJournalLimit {
		return transactionJournal{}, errors.New("transaction journal file is unsafe")
	}
	raw, err := io.ReadAll(io.LimitReader(file, transactionJournalLimit+1))
	if err != nil {
		return transactionJournal{}, err
	}
	if len(raw) > transactionJournalLimit {
		return transactionJournal{}, errors.New("transaction journal exceeds size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var journal transactionJournal
	if err := decoder.Decode(&journal); err != nil {
		return transactionJournal{}, fmt.Errorf("decode transaction journal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return transactionJournal{}, errors.New("transaction journal contains trailing data")
	}
	return journal, nil
}

func existingManagedDirectory(base string, components ...string) (string, bool, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return "", false, errors.New("invalid managed path component")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return current, false, nil
		}
		if err != nil {
			return "", false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", false, errors.New("transaction journal directory is unsafe")
		}
	}
	return current, true, nil
}

func transactionJournalName(planID string) string {
	digest := sha256.Sum256([]byte(planID))
	return hex.EncodeToString(digest[:]) + ".json"
}

func atomicWriteJournal(path string, raw []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".journal-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	cleanup := true
	defer func() {
		_ = temporary.Close()
		if cleanup {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	cleanup = false
	return syncDirectory(directory)
}

func removeJournal(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
