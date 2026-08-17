package install

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestInstallTaskPhaseContract(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"queued", "downloading", "verifying", "installing", "committing", "completed", "failed", "canceled"} {
		if !IsValidTaskPhase(phase) {
			t.Errorf("documented task phase %q is invalid", phase)
		}
	}
	for _, phase := range []string{"", "ready", "extracting", "complete", "error", "COMPLETED"} {
		if IsValidTaskPhase(phase) {
			t.Errorf("undocumented task phase %q is valid", phase)
		}
	}

	for _, phase := range []string{"downloading", "verifying", "installing", "committing"} {
		if !IsProgressTaskPhase(phase) {
			t.Errorf("progress phase %q was rejected", phase)
		}
	}
	for _, phase := range []string{"queued", "completed", "failed", "canceled", "extracting"} {
		if IsProgressTaskPhase(phase) {
			t.Errorf("non-progress phase %q was accepted", phase)
		}
	}

	for _, phase := range []string{"completed", "failed", "canceled"} {
		if !IsTerminalTaskPhase(phase) {
			t.Errorf("terminal phase %q was rejected", phase)
		}
	}
}

func TestEveryInstallerFiltersProgressThroughSharedPhaseContract(t *testing.T) {
	t.Parallel()

	files := []string{
		"task.go",
		filepath.Join("..", "apps", "task.go"),
		filepath.Join("..", "windowsapps", "task_windows.go"),
		filepath.Join("..", "windowsinstall", "task_windows.go"),
		filepath.Join("..", "harnessinstall", "manager.go"),
		filepath.Join("..", "qweninstall", "manager.go"),
		filepath.Join("..", "copilotinstall", "manager.go"),
		filepath.Join("..", "systeminstall", "manager.go"),
	}
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if !strings.Contains(string(content), "IsProgressTaskPhase(") {
			t.Errorf("%s does not enforce the shared progress-phase contract", path)
		}
	}
}

func TestFrontendInstallPhasesMatchBackendContract(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "services", "osverse.ts"))
	if err != nil {
		t.Fatal(err)
	}
	const declaration = "const installTaskPhases = new Set<string>(["
	start := strings.Index(string(content), declaration)
	if start < 0 {
		t.Fatal("frontend install phase declaration is missing")
	}
	rest := string(content)[start+len(declaration):]
	end := strings.Index(rest, "])")
	if end < 0 {
		t.Fatal("frontend install phase declaration is unterminated")
	}
	matches := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(rest[:end], -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		if !IsValidTaskPhase(match[1]) {
			t.Errorf("frontend exposes undocumented task phase %q", match[1])
		}
		got = append(got, match[1])
	}
	want := []string{"queued", "downloading", "verifying", "installing", "committing", "completed", "failed", "canceled"}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("frontend task phases = %q, want %q", got, want)
	}
}
