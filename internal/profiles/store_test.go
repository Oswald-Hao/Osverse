package profiles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreEncryptsSecretsAndReturnsOnlyRedactedProfiles(t *testing.T) {
	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 14, 1, 2, 3, 0, time.UTC)
	store.now = func() time.Time { return now }
	secretKey := "sk-super-secret-1234"
	profile, err := store.Save(context.Background(), Input{
		Name: "工作网关", APIKey: secretKey, BaseURL: "https://API.Example.com/v1/",
		Model: "gpt-5.2-codex",
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID == "" || profile.KeyHint != "••••1234" || profile.BaseURL != "https://api.example.com/v1" ||
		profile.Model != "gpt-5.2-codex" || profile.Protection != "local-file" {
		t.Fatalf("public profile = %#v", profile)
	}
	raw, err := os.ReadFile(store.dataPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{secretKey, "https://api.example.com", "gpt-5.2-codex"} {
		if bytes.Contains(raw, []byte(plaintext)) {
			t.Fatalf("encrypted store contains plaintext %q", plaintext)
		}
	}
	keyInfo, _ := os.Stat(store.keyPath)
	dataInfo, _ := os.Stat(store.dataPath)
	if keyInfo.Mode().Perm() != 0o600 || dataInfo.Mode().Perm() != 0o600 {
		t.Fatalf("storage modes = key %o data %o", keyInfo.Mode().Perm(), dataInfo.Mode().Perm())
	}

	listed, err := store.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0] != profile {
		t.Fatalf("List() = (%#v, %v)", listed, err)
	}
	secret, err := store.Secret(context.Background(), profile.ID)
	if err != nil || secret.APIKey != secretKey || secret.BaseURL != profile.BaseURL {
		t.Fatalf("Secret() = (%#v, %v)", secret, err)
	}
}

func TestStoreUpdatesAndDeletesAtomically(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	firstTime := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return firstTime }
	created, err := store.Save(context.Background(), Input{
		Name: "First", APIKey: "secret-one", BaseURL: "https://one.example/v1", Model: "model-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return firstTime.Add(time.Hour) }
	updated, err := store.Save(context.Background(), Input{
		ID: created.ID, Name: "Updated", APIKey: "secret-two-9999",
		BaseURL: "https://two.example/v1", Model: "model-two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.CreatedAt != created.CreatedAt || !updated.UpdatedAt.After(created.UpdatedAt) || updated.KeyHint != "••••9999" {
		t.Fatalf("updated profile = %#v, created %#v", updated, created)
	}
	if err := store.Delete(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(context.Background())
	if err != nil || len(listed) != 0 {
		t.Fatalf("List() after delete = (%#v, %v)", listed, err)
	}
	if _, err := store.Secret(context.Background(), created.ID); !errors.Is(err, ErrProfileMissing) {
		t.Fatalf("Secret() deleted error = %v", err)
	}
}

func TestStoreDetectsCiphertextAndKeyTampering(t *testing.T) {
	home := t.TempDir()
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.Save(context.Background(), Input{
		Name: "Profile", APIKey: "secret-value", BaseURL: "https://api.example/v1", Model: "model",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(store.dataPath)
	index := bytes.Index(raw, []byte(`"ciphertext": "`))
	if index < 0 {
		t.Fatal("ciphertext field missing")
	}
	position := index + len(`"ciphertext": "`)
	if raw[position] == 'A' {
		raw[position] = 'B'
	} else {
		raw[position] = 'A'
	}
	if err := os.WriteFile(store.dataPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Secret(context.Background(), profile.ID); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("tampered ciphertext error = %v", err)
	}

	if err := os.WriteFile(store.keyPath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(home); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("tampered key error = %v", err)
	}
}

func TestProfileValidationRejectsUnsafeURLsAndControlCharacters(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowPrivate bool
		valid        bool
	}{
		{name: "HTTPS", url: "https://api.example.com/v1", valid: true},
		{name: "localhost confirmed", url: "http://127.0.0.1:8080/v1", allowPrivate: true, valid: true},
		{name: "localhost not confirmed", url: "http://127.0.0.1:8080/v1"},
		{name: "remote HTTP", url: "http://api.example.com/v1", allowPrivate: true},
		{name: "userinfo", url: "https://user:pass@api.example.com/v1"},
		{name: "query", url: "https://api.example.com/v1?key=secret"},
		{name: "fragment", url: "https://api.example.com/v1#fragment"},
		{name: "bad port", url: "https://api.example.com:0/v1"},
		{name: "control", url: "https://api.example.com/v1\nvalue"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeBaseURL(test.url, test.allowPrivate)
			if test.valid && err != nil {
				t.Fatalf("valid URL error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidProfile) {
				t.Fatalf("unsafe URL error = %v", err)
			}
		})
	}
	if _, _, err := validateInput(Input{
		Name: "bad\nname", APIKey: "secret", BaseURL: "https://api.example/v1", Model: "model",
	}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("control name error = %v", err)
	}
}

func TestStoreRejectsSymlinkedStorageComponents(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "share"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, ".local", "share", "osverse")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(home); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("symlink storage error = %v", err)
	}
}

func TestCanceledProfileOperationsDoNotReadOrWrite(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Save(ctx, Input{Name: "n", APIKey: "key", BaseURL: "https://api.example", Model: "m"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save canceled error = %v", err)
	}
	if _, err := os.Stat(store.dataPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled save created data file: %v", err)
	}
}

func TestShortKeysAreStillMasked(t *testing.T) {
	for _, key := range []string{"a", "abcd"} {
		if hint := keyHint(key); !strings.HasPrefix(hint, "••••") || strings.TrimPrefix(hint, "••••") != key {
			t.Fatalf("keyHint(%q) = %q", key, hint)
		}
	}
}
