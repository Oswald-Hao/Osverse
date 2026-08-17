package copilotinstall

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const planLifetime = 10 * time.Minute

type storedPlan struct {
	public   install.Plan
	artifact artifact
	used     bool
}

type taskState struct {
	public install.Task
	cancel context.CancelFunc
}

type Manager struct {
	mu        sync.Mutex
	home      string
	goos      string
	goarch    string
	now       func() time.Time
	randomID  func() (string, error)
	client    func(proxyservice.Protocol, int) (*http.Client, error)
	plans     map[string]*storedPlan
	tasks     map[string]*taskState
	active    map[string]string
	executeFn func(context.Context, storedPlan, proxyservice.Protocol, int, func(string, int, string)) error
}

func NewManager(home string) (*Manager, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || !filepath.IsAbs(resolved) {
		return nil, install.ErrInvalidHome
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() || filepath.Clean(resolved) == volumeRoot(resolved) {
		return nil, install.ErrInvalidHome
	}
	if _, err := artifactForTarget(runtime.GOOS + "/" + runtime.GOARCH); err != nil {
		return nil, install.ErrUnsupportedTarget
	}
	manager := &Manager{
		home: filepath.Clean(resolved), goos: runtime.GOOS, goarch: runtime.GOARCH,
		now: time.Now, randomID: secureID, client: proxyservice.NewHTTPClient,
		plans: make(map[string]*storedPlan), tasks: make(map[string]*taskState), active: make(map[string]string),
	}
	manager.executeFn = manager.execute
	return manager, nil
}

func volumeRoot(path string) string {
	return filepath.Clean(filepath.VolumeName(path) + string(filepath.Separator))
}

func (manager *Manager) CreatePlan(ctx context.Context, id string) (install.Plan, error) {
	if manager == nil || manager.now == nil || manager.randomID == nil {
		return install.Plan{}, install.ErrPlanUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return install.Plan{}, err
	}
	if id != componentID {
		return install.Plan{}, install.ErrUnknownComponent
	}
	item, err := artifactForTarget(manager.goos + "/" + manager.goarch)
	if err != nil {
		return install.Plan{}, install.ErrUnsupportedTarget
	}
	planID, err := manager.randomID()
	if err != nil || planID == "" {
		return install.Plan{}, install.ErrPlanUnavailable
	}
	created := manager.now().UTC()
	paths := managedPathsFor(manager.home, manager.goos, copilotVersion)
	plan := install.Plan{
		ID: planID, ComponentID: componentID, Name: copilotName, Command: "copilot", Version: copilotVersion,
		DownloadBytes: item.Size, CreatedAt: created, ExpiresAt: created.Add(planLifetime),
		Changes: []install.PlannedChange{
			{Kind: "download", Path: "github.com/github/copilot-cli", Description: "下载并校验官方 GitHub Copilot CLI 独立运行时"},
			{Kind: "directory", Path: paths.finalRoot, Description: "写入 Osverse 管理的不可变 Copilot CLI 版本"},
			{Kind: "command", Path: paths.shimPath, Description: "创建 copilot 命令入口并关闭上游自动更新"},
		},
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for key, value := range manager.plans {
		if value.used || !created.Before(value.public.ExpiresAt) {
			delete(manager.plans, key)
		}
	}
	manager.plans[planID] = &storedPlan{public: clonePlan(plan), artifact: item}
	return clonePlan(plan), nil
}

func (manager *Manager) Start(ctx context.Context, planID string, protocol proxyservice.Protocol, port int) (install.Task, error) {
	if manager == nil || manager.executeFn == nil {
		return install.Task{}, install.ErrPlanUnavailable
	}
	if protocol != "" {
		if _, err := proxyservice.NewHTTPClient(protocol, port); err != nil {
			return install.Task{}, err
		}
	}
	manager.mu.Lock()
	stored := manager.plans[planID]
	if stored == nil || stored.used || !manager.now().Before(stored.public.ExpiresAt) {
		manager.mu.Unlock()
		return install.Task{}, install.ErrPlanUnavailable
	}
	if _, exists := manager.active[componentID]; exists {
		manager.mu.Unlock()
		return install.Task{}, install.ErrInstallActive
	}
	taskID, err := manager.randomID()
	if err != nil || taskID == "" {
		manager.mu.Unlock()
		return install.Task{}, install.ErrPlanUnavailable
	}
	stored.used = true
	copy := storedPlan{public: clonePlan(stored.public), artifact: stored.artifact, used: true}
	if ctx == nil {
		ctx = context.Background()
	}
	taskContext, cancel := context.WithCancel(ctx)
	task := install.Task{ID: taskID, PlanID: planID, ComponentID: componentID, Phase: "queued", Message: "等待安装 GitHub Copilot CLI", StartedAt: manager.now().UTC()}
	manager.tasks[taskID] = &taskState{public: task, cancel: cancel}
	manager.active[componentID] = taskID
	manager.mu.Unlock()
	go manager.runTask(taskContext, taskID, copy, protocol, port)
	return task, nil
}

func (manager *Manager) runTask(ctx context.Context, taskID string, stored storedPlan, protocol proxyservice.Protocol, port int) {
	err := manager.executeFn(ctx, stored, protocol, port, func(phase string, progress int, message string) {
		if !install.IsProgressTaskPhase(phase) {
			return
		}
		manager.mu.Lock()
		defer manager.mu.Unlock()
		state := manager.tasks[taskID]
		if state == nil || terminalPhase(state.public.Phase) {
			return
		}
		if progress < state.public.Progress {
			progress = state.public.Progress
		}
		if progress > 99 {
			progress = 99
		}
		state.public.Phase, state.public.Progress, state.public.Message = phase, progress, message
	})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[taskID]
	if state == nil {
		return
	}
	state.cancel = nil
	state.public.FinishedAt = manager.now().UTC()
	delete(manager.active, componentID)
	switch {
	case err == nil:
		state.public.Phase, state.public.Progress, state.public.Message = "completed", 100, "GitHub Copilot CLI 安装完成"
	case errors.Is(err, context.Canceled):
		state.public.Phase, state.public.Message, state.public.ErrorCode = "canceled", "安装已取消，原版本未改变", "INSTALL_CANCELED"
	default:
		state.public.Phase, state.public.Message, state.public.ErrorCode = "failed", publicFailure(err), "INSTALL_FAILED"
	}
}

func (manager *Manager) Task(id string) (install.Task, error) {
	if manager == nil || id == "" {
		return install.Task{}, install.ErrTaskUnavailable
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[id]
	if state == nil {
		return install.Task{}, install.ErrTaskUnavailable
	}
	return state.public, nil
}

func (manager *Manager) Cancel(id string) error {
	if manager == nil || id == "" {
		return install.ErrTaskUnavailable
	}
	manager.mu.Lock()
	state := manager.tasks[id]
	if state == nil {
		manager.mu.Unlock()
		return install.ErrTaskUnavailable
	}
	cancel, terminal := state.cancel, terminalPhase(state.public.Phase)
	manager.mu.Unlock()
	if !terminal && cancel != nil {
		cancel()
	}
	return nil
}

func clonePlan(plan install.Plan) install.Plan {
	plan.Changes = append([]install.PlannedChange(nil), plan.Changes...)
	return plan
}

func terminalPhase(phase string) bool {
	return install.IsTerminalTaskPhase(phase)
}

func secureID() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
