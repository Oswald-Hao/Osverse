// Package removal exposes the platform-neutral removal preview contract.
package removal

import (
	"errors"
	"time"
)

var (
	ErrRemovalUnsupported = errors.New("component removal unsupported")
	ErrPlanUnavailable    = errors.New("removal plan unavailable")
	ErrEvidenceChanged    = errors.New("removal evidence changed")
	ErrComponentInUse     = errors.New("component is in use")
	ErrRemovalFailed      = errors.New("component removal failed")
)

type Effect struct {
	Action      string `json:"action"`
	Path        string `json:"path"`
	Description string `json:"description"`
	Recoverable bool   `json:"recoverable"`
}

type Plan struct {
	ID          string    `json:"id"`
	ComponentID string    `json:"componentId"`
	Name        string    `json:"name"`
	Effects     []Effect  `json:"effects"`
	Warning     string    `json:"warning"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type Result struct {
	PlanID      string `json:"planId"`
	ComponentID string `json:"componentId"`
	Removed     bool   `json:"removed"`
	Message     string `json:"message"`
}
