package scan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
)

var testSystemInfo = domain.SystemInfo{
	Distribution: "Ubuntu 22.04.5 LTS",
	Version:      "22.04",
	Architecture: "x86_64",
	Shell:        "bash",
	Supported:    true,
}

func TestServiceStopsBeforeComponentsWhenSystemProbeFails(t *testing.T) {
	cause := errors.New("system-token=secret")
	system := &fakeSystemProbe{err: cause}
	paths := &fakePathProbe{paths: []string{"/usr/bin"}}
	var componentCalls atomic.Int32
	service := NewService(system, paths, []ComponentProbe{
		fakeComponentProbe{
			descriptor: testDescriptor("unused"),
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				componentCalls.Add(1)
				return domain.Component{}, nil
			},
		},
	}, time.Now)

	snapshot, err := service.Scan(context.Background())

	assertScanFailed(t, err, "system-token=secret")
	assertZeroSnapshot(t, snapshot)
	if got := system.calls.Load(); got != 1 {
		t.Errorf("system probe calls = %d, want 1", got)
	}
	if got := paths.calls.Load(); got != 0 {
		t.Errorf("PATH probe calls = %d, want 0", got)
	}
	if got := componentCalls.Load(); got != 0 {
		t.Errorf("component calls = %d, want 0", got)
	}
}

func TestServiceStopsBeforeComponentsWhenPathProbeFails(t *testing.T) {
	cause := errors.New("path-token=secret")
	system := &fakeSystemProbe{info: testSystemInfo}
	paths := &fakePathProbe{err: cause}
	var componentCalls atomic.Int32
	service := NewService(system, paths, []ComponentProbe{
		fakeComponentProbe{
			descriptor: testDescriptor("unused"),
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				componentCalls.Add(1)
				return domain.Component{}, nil
			},
		},
	}, time.Now)

	snapshot, err := service.Scan(context.Background())

	assertScanFailed(t, err, "path-token=secret")
	assertZeroSnapshot(t, snapshot)
	if got := system.calls.Load(); got != 1 {
		t.Errorf("system probe calls = %d, want 1", got)
	}
	if got := paths.calls.Load(); got != 1 {
		t.Errorf("PATH probe calls = %d, want 1", got)
	}
	if got := componentCalls.Load(); got != 0 {
		t.Errorf("component calls = %d, want 0", got)
	}
}

func TestServiceProbesPrerequisitesOnceAndPassesTheirValues(t *testing.T) {
	system := &fakeSystemProbe{info: testSystemInfo}
	paths := &fakePathProbe{paths: []string{"/custom/bin", "/usr/bin"}}
	var componentCalls atomic.Int32
	component := fakeComponentProbe{
		descriptor: testDescriptor("component"),
		detect: func(_ context.Context, gotSystem domain.SystemInfo, gotPaths []string) (domain.Component, error) {
			componentCalls.Add(1)
			if !reflect.DeepEqual(gotSystem, testSystemInfo) {
				return domain.Component{}, errors.New("component received the wrong system information")
			}
			if !reflect.DeepEqual(gotPaths, paths.paths) {
				return domain.Component{}, errors.New("component received the wrong PATH candidates")
			}
			return installedComponent("component"), nil
		},
	}
	service := NewService(system, paths, []ComponentProbe{component}, time.Now)

	snapshot, err := service.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got := system.calls.Load(); got != 1 {
		t.Errorf("system probe calls = %d, want 1", got)
	}
	if got := paths.calls.Load(); got != 1 {
		t.Errorf("PATH probe calls = %d, want 1", got)
	}
	if got := componentCalls.Load(); got != 1 {
		t.Errorf("component calls = %d, want 1", got)
	}
	if snapshot.Components[0].Status != domain.StatusInstalled {
		t.Errorf("component status = %q, want installed", snapshot.Components[0].Status)
	}
}

