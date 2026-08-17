//go:build linux

package install

import "testing"

func TestTaskUpdaterNeverPublishesUndocumentedPhase(t *testing.T) {
	t.Parallel()

	manager := &Manager{tasks: map[string]*taskState{
		"task": {public: Task{ID: "task", Phase: "queued", Message: "waiting"}},
	}}
	manager.updateTask("task", progressUpdate{phase: "extracting", progress: 50, message: "internal detail"})
	if got := manager.tasks["task"].public; got.Phase != "queued" || got.Progress != 0 || got.Message != "waiting" {
		t.Fatalf("invalid phase escaped through task updater: %#v", got)
	}

	manager.updateTask("task", progressUpdate{phase: "installing", progress: 50, message: "installing"})
	if got := manager.tasks["task"].public; got.Phase != "installing" || got.Progress != 50 || got.Message != "installing" {
		t.Fatalf("valid phase was not published: %#v", got)
	}
}
