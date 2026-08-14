// Package history persists a bounded, redacted local operation ledger.
package history

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxEntries   = 200
	maxFileBytes = 1024 * 1024
)

var (
	ErrInvalidEntry = errors.New("invalid history entry")
	ErrUnsafeStore  = errors.New("unsafe history store")
	identifier      = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
)

type Input struct {
	OperationID string
	ComponentID string
	Name        string
	Action      string
	Status      string
	Message     string
}

type Entry struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operationId"`
	ComponentID string    `json:"componentId"`
	Name        string    `json:"name"`
	Action      string    `json:"action"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
	CreatedAt   time.Time `json:"createdAt"`
}

type document struct {
	SchemaVersion int     `json:"schemaVersion"`
	Entries       []Entry `json:"entries"`
}

type Store struct {
	mu       sync.Mutex
	path     string
	now      func() time.Time
	randomID func() (string, error)
}

func NewStore(home string) (*Store, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) == string(filepath.Separator) {
		return nil, ErrUnsafeStore
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, ErrUnsafeStore
	}
	state, err := ensureStateDirectory(resolved)
	if err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(state, "history.json"), now: time.Now, randomID: secureID}, nil
}

func ensureStateDirectory(home string) (string, error) {
	current := home
	for _, part := range []string{".local", "share", "osverse", "state"} {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", ErrUnsafeStore
		}
	}
	if err := os.Chmod(current, 0o700); err != nil {
		return "", err
	}
	return current, nil
}

func secureID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (store *Store) Append(ctx context.Context, input Input) (Entry, error) {
	if store == nil || store.now == nil || store.randomID == nil || !validInput(input) {
		return Entry{}, ErrInvalidEntry
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	doc, err := store.readLocked()
	if err != nil {
		return Entry{}, err
	}
	for _, existing := range doc.Entries {
		if input.OperationID != "" && existing.OperationID == input.OperationID {
			return existing, nil
		}
	}
	id, err := store.randomID()
	if err != nil || id == "" {
		return Entry{}, ErrInvalidEntry
	}
	entry := Entry{ID: id, OperationID: input.OperationID, ComponentID: input.ComponentID, Name: input.Name, Action: input.Action, Status: input.Status, Message: input.Message, CreatedAt: store.now().UTC()}
	doc.Entries = append([]Entry{entry}, doc.Entries...)
	if len(doc.Entries) > maxEntries {
		doc.Entries = doc.Entries[:maxEntries]
	}
	if err := store.writeLocked(doc); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func validInput(input Input) bool {
	if !identifier.MatchString(input.ComponentID) || !identifier.MatchString(input.Action) ||
		(input.Status != "completed" && input.Status != "failed" && input.Status != "canceled") {
		return false
	}
	for _, value := range []string{input.OperationID, input.Name, input.Message} {
		if len(value) > 240 || strings.ContainsAny(value, "\x00\r\n") {
			return false
		}
	}
	return input.Name != "" && input.Message != ""
}

func (store *Store) List(ctx context.Context) ([]Entry, error) {
	if store == nil {
		return nil, ErrUnsafeStore
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	doc, err := store.readLocked()
	if err != nil {
		return nil, err
	}
	return append([]Entry(nil), doc.Entries...), nil
}

func (store *Store) Clear(ctx context.Context) error {
	if store == nil {
		return ErrUnsafeStore
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	info, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafeStore
	}
	return os.Remove(store.path)
}

func (store *Store) readLocked() (document, error) {
	info, err := os.Lstat(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return document{SchemaVersion: 1, Entries: []Entry{}}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxFileBytes {
		return document{}, ErrUnsafeStore
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		return document{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value document
	if err := decoder.Decode(&value); err != nil {
		return document{}, ErrUnsafeStore
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return document{}, ErrUnsafeStore
	}
	if value.SchemaVersion != 1 || len(value.Entries) > maxEntries {
		return document{}, ErrUnsafeStore
	}
	for _, entry := range value.Entries {
		if entry.ID == "" || entry.CreatedAt.IsZero() || !validInput(Input{OperationID: entry.OperationID, ComponentID: entry.ComponentID, Name: entry.Name, Action: entry.Action, Status: entry.Status, Message: entry.Message}) {
			return document{}, ErrUnsafeStore
		}
	}
	return value, nil
}

func (store *Store) writeLocked(value document) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxFileBytes {
		return ErrUnsafeStore
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".history-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if info, err := os.Lstat(store.path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return ErrUnsafeStore
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(name, store.path)
}
