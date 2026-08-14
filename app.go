package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	historyservice "github.com/Oswald-Hao/Osverse/internal/history"
	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/profiles"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
	"github.com/Oswald-Hao/Osverse/internal/removal"
)

// Scanner is the narrow read-only operation used by the Wails application.
type Scanner interface {
	Scan(context.Context) (domain.EnvironmentSnapshot, error)
}

// ProxyProber is the narrow loopback-only proxy operation used by the app.
type ProxyProber interface {
	Probe(context.Context, int) (proxyservice.Result, error)
}

// InstallPlanner accepts fixed component IDs and returns backend-owned plans.
type InstallPlanner interface {
	CreatePlan(context.Context, string) (install.Plan, error)
}

type InstallExecutor interface {
	Start(context.Context, string, proxyservice.Protocol, int) (install.Task, error)
	Task(string) (install.Task, error)
	Cancel(string) error
}

type AppLauncher interface {
	Launch(string) error
}

type ComponentLauncher interface {
	Launch(context.Context, domain.Component, string) error
}

type RemovalService interface {
	CreatePlan(context.Context, domain.Component) (removal.Plan, error)
	Execute(context.Context, string, domain.Component) (removal.Result, error)
}

type ProfileService interface {
	Save(context.Context, profiles.Input) (profiles.Profile, error)
	List(context.Context) ([]profiles.Profile, error)
	Delete(context.Context, string) error
	Probe(context.Context, string) (profiles.ProbeResult, error)
	Compatibility(string) ([]profiles.TargetCompatibility, error)
	CreateApplyPlan(context.Context, string, []string) (profiles.ApplyPlan, error)
	Apply(context.Context, string) (profiles.ApplyBatchResult, error)
}

type HistoryService interface {
	Append(context.Context, historyservice.Input) (historyservice.Entry, error)
	List(context.Context) ([]historyservice.Entry, error)
	Clear(context.Context) error
}

type proxySelection struct {
	Protocol proxyservice.Protocol
	Port     int
}

type App struct {
	mu                sync.RWMutex
	ctx               context.Context
	scanner           Scanner
	proxyProber       ProxyProber
	proxySelection    proxySelection
	proxyGeneration   uint64
	installPlanner    InstallPlanner
	installExecutor   InstallExecutor
	appPlanner        InstallPlanner
	appExecutor       InstallExecutor
	appLauncher       AppLauncher
	componentLauncher ComponentLauncher
	removal           RemovalService
	systemPlanner     InstallPlanner
	systemExecutor    InstallExecutor
	planOwners        map[string]string
	taskOwners        map[string]string
	profiles          ProfileService
	history           HistoryService
	recordedTasks     map[string]bool
	removalPlans      map[string]string
}

func NewApp(scanner Scanner) *App {
	home, err := os.UserHomeDir()
	var profileService ProfileService
	var history HistoryService
	if err == nil {
		profileService, _ = profiles.NewService(home)
		history, _ = historyservice.NewStore(home)
	}
	app := newAppWithServiceBundle(scanner, proxyservice.NewService(), nil, profileService)
	if app != nil {
		app.history = history
	}
	if err == nil && app != nil {
		configurePlatformServices(app, home)
	}
	return app
}

func newAppWithServices(scanner Scanner, proxyProber ProxyProber) *App {
	return newAppWithAllServices(scanner, proxyProber, nil)
}

func newAppWithAllServices(scanner Scanner, proxyProber ProxyProber, planner InstallPlanner) *App {
	return newAppWithServiceBundle(scanner, proxyProber, planner, nil)
}

func newAppWithServiceBundle(scanner Scanner, proxyProber ProxyProber, planner InstallPlanner, profileService ProfileService) *App {
	if scanner == nil || proxyProber == nil {
		return nil
	}
	app := &App{
		scanner: scanner, proxyProber: proxyProber, installPlanner: planner, profiles: profileService,
		planOwners: make(map[string]string), taskOwners: make(map[string]string),
		recordedTasks: make(map[string]bool),
		removalPlans:  make(map[string]string),
	}
	if executor, ok := planner.(InstallExecutor); ok {
		app.installExecutor = executor
	}
	return app
}

