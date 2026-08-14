// Package removal creates single-use removal previews and moves user-owned
// installations to the desktop Trash. System packages are delegated to a
// fixed privileged remover.
package removal

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

const planLifetime = 10 * time.Minute

var (
	ErrRemovalUnsupported = errors.New("component removal unsupported")
	ErrPlanUnavailable    = errors.New("removal plan unavailable")
	ErrEvidenceChanged    = errors.New("removal evidence changed")
	ErrRemovalFailed      = errors.New("component removal failed")
)

type Effect struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Recoverable bool   `json:"recoverable"`
}

type Plan struct {
	ID          string    `json:"id"`
	ComponentID string    `json:"componentId"`
	Name        string    `json:"name"`
	Effects     []Effect  `json:"effects"`
	Warning     string    `json:"warning"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type Result struct {
	PlanID      string `json:"planId"`
	ComponentID string `json:"componentId"`
	Removed     bool   `json:"removed"`
	Message     string `json:"message"`
}

type systemRemover interface {
	Remove(context.Context, string) error
}

type pathEvidence struct {
	path       string
	info       syscall.Stat_t
	linkTarget string
}

type storedPlan struct {
	public    Plan
	component domain.Component
	paths     []pathEvidence
	system    bool
	used      bool
}

type Manager struct {
	mu       sync.Mutex
	home     string
	system   systemRemover
	now      func() time.Time
	randomID func() (string, error)
	plans    map[string]*storedPlan
}

func NewManager(home string, system systemRemover) (*Manager, error) {
	return newManager(home, system, time.Now, secureID)
}

func newManager(home string, system systemRemover, now func() time.Time, randomID func() (string, error)) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !safeAbsolute(resolved) || filepath.Clean(resolved) == string(filepath.Separator) || now == nil || randomID == nil {
		return nil, ErrRemovalUnsupported
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, ErrRemovalUnsupported
	}
	return &Manager{home: filepath.Clean(resolved), system: system, now: now, randomID: randomID, plans: make(map[string]*storedPlan)}, nil
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "remove-" + base64.RawURLEncoding.EncodeToString(value), nil
}

type componentRule struct {
	category    string
	packageName string
	command     string
	desktopFile string
}

var componentRules = map[string]componentRule{
	"claude-code":      {category: "Core CLI", command: "claude"},
	"codex-cli":        {category: "Core CLI", command: "codex"},
	"opencode-cli":     {category: "Core CLI", command: "opencode"},
	"claude-desktop":   {category: "Desktop Applications", packageName: "claude-desktop", command: "claude-desktop", desktopFile: "claude-desktop.desktop"},
	"chatgpt-desktop":  {category: "Desktop Applications", packageName: "chatgpt-desktop", command: "chatgpt-desktop", desktopFile: "chatgpt-desktop.desktop"},
	"opencode-desktop": {category: "Desktop Applications", packageName: "opencode-desktop", command: "opencode-desktop", desktopFile: "opencode-desktop.desktop"},
	"cc-switch":        {category: "Management Tools", packageName: "cc-switch", command: "cc-switch", desktopFile: "cc-switch.desktop"},
	"cockpit-tools":    {category: "Management Tools", packageName: "cockpit-tools", command: "cockpit-tools", desktopFile: "cockpit-tools.desktop"},
}

func (manager *Manager) CreatePlan(ctx context.Context, component domain.Component) (Plan, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	rule, known := componentRules[component.ID]
	if manager == nil || !known || component.Category != rule.category || len(component.Installations) == 0 || len(component.Installations) > 16 ||
		(component.Status != domain.StatusInstalled && component.Status != domain.StatusUpdateAvailable && component.Status != domain.StatusConflict) {
		return Plan{}, ErrRemovalUnsupported
	}

	allDpkg := rule.packageName != ""
	for _, installation := range component.Installations {
		allDpkg = allDpkg && installation.Source == "dpkg"
	}
	var effects []Effect
	var evidence []pathEvidence
	system := false
	if allDpkg {
		if manager.system == nil {
			return Plan{}, ErrRemovalUnsupported
		}
		system = true
		effects = []Effect{{Action: "package", Path: rule.packageName, Description: "通过系统包管理器卸载固定软件包", Recoverable: false}}
	} else {
		var err error
		effects, evidence, err = manager.userEffects(component, rule)
		if err != nil || len(effects) == 0 {
			return Plan{}, ErrRemovalUnsupported
		}
	}
	id, err := manager.randomID()
	if err != nil || id == "" || strings.ContainsAny(id, "\x00\r\n/\\") {
		return Plan{}, ErrPlanUnavailable
	}
	created := manager.now().UTC()
	warning := "用户级入口将移至系统回收站，可手动恢复；配置、凭据和会话数据不会删除。"
	if system {
		warning = "系统包将通过管理员授权卸载；用户配置、凭据和会话数据不会删除。"
	}
	plan := Plan{ID: id, ComponentID: component.ID, Name: component.Name, Effects: append([]Effect(nil), effects...), Warning: warning, CreatedAt: created, ExpiresAt: created.Add(planLifetime)}
	manager.mu.Lock()
	for key, value := range manager.plans {
		if value.used || !created.Before(value.public.ExpiresAt) {
			delete(manager.plans, key)
		}
	}
	manager.plans[id] = &storedPlan{public: plan, component: cloneComponent(component), paths: evidence, system: system}
	manager.mu.Unlock()
	return clonePlan(plan), nil
}

func (manager *Manager) userEffects(component domain.Component, rule componentRule) ([]Effect, []pathEvidence, error) {
	seen := make(map[string]bool)
	var effects []Effect
	var evidence []pathEvidence
	add := func(path, description string, allowDirectory bool) error {
		path = filepath.Clean(path)
		if seen[path] {
			return nil
		}
		captured, err := manager.capture(path, allowDirectory)
		if err != nil {
			return err
		}
		seen[path] = true
		effects = append(effects, Effect{Action: "trash", Path: path, Description: description, Recoverable: true})
		evidence = append(evidence, captured)
		return nil
	}
	managedAdded := false
	for _, installation := range component.Installations {
		if installation.Managed {
			kind := "tools"
			if component.Category != "Core CLI" {
				kind = "apps"
			}
			root := filepath.Join(manager.home, ".local", "share", "osverse", kind, component.ID)
			if !pathWithin(root, installation.ResolvedPath) {
				return nil, nil, ErrRemovalUnsupported
			}
			if err := add(installation.Path, "移除 Osverse 创建的启动入口", false); err != nil {
				return nil, nil, err
			}
			if !managedAdded {
				if rule.desktopFile != "" {
					desktop := filepath.Join(manager.home, ".local", "share", "applications", rule.desktopFile)
					if _, err := os.Lstat(desktop); err == nil {
						if err := add(desktop, "移除 Osverse 创建的桌面入口", false); err != nil {
							return nil, nil, err
						}
					} else if !errors.Is(err, os.ErrNotExist) {
						return nil, nil, err
					}
				}
				if err := add(root, "移除 Osverse 管理的程序文件", true); err != nil {
					return nil, nil, err
				}
				managedAdded = true
			}
			continue
		}
		if installation.Source == "dpkg" || !pathWithin(manager.home, installation.Path) {
			return nil, nil, ErrRemovalUnsupported
		}
		if err := add(installation.Path, "将检测到的用户级程序入口移至回收站", false); err != nil {
			return nil, nil, err
		}
	}
	if rule.desktopFile != "" && !managedAdded {
		desktop := filepath.Join(manager.home, ".local", "share", "applications", rule.desktopFile)
		if _, err := os.Lstat(desktop); err == nil {
			if err := add(desktop, "将用户级桌面入口移至回收站", false); err != nil {
				return nil, nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
	}
	return effects, evidence, nil
}

func (manager *Manager) capture(path string, allowDirectory bool) (pathEvidence, error) {
	if !safeAbsolute(path) || !pathWithin(manager.home, path) {
		return pathEvidence{}, ErrRemovalUnsupported
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil || !pathWithinOrEqual(manager.home, parent) {
		return pathEvidence{}, ErrRemovalUnsupported
	}
	info, err := os.Lstat(path)
	if err != nil {
		return pathEvidence{}, err
	}
	if (!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0) && !(allowDirectory && info.IsDir()) {
		return pathEvidence{}, ErrRemovalUnsupported
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return pathEvidence{}, ErrRemovalUnsupported
	}
	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err = os.Readlink(path)
		if err != nil {
			return pathEvidence{}, err
		}
	}
	return pathEvidence{path: path, info: *stat, linkTarget: linkTarget}, nil
}

func (manager *Manager) Execute(ctx context.Context, planID string, current domain.Component) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	manager.mu.Lock()
	stored, ok := manager.plans[planID]
	if !ok || stored.used || !manager.now().Before(stored.public.ExpiresAt) {
		manager.mu.Unlock()
		return Result{}, ErrPlanUnavailable
	}
	stored.used = true
	copy := *stored
	manager.mu.Unlock()
	if !sameComponent(copy.component, current) {
		return Result{}, ErrEvidenceChanged
	}
	if copy.system {
		if manager.system == nil || manager.system.Remove(ctx, copy.public.ComponentID) != nil {
			return Result{}, ErrRemovalFailed
		}
	} else if err := manager.moveToTrash(ctx, planID, copy.paths); err != nil {
		if errors.Is(err, ErrEvidenceChanged) || errors.Is(err, context.Canceled) {
			return Result{}, err
		}
		return Result{}, ErrRemovalFailed
	}
	return Result{PlanID: planID, ComponentID: copy.public.ComponentID, Removed: true, Message: "组件入口已移除，配置和用户数据已保留"}, nil
}

func (manager *Manager) moveToTrash(ctx context.Context, planID string, paths []pathEvidence) (returnErr error) {
	trashRoot, err := ensurePrivateDirectories(manager.home, ".local", "share", "Trash")
	if err != nil {
		return err
	}
	files, err := ensurePrivateDirectories(trashRoot, "files")
	if err != nil {
		return err
	}
	infoRoot, err := ensurePrivateDirectories(trashRoot, "info")
	if err != nil {
		return err
	}
	type movedPath struct{ source, destination, info string }
	moved := make([]movedPath, 0, len(paths))
	defer func() {
		if returnErr == nil {
			return
		}
		for index := len(moved) - 1; index >= 0; index-- {
			_ = os.Rename(moved[index].destination, moved[index].source)
			_ = os.Remove(moved[index].info)
		}
	}()
	for index, evidence := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := manager.capture(evidence.path, evidence.info.Mode&syscall.S_IFMT == syscall.S_IFDIR)
		if err != nil || !samePathEvidence(evidence, current) {
			return ErrEvidenceChanged
		}
		name := fmt.Sprintf("%s-%02d-%s", planID, index, filepath.Base(evidence.path))
		destination := filepath.Join(files, name)
		infoPath := filepath.Join(infoRoot, name+".trashinfo")
		if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
			return ErrRemovalFailed
		}
		contents := "[Trash Info]\nPath=" + url.PathEscape(evidence.path) + "\nDeletionDate=" + manager.now().Format("2006-01-02T15:04:05") + "\n"
		if err := writeExclusive(infoPath, []byte(contents)); err != nil {
			return err
		}
		if err := os.Rename(evidence.path, destination); err != nil {
			_ = os.Remove(infoPath)
			return err
		}
		moved = append(moved, movedPath{source: evidence.path, destination: destination, info: infoPath})
	}
	return nil
}

func writeExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	return file.Close()
}

func ensurePrivateDirectories(base string, components ...string) (string, error) {
	current := filepath.Clean(base)
	for _, component := range components {
		if component == "" || filepath.Base(component) != component || component == "." || component == ".." {
			return "", ErrRemovalFailed
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
		case !info.IsDir() || info.Mode()&os.ModeSymlink != 0:
			return "", ErrRemovalFailed
		}
		info, err = os.Lstat(current)
		if err != nil {
			return "", err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			return "", ErrRemovalFailed
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return "", err
		}
	}
	return current, nil
}

func clonePlan(plan Plan) Plan {
	plan.Effects = append([]Effect(nil), plan.Effects...)
	return plan
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
	sort.Slice(leftItems, func(i, j int) bool { return leftItems[i].Path < leftItems[j].Path })
	sort.Slice(rightItems, func(i, j int) bool { return rightItems[i].Path < rightItems[j].Path })
	for index := range leftItems {
		if leftItems[index] != rightItems[index] {
			return false
		}
	}
	return true
}

func samePathEvidence(left, right pathEvidence) bool {
	return left.path == right.path && left.linkTarget == right.linkTarget &&
		left.info.Dev == right.info.Dev && left.info.Ino == right.info.Ino && left.info.Mode == right.info.Mode &&
		left.info.Size == right.info.Size && left.info.Mtim == right.info.Mtim && left.info.Ctim == right.info.Ctim
}

func safeAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func pathWithin(root, target string) bool {
	return pathWithinOrEqual(root, target) && filepath.Clean(root) != filepath.Clean(target)
}

func pathWithinOrEqual(root, target string) bool {
	if !safeAbsolute(root) || !safeAbsolute(target) {
		return false
	}
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
