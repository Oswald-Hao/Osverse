package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const harnessCredentialRef = "OSVERSE_API_KEY"

var credentialReference = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type harnessBackupFile struct {
	Existed bool   `json:"existed"`
	Content []byte `json:"content,omitempty"`
}

type harnessBackup struct {
	Version     int               `json:"version"`
	Settings    harnessBackupFile `json:"settings"`
	Credentials harnessBackupFile `json:"credentials"`
}

func harnessAPIProtocol(probeProtocol string) (string, error) {
	switch probeProtocol {
	case "openai-chat":
		return "openai-completions", nil
	case "openai-responses", "anthropic-messages":
		return probeProtocol, nil
	default:
		return "", ErrProbeRequired
	}
}

func harnessProviderBaseURL(baseURL, api string) string {
	switch api {
	case "openai-completions", "openai-responses":
		return openAIBaseURL(baseURL)
	default:
		return baseURL
	}
}

func (adapters *AdapterSet) applyHarness(ctx context.Context, input Input) (ApplyResult, error) {
	if adapters == nil || adapters.writeConfig == nil || adapters.restoreConfig == nil {
		return ApplyResult{}, ErrUnknownTarget
	}
	api, err := harnessAPIProtocol(input.Protocol)
	if err != nil {
		return ApplyResult{}, err
	}
	settingsPath, err := adapters.TargetPath(TargetHarness)
	if err != nil {
		return ApplyResult{}, err
	}
	dshHome := filepath.Dir(settingsPath)
	credentialsPath := filepath.Join(dshHome, ".credentials.yaml")
	if err := ensureConfigParent(adapters.home, dshHome); err != nil {
		return ApplyResult{}, err
	}
	if dshHome != adapters.home {
		if err := os.Chmod(dshHome, 0o700); err != nil {
			return ApplyResult{}, err
		}
	}

	releaseLocks, err := acquireHarnessLocks(ctx, []string{credentialsPath + ".lock", settingsPath + ".lock"})
	if err != nil {
		return ApplyResult{}, err
	}
	defer releaseLocks()

	settingsBefore, settingsMode, settingsExisted, err := readConfig(settingsPath)
	if err != nil {
		return ApplyResult{}, err
	}
	credentialsBefore, credentialsMode, credentialsExisted, err := readHarnessCredentials(credentialsPath)
	if err != nil {
		return ApplyResult{}, err
	}
	settingsNext, ownedBefore, err := mergeHarnessSettings(settingsBefore, input, api)
	if err != nil {
		return ApplyResult{}, err
	}
	credentialsNext, err := mergeHarnessCredentials(credentialsBefore, input.APIKey, ownedBefore)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	backupPayload, err := json.MarshalIndent(harnessBackup{
		Version:     1,
		Settings:    harnessBackupFile{Existed: settingsExisted, Content: settingsBefore},
		Credentials: harnessBackupFile{Existed: credentialsExisted, Content: credentialsBefore},
	}, "", "  ")
	if err != nil {
		return ApplyResult{}, err
	}
	backupPath, err := adapters.backup(TargetHarness, append(backupPayload, '\n'), true)
	if err != nil {
		return ApplyResult{}, err
	}

	if err := adapters.writeConfig(credentialsPath, credentialsNext, credentialsMode); err != nil {
		if rollbackErr := adapters.restoreConfig(credentialsPath, credentialsBefore, credentialsMode, credentialsExisted); rollbackErr != nil {
			return ApplyResult{}, errors.Join(ErrConfigRollback, err, fmt.Errorf("Harness credential rollback failed: %w", rollbackErr))
		}
		return ApplyResult{}, err
	}
	if err := adapters.writeConfig(settingsPath, settingsNext, settingsMode); err != nil {
		settingsRollback := adapters.restoreConfig(settingsPath, settingsBefore, settingsMode, settingsExisted)
		credentialsRollback := adapters.restoreConfig(credentialsPath, credentialsBefore, credentialsMode, credentialsExisted)
		if settingsRollback != nil || credentialsRollback != nil {
			return ApplyResult{}, errors.Join(ErrConfigRollback, err, settingsRollback, credentialsRollback)
		}
		return ApplyResult{}, err
	}
	if err := verifyHarnessConfig(settingsPath, credentialsPath, input, api); err != nil {
		settingsRollback := adapters.restoreConfig(settingsPath, settingsBefore, settingsMode, settingsExisted)
		credentialsRollback := adapters.restoreConfig(credentialsPath, credentialsBefore, credentialsMode, credentialsExisted)
		if settingsRollback != nil || credentialsRollback != nil {
			return ApplyResult{}, errors.Join(ErrConfigRollback, err, settingsRollback, credentialsRollback)
		}
		return ApplyResult{}, err
	}
	return ApplyResult{
		Target: TargetHarness, Applied: true, Path: settingsPath, BackupPath: backupPath,
		Message: "Harness Provider、默认模型与只写凭据已事务式更新",
	}, nil
}

