//go:build linux

// Package systeminstall owns the narrow privileged Claude Desktop workflow.
package systeminstall

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	platformlinux "github.com/Oswald-Hao/Osverse/internal/platform/linux"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

const (
	componentID = "claude-desktop"
	planTTL     = 10 * time.Minute
)

var (
	ErrUnknownComponent  = errors.New("unknown system component")
	ErrUnsupportedTarget = errors.New("unsupported system target")
	ErrPlanUnavailable   = errors.New("system install plan unavailable")
	ErrTaskUnavailable   = errors.New("system install task unavailable")
	ErrExternalEntry     = errors.New("external system configuration exists")
)

type systemProbe interface {
	Probe(context.Context) (domain.SystemInfo, error)
}

type planState struct {
	public install.Plan
	used   bool
}
type taskState struct {
	public install.Task
	cancel context.CancelFunc
}

type Manager struct {
	mu         sync.Mutex
	now        func() time.Time
	randomID   func() (string, error)
	probe      systemProbe
	runner     platform.CommandRunner
	executable string
	plans      map[string]*planState
	tasks      map[string]*taskState
	active     bool
}

func NewManager() (*Manager, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil || !filepath.IsAbs(executable) {
		return nil, errors.New("invalid Osverse executable")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid Osverse executable")
	}
	return &Manager{
		now: time.Now, randomID: secureID, probe: platformlinux.NewSystemProbe(), runner: platformlinux.NewExecRunner(), executable: executable,
		plans: map[string]*planState{}, tasks: map[string]*taskState{},
	}, nil
}

func secureID() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return "system-" + base64.RawURLEncoding.EncodeToString(data), nil
}

func (manager *Manager) CreatePlan(ctx context.Context, id string) (install.Plan, error) {
	if manager == nil || manager.probe == nil || manager.now == nil || manager.randomID == nil {
		return install.Plan{}, ErrPlanUnavailable
	}
	if id != componentID {
		return install.Plan{}, ErrUnknownComponent
	}
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := manager.probe.Probe(ctx)
	if err != nil {
		return install.Plan{}, err
	}
	if info.Version != "22.04" || info.Architecture != "x86_64" || !info.Supported {
		return install.Plan{}, ErrUnsupportedTarget
	}
	idValue, err := manager.randomID()
	if err != nil || idValue == "" {
		return install.Plan{}, ErrPlanUnavailable
	}
	created := manager.now().UTC()
	plan := install.Plan{
		ID: idValue, ComponentID: componentID, Name: "Claude Desktop", Command: "claude-desktop", Version: "APT stable",
		CreatedAt: created, ExpiresAt: created.Add(planTTL),
		Changes: []install.PlannedChange{
			{Kind: "privilege", Path: "/usr/bin/pkexec", Description: "请求一次系统管理员授权"},
			{Kind: "keyring", Path: "/usr/share/keyrings/claude-desktop-archive-keyring.asc", Description: "验证并安装 Anthropic 官方签名密钥"},
			{Kind: "repository", Path: "/etc/apt/sources.list.d/claude-desktop.list", Description: "配置 Anthropic 官方稳定版 APT 源"},
			{Kind: "package", Path: "claude-desktop", Description: "通过 APT 安装或升级官方软件包"},
		},
	}
	manager.mu.Lock()
	for key, value := range manager.plans {
		if value.used || !created.Before(value.public.ExpiresAt) {
			delete(manager.plans, key)
		}
	}
	manager.plans[plan.ID] = &planState{public: plan}
	manager.mu.Unlock()
	return clonePlan(plan), nil
}

func clonePlan(plan install.Plan) install.Plan {
	plan.Changes = append([]install.PlannedChange(nil), plan.Changes...)
	return plan
}

