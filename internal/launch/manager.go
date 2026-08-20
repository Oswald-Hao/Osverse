// Package launch starts only fixed components backed by fresh scan evidence.
package launch

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
)

var (
	ErrLaunchUnavailable = errors.New("component launch unavailable")
	ErrLaunchFailed      = errors.New("component launch failed")
)

type managedLauncher interface {
	Launch(string) error
}

// Manager routes Osverse-owned desktop applications through their digest-
// verifying launcher and starts every other verified installation directly.
type Manager struct {
	starter platform.ProcessStarter
	managed managedLauncher
}

func NewManager(starter platform.ProcessStarter, managed managedLauncher) *Manager {
	return &Manager{starter: starter, managed: managed}
}

type componentKind struct {
	category       string
	managedDesktop bool
	launchArgs     []string
	localWeb       bool
}

var fixedComponents = map[string]componentKind{
	"claude-code":        {category: "Core CLI"},
	"codex-cli":          {category: "Core CLI"},
	"opencode-cli":       {category: "Core CLI"},
	"deepseek-harness":   {category: "Core CLI", launchArgs: []string{"web"}, localWeb: true},
	"qwen-code":          {category: "Core CLI"},
	"kimi-code":          {category: "Core CLI"},
	"github-copilot-cli": {category: "Core CLI"},
	"claude-desktop":     {category: "Desktop Applications"},
	"chatgpt-desktop":    {category: "Desktop Applications"},
	"codex-desktop":      {category: "Desktop Applications"},
	"opencode-desktop":   {category: "Desktop Applications", managedDesktop: true},
	"cc-switch":          {category: "Management Tools", managedDesktop: true},
	"cockpit-tools":      {category: "Management Tools", managedDesktop: true},
}

func (manager *Manager) Launch(ctx context.Context, component domain.Component, selector string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	kind, known := fixedComponents[component.ID]
	if manager == nil || manager.starter == nil || !known || component.Category != kind.category ||
		(component.Status != domain.StatusInstalled && component.Status != domain.StatusUpdateAvailable && component.Status != domain.StatusConflict) ||
		len(component.Installations) == 0 {
		return ErrLaunchUnavailable
	}
	var installation domain.Installation
	found := false
	if selector == "" && len(component.Installations) == 1 {
		installation, found = component.Installations[0], true
	} else {
		for _, candidate := range component.Installations {
			if candidate.Path == selector {
				installation, found = candidate, true
				break
			}
		}
	}
	if !found {
		return ErrLaunchUnavailable
	}
	if !safeAbsolutePath(installation.Path) || !safeAbsolutePath(installation.ResolvedPath) {
		return ErrLaunchUnavailable
	}
	if kind.managedDesktop && installation.Managed {
		if manager.managed == nil {
			return ErrLaunchUnavailable
		}
		if err := manager.managed.Launch(component.ID); err != nil {
			return ErrLaunchFailed
		}
		return nil
	}
	if err := manager.starter.Start(platform.LaunchRequest{
		Path:                 installation.Path,
		ExpectedResolvedPath: installation.ResolvedPath,
		Args:                 append([]string(nil), kind.launchArgs...),
		Terminal:             component.Category == "Core CLI",
		LocalWeb:             kind.localWeb,
	}); err != nil {
		return ErrLaunchFailed
	}
	return nil
}

func safeAbsolutePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n")
}
