//go:build linux

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

func TestQwenAdapterSelectsAnOpenAICompatibleOsverseProvider(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	path, _ := adapters.TargetPath(TargetQwen)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"Dracula","modelProviders":{"keep":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.Apply(context.Background(), TargetQwen, adapterInput()); err != nil {
		t.Fatal(err)
	}
	root := readJSONMap(t, path)
	if root["theme"] != "Dracula" {
		t.Fatalf("unrelated Qwen setting changed: %#v", root)
	}
	environment := root["env"].(map[string]any)
	if environment["OSVERSE_API_KEY"] != "secret-key-1234" {
		t.Fatalf("Qwen environment = %#v", environment)
	}
	providers := root["modelProviders"].(map[string]any)
	if _, ok := providers["keep"]; !ok {
		t.Fatal("existing Qwen provider was removed")
	}
	osverse := providers["osverse"].([]any)[0].(map[string]any)
	if osverse["id"] != "model-name" || osverse["envKey"] != "OSVERSE_API_KEY" || osverse["baseUrl"] != "https://api.example/v1" {
		t.Fatalf("Qwen Osverse provider = %#v", osverse)
	}
	if root["providerProtocol"].(map[string]any)["osverse"] != "openai" ||
		root["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "openai" ||
		root["model"].(map[string]any)["name"] != "model-name" ||
		root["model"].(map[string]any)["baseUrl"] != "https://api.example/v1" {
		t.Fatalf("Qwen selection = %#v", root)
	}
}

func TestQwenAdapterRejectsAnUnmanagedOsverseProvider(t *testing.T) {
	raw := []byte(`{
  "theme": "Dracula",
  "modelProviders": {
    "osverse": [{"id":"personal-model","name":"Personal","envKey":"PERSONAL_KEY","baseUrl":"https://personal.example/v1"}]
  },
  "providerProtocol": {"osverse":"anthropic"}
}`)
	if _, err := mergeQwenConfig(raw, adapterInput()); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("unmanaged Qwen provider error = %v", err)
	}
}

func TestQwenAdapterCanUpdateItsOwnProvider(t *testing.T) {
	first, err := mergeQwenConfig(nil, adapterInput())
	if err != nil {
		t.Fatal(err)
	}
	next := adapterInput()
	next.Name = "Updated profile"
	next.Model = "deepseek/deepseek-v4-flash"
	next.BaseURL = "https://updated.example"
	second, err := mergeQwenConfig(first, next)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(second, &root); err != nil {
		t.Fatal(err)
	}
	provider := root["modelProviders"].(map[string]any)["osverse"].([]any)[0].(map[string]any)
	if provider["id"] != next.Model || provider["baseUrl"] != "https://updated.example/v1" ||
		root["model"].(map[string]any)["name"] != next.Model {
		t.Fatalf("updated Qwen config = %#v", root)
	}
}

func TestKimiAdapterPreservesUnrelatedTOMLAndExactModelID(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	path, _ := adapters.TargetPath(TargetKimi)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "# keep me\ntelemetry = false\n\n[tools]\nkeep = true\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	input.Model = "deepseek/deepseek-v4-flash"
	if _, err := adapters.Apply(context.Background(), TargetKimi, input); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(updated)
	for _, expected := range []string{
		"# keep me", "telemetry = false", "[tools]", "keep = true",
		`default_model = "osverse"`, `[providers.osverse]`, `type = "openai"`,
		`base_url = "https://api.example/v1"`, `api_key = "secret-key-1234"`,
		`[models.osverse]`, `provider = "osverse"`, `model = "deepseek/deepseek-v4-flash"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Kimi config missing %q:\n%s", expected, text)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("Kimi config mode = %o", info.Mode().Perm())
	}
}

func TestKimiAdapterMapsConfirmedProtocolsAndRejectsUnownedTables(t *testing.T) {
	for protocol, providerType := range map[string]string{
		"openai-chat":        "openai",
		"openai-responses":   "openai_responses",
		"anthropic-messages": "anthropic",
	} {
		t.Run(protocol, func(t *testing.T) {
			input := adapterInput()
			input.Protocol = protocol
			next, err := mergeKimiConfig(nil, input)
			if err != nil || !strings.Contains(string(next), `type = "`+providerType+`"`) {
				t.Fatalf("mergeKimiConfig() = %v\n%s", err, next)
			}
		})
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	for _, raw := range []string{
		"[providers.osverse]\ntype = \"openai\"\n",
		"[models.osverse]\nprovider = \"personal\"\nmodel = \"mine\"\nmax_context_size = 1\n",
	} {
		if _, err := mergeKimiConfig([]byte(raw), input); !errors.Is(err, ErrConfigConflict) {
			t.Fatalf("unowned Kimi table error = %v", err)
		}
	}
}

func TestKimiAdapterUpdatesItsMarkedBlockIdempotently(t *testing.T) {
	first := adapterInput()
	first.Protocol = "openai-chat"
	configured, err := mergeKimiConfig([]byte("# keep\ndefault_model = \"personal\"\n"), first)
	if err != nil {
		t.Fatal(err)
	}
	updated := adapterInput()
	updated.Name = "Updated"
	updated.Model = "deepseek/deepseek-v4-flash"
	updated.BaseURL = "https://updated.example/gateway"
	updated.Protocol = "openai-responses"
	next, err := mergeKimiConfig(configured, updated)
	if err != nil {
		t.Fatal(err)
	}
	text := string(next)
	for _, expected := range []string{
		"# keep", `default_model = "osverse"`, `type = "openai_responses"`,
		`base_url = "https://updated.example/gateway/v1"`,
		`model = "deepseek/deepseek-v4-flash"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("updated Kimi config missing %q:\n%s", expected, text)
		}
	}
	if strings.Count(text, kimiBlockStart) != 1 || strings.Count(text, `default_model = "osverse"`) != 1 || strings.Contains(text, "secret-key-1234\nsecret-key-1234") {
		t.Fatalf("Kimi config was not updated idempotently:\n%s", text)
	}
}

