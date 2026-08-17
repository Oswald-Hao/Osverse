package profiles

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestHarnessAdapterWritesOfficialProviderCredentialAndDefault(t *testing.T) {
	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	settingsPath, err := adapters.TargetPath(TargetHarness)
	if err != nil {
		t.Fatal(err)
	}
	credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `# keep root comment
ui-theme:
  theme: dark
llm-pi-ai:
  providers:
    keep:
      apiKeyEnv: KEEP_API_KEY
      api: openai-completions
      baseURL: https://keep.example/v1
      models:
        - id: keep-model
agent-default-model:
  provider: keep
  model: keep-model
  reasoningEffort: high
`
	credentials := "# keep credential comment\nKEEP_API_KEY: keep-secret\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}

	input := adapterInput()
	input.Name = "工作网关"
	input.Model = "deepseek/deepseek-v4-flash"
	input.Protocol = "openai-chat"
	result, err := adapters.Apply(context.Background(), TargetHarness, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.Path != settingsPath || result.BackupPath == "" || strings.Contains(result.Message, input.APIKey) {
		t.Fatalf("Apply() = %#v", result)
	}

	root := readYAMLMap(t, settingsPath)
	if root["ui-theme"].(map[string]any)["theme"] != "dark" {
		t.Fatalf("unrelated settings changed: %#v", root)
	}
	providers := root["llm-pi-ai"].(map[string]any)["providers"].(map[string]any)
	if _, ok := providers["keep"]; !ok {
		t.Fatalf("existing provider removed: %#v", providers)
	}
	osverse := providers["osverse"].(map[string]any)
	if osverse["displayName"] != "Osverse: 工作网关" || osverse["apiKeyEnv"] != "OSVERSE_API_KEY" ||
		osverse["api"] != "openai-completions" || osverse["baseURL"] != "https://api.example/v1" {
		t.Fatalf("Osverse provider = %#v", osverse)
	}
	models := osverse["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["id"] != input.Model {
		t.Fatalf("Osverse models = %#v", models)
	}
	selection := root["agent-default-model"].(map[string]any)
	if selection["provider"] != "osverse" || selection["model"] != input.Model {
		t.Fatalf("default model = %#v", selection)
	}
	if _, exists := selection["reasoningEffort"]; exists {
		t.Fatalf("stale reasoning effort survived model switch: %#v", selection)
	}

	storedCredentials := readYAMLMap(t, credentialsPath)
	if storedCredentials["KEEP_API_KEY"] != "keep-secret" || storedCredentials["OSVERSE_API_KEY"] != input.APIKey {
		t.Fatalf("credentials = %#v", storedCredentials)
	}
	settingsRaw, _ := os.ReadFile(settingsPath)
	credentialsRaw, _ := os.ReadFile(credentialsPath)
	if !strings.Contains(string(settingsRaw), "# keep root comment") || !strings.Contains(string(credentialsRaw), "# keep credential comment") {
		t.Fatal("Harness comments were not preserved")
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{settingsPath, credentialsPath, result.BackupPath} {
			info, statErr := os.Stat(path)
			if statErr != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("private file %s = (%v, %v)", path, info, statErr)
			}
		}
	}
	backupRaw, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	var backup harnessBackup
	if err := json.Unmarshal(backupRaw, &backup); err != nil {
		t.Fatal(err)
	}
	if backup.Version != 1 || !backup.Settings.Existed || string(backup.Settings.Content) != settings ||
		!backup.Credentials.Existed || string(backup.Credentials.Content) != credentials {
		t.Fatalf("backup does not contain the exact previous Harness files: %#v", backup)
	}
}

