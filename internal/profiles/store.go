// Package profiles stores API configuration profiles encrypted at rest.
package profiles

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	storeVersion  = 1
	maxStoreBytes = 4 * 1024 * 1024
)

var (
	ErrInvalidProfile = errors.New("invalid API profile")
	ErrProfileMissing = errors.New("API profile missing")
	ErrUnsafeStorage  = errors.New("unsafe profile storage")
)

// Input is the secret-bearing payload accepted only by Save.
type Input struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	APIKey              string `json:"apiKey"`
	BaseURL             string `json:"baseUrl"`
	Model               string `json:"model"`
	AllowPrivateNetwork bool   `json:"allowPrivateNetwork"`
}

// Profile is safe for frontend display and never contains the API key.
type Profile struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	KeyHint             string    `json:"keyHint"`
	BaseURL             string    `json:"baseUrl"`
	Model               string    `json:"model"`
	AllowPrivateNetwork bool      `json:"allowPrivateNetwork"`
	Protection          string    `json:"protection"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type secretData struct {
	APIKey              string `json:"apiKey"`
	BaseURL             string `json:"baseUrl"`
	Model               string `json:"model"`
	AllowPrivateNetwork bool   `json:"allowPrivateNetwork"`
}

type encryptedRecord struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	KeyHint    string    `json:"keyHint"`
	Nonce      string    `json:"nonce"`
	Ciphertext string    `json:"ciphertext"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type storeFile struct {
	Version int               `json:"version"`
	Records []encryptedRecord `json:"records"`
}

// Store uses AES-256-GCM with a private local master key. Protection is
// intentionally reported to the UI as local-file rather than a keyring.
type Store struct {
	mu       sync.Mutex
	root     string
	keyPath  string
	dataPath string
	now      func() time.Time
	random   io.Reader
	key      []byte
}

func NewStore(home string) (*Store, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) == string(filepath.Separator) {
		return nil, ErrUnsafeStorage
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, ErrUnsafeStorage
	}
	root, err := ensurePrivateDirectories(resolved, ".local", "share", "osverse", "profiles")
	if err != nil {
		return nil, err
	}
	store := &Store{
		root: root, keyPath: filepath.Join(root, "master.key"),
		dataPath: filepath.Join(root, "profiles.json"), now: time.Now, random: rand.Reader,
	}
	store.key, err = loadOrCreateKey(store.keyPath, store.random)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func ensurePrivateDirectories(base string, components ...string) (string, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || filepath.Base(component) != component || component == "." || component == ".." {
			return "", ErrUnsafeStorage
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return "", ErrUnsafeStorage
		}
	}
	if err := os.Chmod(current, 0o700); err != nil {
		return "", err
	}
	return current, nil
}

func loadOrCreateKey(path string, random io.Reader) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() != 32 {
			return nil, ErrUnsafeStorage
		}
		key, err := os.ReadFile(path)
		if err != nil || len(key) != 32 {
			return nil, ErrUnsafeStorage
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return key, file.Close()
}

// Save validates and encrypts a new or existing profile.
func (store *Store) Save(ctx context.Context, input Input) (Profile, error) {
	if store == nil || len(store.key) != 32 {
		return Profile{}, ErrUnsafeStorage
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	validated, normalizedURL, err := validateInput(input)
	if err != nil {
		return Profile{}, err
	}
	validated.BaseURL = normalizedURL
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.readLocked()
	if err != nil {
		return Profile{}, err
	}
	now := store.now().UTC()
	index := -1
	created := now
	if validated.ID != "" {
		for candidate := range value.Records {
			if value.Records[candidate].ID == validated.ID {
				index = candidate
				created = value.Records[candidate].CreatedAt
				break
			}
		}
		if index < 0 {
			return Profile{}, ErrProfileMissing
		}
	} else {
		validated.ID, err = randomID(store.random)
		if err != nil {
			return Profile{}, err
		}
	}
	secret := secretData{
		APIKey: validated.APIKey, BaseURL: validated.BaseURL, Model: validated.Model,
		AllowPrivateNetwork: validated.AllowPrivateNetwork,
	}
	nonce, ciphertext, err := seal(store.key, validated.ID, secret, store.random)
	if err != nil {
		return Profile{}, err
	}
	record := encryptedRecord{
		ID: validated.ID, Name: validated.Name, KeyHint: keyHint(validated.APIKey),
		Nonce: nonce, Ciphertext: ciphertext, CreatedAt: created, UpdatedAt: now,
	}
	if index >= 0 {
		value.Records[index] = record
	} else {
		value.Records = append(value.Records, record)
	}
	if err := store.writeLocked(value); err != nil {
		return Profile{}, err
	}
	return publicProfile(record, secret), nil
}

// List decrypts only the non-key fields needed for display.
func (store *Store) List(ctx context.Context) ([]Profile, error) {
	if store == nil {
		return nil, ErrUnsafeStorage
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.readLocked()
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(value.Records))
	for _, record := range value.Records {
		secret, err := open(store.key, record)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, publicProfile(record, secret))
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].UpdatedAt.Equal(profiles[j].UpdatedAt) {
			return profiles[i].ID < profiles[j].ID
		}
		return profiles[i].UpdatedAt.After(profiles[j].UpdatedAt)
	})
	return profiles, nil
}

