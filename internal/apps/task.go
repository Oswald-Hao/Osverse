package apps

import (
	"context"
	"errors"

	"github.com/Oswald-Hao/Osverse/internal/install"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

type progressUpdate struct {
	phase string
	value int
	text  string
}

func (manager *Manager) Start(ctx context.Context, planID string, protocol proxyservice.Protocol, port int) (install.Task, error) {
	if manager == nil || manager.client == nil {
		return install.Task{}, ErrPlanUnavailable
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
		return install.Task{}, ErrTaskUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	taskCtx, cancel := context.WithCancel(ctx)
	task := install.Task{ID: id, PlanID: planID, ComponentID: stored.item.ID, Phase: "queued", Message: "等待安装", StartedAt: manager.now().UTC()}
	manager.mu.Lock()
	if _, exists := manager.active[stored.item.ID]; exists {
		if plan := manager.plans[planID]; plan != nil {
			plan.used = false
		}
		manager.mu.Unlock()
		cancel()
		return install.Task{}, ErrInstallActive
	}
	manager.tasks[id] = &taskState{public: task, cancel: cancel}
	manager.active[stored.item.ID] = id
	manager.mu.Unlock()
	go manager.run(taskCtx, id, stored, protocol, port)
	return task, nil
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
	if !ok {
		manager.mu.Unlock()
		return ErrTaskUnavailable
	}
	cancel := state.cancel
	terminal := terminal(state.public.Phase)
	manager.mu.Unlock()
	if !terminal && cancel != nil {
		cancel()
	}
	return nil
}

func (manager *Manager) run(ctx context.Context, id string, stored storedPlan, protocol proxyservice.Protocol, port int) {
	err := manager.execute(ctx, stored.item, protocol, port, func(update progressUpdate) {
		manager.mu.Lock()
		if state := manager.tasks[id]; state != nil && !terminal(state.public.Phase) {
			if update.value < state.public.Progress {
				update.value = state.public.Progress
			}
			if update.value > 99 {
				update.value = 99
			}
			state.public.Phase, state.public.Progress, state.public.Message = update.phase, update.value, update.text
		}
		manager.mu.Unlock()
	})
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
		state.public.Phase, state.public.Progress, state.public.Message = "completed", 100, "安装完成"
	case errors.Is(err, context.Canceled):
		state.public.Phase, state.public.Message, state.public.ErrorCode = "canceled", "安装已取消，原版本未改变", "INSTALL_CANCELED"
	default:
		state.public.Phase, state.public.Message, state.public.ErrorCode = "failed", publicFailure(err), "INSTALL_FAILED"
	}
}

func terminal(phase string) bool {
	return phase == "completed" || phase == "failed" || phase == "canceled"
}

func publicFailure(err error) string {
	switch {
	case errors.Is(err, ErrExternalEntry):
		return "应用入口已被其他程序占用，未修改原有安装"
	case errors.Is(err, errHashMismatch):
		return "下载文件校验失败，未安装"
	case errors.Is(err, errInvalidImage):
		return "安装包格式无效，未安装"
	case errors.Is(err, errDownload):
		return "下载失败，请检查网络或代理"
	default:
		return "安装失败，原版本未改变"
	}
}
