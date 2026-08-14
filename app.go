package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/install"
	"github.com/Oswald-Hao/Osverse/internal/profiles"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
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

type ProfileService interface {
	Save(context.Context, profiles.Input) (profiles.Profile, error)
	List(context.Context) ([]profiles.Profile, error)
	Delete(context.Context, string) error
	Probe(context.Context, string) (profiles.ProbeResult, error)
	Compatibility(string) ([]profiles.TargetCompatibility, error)
	CreateApplyPlan(context.Context, string, []string) (profiles.ApplyPlan, error)
	Apply(context.Context, string) (profiles.ApplyBatchResult, error)
}

type proxySelection struct {
	Protocol proxyservice.Protocol
	Port     int
}

type App struct {
	mu              sync.RWMutex
	ctx             context.Context
	scanner         Scanner
	proxyProber     ProxyProber
	proxySelection  proxySelection
	proxyGeneration uint64
	installPlanner  InstallPlanner
	installExecutor InstallExecutor
	profiles        ProfileService
}

func NewApp(scanner Scanner) *App {
	home, err := os.UserHomeDir()
	var planner InstallPlanner
	if err == nil {
		planner, _ = install.NewManager(home)
	}
	var profileService ProfileService
	if err == nil {
		profileService, _ = profiles.NewService(home)
	}
	return newAppWithServiceBundle(scanner, proxyservice.NewService(), planner, profileService)
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
	app := &App{scanner: scanner, proxyProber: proxyProber, installPlanner: planner, profiles: profileService}
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
	app.mu.RUnlock()
	if planner == nil {
		return unavailableInstallPlan()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	plan, err := planner.CreatePlan(ctx, componentID)
	if err == nil {
		return plan, nil
	}
	if errors.Is(err, install.ErrUnknownComponent) || errors.Is(err, install.ErrUnsupportedTarget) {
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
	return task, nil
}

// GetInstallTask returns one redacted progress snapshot.
func (app *App) GetInstallTask(taskID string) (install.Task, error) {
	if app == nil {
		return unavailableInstallTask()
	}
	app.mu.RLock()
	executor := app.installExecutor
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
	return task, nil
}

// CancelInstallTask requests rollback-safe cancellation.
func (app *App) CancelInstallTask(taskID string) error {
	if app == nil {
		_, err := unavailableInstallTask()
		return err
	}
	app.mu.RLock()
	executor := app.installExecutor
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