func (app *App) appContext() context.Context {
	if app == nil {
		return context.Background()
	}
	app.mu.RLock()
	ctx := app.ctx
	app.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// SaveAPIProfile validates and encrypts a profile. The returned value is redacted.
func (app *App) SaveAPIProfile(input profiles.Input) (profiles.Profile, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[profiles.Profile]()
	}
	profile, err := app.profiles.Save(app.appContext(), input)
	if err != nil {
		message := "无法保存 API 配置档案"
		if errors.Is(err, profiles.ErrInvalidProfile) {
			message = "档案名称、API Key、Base URL 或模型名无效"
		}
		return profiles.Profile{}, domain.NewPublicError(domain.ErrProfileFailed, message, err)
	}
	app.appendHistory(historyservice.Input{OperationID: "profile-save-" + profile.ID + "-" + profile.UpdatedAt.UTC().Format(time.RFC3339Nano), ComponentID: "api-profile", Name: "API 配置档案", Action: "profile-save", Status: "completed", Message: "档案已加密保存"})
	return profile, nil
}

func (app *App) ListAPIProfiles() ([]profiles.Profile, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[[]profiles.Profile]()
	}
	result, err := app.profiles.List(app.appContext())
	if err != nil {
		return nil, domain.NewPublicError(domain.ErrProfileFailed, "无法读取 API 配置档案", err)
	}
	return result, nil
}

func (app *App) DeleteAPIProfile(profileID string) error {
	if app == nil || app.profiles == nil {
		_, err := unavailableProfile[struct{}]()
		return err
	}
	if err := app.profiles.Delete(app.appContext(), profileID); err != nil {
		return domain.NewPublicError(domain.ErrProfileFailed, "无法删除 API 配置档案", err)
	}
	app.appendHistory(historyservice.Input{OperationID: "profile-delete-" + profileID, ComponentID: "api-profile", Name: "API 配置档案", Action: "profile-delete", Status: "completed", Message: "档案已删除"})
	return nil
}

func (app *App) ProbeAPIProfile(profileID string) (profiles.ProbeResult, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[profiles.ProbeResult]()
	}
	result, err := app.profiles.Probe(app.appContext(), profileID)
	if err != nil {
		message := "API 探测失败"
		if errors.Is(err, profiles.ErrPrivateEndpoint) {
			message = "该地址解析到私有网络，需要在档案中明确确认"
		}
		return profiles.ProbeResult{}, domain.NewPublicError(domain.ErrProfileFailed, message, err)
	}
	return result, nil
}

func (app *App) GetAPICompatibility(profileID string) ([]profiles.TargetCompatibility, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[[]profiles.TargetCompatibility]()
	}
	result, err := app.profiles.Compatibility(profileID)
	if err != nil {
		return nil, domain.NewPublicError(domain.ErrProfileFailed, "请先完成 API 协议探测", err)
	}
	return result, nil
}

func (app *App) CreateAPIApplyPlan(profileID string, targets []string) (profiles.ApplyPlan, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[profiles.ApplyPlan]()
	}
	plan, err := app.profiles.CreateApplyPlan(app.appContext(), profileID, targets)
	if err != nil {
		return profiles.ApplyPlan{}, domain.NewPublicError(
			domain.ErrProfileFailed, "无法创建 API 配置应用计划，请重新探测兼容性", err,
		)
	}
	return plan, nil
}

