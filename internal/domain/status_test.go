package domain

import "testing"

func TestComponentStatusValid(t *testing.T) {
	tests := []struct {
		name   string
		status ComponentStatus
		want   bool
	}{
		{name: "detecting", status: StatusDetecting, want: true},
		{name: "missing", status: StatusMissing, want: true},
		{name: "installed", status: StatusInstalled, want: true},
		{name: "update available", status: StatusUpdateAvailable, want: true},
		{name: "conflict", status: StatusConflict, want: true},
		{name: "unsupported", status: StatusUnsupported, want: true},
		{name: "broken", status: StatusBroken, want: true},
		{name: "installing", status: StatusInstalling, want: true},
		{name: "failed", status: StatusFailed, want: true},
		{name: "unknown", status: "ready", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Fatalf("ComponentStatus(%q).Valid() = %t, want %t", tt.status, got, tt.want)
			}
		})
	}
}

func TestEnvironmentSnapshotRecount(t *testing.T) {
	snapshot := EnvironmentSnapshot{
		Ready:          99,
		Total:          99,
		NeedsAttention: 99,
		Components: []Component{
			{ID: "detecting", Status: StatusDetecting},
			{ID: "missing", Status: StatusMissing},
			{ID: "installed", Status: StatusInstalled},
			{ID: "update", Status: StatusUpdateAvailable},
			{ID: "conflict", Status: StatusConflict},
			{ID: "unsupported", Status: StatusUnsupported},
			{ID: "broken", Status: StatusBroken},
			{ID: "installing", Status: StatusInstalling},
			{ID: "failed", Status: StatusFailed},
		},
	}

	snapshot.Recount()

	if snapshot.Total != 9 {
		t.Errorf("Total = %d, want 9", snapshot.Total)
	}
	if snapshot.Ready != 2 {
		t.Errorf("Ready = %d, want 2", snapshot.Ready)
	}
	if snapshot.NeedsAttention != 5 {
		t.Errorf("NeedsAttention = %d, want 5", snapshot.NeedsAttention)
	}
}
