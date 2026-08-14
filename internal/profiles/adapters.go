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
	"strconv"
	"strings"
	"time"
)

const (
	TargetClaude   = "claude-code"
	TargetCodex    = "codex-cli"
	TargetOpenCode = "opencode-cli"
	configLimit    = 2 * 1024 * 1024
)

var (
	ErrUnknownTarget  = errors.New("unknown API profile target")
	ErrConfigConflict = errors.New("target configuration conflict")
)

// ApplyResult is redacted; it contains paths but never config contents.
type ApplyResult struct {
	Target     string `json:"target"`
	Applied    bool   `json:"applied"`
	Path       string `json:"path"`
	BackupPath string `json:"backupPath"`
	Message    string `json:"message"`
}

type AdapterSet struct {
	home       string
	backupRoot string
	now        func() time.Time
}

func NewAdapterSet(home string) (*AdapterSet, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) == string(filepath.Separator) {
		return nil, ErrUnsafeStorage
	}
	backupRoot, err := ensurePrivateDirectories(resolved, ".local", "share", "osverse", "profiles", "backups")
	if err != nil {
		return nil, err
	}
	return &AdapterSet{home: resolved, backupRoot: backupRoot, now: time.Now}, nil
}

func (adapters *AdapterSet) TargetPath(target string) (string, error) {
	if adapters == nil {
		return "", ErrUnknownTarget
	}
	switch target {
	case TargetClaude:
		return filepath.Join(adapters.home, ".claude", "settings.json"), nil
	case TargetCodex:
		return filepath.Join(adapters.home, ".codex", "config.toml"), nil
	case TargetOpenCode:
		return filepath.Join(adapters.home, ".config", "opencode", "opencode.json"), nil
	default:
		return "", ErrUnknownTarget
	}
}

func (adapters *AdapterSet) Apply(ctx context.Context, target string, input Input) (ApplyResult, error) {
	if adapters == nil {
		return ApplyResult{}, ErrUnknownTarget
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	validated, normalized, err := validateInput(input)
	if err != nil {
		return ApplyResult{}, err
	}
	validated.BaseURL = normalized
	path, err := adapters.TargetPath(target)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := ensureConfigParent(adapters.home, filepath.Dir(path)); err != nil {
		return ApplyResult{}, err
	}
	before, mode, existed, err := readConfig(path)
	if err != nil {
		return ApplyResult{}, err
	}
	backup, err := adapters.backup(target, before, existed)
	if err != nil {
		return ApplyResult{}, err
	}
	var next []byte
	switch target {
	case TargetClaude:
		next, err = mergeClaudeConfig(before, validated)
	case TargetCodex:
		next, err = mergeCodexConfig(before, validated)
	case TargetOpenCode:
		next, err = mergeOpenCodeConfig(before, validated)
	default:
		err = ErrUnknownTarget
	}
	if err != nil {
		return ApplyResult{}, err
	}
	if err := atomicWriteConfig(path, next, mode); err != nil {
		return ApplyResult{}, err
	}
	if err := verifyWrittenConfig(target, path); err != nil {
		_ = restoreConfig(path, before, mode, existed)
		return ApplyResult{}, err
	}
	return ApplyResult{
		Target: target, Applied: true, Path: path, BackupPath: backup,
		Message: "配置已原子更新，API Key 已写入权限为 0600 的目标文件",
	}, nil
}

func ensureConfigParent(home, directory string) error {
	relative, err := filepath.Rel(home, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrUnsafeStorage
	}
	current := home
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return ErrUnsafeStorage
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return ErrUnsafeStorage
		}
	}
	return nil
}

func readConfig(path string) ([]byte, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0o600, false, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > configLimit {
		return nil, 0, false, ErrConfigConflict
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, err
	}
	return raw, 0o600, true, nil
}

func (adapters *AdapterSet) backup(target string, content []byte, existed bool) (string, error) {
	stamp := adapters.now().UTC().Format("20060102T150405.000000000Z")
	path := filepath.Join(adapters.backupRoot, target+"-"+stamp+".backup")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if !existed {
		content = []byte("OSVERSE_BACKUP_ORIGINALLY_ABSENT\n")
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", err
	}
	return path, file.Close()
}

func decodeJSONObject(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return make(map[string]any), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, ErrConfigConflict
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrConfigConflict
	}
	return value, nil
}

func mergeClaudeConfig(raw []byte, input Input) ([]byte, error) {
	root, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	environment, ok := root["env"].(map[string]any)
	if !ok {
		if root["env"] != nil {
			return nil, ErrConfigConflict
		}
		environment = make(map[string]any)
		root["env"] = environment
	}
	environment["ANTHROPIC_AUTH_TOKEN"] = input.APIKey
	environment["ANTHROPIC_BASE_URL"] = input.BaseURL
	environment["ANTHROPIC_MODEL"] = input.Model
	return marshalConfigJSON(root)
}

