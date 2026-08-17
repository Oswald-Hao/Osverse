//go:build windows

package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsStoreUsesDPAPIAndSupportsAtomicUpdates(t *testing.T) {
	home := resolvedTestHome(t)
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(home, "AppData", "Local", "Osverse", "profiles")
	if store.root != wantRoot || store.keyPath != filepath.Join(wantRoot, "master.key") {
		t.Fatalf("store paths = %q, %q", store.root, store.keyPath)
	}
	protected, err := os.ReadFile(store.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(protected) == 0 || bytes.Equal(protected, store.key) || bytes.Contains(protected, store.key) {
		t.Fatal("master key was not protected with DPAPI")
	}

	created, err := store.Save(context.Background(), Input{
		Name: "Windows", APIKey: "secret-key-one", BaseURL: "https://api.example/v1", Model: "model-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Protection != "windows-dpapi" {
		t.Fatalf("protection = %q", created.Protection)
	}
	updated, err := store.Save(context.Background(), Input{
		ID: created.ID, Name: "Windows updated", APIKey: "secret-key-two",
		BaseURL: "https://api.example/v1", Model: "model-two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Model != "model-two" {
		t.Fatalf("updated profile = %#v", updated)
	}

	reopened, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reopened.key, store.key) {
		t.Fatal("DPAPI master key did not round trip for the current user")
	}
	secret, err := reopened.Secret(context.Background(), created.ID)
	if err != nil || secret.APIKey != "secret-key-two" {
		t.Fatalf("reopened secret = (%#v, %v)", secret, err)
	}
}

func TestWindowsStoreRejectsTamperedDPAPIKey(t *testing.T) {
	home := resolvedTestHome(t)
	store, err := NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := os.ReadFile(store.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	protected[len(protected)/2] ^= 0xff
	if err := os.WriteFile(store.keyPath, protected, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(home); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("tampered DPAPI key error = %v", err)
	}
}

func TestWindowsAdaptersUseLocalAppDataBackupsAndReplaceConfigs(t *testing.T) {
	home := resolvedTestHome(t)
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	wantBackupRoot := filepath.Join(home, "AppData", "Local", "Osverse", "profiles", "backups")
	if adapters.backupRoot != wantBackupRoot {
		t.Fatalf("backup root = %q", adapters.backupRoot)
	}
	path, err := adapters.TargetPath(TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"system"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first := adapterInput()
	if _, err := adapters.Apply(context.Background(), TargetOpenCode, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Model = "replacement-model"
	result, err := adapters.Apply(context.Background(), TargetOpenCode, second)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.BackupPath) != wantBackupRoot {
		t.Fatalf("backup path = %q", result.BackupPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document["model"] != "osverse/replacement-model" || document["theme"] != "system" {
		t.Fatalf("updated OpenCode config = %#v", document)
	}
}