func TestServiceRunsAllComponentsConcurrentlyAtBarrier(t *testing.T) {
	const componentCount = 4
	started := make(chan string, componentCount)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseAll()

	components := make([]ComponentProbe, 0, componentCount)
	for index := range componentCount {
		id := string(rune('a' + index))
		components = append(components, fakeComponentProbe{
			descriptor: testDescriptor(id),
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				started <- id
				<-release
				return installedComponent(id), nil
			},
		})
	}

	done := make(chan scanOutcome, 1)
	go func() {
		snapshot, err := newTestService(components, time.Now).Scan(context.Background())
		done <- scanOutcome{snapshot: snapshot, err: err}
	}()

	seen := make(map[string]bool, componentCount)
	for range componentCount {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatal("not every component reached the barrier concurrently")
		}
	}
	if len(seen) != componentCount {
		t.Fatalf("components reaching barrier = %v, want %d unique components", seen, componentCount)
	}

	releaseAll()
	select {
	case outcome := <-done:
		if outcome.err != nil {
			t.Fatalf("Scan() error = %v", outcome.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Scan() did not return after the component barrier was released")
	}
}

func TestServiceIsolatesErrorsAndPanicsAndKeepsCatalogOrder(t *testing.T) {
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	thirdRelease := make(chan struct{})
	started := make(chan string, 3)
	finished := make(chan string, 3)

	firstDescriptor := testDescriptor("first")
	errorDescriptor := testDescriptor("error")
	panicDescriptor := testDescriptor("panic")
	errorDescriptor.Installations = []domain.Installation{{Path: "/should/not/survive"}}
	errorDescriptor.Message = "descriptor detail"
	panicDescriptor.Installations = []domain.Installation{{Path: "/should/not/survive"}}
	panicDescriptor.Message = "descriptor detail"

	components := []ComponentProbe{
		fakeComponentProbe{
			descriptor: firstDescriptor,
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				started <- "first"
				<-firstRelease
				finished <- "first"
				return installedComponent("first"), nil
			},
		},
		fakeComponentProbe{
			descriptor: errorDescriptor,
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				started <- "error"
				<-secondRelease
				finished <- "error"
				return domain.Component{}, errors.New("component-error-secret")
			},
		},
		fakeComponentProbe{
			descriptor: panicDescriptor,
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				started <- "panic"
				<-thirdRelease
				defer func() { finished <- "panic" }()
				panic("component-panic-secret")
			},
		},
	}

	done := make(chan scanOutcome, 1)
	go func() {
		snapshot, err := newTestService(components, time.Now).Scan(context.Background())
		done <- scanOutcome{snapshot: snapshot, err: err}
	}()

	for range components {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			close(firstRelease)
			close(secondRelease)
			close(thirdRelease)
			t.Fatal("components did not all start")
		}
	}
	close(thirdRelease)
	assertFinishedComponent(t, finished, "panic")
	close(secondRelease)
	assertFinishedComponent(t, finished, "error")
	close(firstRelease)
	assertFinishedComponent(t, finished, "first")

	var outcome scanOutcome
	select {
	case outcome = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Scan() did not return after every component completed")
	}
	if outcome.err != nil {
		t.Fatalf("Scan() error = %v, want component failures isolated", outcome.err)
	}
	if got, want := componentIDs(outcome.snapshot.Components), []string{"first", "error", "panic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("component order = %v, want catalog order %v", got, want)
	}
	if outcome.snapshot.Components[0].Status != domain.StatusInstalled {
		t.Errorf("peer status = %q, want installed", outcome.snapshot.Components[0].Status)
	}
	assertFailedDescriptor(t, outcome.snapshot.Components[1], errorDescriptor)
	assertFailedDescriptor(t, outcome.snapshot.Components[2], panicDescriptor)
	for _, component := range outcome.snapshot.Components[1:] {
		if strings.Contains(component.Message, "secret") {
			t.Errorf("failed component message exposed private failure: %q", component.Message)
		}
	}
}