func TestHarnessAdapterVersionsUnversionedOpenAIBaseURLsOnly(t *testing.T) {
	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.BaseURL = "https://api.example/gateway"

	for _, protocol := range []string{"openai-chat", "openai-responses"} {
		input.Protocol = protocol
		if _, err := adapters.Apply(context.Background(), TargetHarness, input); err != nil {
			t.Fatalf("Apply(%q) error = %v", protocol, err)
		}
		settingsPath, _ := adapters.TargetPath(TargetHarness)
		root := readYAMLMap(t, settingsPath)
		provider := root["llm-pi-ai"].(map[string]any)["providers"].(map[string]any)["osverse"].(map[string]any)
		if provider["baseURL"] != "https://api.example/gateway/v1" {
			t.Fatalf("%s Base URL = %#v", protocol, provider["baseURL"])
		}
	}

	input.Protocol = "anthropic-messages"
	input.BaseURL = "https://anthropic.example/gateway"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); err != nil {
		t.Fatal(err)
	}
	settingsPath, _ := adapters.TargetPath(TargetHarness)
	root := readYAMLMap(t, settingsPath)
	provider := root["llm-pi-ai"].(map[string]any)["providers"].(map[string]any)["osverse"].(map[string]any)
	if provider["baseURL"] != input.BaseURL {
		t.Fatalf("Anthropic Base URL = %#v, want %#v", provider["baseURL"], input.BaseURL)
	}
}

func TestHarnessAdapterUpdatesOwnedConfigurationIdempotently(t *testing.T) {
	home := t.TempDir()
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	first := adapterInput()
	first.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Name = "Updated"
	second.APIKey = "secret-key-5678"
	second.BaseURL = "https://updated.example/v1"
	second.Model = "deepseek/deepseek-v4-flash"
	second.Protocol = "anthropic-messages"
	if _, err := adapters.Apply(context.Background(), TargetHarness, second); err != nil {
		t.Fatal(err)
	}

	settingsPath, _ := adapters.TargetPath(TargetHarness)
	root := readYAMLMap(t, settingsPath)
	providers := root["llm-pi-ai"].(map[string]any)["providers"].(map[string]any)
	if len(providers) != 1 {
		t.Fatalf("providers after update = %#v", providers)
	}
	provider := providers["osverse"].(map[string]any)
	if provider["displayName"] != "Osverse: Updated" || provider["api"] != "anthropic-messages" ||
		provider["baseURL"] != second.BaseURL {
		t.Fatalf("updated provider = %#v", provider)
	}
	models := provider["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["id"] != second.Model {
		t.Fatalf("updated models = %#v", models)
	}
	credentials := readYAMLMap(t, filepath.Join(home, ".dsh", ".credentials.yaml"))
	if len(credentials) != 1 || credentials[harnessCredentialRef] != second.APIKey {
		t.Fatalf("updated credentials = %#v", credentials)
	}
}

func TestHarnessAdapterRejectsMalformedOrDuplicateYAMLWithoutWriting(t *testing.T) {
	fixtures := map[string]struct {
		settings    string
		credentials string
	}{
		"duplicate settings key": {settings: "llm-pi-ai: {}\nllm-pi-ai: {}\n"},
		"duplicate credential key": {
			credentials: "KEEP_API_KEY: one\nKEEP_API_KEY: two\n",
		},
		"multiple documents": {settings: "ui-theme: {}\n---\nui-theme: {}\n"},
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			adapters, _ := NewAdapterSet(home)
			settingsPath, _ := adapters.TargetPath(TargetHarness)
			credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if fixture.settings != "" {
				if err := os.WriteFile(settingsPath, []byte(fixture.settings), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if fixture.credentials != "" {
				if err := os.WriteFile(credentialsPath, []byte(fixture.credentials), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			input := adapterInput()
			input.Protocol = "openai-chat"
			if _, err := adapters.Apply(context.Background(), TargetHarness, input); !errors.Is(err, ErrConfigConflict) {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, _ := os.ReadFile(settingsPath); string(got) != fixture.settings {
				t.Fatalf("settings changed after rejection: %q", got)
			}
			if got, _ := os.ReadFile(credentialsPath); string(got) != fixture.credentials {
				t.Fatalf("credentials changed after rejection: %q", got)
			}
		})
	}
}

func TestHarnessAdapterRejectsSymlinkConfigurationFiles(t *testing.T) {
	for _, name := range []string{"settings", "credentials"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			adapters, _ := NewAdapterSet(home)
			settingsPath, _ := adapters.TargetPath(TargetHarness)
			credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatal(err)
			}
			external := filepath.Join(home, "external.yaml")
			original := []byte("EXTERNAL_SECRET: keep\n")
			if err := os.WriteFile(external, original, 0o600); err != nil {
				t.Fatal(err)
			}
			linkPath := settingsPath
			if name == "credentials" {
				linkPath = credentialsPath
			}
			if err := os.Symlink(external, linkPath); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			input := adapterInput()
			input.Protocol = "openai-chat"
			if _, err := adapters.Apply(context.Background(), TargetHarness, input); !errors.Is(err, ErrConfigConflict) {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, _ := os.ReadFile(external); string(got) != string(original) {
				t.Fatalf("external file changed: %q", got)
			}
		})
	}
}

func TestHarnessAdapterMapsEverySupportedProbeProtocol(t *testing.T) {
	tests := map[string]string{
		"openai-chat":        "openai-completions",
		"openai-responses":   "openai-responses",
		"anthropic-messages": "anthropic-messages",
	}
	for probeProtocol, harnessProtocol := range tests {
		got, err := harnessAPIProtocol(probeProtocol)
		if err != nil || got != harnessProtocol {
			t.Errorf("harnessAPIProtocol(%q) = (%q, %v), want %q", probeProtocol, got, err, harnessProtocol)
		}
	}
	if _, err := harnessAPIProtocol("unknown"); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("unknown protocol error = %v", err)
	}
}

