//go:build windows

package windowsapps

import (
	"context"
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func (manager *Manager) Start(ctx context.Context, planID string, protocol proxyservice.Protocol, port int) (install.Task, error) {
	if manager == nil || manager.runner == nil || manager.client == nil {
		return install.Task{}, install.ErrPlanUnavailable
	}
	if protocol != "" {
		if _, err := proxyservice.NewHTTPClient(protocol, port); err != nil {
			return install.Task{}, err
		}
	}
	stored, err := manager.consumePlan(planID)
	if err != nil {
		return install.Task{}, err
	}
	id, err := manager.randomID()
	if err != nil || id == "" {
		return install.Task{}, install.ErrTaskUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskContext, cancel := context.WithCancel(ctx)
	task := install.Task{ID: id, PlanID: stored.public.ID, ComponentID: stored.public.ComponentID,
		Phase: "queued", Message: "等待安装", StartedAt: manager.now().UTC()}
	manager.mu.Lock()
	if _, active := manager.active[task.ComponentID]; active {
		if plan := manager.plans[planID]; plan != nil {
			plan.used = false
		}
		manager.mu.Unlock()
		cancel()
		return install.Task{}, install.ErrInstallActive
	}
	manager.tasks[id] = &taskState{public: task, cancel: cancel}
	manager.active[task.ComponentID] = id
	manager.mu.Unlock()
	go manager.runTask(taskContext, id, stored, protocol, port)
	return task, nil
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

func (manager *Manager) runTask(ctx context.Context, id string, stored storedPlan, protocol proxyservice.Protocol, port int) {
	err := manager.execute(ctx, stored.item, protocol, port, func(update progressUpdate) { manager.updateTask(id, update) })
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.tasks[id]
	if state == nil {
		return
	}
	state.cancel = nil
	state.public.FinishedAt = manager.now().UTC()
	delete(manager.active, state.public.ComponentID)
	switch {
	case err == nil:
		state.public.Phase, state.public.Progress, state.public.Message = "completed", 100, "桌面应用安装完成"
	case errors.Is(err, context.Canceled):
		state.public.Phase, state.public.Message, state.public.ErrorCode = "canceled", "安装已取消", "INSTALL_CANCELED"
	default:
		state.public.Phase, state.public.Message, state.public.ErrorCode = "failed", publicFailure(err), "INSTALL_FAILED"
	}
}

func (manager *Manager) updateTask(id string, update progressUpdate) {
	if !install.IsProgressTaskPhase(update.phase) {
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
	state.public.Phase, state.public.Progress, state.public.Message = update.phase, update.progress, update.message
}

func terminalPhase(phase string) bool {
	return install.IsTerminalTaskPhase(phase)
}
