package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeAdapterPreservesUnrelatedJSONAndCreatesPrivateBackup(t *testing.T) {
	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	adapters.now = func() time.Time { return time.Date(2026, time.August, 14, 1, 2, 3, 4, time.UTC) }
	path, _ := adapters.TargetPath(TargetClaude)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"permissions":{"allow":["Read"]},"env":{"KEEP":"yes"}}`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := adapters.Apply(context.Background(), TargetClaude, adapterInput())
	if err != nil || !result.Applied || result.Path != path || result.BackupPath == "" {
		t.Fatalf("Apply() = (%#v, %v)", result, err)
	}
	root := readJSONMap(t, path)
	permissions := root["permissions"].(map[string]any)
	if len(permissions["allow"].([]any)) != 1 {
		t.Fatalf("unrelated permissions changed: %#v", permissions)
	}
	environment := root["env"].(map[string]any)
	if environment["KEEP"] != "yes" || environment["ANTHROPIC_AUTH_TOKEN"] != "secret-key-1234" ||
		environment["ANTHROPIC_BASE_URL"] != "https://api.example/v1" || environment["ANTHROPIC_MODEL"] != "model-name" {
		t.Fatalf("Claude environment = %#v", environment)
	}
	backup, _ := os.ReadFile(result.BackupPath)
	backupInfo, _ := os.Stat(result.BackupPath)
	configInfo, _ := os.Stat(path)
	if string(backup) != string(original) || backupInfo.Mode().Perm() != 0o600 || configInfo.Mode().Perm() != 0o600 {
		t.Fatalf("backup/config modes = %o/%o backup %q", backupInfo.Mode().Perm(), configInfo.Mode().Perm(), backup)
	}
}

func TestOpenCodeAdapterMergesOsverseProviderWithoutReplacingOthers(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	path, _ := adapters.TargetPath(TargetOpenCode)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"system","provider":{"existing":{"name":"Keep"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := adapters.Apply(context.Background(), TargetOpenCode, adapterInput())
	if err != nil {
		t.Fatal(err)
	}
	root := readJSONMap(t, path)
	if root["theme"] != "system" || root["model"] != "osverse/model-name" {
		t.Fatalf("OpenCode root = %#v", root)
	}
	providers := root["provider"].(map[string]any)
	if providers["existing"].(map[string]any)["name"] != "Keep" {
		t.Fatal("existing provider was replaced")
	}
	osverse := providers["osverse"].(map[string]any)
	if osverse["npm"] != "@ai-sdk/openai-compatible" {
		t.Fatalf("OpenCode provider package = %#v", osverse["npm"])
	}
	options := osverse["options"].(map[string]any)
	if options["apiKey"] != "secret-key-1234" || options["baseURL"] != "https://api.example/v1" {
		t.Fatalf("Osverse provider options = %#v", options)
	}
}

func TestOpenAIAdaptersAppendV1ToAnUnversionedBaseURL(t *testing.T) {
	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.BaseURL = "https://api.example/gateway"

	if _, err := adapters.Apply(context.Background(), TargetOpenCode, input); err != nil {
		t.Fatal(err)
	}
	openCodePath, _ := adapters.TargetPath(TargetOpenCode)
	root := readJSONMap(t, openCodePath)
	osverse := root["provider"].(map[string]any)["osverse"].(map[string]any)
	options := osverse["options"].(map[string]any)
	if options["baseURL"] != "https://api.example/gateway/v1" {
		t.Fatalf("OpenCode Base URL = %#v", options["baseURL"])
	}

	if _, err := adapters.Apply(context.Background(), TargetCodex, input); err != nil {
		t.Fatal(err)
	}
	codexPath, _ := adapters.TargetPath(TargetCodex)
	codex, err := os.ReadFile(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codex), `base_url = "https://api.example/gateway/v1"`) {
		t.Fatalf("Codex config did not receive versioned Base URL:\n%s", codex)
	}
}

func TestCodexAdapterPreservesCommentsAndTablesWhileUpdatingManagedFields(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	path, _ := adapters.TargetPath(TargetCodex)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := `# keep this comment
approval_policy = "on-request"
model = "old-model"

[projects."/work"]
trust_level = "trusted"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := adapters.Apply(context.Background(), TargetCodex, adapterInput()); err != nil {
		t.Fatal(err)
	}
	updatedBytes, _ := os.ReadFile(path)
	updated := string(updatedBytes)
	for _, preserved := range []string{"# keep this comment", `approval_policy = "on-request"`, `[projects."/work"]`, `trust_level = "trusted"`} {
		if !strings.Contains(updated, preserved) {
			t.Fatalf("Codex config lost %q:\n%s", preserved, updated)
		}
	}
	for _, managed := range []string{
		`model = "model-name" # osverse-managed-profile`,
		`model_provider = "osverse" # osverse-managed-profile`,
		`[model_providers.osverse]`,
		`base_url = "https://api.example/v1"`,
		`experimental_bearer_token = "secret-key-1234"`,
	} {
		if strings.Count(updated, managed) != 1 {
			t.Fatalf("managed value %q count != 1:\n%s", managed, updated)
		}
	}

	input := adapterInput()
	input.Model = "new-model"
	if _, err := adapters.Apply(context.Background(), TargetCodex, input); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if strings.Count(string(second), codexBlockStart) != 1 || !strings.Contains(string(second), `model = "new-model"`) {
		t.Fatalf("idempotent Codex update failed:\n%s", second)
	}
}

func TestAdaptersRejectMalformedOrSymlinkedTargetsWithoutChangingThem(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	claudePath, _ := adapters.TargetPath(TargetClaude)
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o700); err != nil {
		t.Fatal(err)
	}
	malformed := []byte(`{"env":`)
	if err := os.WriteFile(claudePath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.Apply(context.Background(), TargetClaude, adapterInput()); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("malformed JSON error = %v", err)
	}
	unchanged, _ := os.ReadFile(claudePath)
	if string(unchanged) != string(malformed) {
		t.Fatal("malformed config changed")
	}

	openCodePath, _ := adapters.TargetPath(TargetOpenCode)
	if err := os.MkdirAll(filepath.Dir(openCodePath), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "outside.json")
	if err := os.WriteFile(target, []byte(`{"outside":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, openCodePath); err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.Apply(context.Background(), TargetOpenCode, adapterInput()); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("symlink target error = %v", err)
	}
	out, _ := os.ReadFile(target)
	if string(out) != `{"outside":true}` {
		t.Fatal("symlink target changed")
	}
}

func TestCodexAdapterRejectsAnUnmanagedOsverseProvider(t *testing.T) {
	_, err := mergeCodexConfig([]byte("[model_providers.osverse]\nname = \"User\"\n"), adapterInput())
	if !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("unmanaged provider error = %v", err)
	}
}

func adapterInput() Input {
	return Input{
		ID: "profile", Name: "Work", APIKey: "secret-key-1234",
		BaseURL: "https://api.example/v1", Model: "model-name",
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
