//go:build linux

package install

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const planLifetime = 10 * time.Minute

type storedPlan struct {
	public   Plan
	artifact artifact
	used     bool
}

// Manager owns server-created plans and installation tasks.
type Manager struct {
	mu          sync.Mutex
	home        string
	arch        string
	now         func() time.Time
	randomID    func() (string, error)
	catalog     map[string]artifact
	plans       map[string]*storedPlan
	tasks       map[string]*taskState
	active      map[string]string
	runner      platform.CommandRunner
	client      func(proxyservice.Protocol, int) (*http.Client, error)
	replaceLink func(string, string) error
	profiles    []string
}

// NewManager creates the production installer planner for one absolute home.
func NewManager(home string) (*Manager, error) {
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, ErrInvalidHome
	}
	info, err := os.Stat(resolvedHome)
	if err != nil || !info.IsDir() {
		return nil, ErrInvalidHome
	}
	value, err := builtInManifest()
	if err != nil {
		return nil, err
	}
	manager, err := newManager(resolvedHome, runtime.GOARCH, artifactCatalog(value), time.Now, secureID)
	if err != nil {
		return nil, err
	}
	manager.runner = platformlinux.NewExecRunner()
	manager.client = proxyservice.NewHTTPClient
	manager.replaceLink = replaceSymlink
	manager.profiles = shellProfiles(manager.home, os.Getenv("SHELL"))
	if err := manager.recoverTransactions(); err != nil {
		return nil, err
	}
	return manager, nil
}

func newManager(
	home, arch string,
	catalog map[string]artifact,
	now func() time.Time,
	randomID func() (string, error),
) (*Manager, error) {
	cleanHome, err := validatedHome(home)
	if err != nil {
		return nil, err
	}
	if now == nil || randomID == nil || len(catalog) == 0 {
		return nil, errors.New("installer dependencies unavailable")
	}
	return &Manager{
		home: cleanHome, arch: arch, now: now, randomID: randomID,
		catalog: catalog, plans: make(map[string]*storedPlan), tasks: make(map[string]*taskState),
		active:      make(map[string]string),
		replaceLink: replaceSymlink,
	}, nil
}

// CreatePlan accepts only a fixed catalog component ID and computes every
// filesystem effect beneath the validated user home.
func (manager *Manager) CreatePlan(ctx context.Context, componentID string) (Plan, error) {
	if manager == nil {
		return Plan{}, ErrPlanUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	item, ok := manager.catalog[componentID]
	if !ok {
		return Plan{}, ErrUnknownComponent
	}
	if manager.arch != item.Architecture {
		return Plan{}, ErrUnsupportedTarget
	}
	id, err := manager.randomID()
	if err != nil || id == "" {
		return Plan{}, errors.New("create plan identifier")
	}
	created := manager.now().UTC()
	toolRoot := filepath.Join(manager.home, ".local", "share", "osverse", "tools", item.ID)
	plan := Plan{
		ID: id, ComponentID: item.ID, Name: item.Name, Command: item.Command,
		Version: item.Version, DownloadBytes: item.DownloadBytes,
		CreatedAt: created, ExpiresAt: created.Add(planLifetime),
		Changes: []PlannedChange{
			{Kind: "download", Path: "registry.npmjs.org", Description: fmt.Sprintf("下载并校验 %s %s", item.Name, item.Version)},
			{Kind: "directory", Path: filepath.Join(toolRoot, item.Version), Description: "写入不可变版本目录"},
			{Kind: "symlink", Path: filepath.Join(toolRoot, "current"), Description: "切换 Osverse 当前版本"},
			{Kind: "command", Path: filepath.Join(manager.home, ".local", "bin", item.Command), Description: "创建 Osverse 管理的命令入口"},
		},
	}
	for _, profile := range manager.profiles {
		plan.Changes = append(plan.Changes, PlannedChange{
			Kind: "profile", Path: profile,
			Description: "备份并确保新终端可找到 ~/.local/bin",
		})
		plan.Changes = append(plan.Changes, PlannedChange{
			Kind: "backup", Path: manager.profileBackupPath(profile),
			Description: "保留修改前的 Shell 配置备份",
		})
	}
	manager.prunePlansLocked(created)
	manager.plans[id] = &storedPlan{public: clonePlan(plan), artifact: item}
	return clonePlan(plan), nil
}

func (manager *Manager) profileBackupPath(profile string) string {
	name := strings.TrimPrefix(filepath.Base(profile), ".")
	return filepath.Join(manager.home, ".local", "share", "osverse", "state", "profile-backups", name+".before-osverse")
}

func shellProfiles(home, shell string) []string {
	profiles := []string{filepath.Join(home, ".profile")}
	switch filepath.Base(shell) {
	case "bash":
		profiles = append(profiles, filepath.Join(home, ".bashrc"))
	case "zsh":
		profiles = append(profiles, filepath.Join(home, ".zprofile"), filepath.Join(home, ".zshrc"))
	}
	return profiles
}

func (manager *Manager) consumePlan(id string) (storedPlan, error) {
	if manager == nil || id == "" {
		return storedPlan{}, ErrPlanUnavailable
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	stored, ok := manager.plans[id]
	if !ok || stored.used || !manager.now().Before(stored.public.ExpiresAt) {
		return storedPlan{}, ErrPlanUnavailable
	}
	stored.used = true
	copy := *stored
	copy.public = clonePlan(stored.public)
	copy.artifact.VersionArgs = append([]string(nil), stored.artifact.VersionArgs...)
	return copy, nil
}

func (manager *Manager) prunePlansLocked(now time.Time) {
	for id, stored := range manager.plans {
		if stored.used || !now.Before(stored.public.ExpiresAt) {
			delete(manager.plans, id)
		}
	}
}

func clonePlan(plan Plan) Plan {
	plan.Changes = append([]PlannedChange(nil), plan.Changes...)
	return plan
}

func validatedHome(home string) (string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return "", ErrInvalidHome
	}
	clean := filepath.Clean(home)
	if clean == string(filepath.Separator) || clean == "." {
		return "", ErrInvalidHome
	}
	return clean, nil
}

func secureID() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