func TestServiceCopiesPathsForEveryComponent(t *testing.T) {
	mutated := make(chan struct{})
	paths := &fakePathProbe{paths: []string{"/safe/bin", "/usr/bin"}}
	components := []ComponentProbe{
		fakeComponentProbe{
			descriptor: testDescriptor("mutator"),
			detect: func(_ context.Context, _ domain.SystemInfo, got []string) (domain.Component, error) {
				got[0] = "/poisoned"
				close(mutated)
				return installedComponent("mutator"), nil
			},
		},
		fakeComponentProbe{
			descriptor: testDescriptor("observer"),
			detect: func(_ context.Context, _ domain.SystemInfo, got []string) (domain.Component, error) {
				<-mutated
				if want := []string{"/safe/bin", "/usr/bin"}; !reflect.DeepEqual(got, want) {
					return domain.Component{}, errors.New("PATH input was shared between components")
				}
				return installedComponent("observer"), nil
			},
		},
	}
	service := NewService(&fakeSystemProbe{info: testSystemInfo}, paths, components, time.Now)

	snapshot, err := service.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if snapshot.Components[1].Status != domain.StatusInstalled {
		t.Fatalf("observer status = %q, want installed with isolated PATH input", snapshot.Components[1].Status)
	}
	if got, want := paths.paths, []string{"/safe/bin", "/usr/bin"}; !reflect.DeepEqual(got, want) {
		t.Errorf("PATH probe result was mutated: got %v, want %v", got, want)
	}
}

func TestServiceUsesInjectedClockAndRecountsCompletedComponents(t *testing.T) {
	wantTime := time.Date(2026, time.August, 13, 9, 8, 7, 6, time.FixedZone("test", 8*60*60))
	var clockCalls atomic.Int32
	var completed atomic.Int32
	var completedAtClock atomic.Int32
	statuses := []domain.ComponentStatus{
		domain.StatusInstalled,
		domain.StatusUpdateAvailable,
		domain.StatusMissing,
		domain.StatusBroken,
		domain.StatusDetecting,
	}
	components := make([]ComponentProbe, 0, len(statuses))
	for index, status := range statuses {
		id := string(rune('a' + index))
		components = append(components, fakeComponentProbe{
			descriptor: testDescriptor(id),
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				completed.Add(1)
				return domain.Component{ID: id, Status: status}, nil
			},
		})
	}
	service := newTestService(components, func() time.Time {
		clockCalls.Add(1)
		completedAtClock.Store(completed.Load())
		return wantTime
	})

	snapshot, err := service.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !snapshot.ScannedAt.Equal(wantTime) || snapshot.ScannedAt.Location() != wantTime.Location() {
		t.Errorf("ScannedAt = %v, want injected time %v", snapshot.ScannedAt, wantTime)
	}
	if got := clockCalls.Load(); got != 1 {
		t.Errorf("clock calls = %d, want 1", got)
	}
	if got := completedAtClock.Load(); got != int32(len(components)) {
		t.Errorf("components complete when clock ran = %d, want %d", got, len(components))
	}
	if snapshot.Ready != 2 || snapshot.Total != 5 || snapshot.NeedsAttention != 2 {
		t.Errorf("counts = ready %d, total %d, attention %d; want 2, 5, 2",
			snapshot.Ready, snapshot.Total, snapshot.NeedsAttention)
	}
}

func TestServiceCancellationWaitsForBoundedComponentsAndReturnsNoPartialSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 3)
	var active atomic.Int32
	var clockCalls atomic.Int32
	components := make([]ComponentProbe, 0, 3)
	for index := range 3 {
		id := string(rune('a' + index))
		components = append(components, fakeComponentProbe{
			descriptor: testDescriptor(id),
			detect: func(ctx context.Context, _ domain.SystemInfo, _ []string) (domain.Component, error) {
				active.Add(1)
				defer active.Add(-1)
				started <- struct{}{}
				<-ctx.Done()
				// Even a probe that reports a result after cancellation must not make
				// the aggregate scan look partially successful.
				return installedComponent(id), nil
			},
		})
	}
	service := newTestService(components, func() time.Time {
		clockCalls.Add(1)
		return time.Now()
	})
	done := make(chan scanOutcome, 1)
	go func() {
		snapshot, err := service.Scan(ctx)
		done <- scanOutcome{snapshot: snapshot, err: err}
	}()

	for range components {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("components did not reach cancellation barrier")
		}
	}
	cancel()

	select {
	case outcome := <-done:
		assertScanFailed(t, outcome.err, "")
		assertZeroSnapshot(t, outcome.snapshot)
	case <-time.After(2 * time.Second):
		t.Fatal("Scan() did not return after cancellation released every bounded component")
	}
	if got := active.Load(); got != 0 {
		t.Errorf("active components after Scan returned = %d, want 0", got)
	}
	if got := clockCalls.Load(); got != 0 {
		t.Errorf("clock calls on canceled scan = %d, want 0", got)
	}
}

func TestServiceHonorsCancellationAtClockBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := newTestService(nil, func() time.Time {
		cancel()
		return time.Date(2026, time.August, 13, 1, 2, 3, 0, time.UTC)
	})

	snapshot, err := service.Scan(ctx)

	assertScanFailed(t, err, "")
	assertZeroSnapshot(t, snapshot)
}

func TestServiceHonorsPreCanceledContextBeforeAnyProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	system := &fakeSystemProbe{info: testSystemInfo}
	paths := &fakePathProbe{paths: []string{"/usr/bin"}}
	var componentCalls atomic.Int32
	service := NewService(system, paths, []ComponentProbe{
		fakeComponentProbe{
			descriptor: testDescriptor("unused"),
			detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
				componentCalls.Add(1)
				return domain.Component{}, nil
			},
		},
	}, time.Now)

	_, err := service.Scan(ctx)

	assertScanFailed(t, err, "")
	if got := system.calls.Load(); got != 0 {
		t.Errorf("system calls = %d, want 0", got)
	}
	if got := paths.calls.Load(); got != 0 {
		t.Errorf("PATH calls = %d, want 0", got)
	}
	if got := componentCalls.Load(); got != 0 {
		t.Errorf("component calls = %d, want 0", got)
	}
}

func TestServiceHandlesNilDependenciesWithoutPanicking(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var service *Service
		_, err := service.Scan(context.Background())
		assertScanFailed(t, err, "")
		if got := service.ComponentCount(); got != 0 {
			t.Errorf("nil service ComponentCount() = %d, want 0", got)
		}
	})

	t.Run("nil system probe", func(t *testing.T) {
		service := NewService(nil, &fakePathProbe{}, nil, time.Now)
		if service == nil {
			t.Fatal("NewService() returned nil")
		}
		_, err := service.Scan(context.Background())
		assertScanFailed(t, err, "")
	})

	t.Run("nil PATH probe", func(t *testing.T) {
		service := NewService(&fakeSystemProbe{info: testSystemInfo}, nil, nil, time.Now)
		_, err := service.Scan(context.Background())
		assertScanFailed(t, err, "")
	})

	t.Run("typed nil system probe", func(t *testing.T) {
		var system *fakeSystemProbe
		service := NewService(system, &fakePathProbe{}, nil, time.Now)
		_, err := service.Scan(context.Background())
		assertScanFailed(t, err, "")
	})

	t.Run("typed nil PATH probe", func(t *testing.T) {
		var paths *fakePathProbe
		service := NewService(&fakeSystemProbe{info: testSystemInfo}, paths, nil, time.Now)
		_, err := service.Scan(context.Background())
		assertScanFailed(t, err, "")
	})

	t.Run("nil component is isolated", func(t *testing.T) {
		service := NewService(
			&fakeSystemProbe{info: testSystemInfo},
			&fakePathProbe{},
			[]ComponentProbe{nil},
			time.Now,
		)
		snapshot, err := service.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if len(snapshot.Components) != 1 || snapshot.Components[0].Status != domain.StatusFailed {
			t.Errorf("components = %#v, want one isolated failed component", snapshot.Components)
		}
	})

	t.Run("nil clock defaults safely", func(t *testing.T) {
		service := NewService(
			&fakeSystemProbe{info: testSystemInfo},
			&fakePathProbe{paths: []string{"/usr/bin"}},
			nil,
			nil,
		)
		snapshot, err := service.Scan(context.Background())
		if err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if snapshot.ScannedAt.IsZero() {
			t.Error("ScannedAt is zero with a nil clock")
		}
	})
}

func TestNewServiceCopiesTheComponentCatalog(t *testing.T) {
	first := fakeComponentProbe{
		descriptor: testDescriptor("first"),
		detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
			return installedComponent("first"), nil
		},
	}
	second := fakeComponentProbe{
		descriptor: testDescriptor("second"),
		detect: func(context.Context, domain.SystemInfo, []string) (domain.Component, error) {
			return installedComponent("second"), nil
		},
	}
	catalog := []ComponentProbe{first}
	service := newTestService(catalog, time.Now)
	catalog[0] = second

	snapshot, err := service.Scan(context.Background())

	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got, want := componentIDs(snapshot.Components), []string{"first"}; !reflect.DeepEqual(got, want) {
		t.Errorf("components = %v, want constructor-owned catalog %v", got, want)
	}
	if got := service.ComponentCount(); got != 1 {
		t.Errorf("ComponentCount() = %d, want 1", got)
	}
}

