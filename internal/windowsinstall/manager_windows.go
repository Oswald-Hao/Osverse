//go:build windows

package windowsinstall

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
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const planLifetime = 10 * time.Minute

type storedPlan struct {
	public install.Plan
	item   artifact
	used   bool
}

type taskState struct {
	public install.Task
	cancel context.CancelFunc
}

type progressUpdate struct {
	phase    string
	progress int
	message  string
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
	runner   platform.CommandRunner
	client   func(proxyservice.Protocol, int) (*http.Client, error)
}

func NewManager(home string) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) || filepath.Clean(resolved) == filepath.VolumeName(resolved)+string(filepath.Separator) {
		return nil, install.ErrInvalidHome
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return nil, install.ErrInvalidHome
	}
	catalog, err := builtInCatalog()
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		home: filepath.Clean(resolved), arch: runtime.GOARCH, now: time.Now, randomID: secureID,
		catalog: catalog, plans: make(map[string]*storedPlan), tasks: make(map[string]*taskState), active: make(map[string]string),
		runner: platformwindows.NewExecRunner(), client: proxyservice.NewHTTPClient,
	}
	return manager, nil
}

func (manager *Manager) CreatePlan(ctx context.Context, componentID string) (install.Plan, error) {
	if manager == nil {
		return install.Plan{}, install.ErrPlanUnavailable
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
		return install.Plan{}, install.ErrUnknownComponent
	}
	if manager.arch != item.Architecture {
		return install.Plan{}, install.ErrUnsupportedTarget
	}
	id, err := manager.randomID()
	if err != nil || id == "" {
		return install.Plan{}, errors.New("create Windows install plan identifier")
	}
	created := manager.now().UTC()
	root := filepath.Join(manager.home, "AppData", "Local", "Osverse")
	versionRoot := filepath.Join(root, "tools", item.ID, item.Version)
	binRoot := filepath.Join(manager.home, ".local", "bin")
	plan := install.Plan{
		ID: id, ComponentID: item.ID, Name: item.Name, Command: item.Command, Version: item.Version,
		DownloadBytes: item.DownloadBytes, CreatedAt: created, ExpiresAt: created.Add(planLifetime),
		Changes: []install.PlannedChange{
			{Kind: "download", Path: "registry.npmjs.org", Description: fmt.Sprintf("下载并校验 %s %s 的官方 Windows 制品", item.Name, item.Version)},
			{Kind: "directory", Path: versionRoot, Description: "写入 Osverse 不可变版本目录"},
			{Kind: "command", Path: filepath.Join(binRoot, item.Command+".cmd"), Description: "创建 Osverse 管理的命令入口"},
			{Kind: "registry", Path: `HKCU\Environment\Path`, Description: "确保新终端可找到用户命令目录"},
		},
	}
	for planID, stored := range manager.plans {
		if stored.used || !created.Before(stored.public.ExpiresAt) {
			delete(manager.plans, planID)
		}
	}
	manager.plans[id] = &storedPlan{public: clonePlan(plan), item: item}
	return clonePlan(plan), nil
}

func (manager *Manager) consumePlan(id string) (storedPlan, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	stored := manager.plans[id]
	if stored == nil || stored.used || !manager.now().Before(stored.public.ExpiresAt) {
		return storedPlan{}, install.ErrPlanUnavailable
	}
	stored.used = true
	result := *stored
	result.public = clonePlan(stored.public)
	result.item.VersionArgs = append([]string(nil), stored.item.VersionArgs...)
	return result, nil
}

func clonePlan(plan install.Plan) install.Plan {
	plan.Changes = append([]install.PlannedChange(nil), plan.Changes...)
	return plan
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
