//go:build windows

// Package windowsremoval implements fixed, evidence-bound Windows removals.
package windowsremoval

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/removal"
)

const planLifetime = 3 * time.Minute

type componentRule struct {
	category         string
	command          string
	uninstallKind    string
	uninstallID      string
	uninstallerPaths []string
	uninstallArgs    []string
}

var componentRules = map[string]componentRule{
	"claude-code":        {category: "Core CLI", command: "claude"},
	"codex-cli":          {category: "Core CLI", command: "codex"},
	"opencode-cli":       {category: "Core CLI", command: "opencode"},
	"deepseek-harness":   {category: "Core CLI", command: "dsh"},
	"qwen-code":          {category: "Core CLI", command: "qwen"},
	"kimi-code":          {category: "Core CLI", command: "kimi"},
	"github-copilot-cli": {category: "Core CLI", command: "copilot"},
	"claude-desktop":     {category: "Desktop Applications", uninstallKind: "winget", uninstallID: "Anthropic.Claude"},
	"chatgpt-desktop":    {category: "Desktop Applications", uninstallKind: "store", uninstallID: "9NT1R1C2HH7J"},
	"codex-desktop":      {category: "Desktop Applications", uninstallKind: "store", uninstallID: "9PLM9XGG6VKS"},
	"opencode-desktop":   {category: "Desktop Applications", uninstallKind: "exe", uninstallerPaths: []string{`AppData\Local\Programs\OpenCode\Uninstall OpenCode.exe`, `AppData\Local\Programs\OpenCode Beta\Uninstall OpenCode Beta.exe`, `AppData\Local\Programs\opencode\Uninstall OpenCode.exe`, `AppData\Local\Programs\@opencode-aidesktop\Uninstall OpenCode.exe`}, uninstallArgs: []string{"/S"}},
	"cc-switch":          {category: "Management Tools", uninstallKind: "msi", uninstallID: "{634D5E13-C751-4997-A707-B6B27B354D77}"},
	"cockpit-tools":      {category: "Management Tools", uninstallKind: "exe", uninstallerPaths: []string{`AppData\Local\Programs\Cockpit Tools\uninstall.exe`, `AppData\Local\Cockpit Tools\uninstall.exe`, `AppData\Local\Programs\Cockpit Tools\Uninstall Cockpit Tools.exe`}, uninstallArgs: []string{"/S"}},
}

type capturedPath struct {
	original string
	evidence *platformwindows.MovableEvidence
}

type storedPlan struct {
	public      removal.Plan
	component   domain.Component
	rule        componentRule
	paths       []capturedPath
	uninstaller *platformwindows.ExecutableEvidence
	timer       *time.Timer
	used        bool
}

type Manager struct {
	mu       sync.Mutex
	home     string
	now      func() time.Time
	randomID func() (string, error)
	plans    map[string]*storedPlan
	runner   platform.CommandRunner
}

func NewManager(home string) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !safeAbsolute(resolved) || filepath.Clean(resolved) == filepath.VolumeName(resolved)+string(filepath.Separator) {
		return nil, removal.ErrRemovalUnsupported
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, removal.ErrRemovalUnsupported
	}
	return &Manager{
		home: filepath.Clean(resolved), now: time.Now, randomID: secureID,
		plans: make(map[string]*storedPlan), runner: platformwindows.NewExecRunner(),
	}, nil
}

