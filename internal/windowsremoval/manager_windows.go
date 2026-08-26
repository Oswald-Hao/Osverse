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

const (
	planLifetime            = 3 * time.Minute
	transientMoveAttempts   = 26
	transientMoveRetryDelay = 200 * time.Millisecond
)

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

type recoveryCandidate struct {
	path        string
	description string
	required    bool
	withinHome  bool
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
	mu           sync.Mutex
	home         string
	now          func() time.Time
	randomID     func() (string, error)
	plans        map[string]*storedPlan
	runner       platform.CommandRunner
	moveAttempts int
	moveDelay    time.Duration
	wait         func(context.Context, time.Duration) error
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
		moveAttempts: transientMoveAttempts, moveDelay: transientMoveRetryDelay, wait: waitForRemovalRetry,
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
	cliRecovery := component.Category == "Core CLI" && removableCLIStatus(component.Status)
	if manager == nil || !known || component.Category != rule.category ||
		(component.Category == "Core CLI" && !cliRecovery) ||
		(component.Category != "Core CLI" && component.Status != domain.StatusInstalled &&
			component.Status != domain.StatusUpdateAvailable && component.Status != domain.StatusConflict) {
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
		if err != nil {
			return removal.Plan{}, errors.Join(removal.ErrRemovalUnsupported, err)
		}
		return removal.Plan{}, removal.ErrRemovalUnsupported
	}
	warning := "选中的当前用户命令入口和 Osverse 管理的程序文件将移入恢复区；入口指向的外部运行时、API 配置、凭据和会话不会删除。"
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
	candidates := make([]recoveryCandidate, 0, len(component.Installations)+2)
	// A fixed path plus the exact component marker is sufficient ownership for
	// a recoverable move. The referenced command is deliberately not followed:
	// older shims can point at an external npm runtime that belongs to the user.
	if markedManagedShim(shim, component.ID) {
		candidates = append(candidates, recoveryCandidate{
			path: shim, description: "将 Osverse 命令入口移入恢复区",
		})
	}
	candidates = append(candidates, recoveryCandidate{
		path: toolRoot, description: "将 Osverse 管理的程序文件移入恢复区",
	})

	// A fresh scan is allowed to nominate an exact per-user command entry even
	// when it was installed by npm or another tool. Only that entry is moved;
	// its package/runtime and all profiles remain untouched.
	for _, installation := range component.Installations {
		path := filepath.Clean(installation.Path)
		if !recoverableScannedEntry(manager.home, path) || pathWithin(toolRoot, path) || hasCandidate(candidates, path) {
			continue
		}
		candidates = append(candidates, recoveryCandidate{
			path: path, description: "将检测到的当前用户命令入口移入恢复区", required: true, withinHome: true,
		})
	}
	return manager.captureRecoveryCandidates(candidates, planID)
}

func (manager *Manager) captureRecoveryCandidates(candidates []recoveryCandidate, planID string) ([]capturedPath, []removal.Effect, error) {
	paths := make([]capturedPath, 0, len(candidates))
	effects := make([]removal.Effect, 0, len(candidates)+1)
	for _, candidate := range candidates {
		evidence, err := platformwindows.OpenMovableEvidence(candidate.path)
		if errors.Is(err, os.ErrNotExist) && !candidate.required {
			continue
		}
		if err != nil {
			for _, captured := range paths {
				_ = captured.evidence.Close()
			}
			return nil, nil, err
		}
		if candidate.withinHome && !pathWithin(manager.home, filepath.Clean(evidence.Path())) {
			_ = evidence.Close()
			for _, captured := range paths {
				_ = captured.evidence.Close()
			}
			return nil, nil, errors.New("scanned command entry escaped the user profile")
		}
		paths = append(paths, capturedPath{original: candidate.path, evidence: evidence})
		effects = append(effects, removal.Effect{
			Action: "recover", Path: candidate.path, Description: candidate.description, Recoverable: true,
		})
	}
	if len(paths) == 0 {
		return nil, nil, errors.New("no recoverable per-user command entries or Osverse residual paths were found")
	}
	recovery := filepath.Join(manager.home, "AppData", "Local", "Osverse", "recovery", planID)
	effects = append(effects, removal.Effect{Action: "manifest", Path: filepath.Join(recovery, "recovery.json"), Description: "记录原始路径以便恢复", Recoverable: true})
	return paths, effects, nil
}

