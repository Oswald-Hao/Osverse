//go:build linux

package bootstrap

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/detect"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

func TestNewLinuxScannerHasExactlyTwelveComponentsWithoutScanningHost(t *testing.T) {
	service := NewLinuxScanner()

	if service == nil {
		t.Fatal("NewLinuxScanner() returned nil")
	}
	if got := service.ComponentCount(); got != 12 {
		t.Fatalf("ComponentCount() = %d, want 12", got)
	}
}

func TestLinuxComponentProbesPreserveCatalogOrderAndDependencies(t *testing.T) {
	runner := &inertRunner{}
	fsys := inertFS{}
	const home = "/home/test-user"
	components := linuxComponentProbes(runner, fsys, home)

	coreSpecs := detect.CoreCLISpecs()
	desktopSpecs := detect.DesktopSpecs()
	wantIDs := make([]string, 0, len(coreSpecs)+len(desktopSpecs))
	for _, spec := range coreSpecs {
		wantIDs = append(wantIDs, spec.ID)
	}
	for _, spec := range desktopSpecs {
		wantIDs = append(wantIDs, spec.ID)
	}
	gotIDs := make([]string, len(components))
	for index, component := range components {
		gotIDs[index] = component.Descriptor().ID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("component IDs = %v, want catalog order %v", gotIDs, wantIDs)
	}

	for index := range coreSpecs {
		probe, ok := components[index].(detect.CommandComponentProbe)
		if !ok {
			t.Fatalf("component %d type = %T, want detect.CommandComponentProbe", index, components[index])
		}
		if probe.Detector.Runner != runner {
			t.Errorf("command component %d did not receive the shared runner", index)
		}
	}
	for index := range desktopSpecs {
		componentIndex := len(coreSpecs) + index
		probe, ok := components[componentIndex].(detect.DesktopComponentProbe)
		if !ok {
			t.Fatalf("component %d type = %T, want detect.DesktopComponentProbe", componentIndex, components[componentIndex])
		}
		query, ok := probe.Packages.(detect.DpkgQuery)
		if !ok {
			t.Fatalf("component %d package query type = %T, want detect.DpkgQuery", componentIndex, probe.Packages)
		}
		if query.Runner != runner {
			t.Errorf("desktop component %d did not receive the shared runner", componentIndex)
		}
		if probe.FS != fsys {
			t.Errorf("desktop component %d did not receive the root filesystem", componentIndex)
		}
		if probe.Home != home {
			t.Errorf("desktop component %d Home = %q, want %q", componentIndex, probe.Home, home)
		}
	}
}

type inertRunner struct{}

func (*inertRunner) Run(context.Context, platform.CommandRequest) (platform.CommandResult, error) {
	return platform.CommandResult{}, errors.New("inert test runner must not execute")
}

type inertFS struct{}

func (inertFS) Open(string) (fs.File, error) {
	return nil, errors.New("inert test filesystem must not open")
}
