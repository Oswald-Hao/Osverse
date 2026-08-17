//go:build linux

package install

import (
	"context"
	"errors"
	"strings"

	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

type taskState struct {
	public Task
	cancel context.CancelFunc
}

type progressUpdate struct {
	phase    string
	progress int
	message  string
}

// Start consumes a server-created plan and begins its transaction exactly once.
func (manager *Manager) Start(
	ctx context.Context,
	planID string,
	protocol proxyservice.Protocol,
	port int,
) (Task, error) {
	if manager == nil || manager.runner == nil || manager.client == nil {
		return Task{}, ErrPlanUnavailable
	}
	if protocol != "" {
		if _, err := proxyservice.NewHTTPClient(protocol, port); err != nil {
			return Task{}, err
		}
	}
	stored, err := manager.consumePlan(planID)
	if err != nil {
		return Task{}, err
	}
	taskID, err := manager.randomID()
	if err != nil || taskID == "" {
		return Task{}, errors.New("create task identifier")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskContext, cancel := context.WithCancel(ctx)
	started := manager.now().UTC()
	task := Task{
		ID: taskID, PlanID: stored.public.ID, ComponentID: stored.public.ComponentID,
		Phase: "queued", Progress: 0, Message: "等待安装", StartedAt: started,
	}
	manager.mu.Lock()
	if _, active := manager.active[task.ComponentID]; active {
		if plan := manager.plans[planID]; plan != nil {
			plan.used = false
		}
		manager.mu.Unlock()
		cancel()
		return Task{}, ErrInstallActive
	}
	manager.tasks[taskID] = &taskState{public: task, cancel: cancel}
	manager.active[task.ComponentID] = taskID
	manager.mu.Unlock()

	go manager.runTask(taskContext, taskID, stored, protocol, port)
	return task, nil
}

// Task returns a copy of the latest redacted task state.
func (manager *Manager) Task(id string) (Task, error) {
	if manager == nil || id == "" {
		return Task{}, ErrTaskUnavailable
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state, ok := manager.tasks[id]
	if !ok {
		return Task{}, ErrTaskUnavailable
	}
	return state.public, nil
}

// Cancel requests cancellation. Completed tasks remain unchanged.
func (manager *Manager) Cancel(id string) error {
	if manager == nil || id == "" {
		return ErrTaskUnavailable
	}
	manager.mu.Lock()
	state, ok := manager.tasks[id]
	if !ok {
		manager.mu.Unlock()
		return ErrTaskUnavailable
	}
	cancel := state.cancel
	terminal := terminalPhase(state.public.Phase)
	manager.mu.Unlock()
	if !terminal && cancel != nil {
		cancel()
	}
	return nil
}

func (manager *Manager) runTask(
	ctx context.Context,
	taskID string,
	stored storedPlan,
	protocol proxyservice.Protocol,
	port int,
) {
	err := manager.execute(ctx, stored, protocol, port, func(update progressUpdate) {
		manager.updateTask(taskID, update)
	})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[taskID]
	if state == nil {
		return
	}
	state.cancel = nil
	state.public.FinishedAt = manager.now().UTC()
	delete(manager.active, state.public.ComponentID)
	switch {
	case err == nil:
		state.public.Phase = "completed"
		state.public.Progress = 100
		state.public.Message = "安装完成"
		state.public.ErrorCode = ""
	case errors.Is(err, context.Canceled):
		state.public.Phase = "canceled"
		state.public.Message = "安装已取消，原版本未改变"
		state.public.ErrorCode = "INSTALL_CANCELED"
	default:
		state.public.Phase = "failed"
		state.public.Message = publicInstallFailure(err)
		state.public.ErrorCode = "INSTALL_FAILED"
	}
}

func (manager *Manager) updateTask(id string, update progressUpdate) {
	if !IsProgressTaskPhase(update.phase) {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[id]
	if state == nil || terminalPhase(state.public.Phase) {
		return
	}
	if update.progress < state.public.Progress {
		update.progress = state.public.Progress
	}
	if update.progress > 99 {
		update.progress = 99
	}
	state.public.Phase = update.phase
	state.public.Progress = update.progress
	state.public.Message = update.message
}

func terminalPhase(phase string) bool {
	return IsTerminalTaskPhase(phase)
}

func publicInstallFailure(err error) string {
	switch {
	case errors.Is(err, errExternalCommand):
		return "命令入口已被其他程序占用，未修改原有安装"
	case errors.Is(err, errHashMismatch):
		return "下载文件校验失败，未安装"
	case errors.Is(err, errVersionVerification):
		return "工具版本验证失败，未安装"
	case errors.Is(err, errUnsafeArchive):
		return "安装包内容不安全，已拒绝安装"
	case errors.Is(err, errDownload):
		return "下载失败，请检查网络或代理"
	default:
		message := "安装失败，原版本未改变"
		if strings.TrimSpace(message) == "" {
			return "安装失败"
		}
		return message
	}
}