func readHarnessCredentials(path string) ([]byte, os.FileMode, bool, error) {
	raw, mode, existed, err := readConfig(path)
	if err != nil || !existed || runtime.GOOS == "windows" {
		return raw, mode, existed, err
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		return nil, 0, false, ErrConfigConflict
	}
	return raw, mode, existed, nil
}

func mergeHarnessSettings(raw []byte, input Input, api string) ([]byte, bool, error) {
	document, root, err := parseYAMLDocument(raw)
	if err != nil {
		return nil, false, err
	}
	llm, err := ensureYAMLMapping(root, "llm-pi-ai")
	if err != nil {
		return nil, false, err
	}
	providers, err := ensureYAMLMapping(llm, "providers")
	if err != nil {
		return nil, false, err
	}
	provider, exists, err := yamlMappingValue(providers, "osverse")
	if err != nil {
		return nil, false, err
	}
	owned := false
	if exists {
		if provider.Kind != yaml.MappingNode || !ownedHarnessProvider(provider) {
			return nil, false, ErrConfigConflict
		}
		owned = true
	} else {
		provider = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setYAMLMappingValue(providers, "osverse", provider)
	}
	setYAMLScalar(provider, "displayName", "Osverse: "+input.Name)
	setYAMLScalar(provider, "apiKeyEnv", harnessCredentialRef)
	setYAMLScalar(provider, "api", api)
	setYAMLScalar(provider, "baseURL", harnessProviderBaseURL(input.BaseURL, api))
	models := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{
		{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "id"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: input.Model},
		}},
	}}
	setYAMLMappingValue(provider, "models", models)

	selection, err := ensureYAMLMapping(root, "agent-default-model")
	if err != nil {
		return nil, false, err
	}
	setYAMLScalar(selection, "provider", "osverse")
	setYAMLScalar(selection, "model", input.Model)
	deleteYAMLMappingValue(selection, "reasoningEffort")

	next, err := encodeYAMLDocument(document)
	return next, owned, err
}

func ownedHarnessProvider(provider *yaml.Node) bool {
	credential, ok, _ := yamlStringValue(provider, "apiKeyEnv")
	if !ok || credential != harnessCredentialRef {
		return false
	}
	displayName, ok, _ := yamlStringValue(provider, "displayName")
	if !ok || !strings.HasPrefix(displayName, "Osverse: ") {
		return false
	}
	api, ok, _ := yamlStringValue(provider, "api")
	if !ok {
		return false
	}
	if _, err := harnessAPIProtocol(mapHarnessProtocolToProbe(api)); err != nil {
		return false
	}
	baseURL, ok, _ := yamlStringValue(provider, "baseURL")
	if !ok || baseURL == "" {
		return false
	}
	models, ok, _ := yamlMappingValue(provider, "models")
	if !ok || models.Kind != yaml.SequenceNode || len(models.Content) != 1 || models.Content[0].Kind != yaml.MappingNode {
		return false
	}
	model, ok, _ := yamlStringValue(models.Content[0], "id")
	return ok && model != ""
}

func mapHarnessProtocolToProbe(api string) string {
	if api == "openai-completions" {
		return "openai-chat"
	}
	return api
}

func mergeHarnessCredentials(raw []byte, apiKey string, allowExisting bool) ([]byte, error) {
	document, root, err := parseYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	for index := 0; index < len(root.Content); index += 2 {
		key, value := root.Content[index], root.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || !credentialReference.MatchString(key.Value) ||
			value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
			return nil, ErrConfigConflict
		}
		if key.Value == harnessCredentialRef && !allowExisting {
			return nil, ErrConfigConflict
		}
	}
	setYAMLScalar(root, harnessCredentialRef, apiKey)
	return encodeYAMLDocument(document)
}

func verifyHarnessConfig(settingsPath, credentialsPath string, input Input, api string) error {
	settings, _, _, err := readConfig(settingsPath)
	if err != nil {
		return err
	}
	credentials, _, _, err := readHarnessCredentials(credentialsPath)
	if err != nil {
		return err
	}
	_, settingsRoot, err := parseYAMLDocument(settings)
	if err != nil {
		return err
	}
	llm, ok, _ := yamlMappingValue(settingsRoot, "llm-pi-ai")
	if !ok {
		return ErrConfigConflict
	}
	providers, ok, _ := yamlMappingValue(llm, "providers")
	if !ok {
		return ErrConfigConflict
	}
	provider, ok, _ := yamlMappingValue(providers, "osverse")
	if !ok || !ownedHarnessProvider(provider) {
		return ErrConfigConflict
	}
	configuredAPI, _, _ := yamlStringValue(provider, "api")
	baseURL, _, _ := yamlStringValue(provider, "baseURL")
	models, _, _ := yamlMappingValue(provider, "models")
	model, _, _ := yamlStringValue(models.Content[0], "id")
	selection, ok, _ := yamlMappingValue(settingsRoot, "agent-default-model")
	if !ok {
		return ErrConfigConflict
	}
	selectedProvider, _, _ := yamlStringValue(selection, "provider")
	selectedModel, _, _ := yamlStringValue(selection, "model")
	if configuredAPI != api || baseURL != harnessProviderBaseURL(input.BaseURL, api) || model != input.Model || selectedProvider != "osverse" || selectedModel != input.Model {
		return ErrConfigConflict
	}
	_, credentialsRoot, err := parseYAMLDocument(credentials)
	if err != nil {
		return err
	}
	key, ok, _ := yamlStringValue(credentialsRoot, harnessCredentialRef)
	if !ok || key != input.APIKey {
		return ErrConfigConflict
	}
	return nil
}

