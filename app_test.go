package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
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
	if typeOfApp.NumMethod() != 1 {
		t.Fatalf("exported App methods = %d, want only ScanEnvironment", typeOfApp.NumMethod())
	}
	if method := typeOfApp.Method(0); method.Name != "ScanEnvironment" {
		t.Fatalf("exported App method = %q, want ScanEnvironment", method.Name)
	}
}

type fakeScanner struct {
	scan func(context.Context) (domain.EnvironmentSnapshot, error)
}

func (scanner fakeScanner) Scan(ctx context.Context) (domain.EnvironmentSnapshot, error) {
	return scanner.scan(ctx)
}