func mergeOpenCodeConfig(raw []byte, input Input) ([]byte, error) {
	root, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	providers, ok := root["provider"].(map[string]any)
	if !ok {
		if root["provider"] != nil {
			return nil, ErrConfigConflict
		}
		providers = make(map[string]any)
		root["provider"] = providers
	}
	providers["osverse"] = map[string]any{
		"npm":  "@ai-sdk/openai",
		"name": "Osverse: " + input.Name,
		"options": map[string]any{
			"baseURL": input.BaseURL,
			"apiKey":  input.APIKey,
		},
		"models": map[string]any{
			input.Model: map[string]any{"name": input.Model},
		},
	}
	root["model"] = "osverse/" + input.Model
	return marshalConfigJSON(root)
}

func marshalConfigJSON(root map[string]any) ([]byte, error) {
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil || len(raw) > configLimit {
		return nil, ErrConfigConflict
	}
	return append(raw, '\n'), nil
}

const (
	codexBlockStart = "# >>> osverse managed provider >>>"
	codexBlockEnd   = "# <<< osverse managed provider <<<"
)

var codexRootAssignment = regexp.MustCompile(`^([A-Za-z0-9_-]+)[ \t]*=`)

func mergeCodexConfig(raw []byte, input Input) ([]byte, error) {
	if len(raw) > configLimit || bytes.IndexByte(raw, 0) >= 0 {
		return nil, ErrConfigConflict
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.Count(text, codexBlockStart) != strings.Count(text, codexBlockEnd) || strings.Count(text, codexBlockStart) > 1 {
		return nil, ErrConfigConflict
	}
	text, err := removeMarkedBlock(text, codexBlockStart, codexBlockEnd)
	if err != nil {
		return nil, err
	}
	if regexp.MustCompile(`(?m)^\s*\[model_providers\.osverse\]\s*(?:#.*)?$`).MatchString(text) {
		return nil, ErrConfigConflict
	}
	lines := strings.Split(text, "\n")
	rootEnd := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			rootEnd = index
			break
		}
	}
	counts := map[string]int{"model": 0, "model_provider": 0}
	for index := 0; index < rootEnd; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		match := codexRootAssignment.FindStringSubmatch(trimmed)
		if len(match) != 2 {
			continue
		}
		key := match[1]
		if _, managed := counts[key]; !managed {
			continue
		}
		counts[key]++
		if counts[key] > 1 {
			return nil, ErrConfigConflict
		}
		value := input.Model
		if key == "model_provider" {
			value = "osverse"
		}
		lines[index] = key + " = " + strconv.Quote(value) + " # osverse-managed-profile"
	}
	insert := make([]string, 0, 2)
	if counts["model"] == 0 {
		insert = append(insert, "model = "+strconv.Quote(input.Model)+" # osverse-managed-profile")
	}
	if counts["model_provider"] == 0 {
		insert = append(insert, "model_provider = \"osverse\" # osverse-managed-profile")
	}
	if len(insert) > 0 {
		combined := make([]string, 0, len(lines)+len(insert))
		combined = append(combined, lines[:rootEnd]...)
		combined = append(combined, insert...)
		combined = append(combined, lines[rootEnd:]...)
		lines = combined
	}
	text = strings.TrimRight(strings.Join(lines, "\n"), "\n")
	providerBlock := strings.Join([]string{
		codexBlockStart,
		"[model_providers.osverse]",
		"name = " + strconv.Quote("Osverse: "+input.Name),
		"base_url = " + strconv.Quote(input.BaseURL),
		"experimental_bearer_token = " + strconv.Quote(input.APIKey),
		"requires_openai_auth = false",
		codexBlockEnd,
	}, "\n")
	next := strings.TrimRight(text, "\n")
	if next != "" {
		next += "\n\n"
	}
	next += providerBlock + "\n"
	if len(next) > configLimit {
		return nil, ErrConfigConflict
	}
	return []byte(next), nil
}

func removeMarkedBlock(text, start, end string) (string, error) {
	startIndex := strings.Index(text, start)
	if startIndex < 0 {
		return text, nil
	}
	endRelative := strings.Index(text[startIndex:], end)
	if endRelative < 0 {
		return "", ErrConfigConflict
	}
	endIndex := startIndex + endRelative + len(end)
	if endIndex < len(text) && text[endIndex] == '\n' {
		endIndex++
	}
	return text[:startIndex] + text[endIndex:], nil
}

func atomicWriteConfig(path string, content []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	temporary := filepath.Join(filepath.Dir(path), fmt.Sprintf(".osverse-config-%d", time.Now().UnixNano()))
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
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func verifyWrittenConfig(target, path string) error {
	raw, _, _, err := readConfig(path)
	if err != nil {
		return err
	}
	switch target {
	case TargetClaude, TargetOpenCode:
		_, err = decodeJSONObject(raw)
	case TargetCodex:
		text := string(raw)
		if strings.Count(text, codexBlockStart) != 1 || strings.Count(text, "[model_providers.osverse]") != 1 {
			err = ErrConfigConflict
		}
	default:
		err = ErrUnknownTarget
	}
	return err
}

func restoreConfig(path string, content []byte, mode os.FileMode, existed bool) error {
	if !existed {
		return os.Remove(path)
	}
	return atomicWriteConfig(path, content, mode)
}