func (manager *Manager) CreatePlan(ctx context.Context, component domain.Component) (removal.Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return removal.Plan{}, err
	}
	rule, known := componentRules[component.ID]
	if manager == nil || !known || component.Category != rule.category || (len(component.Installations) == 0 && component.Category == "Core CLI") ||
		(component.Status != domain.StatusInstalled && component.Status != domain.StatusUpdateAvailable && component.Status != domain.StatusConflict) {
		return removal.Plan{}, removal.ErrRemovalUnsupported
	}
	id, err := manager.randomID()
	if err != nil || id == "" || strings.ContainsAny(id, "\x00\r\n/\\") {
		return removal.Plan{}, removal.ErrPlanUnavailable
	}
	created := manager.now().UTC()
	stored := &storedPlan{component: cloneComponent(component), rule: cloneRule(rule)}
	if component.Category == "Core CLI" {
		stored.paths, stored.public.Effects, err = manager.captureManagedCLI(component, rule, id)
	} else {
		stored.uninstaller, stored.public.Effects, err = manager.desktopEffects(rule)
	}
	if err != nil || len(stored.public.Effects) == 0 {
		stored.close()
		return removal.Plan{}, removal.ErrRemovalUnsupported
	}
	warning := "Osverse 管理的命令入口和程序文件将移入恢复区；API 配置、凭据和会话数据不会删除。"
	if component.Category != "Core CLI" {
		warning = "应用将通过固定卸载身份移除；应用配置、API 凭据和会话数据不会删除。"
	}
	if rule.uninstallKind == "store" {
		warning = "该 Microsoft Store 应用将通过固定产品 ID 移除；不会影响另一个 OpenAI 桌面应用，也不会删除用户数据。"
	}
	stored.public.ID, stored.public.ComponentID, stored.public.Name = id, component.ID, component.Name
	stored.public.Warning, stored.public.CreatedAt, stored.public.ExpiresAt = warning, created, created.Add(planLifetime)
	manager.mu.Lock()
	manager.pruneLocked(created)
	manager.plans[id] = stored
	stored.timer = time.AfterFunc(planLifetime, func() { manager.expire(id) })
	manager.mu.Unlock()
	return clonePlan(stored.public), nil
}

func (manager *Manager) captureManagedCLI(component domain.Component, rule componentRule, planID string) ([]capturedPath, []removal.Effect, error) {
	shim := filepath.Join(manager.home, ".local", "bin", rule.command+".cmd")
	toolRoot := filepath.Join(manager.home, "AppData", "Local", "Osverse", "tools", component.ID)
	found := false
	for _, installation := range component.Installations {
		if installation.Managed && installation.Source == "osverse" && samePath(installation.Path, shim) && validManagedShim(shim, component.ID, toolRoot) {
			found = true
			break
		}
	}
	if !found {
		return nil, nil, removal.ErrRemovalUnsupported
	}
	paths := make([]capturedPath, 0, 2)
	for _, path := range []string{shim, toolRoot} {
		evidence, err := platformwindows.OpenMovableEvidence(path)
		if err != nil {
			for _, captured := range paths {
				_ = captured.evidence.Close()
			}
			return nil, nil, err
		}
		paths = append(paths, capturedPath{original: path, evidence: evidence})
	}
	recovery := filepath.Join(manager.home, "AppData", "Local", "Osverse", "recovery", planID)
	effects := []removal.Effect{
		{Action: "recover", Path: shim, Description: "将 Osverse 命令入口移入恢复区", Recoverable: true},
		{Action: "recover", Path: toolRoot, Description: "将 Osverse 管理的程序文件移入恢复区", Recoverable: true},
		{Action: "manifest", Path: filepath.Join(recovery, "recovery.json"), Description: "记录原始路径以便恢复", Recoverable: true},
	}
	return paths, effects, nil
}

func (manager *Manager) desktopEffects(rule componentRule) (*platformwindows.ExecutableEvidence, []removal.Effect, error) {
	switch rule.uninstallKind {
	case "winget":
		return nil, []removal.Effect{{Action: "package", Path: rule.uninstallID, Description: "通过固定 WinGet 包 ID 静默卸载", Recoverable: false}}, nil
	case "store":
		return nil, []removal.Effect{{Action: "store", Path: rule.uninstallID, Description: "通过精确 Microsoft Store 产品 ID 卸载", Recoverable: false}}, nil
	case "msi":
		return nil, []removal.Effect{{Action: "msi", Path: rule.uninstallID, Description: "通过固定 MSI ProductCode 静默卸载", Recoverable: false}}, nil
	case "exe":
		for _, relative := range rule.uninstallerPaths {
			path := filepath.Join(manager.home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, `/`)))
			evidence, err := platformwindows.OpenExecutableEvidence(path)
			if err == nil && pathWithin(manager.home, evidence.ResolvedPath()) {
				return evidence, []removal.Effect{{Action: "uninstaller", Path: path, Description: "运行已锁定的固定应用卸载器", Recoverable: false}}, nil
			}
			if evidence != nil {
				_ = evidence.Close()
			}
		}
	}
	return nil, nil, removal.ErrRemovalUnsupported
}