func TestHarnessAdapterRefusesUnownedProviderOrCredential(t *testing.T) {
	for name, fixture := range map[string]struct {
		settings    string
		credentials string
	}{
		"provider": {
			settings: "llm-pi-ai:\n  providers:\n    osverse:\n      displayName: Personal\n      apiKeyEnv: PERSONAL_KEY\n      api: openai-completions\n      baseURL: https://personal.example/v1\n      models:\n        - id: personal\n",
		},
		"credential": {
			credentials: "OSVERSE_API_KEY: personal-secret\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			settings, credentials := fixture.settings, fixture.credentials
			home := t.TempDir()
			adapters, _ := NewAdapterSet(home)
			settingsPath, _ := adapters.TargetPath(TargetHarness)
			credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatal(err)
			}
			if settings != "" {
				if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if credentials != "" {
				if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			input := adapterInput()
			input.Protocol = "openai-chat"
			if _, err := adapters.Apply(context.Background(), TargetHarness, input); !errors.Is(err, ErrConfigConflict) {
				t.Fatalf("Apply() error = %v", err)
			}
			if got, _ := os.ReadFile(settingsPath); string(got) != settings {
				t.Fatalf("settings changed after refusal: %q", got)
			}
			if got, _ := os.ReadFile(credentialsPath); string(got) != credentials {
				t.Fatalf("credentials changed after refusal: %q", got)
			}
		})
	}
}

func TestHarnessAdapterRollsBackCredentialWhenSettingsCommitFails(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	settingsPath, _ := adapters.TargetPath(TargetHarness)
	credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	originalCredentials := []byte("KEEP_API_KEY: keep-secret\n")
	if err := os.WriteFile(credentialsPath, originalCredentials, 0o600); err != nil {
		t.Fatal(err)
	}
	adapters.writeConfig = func(path string, content []byte, mode os.FileMode) error {
		if path == settingsPath {
			return errors.New("injected settings commit failure")
		}
		return atomicWriteConfig(path, content, mode)
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); err == nil {
		t.Fatal("Apply() succeeded despite injected settings failure")
	}
	if got, _ := os.ReadFile(credentialsPath); string(got) != string(originalCredentials) {
		t.Fatalf("credentials were not rolled back: %q", got)
	}
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings file survived failed transaction: %v", err)
	}
}