type fakeSystemProbe struct {
	info  domain.SystemInfo
	err   error
	calls atomic.Int32
}

func (probe *fakeSystemProbe) Probe(context.Context) (domain.SystemInfo, error) {
	probe.calls.Add(1)
	return probe.info, probe.err
}

type fakePathProbe struct {
	paths []string
	err   error
	calls atomic.Int32
}

func (probe *fakePathProbe) Paths(context.Context) ([]string, error) {
	probe.calls.Add(1)
	return probe.paths, probe.err
}

type fakeComponentProbe struct {
	descriptor domain.Component
	detect     func(context.Context, domain.SystemInfo, []string) (domain.Component, error)
}

func (probe fakeComponentProbe) Descriptor() domain.Component {
	return probe.descriptor
}

func (probe fakeComponentProbe) Detect(
	ctx context.Context,
	system domain.SystemInfo,
	paths []string,
) (domain.Component, error) {
	return probe.detect(ctx, system, paths)
}

type scanOutcome struct {
	snapshot domain.EnvironmentSnapshot
	err      error
}

func newTestService(components []ComponentProbe, clock func() time.Time) *Service {
	return NewService(
		&fakeSystemProbe{info: testSystemInfo},
		&fakePathProbe{paths: []string{"/usr/local/bin", "/usr/bin"}},
		components,
		clock,
	)
}

func testDescriptor(id string) domain.Component {
	return domain.Component{
		ID:        id,
		Name:      "Name " + id,
		Category:  "Test category",
		Status:    domain.StatusDetecting,
		MinimumOS: "Ubuntu 20.04",
	}
}

func installedComponent(id string) domain.Component {
	component := testDescriptor(id)
	component.Status = domain.StatusInstalled
	component.Message = "Installed"
	return component
}

func componentIDs(components []domain.Component) []string {
	ids := make([]string, len(components))
	for index, component := range components {
		ids[index] = component.ID
	}
	return ids
}

func assertScanFailed(t *testing.T, err error, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want SCAN_FAILED")
	}
	var public *domain.PublicError
	if !errors.As(err, &public) {
		t.Fatalf("error type = %T, want *domain.PublicError", err)
	}
	if public.Code != domain.ErrScanFailed {
		t.Errorf("error code = %q, want %q", public.Code, domain.ErrScanFailed)
	}
	encoded, marshalErr := json.Marshal(public)
	if marshalErr != nil {
		t.Fatalf("json.Marshal(error): %v", marshalErr)
	}
	if secret != "" && (strings.Contains(err.Error(), secret) || strings.Contains(string(encoded), secret)) {
		t.Errorf("public error exposed private cause %q: error=%q json=%s", secret, err, encoded)
	}
}

func assertZeroSnapshot(t *testing.T, snapshot domain.EnvironmentSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(snapshot, domain.EnvironmentSnapshot{}) {
		t.Errorf("snapshot = %#v, want zero value on aggregate failure", snapshot)
	}
}

func assertFinishedComponent(t *testing.T, finished <-chan string, want string) {
	t.Helper()
	select {
	case got := <-finished:
		if got != want {
			t.Fatalf("component finished = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("component %q did not finish after release", want)
	}
}

func assertFailedDescriptor(t *testing.T, got, descriptor domain.Component) {
	t.Helper()
	if got.ID != descriptor.ID || got.Name != descriptor.Name || got.Category != descriptor.Category ||
		got.MinimumOS != descriptor.MinimumOS {
		t.Errorf("failed descriptor identity = %#v, want ID/name/category/minimum OS from %#v", got, descriptor)
	}
	if got.Status != domain.StatusFailed {
		t.Errorf("failed descriptor status = %q, want %q", got.Status, domain.StatusFailed)
	}
	if len(got.Installations) != 0 {
		t.Errorf("failed descriptor installations = %#v, want none", got.Installations)
	}
	if got.Message == "" {
		t.Error("failed descriptor message is empty")
	}
}