func (app *App) ApplyAPIPlan(planID string) (profiles.ApplyBatchResult, error) {
	if app == nil || app.profiles == nil {
		return unavailableProfile[profiles.ApplyBatchResult]()
	}
	result, err := app.profiles.Apply(app.appContext(), planID)
	if err != nil {
		return profiles.ApplyBatchResult{}, domain.NewPublicError(
			domain.ErrProfileFailed, "API 配置应用失败", err,
		)
	}
	status, message := "completed", "API 配置已应用"
	if result.Failed > 0 {
		status, message = "failed", "部分 API 配置目标未完成"
	}
	app.appendHistory(historyservice.Input{OperationID: result.PlanID, ComponentID: "api-profile", Name: "API 配置", Action: "configure", Status: status, Message: message})
	return result, nil
}

func unavailableProfile[T any]() (T, error) {
	var zero T
	return zero, domain.NewPublicError(domain.ErrProfileFailed, "API 配置服务不可用", nil)
}

// CreateInstallPlan previews an allowlisted CLI install without changing disk.
func (app *App) CreateInstallPlan(componentID string) (install.Plan, error) {
	if app == nil {
		return unavailableInstallPlan()
	}
	app.mu.RLock()
	ctx := app.ctx
	planner := app.installPlanner
	owner := "cli"
	if isUserDesktopComponent(componentID) && app.appPlanner != nil {
		planner = app.appPlanner
		owner = "app"
	} else if componentID == "claude-desktop" && app.systemPlanner != nil {
		planner = app.systemPlanner
		owner = "system"
	}
	app.mu.RUnlock()
	if planner == nil {
		return unavailableInstallPlan()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	plan, err := planner.CreatePlan(ctx, componentID)
	if err == nil {
		app.mu.Lock()
		app.planOwners[plan.ID] = owner
		app.mu.Unlock()
		return plan, nil
	}
	if isUnsupportedInstallError(err) {
		return install.Plan{}, domain.NewPublicError(
			domain.ErrInstallPlanFailed, "该工具暂不支持在当前系统安装", err,
		)
	}
	return install.Plan{}, domain.NewPublicError(
		domain.ErrInstallPlanFailed, "无法创建安装计划", err,
	)
}

// StartInstall is the explicit confirmation boundary. It accepts only a
// single-use plan ID previously returned by CreateInstallPlan.
func (app *App) StartInstall(planID string) (install.Task, error) {
	if app == nil {
		return unavailableInstallTask()
	}
	app.mu.RLock()
	ctx := app.ctx
	executor := app.installExecutor
	owner := app.planOwners[planID]
	if owner == "app" {
		executor = app.appExecutor
	} else if owner == "system" {
		executor = app.systemExecutor
	}
	selection := app.proxySelection
	app.mu.RUnlock()
	if executor == nil {
		return unavailableInstallTask()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := executor.Start(ctx, planID, selection.Protocol, selection.Port)
	if err != nil {
		return install.Task{}, domain.NewPublicError(
			domain.ErrInstallTaskFailed, "无法开始安装，请重新确认安装计划", err,
		)
	}
	app.mu.Lock()
	delete(app.planOwners, planID)
	app.taskOwners[task.ID] = owner
	app.mu.Unlock()
	return task, nil
}

// GetInstallTask returns one redacted progress snapshot.
func (app *App) GetInstallTask(taskID string) (install.Task, error) {
	if app == nil {
		return unavailableInstallTask()
	}
	app.mu.RLock()
	executor := app.installExecutor
	if app.taskOwners[taskID] == "app" {
		executor = app.appExecutor
	} else if app.taskOwners[taskID] == "system" {
		executor = app.systemExecutor
	}
	app.mu.RUnlock()
	if executor == nil {
		return unavailableInstallTask()
	}
	task, err := executor.Task(taskID)
	if err != nil {
		return install.Task{}, domain.NewPublicError(
			domain.ErrInstallTaskFailed, "无法读取安装进度", err,
		)
	}
	app.recordInstallHistory(task)
	return task, nil
}

func (app *App) appendHistory(input historyservice.Input) {
	if app == nil {
		return
	}
	app.mu.RLock()
	service := app.history
	app.mu.RUnlock()
	if service != nil {
		_, _ = service.Append(app.appContext(), input)
	}
}

func (app *App) recordInstallHistory(task install.Task) {
	if !terminalInstallPhase(task.Phase) {
		return
	}
	app.mu.Lock()
	if app.recordedTasks == nil {
		app.recordedTasks = make(map[string]bool)
	}
	if app.recordedTasks[task.ID] {
		app.mu.Unlock()
		return
	}
	app.recordedTasks[task.ID] = true
	app.mu.Unlock()
	app.appendHistory(historyservice.Input{
		OperationID: task.ID, ComponentID: task.ComponentID, Name: componentDisplayName(task.ComponentID),
		Action: "install", Status: task.Phase, Message: task.Message,
	})
}

func terminalInstallPhase(phase string) bool {
	return phase == "completed" || phase == "failed" || phase == "canceled"
}

func componentDisplayName(id string) string {
	switch id {
	case "claude-code":
		return "Claude Code"
	case "codex-cli":
		return "Codex CLI"
	case "opencode-cli":
		return "OpenCode CLI"
	case "claude-desktop":
		return "Claude Desktop"
	case "chatgpt-desktop":
		return "ChatGPT Desktop"
	case "opencode-desktop":
		return "OpenCode Desktop"
	case "cc-switch":
		return "CC Switch"
	case "cockpit-tools":
		return "Cockpit Tools"
	default:
		return "Osverse 组件"
	}
}

func knownComponentID(id string) bool {
	switch id {
	case "claude-code", "codex-cli", "opencode-cli", "claude-desktop", "chatgpt-desktop",
		"opencode-desktop", "cc-switch", "cockpit-tools":
		return true
	default:
		return false
	}
}

func (app *App) scanComponent(componentID string) (domain.Component, error) {
	if app == nil || !knownComponentID(componentID) {
		return domain.Component{}, errors.New("unknown component")
	}
	app.mu.RLock()
	scanner := app.scanner
	app.mu.RUnlock()
	if scanner == nil {
		return domain.Component{}, errors.New("scanner unavailable")
	}
	snapshot, err := scanner.Scan(app.appContext())
	if err != nil {
		return domain.Component{}, err
	}
	for _, component := range snapshot.Components {
		if component.ID == componentID {
			return component, nil
		}
	}
	return domain.Component{}, errors.New("component missing")
}

// CreateRemovalPlan previews exact package or recoverable Trash effects from a
// fresh backend scan. It never accepts a filesystem path from the frontend.
func (app *App) CreateRemovalPlan(componentID string) (removal.Plan, error) {
	if app == nil || !knownComponentID(componentID) {
		return removal.Plan{}, domain.NewPublicError(domain.ErrRemovalPlanFailed, "该工具不能由 Osverse 安全移除", nil)
	}
	app.mu.RLock()
	service := app.removal
	app.mu.RUnlock()
	if service == nil {
		return removal.Plan{}, domain.NewPublicError(domain.ErrRemovalPlanFailed, "移除服务不可用", nil)
	}
	component, err := app.scanComponent(componentID)
	if err != nil {
		return removal.Plan{}, domain.NewPublicError(domain.ErrRemovalPlanFailed, "无法确认当前安装状态", err)
	}
	plan, err := service.CreatePlan(app.appContext(), component)
	if err != nil {
		message := "无法创建移除计划"
		if errors.Is(err, removal.ErrRemovalUnsupported) {
			message = "该安装来源无法安全自动移除，请使用原安装方式"
		}
		return removal.Plan{}, domain.NewPublicError(domain.ErrRemovalPlanFailed, message, err)
	}
	app.mu.Lock()
	app.removalPlans[plan.ID] = componentID
	app.mu.Unlock()
	return plan, nil
}

// RemoveComponent confirms one single-use removal plan after rescanning the
// component and revalidating every captured filesystem identity.
func (app *App) RemoveComponent(planID string) (removal.Result, error) {
	if app == nil || planID == "" {
		return removal.Result{}, domain.NewPublicError(domain.ErrRemovalTaskFailed, "移除计划不可用", nil)
	}
	app.mu.RLock()
	service := app.removal
	componentID := app.removalPlans[planID]
	app.mu.RUnlock()
	if service == nil || componentID == "" {
		return removal.Result{}, domain.NewPublicError(domain.ErrRemovalTaskFailed, "移除计划不可用", nil)
	}
	component, err := app.scanComponent(componentID)
	if err != nil {
		return removal.Result{}, domain.NewPublicError(domain.ErrRemovalTaskFailed, "安装状态已变化，请重新预览", err)
	}
	result, err := service.Execute(app.appContext(), planID, component)
	app.mu.Lock()
	delete(app.removalPlans, planID)
	app.mu.Unlock()
	if err != nil {
		message := "移除未完成，原安装保持不变"
		if errors.Is(err, removal.ErrEvidenceChanged) {
			message = "安装状态已变化，请重新预览"
		}
		return removal.Result{}, domain.NewPublicError(domain.ErrRemovalTaskFailed, message, err)
	}
	app.appendHistory(historyservice.Input{
		OperationID: planID, ComponentID: componentID, Name: componentDisplayName(componentID),
		Action: "remove", Status: "completed", Message: result.Message,
	})
	return result, nil
}

// ListHistory returns only the bounded redacted operation ledger.
func (app *App) ListHistory() ([]historyservice.Entry, error) {
	if app == nil {
		return nil, domain.NewPublicError(domain.ErrHistoryFailed, "历史记录服务不可用", nil)
	}
	app.mu.RLock()
	service := app.history
	app.mu.RUnlock()
	if service == nil {
		return nil, domain.NewPublicError(domain.ErrHistoryFailed, "历史记录服务不可用", nil)
	}
	entries, err := service.List(app.appContext())
	if err != nil {
		return nil, domain.NewPublicError(domain.ErrHistoryFailed, "无法读取历史记录", err)
	}
	return entries, nil
}

// ClearHistory removes only Osverse's redacted local ledger.
func (app *App) ClearHistory() error {
	if app == nil {
		return domain.NewPublicError(domain.ErrHistoryFailed, "历史记录服务不可用", nil)
	}
	app.mu.RLock()
	service := app.history
	app.mu.RUnlock()
	if service == nil {
		return domain.NewPublicError(domain.ErrHistoryFailed, "历史记录服务不可用", nil)
	}
	if err := service.Clear(app.appContext()); err != nil {
		return domain.NewPublicError(domain.ErrHistoryFailed, "无法清除历史记录", err)
	}
	return nil
}

// CancelInstallTask requests rollback-safe cancellation.
func (app *App) CancelInstallTask(taskID string) error {
	if app == nil {
		_, err := unavailableInstallTask()
		return err
	}
	app.mu.RLock()
	executor := app.installExecutor
	if app.taskOwners[taskID] == "app" {
		executor = app.appExecutor
	} else if app.taskOwners[taskID] == "system" {
		executor = app.systemExecutor
	}
	app.mu.RUnlock()
	if executor == nil {
		_, err := unavailableInstallTask()
		return err
	}
	if err := executor.Cancel(taskID); err != nil {
		return domain.NewPublicError(domain.ErrInstallTaskFailed, "无法取消安装任务", err)
	}
	return nil
}

func isUserDesktopComponent(id string) bool {
	switch id {
	case "opencode-desktop", "cc-switch", "cockpit-tools":
		return true
	default:
		return false
	}
}

// LaunchComponent rescans a fixed component ID and starts only the exact
// backend-verified installation. No filesystem path is accepted from the UI.
func (app *App) LaunchComponent(componentID, installationPath string) error {
	if app == nil || !knownComponentID(componentID) {
		return domain.NewPublicError(domain.ErrInstallTaskFailed, "该应用不能由 Osverse 启动", nil)
	}
	app.mu.RLock()
	launcher := app.componentLauncher
	app.mu.RUnlock()
	if launcher == nil {
		return domain.NewPublicError(domain.ErrInstallTaskFailed, "应用启动服务不可用", nil)
	}
	ctx := app.appContext()
	component, err := app.scanComponent(componentID)
	if err != nil {
		return domain.NewPublicError(domain.ErrInstallTaskFailed, "启动前重新扫描失败", err)
	}
	if err := launcher.Launch(ctx, component, installationPath); err != nil {
		return domain.NewPublicError(domain.ErrInstallTaskFailed, "无法启动应用，请重新扫描或安装", err)
	}
	app.appendHistory(historyservice.Input{
		OperationID: "launch-" + componentID + "-" + time.Now().UTC().Format(time.RFC3339Nano),
		ComponentID: componentID, Name: componentDisplayName(componentID), Action: "launch",
		Status: "completed", Message: "已启动",
	})
	return nil
}

// ProbeProxy checks one loopback port using fixed protocol probes. A new probe
// immediately clears any prior selection so a failed port is never reused.
func (app *App) ProbeProxy(port int) (proxyservice.Result, error) {
	if app == nil {
		return unavailableProxy()
	}
	app.mu.Lock()
	ctx := app.ctx
	prober := app.proxyProber
	app.proxyGeneration++
	generation := app.proxyGeneration
	app.proxySelection = proxySelection{}
	app.mu.Unlock()
	if prober == nil {
		return unavailableProxy()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	result, err := prober.Probe(ctx, port)
	if err != nil {
		if errors.Is(err, proxyservice.ErrInvalidPort) {
			return proxyservice.Result{}, domain.NewPublicError(
				domain.ErrInvalidInput, "代理端口必须在 1 到 65535 之间", err,
			)
		}
		return proxyservice.Result{}, domain.NewPublicError(
			domain.ErrProxyProbeFailed, "代理探测失败", err,
		)
	}

	app.mu.Lock()
	if app.proxyGeneration == generation && result.Reachable && result.Recommended != "" {
		app.proxySelection = proxySelection{Protocol: result.Recommended, Port: result.Port}
	}
	app.mu.Unlock()
	return result, nil
}

// UseDirectConnection clears the task-scoped proxy selection. It does not
// modify system or shell proxy settings.
func (app *App) UseDirectConnection() {
	if app == nil {
		return
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	app.proxyGeneration++
	app.proxySelection = proxySelection{}
}

func (app *App) currentProxy() proxySelection {
	if app == nil {
		return proxySelection{}
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	return app.proxySelection
}

func (app *App) startup(ctx context.Context) {
	if app == nil {
		return
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	app.ctx = ctx
}

// ScanEnvironment exposes the read-only environment snapshot through Wails.
func (app *App) ScanEnvironment() (domain.EnvironmentSnapshot, error) {
	if app == nil {
		return unavailableSnapshot()
	}
	app.mu.RLock()
	ctx := app.ctx
	scanner := app.scanner
	app.mu.RUnlock()
	if scanner == nil {
		return unavailableSnapshot()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return scanner.Scan(ctx)
}

func unavailableSnapshot() (domain.EnvironmentSnapshot, error) {
	return domain.EnvironmentSnapshot{}, domain.NewPublicError(
		domain.ErrScanFailed,
		"环境扫描服务不可用",
		nil,
	)
}

func unavailableProxy() (proxyservice.Result, error) {
	return proxyservice.Result{}, domain.NewPublicError(
		domain.ErrProxyProbeFailed,
		"代理探测服务不可用",
		nil,
	)
}

func unavailableInstallPlan() (install.Plan, error) {
	return install.Plan{}, domain.NewPublicError(
		domain.ErrInstallUnavailable,
		"安装服务不可用",
		nil,
	)
}

func unavailableInstallTask() (install.Task, error) {
	return install.Task{}, domain.NewPublicError(
		domain.ErrInstallUnavailable,
		"安装服务不可用",
		nil,
	)
}