func (manager *Manager) Start(ctx context.Context, planID string, protocol proxyservice.Protocol, port int) (install.Task, error) {
	if manager == nil || manager.runner == nil {
		return install.Task{}, ErrPlanUnavailable
	}
	if protocol != "" {
		if _, err := proxyservice.NewHTTPClient(protocol, port); err != nil {
			return install.Task{}, err
		}
	}
	manager.mu.Lock()
	plan, ok := manager.plans[planID]
	if !ok || plan.used || !manager.now().Before(plan.public.ExpiresAt) || manager.active {
		manager.mu.Unlock()
		return install.Task{}, ErrPlanUnavailable
	}
	plan.used = true
	manager.active = true
	manager.mu.Unlock()
	id, err := manager.randomID()
	if err != nil {
		manager.mu.Lock()
		manager.active = false
		plan.used = false
		manager.mu.Unlock()
		return install.Task{}, ErrTaskUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskCtx, cancel := context.WithCancel(ctx)
	task := install.Task{ID: id, PlanID: planID, ComponentID: componentID, Phase: "queued", Message: "等待系统授权", StartedAt: manager.now().UTC()}
	manager.mu.Lock()
	manager.tasks[id] = &taskState{public: task, cancel: cancel}
	manager.mu.Unlock()
	go manager.run(taskCtx, id, protocol, port)
	return task, nil
}

func (manager *Manager) run(ctx context.Context, id string, protocol proxyservice.Protocol, port int) {
	manager.update(id, "committing", 10, "请在系统窗口中确认管理员授权")
	args := []string{manager.executable, privilegedFlag, privilegedAction}
	if protocol != "" {
		args = append(args, string(protocol), strconv.Itoa(port))
	}
	result, err := manager.runner.Run(ctx, platform.CommandRequest{
		Path: "/usr/bin/pkexec", Args: args, Timeout: 30 * time.Minute, OutputLimit: 128 * 1024,
	})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[id]
	manager.active = false
	if state == nil {
		return
	}
	state.cancel = nil
	state.public.FinishedAt = manager.now().UTC()
	switch {
	case err == nil && result.ExitCode == 0 && !result.TimedOut && !result.Truncated:
		state.public.Phase, state.public.Progress, state.public.Message = "completed", 100, "Claude Desktop 安装完成"
	case errors.Is(err, context.Canceled):
		state.public.Phase, state.public.Message, state.public.ErrorCode = "canceled", "安装已取消", "INSTALL_CANCELED"
	default:
		state.public.Phase, state.public.Message, state.public.ErrorCode = "failed", "系统安装未完成，原有 Claude Desktop 未被移除", "INSTALL_FAILED"
	}
}

func (manager *Manager) update(id, phase string, progress int, message string) {
	if !install.IsProgressTaskPhase(phase) {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if state := manager.tasks[id]; state != nil {
		state.public.Phase, state.public.Progress, state.public.Message = phase, progress, message
	}
}

func (manager *Manager) Task(id string) (install.Task, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state, ok := manager.tasks[id]
	if !ok {
		return install.Task{}, ErrTaskUnavailable
	}
	return state.public, nil
}

func (manager *Manager) Cancel(id string) error {
	manager.mu.Lock()
	state, ok := manager.tasks[id]
	manager.mu.Unlock()
	if !ok {
		return ErrTaskUnavailable
	}
	if state.cancel != nil {
		state.cancel()
	}
	return nil
}

// Remove delegates one fixed desktop package to the private privileged helper.
// It never accepts a package name from the frontend.
func (manager *Manager) Remove(ctx context.Context, componentID string) error {
	if manager == nil || manager.runner == nil || manager.executable == "" {
		return ErrPlanUnavailable
	}
	if _, ok := removableSystemPackages[componentID]; !ok {
		return ErrUnknownComponent
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := manager.runner.Run(ctx, platform.CommandRequest{
		Path:    "/usr/bin/pkexec",
		Args:    []string{manager.executable, privilegedFlag, privilegedRemoveAction, componentID},
		Timeout: 30 * time.Minute, OutputLimit: 128 * 1024,
	})
	if err != nil || result.ExitCode != 0 || result.TimedOut || result.Truncated {
		return errors.New("system package removal failed")
	}
	return nil
}