func TestHarnessAdapterRollsBackFilesWhenWriterReportsAfterCommit(t *testing.T) {
	for _, failedPath := range []string{"credentials", "settings"} {
		t.Run(failedPath, func(t *testing.T) {
			home := t.TempDir()
			adapters, _ := NewAdapterSet(home)
			settingsPath, _ := adapters.TargetPath(TargetHarness)
			credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
			if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
				t.Fatal(err)
			}
			originalSettings := []byte("ui-theme:\n  theme: dark\n")
			originalCredentials := []byte("KEEP_API_KEY: keep-secret\n")
			if err := os.WriteFile(settingsPath, originalSettings, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(credentialsPath, originalCredentials, 0o600); err != nil {
				t.Fatal(err)
			}
			adapters.writeConfig = func(path string, content []byte, mode os.FileMode) error {
				if err := atomicWriteConfig(path, content, mode); err != nil {
					return err
				}
				if path == credentialsPath && failedPath == "credentials" || path == settingsPath && failedPath == "settings" {
					return errors.New("injected post-commit failure")
				}
				return nil
			}
			input := adapterInput()
			input.Protocol = "openai-chat"
			if _, err := adapters.Apply(context.Background(), TargetHarness, input); err == nil || errors.Is(err, ErrConfigRollback) {
				t.Fatalf("post-commit failure classification = %v", err)
			}
			if got, _ := os.ReadFile(settingsPath); string(got) != string(originalSettings) {
				t.Fatalf("settings were not restored: %q", got)
			}
			if got, _ := os.ReadFile(credentialsPath); string(got) != string(originalCredentials) {
				t.Fatalf("credentials were not restored: %q", got)
			}
		})
	}
}

func TestHarnessAdapterReportsIncompleteRollbackWithoutClaimingPreservation(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	settingsPath, _ := adapters.TargetPath(TargetHarness)
	credentialsPath := filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte("KEEP_API_KEY: keep-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapters.writeConfig = func(path string, content []byte, mode os.FileMode) error {
		if path == settingsPath {
			return errors.New("injected settings failure")
		}
		return atomicWriteConfig(path, content, mode)
	}
	adapters.restoreConfig = func(string, []byte, os.FileMode, bool) error {
		return errors.New("injected rollback failure")
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); !errors.Is(err, ErrConfigRollback) {
		t.Fatalf("rollback failure = %v", err)
	}
}

func TestHarnessAdapterRespectsOfficialCrossProcessLocks(t *testing.T) {
	home := t.TempDir()
	adapters, _ := NewAdapterSet(home)
	settingsPath, _ := adapters.TargetPath(TargetHarness)
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := settingsPath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	defer os.Remove(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	input := adapterInput()
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(ctx, TargetHarness, input); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock contention error = %v", err)
	}
	if _, err := os.Stat(settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings changed while official lock was held: %v", err)
	}
}

func TestHarnessTargetPathHonorsOnlySafeDSHHome(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "private", "harness")
	t.Setenv("DSH_HOME", custom)
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	path, err := adapters.TargetPath(TargetHarness)
	if err != nil || path != filepath.Join(custom, "settings.yaml") {
		t.Fatalf("custom DSH_HOME target = (%q, %v)", path, err)
	}

	t.Setenv("DSH_HOME", filepath.Join(filepath.Dir(home), "outside"))
	adapters, err = NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapters.TargetPath(TargetHarness); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("outside DSH_HOME error = %v", err)
	}

	t.Setenv("DSH_HOME", "relative/path")
	adapters, _ = NewAdapterSet(home)
	if _, err := adapters.TargetPath(TargetHarness); !errors.Is(err, ErrUnsafeStorage) {
		t.Fatalf("relative DSH_HOME error = %v", err)
	}
}

func TestHarnessAdapterSupportsHomeAsDSHHomeWithoutChangingItsMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", "~")
	if runtime.GOOS != "windows" {
		if err := os.Chmod(home, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	adapters, err := NewAdapterSet(home)
	if err != nil {
		t.Fatal(err)
	}
	input := adapterInput()
	input.Protocol = "openai-chat"
	if _, err := adapters.Apply(context.Background(), TargetHarness, input); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "settings.yaml")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(home)
		if err != nil || info.Mode().Perm() != 0o750 {
			t.Fatalf("home mode changed: info=%v err=%v", info, err)
		}
	}
}

func readYAMLMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := yaml.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
