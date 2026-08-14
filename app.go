package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
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
}

func NewApp(scanner Scanner) *App {
	return newAppWithServices(scanner, proxyservice.NewService())
}

func newAppWithServices(scanner Scanner, proxyProber ProxyProber) *App {
	if scanner == nil || proxyProber == nil {
		return nil
	}
	return &App{scanner: scanner, proxyProber: proxyProber}
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
