package domain

import "time"

// ComponentStatus describes the verified state of a component.
type ComponentStatus string

const (
	StatusDetecting       ComponentStatus = "detecting"
	StatusMissing         ComponentStatus = "missing"
	StatusInstalled       ComponentStatus = "installed"
	StatusUpdateAvailable ComponentStatus = "update_available"
	StatusConflict        ComponentStatus = "conflict"
	StatusUnsupported     ComponentStatus = "unsupported"
	StatusBroken          ComponentStatus = "broken"
	StatusInstalling      ComponentStatus = "installing"
	StatusFailed          ComponentStatus = "failed"
)

// Valid reports whether s is a status exposed by the scan domain.
func (s ComponentStatus) Valid() bool {
	switch s {
	case StatusDetecting,
		StatusMissing,
		StatusInstalled,
		StatusUpdateAvailable,
		StatusConflict,
		StatusUnsupported,
		StatusBroken,
		StatusInstalling,
		StatusFailed:
		return true
	default:
		return false
	}
}

// SystemInfo contains the system details relevant to scan support.
type SystemInfo struct {
	Distribution      string `json:"distribution"`
	Version           string `json:"version"`
	Architecture      string `json:"architecture"`
	Shell             string `json:"shell"`
	Supported         bool   `json:"supported"`
	UnsupportedReason string `json:"unsupportedReason"`
}

// Installation identifies one detected component installation.
type Installation struct {
	Path         string `json:"path"`
	ResolvedPath string `json:"resolvedPath"`
	Version      string `json:"version"`
	Source       string `json:"source"`
	Managed      bool   `json:"managed"`
}

// Component is the frontend-safe result for one scanned component.
type Component struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Category      string          `json:"category"`
	Status        ComponentStatus `json:"status"`
	Installations []Installation  `json:"installations"`
	Message       string          `json:"message"`
	MinimumOS     string          `json:"minimumOS"`
}

// EnvironmentSnapshot is the complete result of one environment scan.
type EnvironmentSnapshot struct {
	ScannedAt      time.Time   `json:"scannedAt"`
	System         SystemInfo  `json:"system"`
	Components     []Component `json:"components"`
	Ready          int         `json:"ready"`
	Total          int         `json:"total"`
	NeedsAttention int         `json:"needsAttention"`
}

// Recount refreshes scan-summary counts from Components.
func (s *EnvironmentSnapshot) Recount() {
	s.Ready = 0
	s.Total = len(s.Components)
	s.NeedsAttention = 0

	for _, component := range s.Components {
		switch component.Status {
		case StatusInstalled, StatusUpdateAvailable:
			s.Ready++
		case StatusMissing, StatusConflict, StatusUnsupported, StatusBroken, StatusFailed:
			s.NeedsAttention++
		}
	}
}
