//go:build windows

package windowsapps

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/detect"
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
	packages detect.WindowsPackageQuery
}

func NewManager(home string) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) || strings.ContainsAny(resolved, "\x00\r\n") ||
		filepath.Clean(resolved) == filepath.VolumeName(resolved)+string(filepath.Separator) {
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
	return &Manager{
		home: filepath.Clean(resolved), arch: runtime.GOARCH, now: time.Now, randomID: secureID,
		catalog: catalog, plans: map[string]*storedPlan{}, tasks: map[string]*taskState{}, active: map[string]string{},
		runner: platformwindows.NewExecRunner(), client: proxyservice.NewHTTPClient, packages: detect.RegistryPackageQuery{},
	}, nil
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
	if manager.arch != "amd64" {
		return install.Plan{}, install.ErrUnsupportedTarget
	}
	id, err := manager.randomID()
	if err != nil || id == "" {
		return install.Plan{}, install.ErrPlanUnavailable
	}
	created := manager.now().UTC()
	plan := install.Plan{ID: id, ComponentID: item.ID, Name: item.Name, Command: item.ID, Version: item.Version,
		DownloadBytes: item.DownloadBytes, CreatedAt: created, ExpiresAt: created.Add(planLifetime)}
	if item.Kind == "store" {
		plan.Changes = []install.PlannedChange{{Kind: "store", Path: item.StoreID, Description: "通过精确 Microsoft Store 产品 ID 安装 OpenAI 官方应用"}}
	} else {
		plan.Changes = []install.PlannedChange{
			{Kind: "download", Path: artifactHost(item.URL), Description: fmt.Sprintf("下载并校验 %s %s 官方安装包", item.Name, item.Version)},
			{Kind: "verify", Path: item.SHA256, Description: "核对固定 SHA-256 和安装包格式"},
			{Kind: "installer", Path: "current-user", Description: "以静默模式安装到当前 Windows 用户"},
		}
	}
	for key, stored := range manager.plans {
		if stored.used || !created.Before(stored.public.ExpiresAt) {
			delete(manager.plans, key)
		}
	}
	manager.plans[id] = &storedPlan{public: clonePlan(plan), item: cloneArtifact(item)}
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
	return storedPlan{public: clonePlan(stored.public), item: cloneArtifact(stored.item), used: true}, nil
}

func clonePlan(plan install.Plan) install.Plan {
	plan.Changes = append([]install.PlannedChange(nil), plan.Changes...)
	return plan
}

func cloneArtifact(item artifact) artifact {
	item.SilentArgs = append([]string(nil), item.SilentArgs...)
	item.ExpectedPaths = append([]string(nil), item.ExpectedPaths...)
	return item
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "winapp-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func artifactHost(value string) string {
	if len(value) > 8 && value[:8] == "https://" {
		value = value[8:]
	}
	for index, character := range value {
		if character == '/' {
			return value[:index]
		}
	}
	return value
}
