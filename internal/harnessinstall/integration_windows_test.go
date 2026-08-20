//go:build windows

package harnessinstall

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Oswald-Hao/Osverse/internal/detect"
	"github.com/Oswald-Hao/Osverse/internal/domain"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"github.com/Oswald-Hao/Osverse/internal/windowsremoval"
)

func assertWindowsProductionRemoval(t *testing.T, ctx context.Context, home string, paths managedPaths) {
	t.Helper()
	var harnessSpec detect.CommandSpec
	for _, spec := range detect.CoreCLISpecs() {
		if spec.ID == componentID {
			harnessSpec = spec
			break
		}
	}
	component := (detect.CommandDetector{Runner: platformwindows.NewExecRunner()}).Detect(ctx, harnessSpec, []string{paths.binRoot})
	if component.ID != componentID || len(component.Installations) != 1 {
		t.Fatalf("production Harness detection = %#v", component)
	}
	remover, err := windowsremoval.NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := remover.CreatePlan(ctx, component)
	if err != nil {
		t.Fatalf("production Harness removal plan error = %v", err)
	}

	// Reproduce a slow/fast version-probe transition across the preview and
	// confirmation scans while the already-pinned filesystem target is stable.
	refreshed := component
	refreshed.Status = domain.StatusBroken
	refreshed.Message = "版本检测失败"
	refreshed.Installations = append([]domain.Installation(nil), component.Installations...)
	refreshed.Installations[0].Version = "unknown"
	refreshed.Installations[0].Source = "path"
	refreshed.Installations[0].Managed = false
	result, err := remover.Execute(ctx, plan.ID, refreshed)
	if err != nil || !result.Removed {
		t.Fatalf("production Harness removal = (%#v, %v)", result, err)
	}
	for _, removed := range []string{paths.shimPath, paths.toolRoot} {
		if _, err := os.Lstat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("production managed path remains after removal: %s: %v", removed, err)
		}
	}
}
