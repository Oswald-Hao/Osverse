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
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const planLifetime = 10 * time.Minute

var (
	ErrUnknownComponent  = errors.New("unknown install component")
	ErrUnsupportedTarget = errors.New("unsupported install target")
	ErrInvalidHome       = errors.New("invalid user home")
	ErrPlanUnavailable   = errors.New("install plan unavailable")
)

// PlannedChange is one user-visible effect of an install plan.
type PlannedChange struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// Plan is a frontend-safe immutable preview. Network URLs and hashes remain
// backend-owned so the frontend cannot turn the installer into a downloader.
type Plan struct {
	ID            string          `json:"id"`
	ComponentID   string          `json:"componentId"`
	Name          string          `json:"name"`
	Command       string          `json:"command"`
	Version       string          `json:"version"`
	DownloadBytes int64           `json:"downloadBytes"`
	Changes       []PlannedChange `json:"changes"`
	CreatedAt     time.Time       `json:"createdAt"`
	ExpiresAt     time.Time       `json:"expiresAt"`
}

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
	manager.prunePlansLocked(created)
	manager.plans[id] = &storedPlan{public: clonePlan(plan), artifact: item}
	return clonePlan(plan), nil
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
