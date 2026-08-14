//go:build windows

package windows

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	"github.com/Oswald-Hao/Osverse/internal/platform"
	"golang.org/x/sys/windows/registry"
)

type PathInputs struct {
	ProcessPath  string
	UserPath     string
	Home         string
	AppData      string
	LocalAppData string
}

func DiscoverPaths(inputs PathInputs) []string {
	seen := make(map[string]bool)
	paths := make([]string, 0, 16)
	add := func(value string) {
		value = expandKnownPath(value, inputs)
		if value == "" || !filepath.IsAbs(value) {
			return
		}
		value = filepath.Clean(value)
		key := strings.ToLower(value)
		if !seen[key] {
			seen[key] = true
			paths = append(paths, value)
		}
	}
	for _, source := range []string{inputs.ProcessPath, inputs.UserPath} {
		for _, entry := range filepath.SplitList(source) {
			add(strings.TrimSpace(entry))
		}
	}
	if validAbsolute(inputs.Home) {
		add(filepath.Join(inputs.Home, ".local", "bin"))
		add(filepath.Join(inputs.Home, ".bun", "bin"))
		add(filepath.Join(inputs.Home, ".opencode", "bin"))
		add(filepath.Join(inputs.Home, "bin"))
	}
	if validAbsolute(inputs.AppData) {
		add(filepath.Join(inputs.AppData, "npm"))
	}
	if validAbsolute(inputs.LocalAppData) {
		add(filepath.Join(inputs.LocalAppData, "Microsoft", "WindowsApps"))
	}
	return paths
}

func expandKnownPath(value string, inputs PathInputs) string {
	replacer := strings.NewReplacer(
		"%USERPROFILE%", inputs.Home, "%userprofile%", inputs.Home,
		"%APPDATA%", inputs.AppData, "%appdata%", inputs.AppData,
		"%LOCALAPPDATA%", inputs.LocalAppData, "%localappdata%", inputs.LocalAppData,
	)
	value = replacer.Replace(value)
	if strings.Contains(value, "%") || strings.ContainsAny(value, "\x00\r\n") {
		return ""
	}
	return value
}

func validAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && !strings.ContainsAny(value, "\x00\r\n")
}

type pathProbe struct{}

func NewPathProbe() platform.PathProbe { return pathProbe{} }

func (pathProbe) Paths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, domain.NewPublicError(domain.ErrScanFailed, "path discovery failed", err)
	}
	home, err := os.UserHomeDir()
	if err != nil || !validAbsolute(home) {
		return nil, domain.NewPublicError(domain.ErrScanFailed, "path discovery failed", err)
	}
	userPath := ""
	if key, openErr := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE); openErr == nil {
		userPath, _, _ = key.GetStringValue("Path")
		_ = key.Close()
	}
	if err := ctx.Err(); err != nil {
		return nil, domain.NewPublicError(domain.ErrScanFailed, "path discovery failed", err)
	}
	return DiscoverPaths(PathInputs{
		ProcessPath: os.Getenv("PATH"), UserPath: userPath, Home: home,
		AppData: os.Getenv("APPDATA"), LocalAppData: os.Getenv("LOCALAPPDATA"),
	}), nil
}
