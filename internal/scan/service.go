// Package scan coordinates read-only host and component probes.
package scan

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

const (
	scanFailedMessage        = "environment scan failed"
	componentDetectionFailed = "Detection failed"
)

// ComponentProbe detects one bounded catalog component.
type ComponentProbe interface {
	Descriptor() domain.Component
	Detect(context.Context, domain.SystemInfo, []string) (domain.Component, error)
}

// Service coordinates one complete, deterministic environment scan.
type Service struct {
	system     platform.SystemProbe
	paths      platform.PathProbe
	components []ComponentProbe
	clock      func() time.Time
}

// NewService constructs a scan service and takes ownership of a copy of the
// component catalog. A nil clock safely falls back to the production clock.
func NewService(
	system platform.SystemProbe,
	paths platform.PathProbe,
	components []ComponentProbe,
	clock func() time.Time,
) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		system:     system,
		paths:      paths,
		components: append([]ComponentProbe(nil), components...),
		clock:      clock,
	}
}

// ComponentCount returns the fixed number of component probes in the catalog.
func (service *Service) ComponentCount() int {
	if service == nil {
		return 0
	}
	return len(service.components)
}

// Scan probes system and PATH prerequisites once, then runs every bounded
// component concurrently. Aggregate failures never expose a partial snapshot.
func (service *Service) Scan(ctx context.Context) (domain.EnvironmentSnapshot, error) {
	if service == nil || service.system == nil || service.paths == nil {
		return domain.EnvironmentSnapshot{}, scanFailed(errors.New("scan service dependency is nil"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}

	system, err := probeSystem(ctx, service.system)
	if err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}

	paths, err := probePaths(ctx, service.paths)
	if err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}

	components := make([]domain.Component, len(service.components))
	var componentGroup sync.WaitGroup
	componentGroup.Add(len(service.components))
	for index, componentProbe := range service.components {
		componentPaths := append([]string(nil), paths...)
		go func() {
			defer componentGroup.Done()
			components[index] = detectComponent(ctx, componentProbe, system, componentPaths)
		}()
	}
	componentGroup.Wait()

	// Bounded probes are allowed to finish their cleanup, but cancellation
	// always wins over their individual results at the aggregate boundary.
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	scannedAt, err := readClock(service.clock)
	if err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	snapshot := domain.EnvironmentSnapshot{
		ScannedAt:  scannedAt,
		System:     system,
		Components: components,
	}
	snapshot.Recount()
	if err := ctx.Err(); err != nil {
		return domain.EnvironmentSnapshot{}, scanFailed(err)
	}
	return snapshot, nil
}

func probeSystem(ctx context.Context, probe platform.SystemProbe) (info domain.SystemInfo, err error) {
	defer func() {
		if recover() != nil {
			info = domain.SystemInfo{}
			err = errors.New("system probe panicked")
		}
	}()
	return probe.Probe(ctx)
}

func probePaths(ctx context.Context, probe platform.PathProbe) (paths []string, err error) {
	defer func() {
		if recover() != nil {
			paths = nil
			err = errors.New("PATH probe panicked")
		}
	}()
	return probe.Paths(ctx)
}

func detectComponent(
	ctx context.Context,
	probe ComponentProbe,
	system domain.SystemInfo,
	paths []string,
) (component domain.Component) {
	descriptor := domain.Component{}
	defer func() {
		if recover() != nil {
			component = failedComponent(descriptor)
		}
	}()

	descriptor = probe.Descriptor()
	component, err := probe.Detect(ctx, system, paths)
	if err != nil {
		return failedComponent(descriptor)
	}
	return component
}

func failedComponent(descriptor domain.Component) domain.Component {
	return domain.Component{
		ID:        descriptor.ID,
		Name:      descriptor.Name,
		Category:  descriptor.Category,
		Status:    domain.StatusFailed,
		Message:   componentDetectionFailed,
		MinimumOS: descriptor.MinimumOS,
	}
}

func readClock(clock func() time.Time) (now time.Time, err error) {
	defer func() {
		if recover() != nil {
			now = time.Time{}
			err = errors.New("scan clock panicked")
		}
	}()
	return clock(), nil
}

func scanFailed(cause error) error {
	return domain.NewPublicError(domain.ErrScanFailed, scanFailedMessage, cause)
}