func (manager *Manager) Execute(ctx context.Context, planID string, current domain.Component) (removal.Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return removal.Result{}, err
	}
	manager.mu.Lock()
	stored := manager.plans[planID]
	if stored == nil || stored.used || !manager.now().Before(stored.public.ExpiresAt) {
		manager.mu.Unlock()
		return removal.Result{}, removal.ErrPlanUnavailable
	}
	stored.used = true
	if stored.timer != nil {
		stored.timer.Stop()
	}
	delete(manager.plans, planID)
	manager.mu.Unlock()
	defer stored.close()
	if !sameComponent(stored.component, current) {
		return removal.Result{}, removal.ErrEvidenceChanged
	}
	var err error
	if stored.component.Category == "Core CLI" {
		err = manager.moveToRecovery(ctx, stored)
	} else {
		err = manager.uninstallDesktop(ctx, stored)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, removal.ErrEvidenceChanged) {
			return removal.Result{}, err
		}
		return removal.Result{}, removal.ErrRemovalFailed
	}
	return removal.Result{PlanID: planID, ComponentID: stored.public.ComponentID, Removed: true, Message: "组件已移除，配置、凭据和用户数据已保留"}, nil
}

func (manager *Manager) moveToRecovery(ctx context.Context, stored *storedPlan) (returnErr error) {
	recovery, err := ensureDirectories(manager.home, "AppData", "Local", "Osverse", "recovery", stored.public.ID)
	if err != nil {
		return err
	}
	type movedPath struct {
		captured    capturedPath
		destination string
	}
	moved := make([]movedPath, 0, len(stored.paths))
	defer func() {
		if returnErr == nil {
			return
		}
		for index := len(moved) - 1; index >= 0; index-- {
			_ = moved[index].captured.evidence.MoveTo(moved[index].captured.original)
		}
	}()
	for index, captured := range stored.paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		destination := filepath.Join(recovery, strings.Join([]string{string(rune('0' + index)), filepath.Base(captured.original)}, "-"))
		if err := captured.evidence.MoveTo(destination); err != nil {
			return err
		}
		moved = append(moved, movedPath{captured: captured, destination: destination})
	}
	manifest := struct {
		SchemaVersion int               `json:"schemaVersion"`
		ComponentID   string            `json:"componentId"`
		Paths         map[string]string `json:"paths"`
	}{SchemaVersion: 1, ComponentID: stored.public.ComponentID, Paths: make(map[string]string, len(moved))}
	for _, value := range moved {
		manifest.Paths[value.destination] = value.captured.original
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(recovery, "recovery.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	returnErr = file.Close()
	return returnErr
}

func (manager *Manager) uninstallDesktop(ctx context.Context, stored *storedPlan) error {
	var path string
	var args []string
	switch stored.rule.uninstallKind {
	case "winget", "store":
		path = filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps", "winget.exe")
		args = []string{"uninstall", "--exact", "--id", stored.rule.uninstallID, "--disable-interactivity", "--silent"}
		if stored.rule.uninstallKind == "store" {
			args = append(args, "--source", "msstore")
		} else {
			args = append(args, "--source", "winget")
		}
	case "msi":
		path = filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe")
		args = []string{"/x", stored.rule.uninstallID, "/qn", "/norestart"}
	case "exe":
		if stored.uninstaller == nil {
			return removal.ErrRemovalUnsupported
		}
		path = stored.uninstaller.ResolvedPath()
		args = append([]string(nil), stored.rule.uninstallArgs...)
	default:
		return removal.ErrRemovalUnsupported
	}
	if !safeAbsolute(path) {
		return removal.ErrRemovalUnsupported
	}
	var evidence *platformwindows.ExecutableEvidence
	if stored.rule.uninstallKind == "exe" {
		evidence = stored.uninstaller
	} else {
		var err error
		evidence, err = platformwindows.OpenExecutableEvidence(path)
		if err != nil {
			return err
		}
		defer evidence.Close()
	}
	result, runErr := manager.runner.Run(ctx, platform.CommandRequest{Path: path, Args: args, Timeout: 20 * time.Minute, OutputLimit: 256 * 1024})
	if !evidence.Unchanged(path) || result.TimedOut || result.Truncated || (result.ExitCode != 0 && result.ExitCode != 3010) {
		return removal.ErrRemovalFailed
	}
	if runErr != nil && result.ExitCode != 3010 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return removal.ErrRemovalFailed
	}
	return nil
}

