// Package install exposes the platform-neutral install plan and task contract.
package install

import (
	"errors"
	"time"
)

var (
	ErrUnknownComponent  = errors.New("unknown install component")
	ErrUnsupportedTarget = errors.New("unsupported install target")
	ErrInvalidHome       = errors.New("invalid user home")
	ErrPlanUnavailable   = errors.New("install plan unavailable")
	ErrTaskUnavailable   = errors.New("install task unavailable")
	ErrInstallActive     = errors.New("component install already active")
)

type PlannedChange struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type Plan struct {
	ID            string          `json:"id"`
	ComponentID   string          `json:"componentId"`
	Name          string          `json:"name"`
	Command       string          `json:"command"`
	Version       string          `json:"version"`
	DownloadBytes int64           `json:"downloadBytes"`
	Changes       []PlannedChange `json:"changes"`
	CreatedAt     time.Time       `json:"createdAt"`
	ExpiresAt     time.Time       `json:"expiresAt"`
}

type Task struct {
	ID          string    `json:"id"`
	PlanID      string    `json:"planId"`
	ComponentID string    `json:"componentId"`
	Phase       string    `json:"phase"`
	Progress    int       `json:"progress"`
	Message     string    `json:"message"`
	ErrorCode   string    `json:"errorCode"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt"`
}

// IsProgressTaskPhase reports whether phase may be emitted by an installer
// while a task is running. Keeping this contract in the backend prevents an
// internal implementation detail from becoming an invalid frontend state.
func IsProgressTaskPhase(phase string) bool {
	switch phase {
	case "downloading", "verifying", "installing", "committing":
		return true
	default:
		return false
	}
}

// IsTerminalTaskPhase reports whether phase is a final task state.
func IsTerminalTaskPhase(phase string) bool {
	return phase == "completed" || phase == "failed" || phase == "canceled"
}

// IsValidTaskPhase reports whether phase is part of the public install-task
// contract shared by every installer and the frontend.
func IsValidTaskPhase(phase string) bool {
	return phase == "queued" || IsProgressTaskPhase(phase) || IsTerminalTaskPhase(phase)
}