// Secret returns one decrypted profile for internal probes/adapters only.
func (store *Store) Secret(ctx context.Context, id string) (Input, error) {
	if store == nil || id == "" {
		return Input{}, ErrProfileMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Input{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.readLocked()
	if err != nil {
		return Input{}, err
	}
	for _, record := range value.Records {
		if record.ID != id {
			continue
		}
		secret, err := open(store.key, record)
		if err != nil {
			return Input{}, err
		}
		return Input{
			ID: id, Name: record.Name, APIKey: secret.APIKey, BaseURL: secret.BaseURL,
			Model: secret.Model, AllowPrivateNetwork: secret.AllowPrivateNetwork,
		}, nil
	}
	return Input{}, ErrProfileMissing
}

// Delete atomically removes one encrypted record.
func (store *Store) Delete(ctx context.Context, id string) error {
	if store == nil || id == "" {
		return ErrProfileMissing
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, err := store.readLocked()
	if err != nil {
		return err
	}
	found := false
	kept := value.Records[:0]
	for _, record := range value.Records {
		if record.ID == id {
			found = true
			continue
		}
		kept = append(kept, record)
	}
	if !found {
		return ErrProfileMissing
	}
	value.Records = kept
	return store.writeLocked(value)
}

func validateInput(input Input) (Input, string, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.APIKey = strings.TrimSpace(input.APIKey)
	input.Model = strings.TrimSpace(input.Model)
	if input.Name == "" || len(input.Name) > 80 || containsControl(input.Name) ||
		input.APIKey == "" || len(input.APIKey) > 16*1024 || containsControl(input.APIKey) ||
		input.Model == "" || len(input.Model) > 256 || containsControl(input.Model) {
		return Input{}, "", ErrInvalidProfile
	}
	normalized, err := normalizeBaseURL(input.BaseURL, input.AllowPrivateNetwork)
	if err != nil {
		return Input{}, "", err
	}
	return input, normalized, nil
}

func normalizeBaseURL(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || containsControl(raw) || strings.Contains(raw, "#") {
		return "", ErrInvalidProfile
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.Host == "" {
		return "", ErrInvalidProfile
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", ErrInvalidProfile
	}
	if parsed.Port() != "" {
		port, err := strconv.ParseUint(parsed.Port(), 10, 16)
		if err != nil || port == 0 {
			return "", ErrInvalidProfile
		}
	}
	loopbackHost := host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost && allowPrivate) {
		return "", ErrInvalidProfile
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	if parsed.Path == "" {
		parsed.Path = ""
	}
	return parsed.String(), nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func seal(key []byte, id string, secret secretData, random io.Reader) (string, string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", err
	}
	plaintext, err := json.Marshal(secret)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return "", "", err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(id))
	return base64.RawStdEncoding.EncodeToString(nonce), base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func open(key []byte, record encryptedRecord) (secretData, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return secretData{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return secretData{}, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(record.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return secretData{}, ErrUnsafeStorage
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(record.Ciphertext)
	if err != nil {
		return secretData{}, ErrUnsafeStorage
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(record.ID))
	if err != nil {
		return secretData{}, ErrUnsafeStorage
	}
	var secret secretData
	decoder := json.NewDecoder(strings.NewReader(string(plaintext)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secret); err != nil {
		return secretData{}, ErrUnsafeStorage
	}
	return secret, nil
}

func keyHint(key string) string {
	runes := []rune(key)
	if len(runes) <= 4 {
		return "••••" + string(runes)
	}
	return "••••" + string(runes[len(runes)-4:])
}

func publicProfile(record encryptedRecord, secret secretData) Profile {
	return Profile{
		ID: record.ID, Name: record.Name, KeyHint: record.KeyHint,
		BaseURL: secret.BaseURL, Model: secret.Model,
		AllowPrivateNetwork: secret.AllowPrivateNetwork, Protection: "local-file",
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func randomID(random io.Reader) (string, error) {
	value := make([]byte, 18)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (store *Store) readLocked() (storeFile, error) {
	info, err := os.Lstat(store.dataPath)
	if errors.Is(err, os.ErrNotExist) {
		return storeFile{Version: storeVersion, Records: []encryptedRecord{}}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxStoreBytes {
		return storeFile{}, ErrUnsafeStorage
	}
	raw, err := os.ReadFile(store.dataPath)
	if err != nil {
		return storeFile{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var value storeFile
	if err := decoder.Decode(&value); err != nil || value.Version != storeVersion || len(value.Records) > 1000 {
		return storeFile{}, ErrUnsafeStorage
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return storeFile{}, ErrUnsafeStorage
	}
	seen := make(map[string]struct{}, len(value.Records))
	for _, record := range value.Records {
		if record.ID == "" || record.Name == "" {
			return storeFile{}, ErrUnsafeStorage
		}
		if _, exists := seen[record.ID]; exists {
			return storeFile{}, ErrUnsafeStorage
		}
		seen[record.ID] = struct{}{}
	}
	return value, nil
}

func (store *Store) writeLocked(value storeFile) error {
	sort.Slice(value.Records, func(i, j int) bool { return value.Records[i].ID < value.Records[j].ID })
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(raw) > maxStoreBytes {
		return ErrUnsafeStorage
	}
	raw = append(raw, '\n')
	temporary := filepath.Join(store.root, fmt.Sprintf(".profiles-%d", store.now().UnixNano()))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = file.Close()
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, store.dataPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}
