//go:build linux

package apps

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const planLifetime = 10 * time.Minute

var (
	ErrUnknownComponent  = errors.New("unknown desktop component")
	ErrUnsupportedTarget = errors.New("unsupported desktop target")
	ErrPlanUnavailable   = errors.New("desktop install plan unavailable")
	ErrTaskUnavailable   = errors.New("desktop install task unavailable")
	ErrInstallActive     = errors.New("desktop install already active")
	ErrExternalEntry     = errors.New("external desktop entry exists")
)

type storedPlan struct {
	public install.Plan
	item   artifact
	used   bool
}

type taskState struct {
	public install.Task
	cancel context.CancelFunc
}

type launcher interface {
	Start(string) error
}

type Manager struct {
	mu       sync.Mutex
	home     string
	arch     string
	now      func() time.Time
	randomID func() (string, error)
	catalog  map[string]artifact
	plans    map[string]*storedPlan
	tasks    map[string]*taskState
	active   map[string]string
	client   func(proxyservice.Protocol, int) (*http.Client, error)
	launcher launcher
}

func NewManager(home string) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !validHome(resolved) {
		return nil, errors.New("invalid user home")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, errors.New("invalid user home")
	}
	catalog, err := builtInCatalog()
	if err != nil {
		return nil, err
	}
	manager := newManager(resolved, runtime.GOARCH, catalog, time.Now, secureID)
	manager.client = proxyservice.NewHTTPClient
	manager.launcher = processLauncher{}
	return manager, nil
}

func newManager(home, arch string, catalog map[string]artifact, now func() time.Time, randomID func() (string, error)) *Manager {
	return &Manager{
		home: filepath.Clean(home), arch: arch, catalog: catalog, now: now, randomID: randomID,
		plans: map[string]*storedPlan{}, tasks: map[string]*taskState{}, active: map[string]string{},
	}
}

func validHome(home string) bool {
	return home != "" && filepath.IsAbs(home) && filepath.Clean(home) != string(filepath.Separator) &&
		!strings.ContainsAny(home, "\x00\r\n")
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "app-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func (manager *Manager) CreatePlan(ctx context.Context, componentID string) (install.Plan, error) {
	if manager == nil || manager.now == nil || manager.randomID == nil || !validHome(manager.home) {
		return install.Plan{}, ErrPlanUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return install.Plan{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	item, ok := manager.catalog[componentID]
	if !ok {
		return install.Plan{}, ErrUnknownComponent
	}
	if manager.arch != item.Architecture {
		return install.Plan{}, ErrUnsupportedTarget
	}
	id, err := manager.randomID()
	if err != nil || !strings.HasPrefix(id, "app-") {
		return install.Plan{}, ErrPlanUnavailable
	}
	created := manager.now().UTC()
	root := filepath.Join(manager.home, ".local", "share", "osverse", "apps", item.ID)
	plan := install.Plan{
		ID: id, ComponentID: item.ID, Name: item.Name, Command: item.Command,
		Version: item.Version, DownloadBytes: item.DownloadBytes, CreatedAt: created, ExpiresAt: created.Add(planLifetime),
		Changes: []install.PlannedChange{
			{Kind: "download", Path: "github.com", Description: "下载并校验官方 AppImage"},
			{Kind: "directory", Path: filepath.Join(root, item.Version), Description: "写入不可变版本目录"},
			{Kind: "symlink", Path: filepath.Join(root, "current"), Description: "原子切换 Osverse 当前版本"},
			{Kind: "command", Path: filepath.Join(manager.home, ".local", "bin", item.Command), Description: "创建用户级应用入口"},
			{Kind: "desktop", Path: filepath.Join(manager.home, ".local", "share", "applications", item.DesktopFile), Description: "创建桌面菜单入口"},
		},
	}
	for key, value := range manager.plans {
		if value.used || !created.Before(value.public.ExpiresAt) {
			delete(manager.plans, key)
		}
	}
	manager.plans[id] = &storedPlan{public: clonePlan(plan), item: item}
	return clonePlan(plan), nil
}

func clonePlan(plan install.Plan) install.Plan {
	plan.Changes = append([]install.PlannedChange(nil), plan.Changes...)
	return plan
}

func (manager *Manager) consumePlan(id string) (storedPlan, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	value, ok := manager.plans[id]
	if !ok || value.used || !manager.now().Before(value.public.ExpiresAt) {
		return storedPlan{}, ErrPlanUnavailable
	}
	value.used = true
	return *value, nil
}