func (manager *Manager) expire(id string) {
	manager.mu.Lock()
	stored := manager.plans[id]
	delete(manager.plans, id)
	manager.mu.Unlock()
	if stored != nil {
		stored.close()
	}
}

func (manager *Manager) pruneLocked(now time.Time) {
	for id, stored := range manager.plans {
		if stored.used || !now.Before(stored.public.ExpiresAt) {
			delete(manager.plans, id)
			if stored.timer != nil {
				stored.timer.Stop()
			}
			stored.close()
		}
	}
}

func (stored *storedPlan) close() {
	if stored == nil {
		return
	}
	for _, path := range stored.paths {
		_ = path.evidence.Close()
	}
	stored.paths = nil
	if stored.uninstaller != nil {
		_ = stored.uninstaller.Close()
		stored.uninstaller = nil
	}
}

func validManagedShim(path, componentID, toolRoot string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16*1024 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(string(raw), "\r\n")
	if len(lines) != 5 || lines[0] != "@rem Osverse managed shim v1: "+componentID || lines[1] != "@echo off" ||
		lines[2] != "setlocal DisableDelayedExpansion" || lines[4] != "" || !strings.HasPrefix(lines[3], `"`) || !strings.HasSuffix(lines[3], `" %*`) {
		return false
	}
	target := strings.TrimSuffix(strings.TrimPrefix(lines[3], `"`), `" %*`)
	target, ok := decodeShimPath(target)
	if !ok || !filepath.IsAbs(target) {
		return false
	}
	target = filepath.Clean(target)
	if !pathWithin(toolRoot, target) {
		return false
	}
	if wrapper, ok := managedCommandWrapper(componentID); ok && strings.EqualFold(filepath.Ext(target), ".cmd") {
		return strings.EqualFold(filepath.Base(target), wrapper+".cmd")
	}
	return strings.EqualFold(filepath.Ext(target), ".exe")
}

func managedCommandWrapper(componentID string) (string, bool) {
	rule, ok := componentRules[componentID]
	return rule.command, ok && rule.category == "Core CLI" && rule.command != ""
}

func decodeShimPath(value string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) || value[index+1] != '%' {
			return "", false
		}
		result.WriteByte('%')
		index++
	}
	return result.String(), true
}

func ensureDirectories(base string, components ...string) (string, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || component == "." || component == ".." || filepath.Base(component) != component {
			return "", removal.ErrRemovalFailed
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return "", err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", removal.ErrRemovalFailed
		}
	}
	return current, nil
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "remove-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func clonePlan(plan removal.Plan) removal.Plan {
	plan.Effects = append([]removal.Effect(nil), plan.Effects...)
	return plan
}

func cloneRule(rule componentRule) componentRule {
	rule.uninstallerPaths = append([]string(nil), rule.uninstallerPaths...)
	rule.uninstallArgs = append([]string(nil), rule.uninstallArgs...)
	return rule
}

func cloneComponent(component domain.Component) domain.Component {
	component.Installations = append([]domain.Installation(nil), component.Installations...)
	return component
}

func sameComponent(left, right domain.Component) bool {
	if left.ID != right.ID || left.Category != right.Category || left.Status != right.Status || len(left.Installations) != len(right.Installations) {
		return false
	}
	leftItems := append([]domain.Installation(nil), left.Installations...)
	rightItems := append([]domain.Installation(nil), right.Installations...)
	sort.Slice(leftItems, func(i, j int) bool { return strings.ToLower(leftItems[i].Path) < strings.ToLower(leftItems[j].Path) })
	sort.Slice(rightItems, func(i, j int) bool { return strings.ToLower(rightItems[i].Path) < strings.ToLower(rightItems[j].Path) })
	for index := range leftItems {
		if leftItems[index] != rightItems[index] {
			return false
		}
	}
	return true
}

func safeAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func pathWithin(root, target string) bool {
	if !safeAbsolute(root) || !safeAbsolute(target) {
		return false
	}
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
