package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	proxyservice "github.com/Oswald-Hao/Osverse/internal/proxy"
)

func TestNewAppRejectsNilScanner(t *testing.T) {
	if NewApp(nil) != nil {
		t.Fatal("NewApp(nil) returned non-nil")
	}
}

func TestScanEnvironmentUsesStartupContextAndReturnsSnapshot(t *testing.T) {
	contextKey := struct{}{}
	startupContext := context.WithValue(context.Background(), contextKey, "startup-value")
	want := domain.EnvironmentSnapshot{
		ScannedAt: time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC),
		System:    domain.SystemInfo{Distribution: "Ubuntu 22.04"},
		Total:     8,
	}
	var gotContext context.Context
	app := NewApp(fakeScanner{scan: func(ctx context.Context) (domain.EnvironmentSnapshot, error) {
		gotContext = ctx
		return want, nil
	}})
	app.startup(startupContext)

	got, err := app.ScanEnvironment()

	if err != nil {
		t.Fatalf("ScanEnvironment() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanEnvironment() = %#v, want %#v", got, want)
	}
	if gotContext != startupContext || gotContext.Value(contextKey) != "startup-value" {
		t.Errorf("scanner context = %#v, want Wails startup context", gotContext)
	}
}

func TestScanEnvironmentBeforeStartupUsesNonNilBackgroundContext(t *testing.T) {
	want := domain.EnvironmentSnapshot{Total: 8}
	var gotContext context.Context
	app := NewApp(fakeScanner{scan: func(ctx context.Context) (domain.EnvironmentSnapshot, error) {
		gotContext = ctx
		return want, nil
	}})

	got, err := app.ScanEnvironment()

	if err != nil {
		t.Fatalf("ScanEnvironment() error = %v", err)
	}
	if gotContext == nil {
		t.Fatal("scanner received nil context before startup")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ScanEnvironment() = %#v, want %#v", got, want)
	}
}

func TestScanEnvironmentReturnsRedactedUnavailableErrorForNilState(t *testing.T) {
	tests := []struct {
		name string
		app  *App
	}{
		{name: "nil app", app: nil},
		{name: "nil scanner", app: &App{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := test.app.ScanEnvironment()
			if !reflect.DeepEqual(snapshot, domain.EnvironmentSnapshot{}) {
				t.Errorf("snapshot = %#v, want zero value", snapshot)
			}
			var public *domain.PublicError
			if !errors.As(err, &public) {
				t.Fatalf("error = %v (%T), want *domain.PublicError", err, err)
			}
			if public.Code != domain.ErrScanFailed {
				t.Errorf("error code = %q, want %q", public.Code, domain.ErrScanFailed)
			}
			if public.Message == "" {
				t.Error("public service-unavailable message is empty")
			}
		})
	}
}

func TestAppExposesOnlyScanEnvironmentAsWailsOperation(t *testing.T) {
	typeOfApp := reflect.TypeOf((*App)(nil))
	want := []string{"ProbeProxy", "ScanEnvironment", "UseDirectConnection"}
	if typeOfApp.NumMethod() != len(want) {
		t.Fatalf("exported App methods = %d, want %d fixed operations", typeOfApp.NumMethod(), len(want))
	}
	for index, name := range want {
		if method := typeOfApp.Method(index); method.Name != name {
			t.Fatalf("exported App method %d = %q, want %q", index, method.Name, name)
		}
	}
}

func TestProbeProxyStoresOnlyHTTPSCapableSuccessfulSelection(t *testing.T) {
	prober := &fakeProxyProber{result: proxyservice.Result{
		Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolSOCKS5,
	}}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, prober)

	result, err := app.ProbeProxy(7890)

	if err != nil || !reflect.DeepEqual(result, prober.result) {
		t.Fatalf("ProbeProxy() = (%#v, %v), want %#v", result, err, prober.result)
	}
	selection := app.currentProxy()
	if selection.Port != 7890 || selection.Protocol != proxyservice.ProtocolSOCKS5 {
		t.Fatalf("stored proxy = %#v", selection)
	}

	prober.result = proxyservice.Result{Port: 8080}
	if _, err := app.ProbeProxy(8080); err != nil {
		t.Fatalf("failed-protocol ProbeProxy() error = %v", err)
	}
	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("unusable probe retained selection: %#v", selection)
	}
}

func TestProbeProxyMapsInvalidInputAndRedactsBackendFailure(t *testing.T) {
	secret := "proxy-password-secret"
	tests := []struct {
		name string
		port int
		err  error
		code domain.ErrorCode
	}{
		{name: "invalid port", port: 0, err: proxyservice.ErrInvalidPort, code: domain.ErrInvalidInput},
		{name: "backend failure", port: 7890, err: errors.New(secret), code: domain.ErrProxyProbeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
				return domain.EnvironmentSnapshot{}, nil
			}}, &fakeProxyProber{err: tt.err})

			_, err := app.ProbeProxy(tt.port)

			var public *domain.PublicError
			if !errors.As(err, &public) || public.Code != tt.code {
				t.Fatalf("error = %v, want public code %q", err, tt.code)
			}
			if strings.Contains(public.Error(), secret) {
				t.Fatalf("public error leaked backend failure: %v", public)
			}
		})
	}
}

func TestUseDirectConnectionClearsProxySelection(t *testing.T) {
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, &fakeProxyProber{result: proxyservice.Result{
		Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolHTTPSConnect,
	}})
	if _, err := app.ProbeProxy(7890); err != nil {
		t.Fatalf("ProbeProxy() error = %v", err)
	}

	app.UseDirectConnection()

	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("proxy selection after direct mode = %#v", selection)
	}
}

func TestUseDirectConnectionInvalidatesAnOlderProbeResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	prober := &fakeProxyProber{probe: func(context.Context, int) (proxyservice.Result, error) {
		close(started)
		<-release
		return proxyservice.Result{
			Port: 7890, Reachable: true, Recommended: proxyservice.ProtocolHTTPSConnect,
		}, nil
	}}
	app := newAppWithServices(fakeScanner{scan: func(context.Context) (domain.EnvironmentSnapshot, error) {
		return domain.EnvironmentSnapshot{}, nil
	}}, prober)
	done := make(chan error, 1)
	go func() {
		_, err := app.ProbeProxy(7890)
		done <- err
	}()
	<-started

	app.UseDirectConnection()
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("ProbeProxy() error = %v", err)
	}
	if selection := app.currentProxy(); selection != (proxySelection{}) {
		t.Fatalf("stale probe restored proxy: %#v", selection)
	}
}

type fakeScanner struct {
	scan func(context.Context) (domain.EnvironmentSnapshot, error)
}

type fakeProxyProber struct {
	result proxyservice.Result
	err    error
	probe  func(context.Context, int) (proxyservice.Result, error)
}

func (prober *fakeProxyProber) Probe(ctx context.Context, port int) (proxyservice.Result, error) {
	if prober.probe != nil {
		return prober.probe(ctx, port)
	}
	return prober.result, prober.err
}

func TestDefaultWindowMatchesCockpitTools(t *testing.T) {
	if defaultWindowWidth != 1280 || defaultWindowHeight != 800 {
		t.Fatalf("default window = %dx%d, want Cockpit Tools 1280x800",
			defaultWindowWidth, defaultWindowHeight)
	}
}

func (scanner fakeScanner) Scan(ctx context.Context) (domain.EnvironmentSnapshot, error) {
	return scanner.scan(ctx)
}