func removableCLIStatus(status domain.ComponentStatus) bool {
	switch status {
	case domain.StatusInstalled, domain.StatusUpdateAvailable, domain.StatusConflict, domain.StatusBroken, domain.StatusFailed:
		return true
	default:
		return false
	}
}

// markedManagedShim deliberately accepts an incomplete or edited shim. The
// fixed per-user path and exact first-line ownership marker identify a residue;
// recovery never follows or removes the target referenced by the batch file.
func markedManagedShim(path, componentID string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16*1024 {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	return len(lines) > 0 && lines[0] == "@rem Osverse managed shim v1: "+componentID
}

func recoverableScannedEntry(home, path string) bool {
	if !safeAbsolute(path) || !pathWithin(home, path) {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".exe", ".com":
	default:
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && pathWithin(home, filepath.Clean(resolved))
}

func hasCandidate(candidates []recoveryCandidate, path string) bool {
	for _, candidate := range candidates {
		if samePath(candidate.path, path) {
			return true
		}
	}
	return false
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
	if !sameRemovalTarget(stored.component, current) {
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
		if errors.Is(err, platformwindows.ErrMoveInUse) {
			return removal.Result{}, removal.ErrComponentInUse
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
		if err := retryTransientMove(ctx, manager.moveAttempts, manager.moveDelay, manager.wait, func() error {
			return captured.evidence.MoveTo(destination)
		}); err != nil {
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

func retryTransientMove(ctx context.Context, attempts int, delay time.Duration, wait func(context.Context, time.Duration) error, move func() error) error {
	if attempts < 1 || delay < 0 || wait == nil || move == nil {
		return errors.New("invalid transient move retry")
	}
	for attempt := 0; attempt < attempts; attempt++ {
		err := move()
		if err == nil || !errors.Is(err, platformwindows.ErrMoveInUse) || attempt == attempts-1 {
			return err
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
	return errors.New("transient move retry exhausted")
}

func waitForRemovalRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	var pinnedExecutable *os.File
	releasePinnedAfterStart := false
	if stored.rule.uninstallKind == "exe" {
		evidence = stored.uninstaller
		if !evidence.Unchanged(path) {
			return removal.ErrEvidenceChanged
		}
		pinnedExecutable = evidence.TakeFile()
		if pinnedExecutable == nil {
			return removal.ErrRemovalFailed
		}
		releasePinnedAfterStart = true
	} else {
		var err error
		evidence, err = platformwindows.OpenExecutableEvidence(path)
		if err != nil {
			return err
		}
		defer evidence.Close()
	}
	result, runErr := manager.runner.Run(ctx, platform.CommandRequest{
		Path: path, PinnedExecutable: pinnedExecutable, ReleasePinnedAfterStart: releasePinnedAfterStart,
		Args: args, Timeout: 20 * time.Minute, OutputLimit: 256 * 1024,
	})
	if (!releasePinnedAfterStart && !evidence.Unchanged(path)) || result.TimedOut || result.Truncated || (result.ExitCode != 0 && result.ExitCode != 3010) {
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

func sameRemovalTarget(left, right domain.Component) bool {
	if left.ID != right.ID || left.Category != right.Category || len(left.Installations) != len(right.Installations) {
		return false
	}
	leftItems := append([]domain.Installation(nil), left.Installations...)
	rightItems := append([]domain.Installation(nil), right.Installations...)
	sort.Slice(leftItems, func(i, j int) bool { return strings.ToLower(leftItems[i].Path) < strings.ToLower(leftItems[j].Path) })
	sort.Slice(rightItems, func(i, j int) bool { return strings.ToLower(rightItems[i].Path) < strings.ToLower(rightItems[j].Path) })
	for index := range leftItems {
		if !samePath(leftItems[index].Path, rightItems[index].Path) ||
			!samePath(leftItems[index].ResolvedPath, rightItems[index].ResolvedPath) {
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
	if strings.EqualFold(filepath.Clean(left), filepath.Clean(right)) {
		return true
	}
	leftInfo, leftErr := os.Lstat(left)
	rightInfo, rightErr := os.Lstat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}