func parseYAMLDocument(raw []byte) (*yaml.Node, *yaml.Node, error) {
	if len(raw) > configLimit || bytes.IndexByte(raw, 0) >= 0 {
		return nil, nil, ErrConfigConflict
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}, root, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	document := &yaml.Node{}
	if err := decoder.Decode(document); err != nil || document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, nil, ErrConfigConflict
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, nil, ErrConfigConflict
	}
	if err := validateYAMLNode(document.Content[0], make(map[*yaml.Node]bool)); err != nil {
		return nil, nil, err
	}
	return document, document.Content[0], nil
}

func validateYAMLNode(node *yaml.Node, visiting map[*yaml.Node]bool) error {
	if node == nil || visiting[node] {
		return ErrConfigConflict
	}
	if node.Kind == yaml.AliasNode {
		if node.Alias == nil {
			return ErrConfigConflict
		}
		return nil
	}
	visiting[node] = true
	defer delete(visiting, node)
	if node.Kind == yaml.MappingNode {
		if len(node.Content)%2 != 0 {
			return ErrConfigConflict
		}
		seen := make(map[string]struct{}, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return ErrConfigConflict
			}
			if _, duplicate := seen[key.Value]; duplicate {
				return ErrConfigConflict
			}
			seen[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLNode(child, visiting); err != nil {
			return err
		}
	}
	return nil
}

func encodeYAMLDocument(document *yaml.Node) ([]byte, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, ErrConfigConflict
	}
	if err := encoder.Close(); err != nil || output.Len() > configLimit {
		return nil, ErrConfigConflict
	}
	return output.Bytes(), nil
}

func yamlMappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool, error) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, false, ErrConfigConflict
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			value := mapping.Content[index+1]
			if value.Kind == yaml.AliasNode {
				return nil, false, ErrConfigConflict
			}
			return value, true, nil
		}
	}
	return nil, false, nil
}

func yamlStringValue(mapping *yaml.Node, key string) (string, bool, error) {
	value, ok, err := yamlMappingValue(mapping, key)
	if err != nil || !ok {
		return "", ok, err
	}
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return "", false, ErrConfigConflict
	}
	return value.Value, true, nil
}

func ensureYAMLMapping(mapping *yaml.Node, key string) (*yaml.Node, error) {
	value, ok, err := yamlMappingValue(mapping, key)
	if err != nil {
		return nil, err
	}
	if ok {
		if value.Kind != yaml.MappingNode {
			return nil, ErrConfigConflict
		}
		return value, nil
	}
	value = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setYAMLMappingValue(mapping, key, value)
	return value, nil
}

func setYAMLScalar(mapping *yaml.Node, key, value string) {
	setYAMLMappingValue(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setYAMLMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value,
	)
}

func deleteYAMLMappingValue(mapping *yaml.Node, key string) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}

type harnessLock struct {
	path string
	file *os.File
}

func acquireHarnessLocks(ctx context.Context, paths []string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	locks := make([]harnessLock, 0, len(paths))
	release := func() {
		for index := len(locks) - 1; index >= 0; index-- {
			owned, ownErr := locks[index].file.Stat()
			pathInfo, pathErr := os.Lstat(locks[index].path)
			_ = locks[index].file.Close()
			if ownErr == nil && pathErr == nil && os.SameFile(owned, pathInfo) {
				_ = os.Remove(locks[index].path)
			}
		}
	}
	for _, path := range paths {
		lock, err := acquireHarnessLock(ctx, path)
		if err != nil {
			release()
			return func() {}, err
		}
		locks = append(locks, lock)
	}
	return release, nil
}

func acquireHarnessLock(ctx context.Context, path string) (harnessLock, error) {
	deadline := time.Now().Add(2 * time.Second)
	delay := 10 * time.Millisecond
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return harnessLock{path: path, file: file}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return harnessLock{}, err
		}
		if err := ctx.Err(); err != nil {
			return harnessLock{}, err
		}
		if !time.Now().Before(deadline) {
			return harnessLock{}, ErrConfigConflict
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return harnessLock{}, ctx.Err()
		case <-timer.C:
		}
		if delay < 160*time.Millisecond {
			delay *= 2
		}
	}
}