func TestKimiAdapterRejectsMalformedOwnershipAndDuplicateRootDefaults(t *testing.T) {
	input := adapterInput()
	input.Protocol = "openai-chat"
	for _, raw := range []string{
		kimiBlockStart + "\n[providers.osverse]\ntype = \"openai\"\n",
		kimiBlockEnd + "\n",
		"default_model = \"first\"\ndefault_model = \"second\"\n",
		string([]byte{'d', 'e', 'f', 0, 'a', 'u', 'l', 't'}),
	} {
		if _, err := mergeKimiConfig([]byte(raw), input); !errors.Is(err, ErrConfigConflict) {
			t.Fatalf("malformed Kimi config %q error = %v", raw, err)
		}
	}
}

func TestKimiProviderBaseURLKeepsAnthropicRootAndVersionsOpenAI(t *testing.T) {
	if got := kimiProviderBaseURL("https://api.example/gateway/", "anthropic-messages"); got != "https://api.example/gateway" {
		t.Fatalf("Anthropic base URL = %q", got)
	}
	if got := kimiProviderBaseURL("https://api.example/gateway/", "openai-chat"); got != "https://api.example/gateway/v1" {
		t.Fatalf("OpenAI base URL = %q", got)
	}
	if got := kimiProviderBaseURL("https://api.example/v1", "openai-responses"); got != "https://api.example/v1" {
		t.Fatalf("OpenAI Responses base URL = %q", got)
	}
}

func TestKimiAdapterHonorsSafeKimiCodeHome(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "config", "kimi")
	t.Setenv("KIMI_CODE_HOME", custom)
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	path, err := adapters.TargetPath(TargetKimi)
	if err != nil || path != filepath.Join(custom, "config.toml") {
		t.Fatalf("custom KIMI_CODE_HOME target = (%q, %v)", path, err)
	}

	t.Setenv("KIMI_CODE_HOME", filepath.Join(filepath.Dir(home), "outside"))
	adapters, err = NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.TargetPath(TargetKimi); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("outside KIMI_CODE_HOME error = %v", err)
	}

	t.Setenv("KIMI_CODE_HOME", "relative/path")
	adapters, err = NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.TargetPath(TargetKimi); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("relative KIMI_CODE_HOME error = %v", err)
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
