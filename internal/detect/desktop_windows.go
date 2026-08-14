//go:build windows

package detect

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oswald-Hao/Osverse/internal/domain"
	platformwindows "github.com/Oswald-Hao/Osverse/internal/platform/windows"
	"golang.org/x/sys/windows/registry"
)

type WindowsDesktopSpec struct {
	ID                  string
	Name                string
	Category            string
	ExecutableNames     []string
	RelativeExecutables []string
	RegistryNames       []string
	AppModelPrefixes    []string
	MinimumOS           string
	LatestVersion       string
}

func WindowsDesktopSpecs() []WindowsDesktopSpec {
	return []WindowsDesktopSpec{
		{ID: "claude-desktop", Name: "Claude Desktop", Category: "Desktop Applications",
			ExecutableNames: []string{"Claude.exe"}, RegistryNames: []string{"Claude"}, AppModelPrefixes: []string{"Claude_"},
			RelativeExecutables: []string{`AppData\Local\Programs\Claude\Claude.exe`, `AppData\Local\AnthropicClaude\Claude.exe`, `AppData\Local\Microsoft\WindowsApps\Claude.exe`},
			MinimumOS:           "Windows 10 1903"},
		{ID: "chatgpt-desktop", Name: "ChatGPT Desktop", Category: "Desktop Applications",
			ExecutableNames: []string{"ChatGPT.exe"}, RegistryNames: []string{"ChatGPT"}, AppModelPrefixes: []string{"OpenAI.ChatGPT"},
			RelativeExecutables: []string{`AppData\Local\Microsoft\WindowsApps\ChatGPT.exe`}, MinimumOS: "Windows 10 1809"},
		{ID: "codex-desktop", Name: "Codex Desktop", Category: "Desktop Applications",
			ExecutableNames: []string{"Codex.exe"}, RegistryNames: []string{"Codex"}, AppModelPrefixes: []string{"OpenAI.Codex"},
			RelativeExecutables: []string{`AppData\Local\Microsoft\WindowsApps\Codex.exe`}, MinimumOS: "Windows 10 1809"},
		{ID: "opencode-desktop", Name: "OpenCode Desktop", Category: "Desktop Applications",
			ExecutableNames: []string{"OpenCode.exe", "opencode-desktop.exe"}, RegistryNames: []string{"OpenCode"},
			RelativeExecutables: []string{`AppData\Local\Programs\OpenCode\OpenCode.exe`, `AppData\Local\Programs\opencode\OpenCode.exe`},
			MinimumOS:           "Windows 10 1809", LatestVersion: "1.18.18"},
		{ID: "cc-switch", Name: "CC Switch", Category: "Management Tools",
			ExecutableNames: []string{"CC Switch.exe", "cc-switch.exe"}, RegistryNames: []string{"CC Switch"},
			RelativeExecutables: []string{`AppData\Local\Programs\CC Switch\CC Switch.exe`},
			MinimumOS:           "Windows 10 1809", LatestVersion: "3.19.2"},
		{ID: "cockpit-tools", Name: "Cockpit Tools", Category: "Management Tools",
			ExecutableNames: []string{"Cockpit Tools.exe", "cockpit-tools.exe"}, RegistryNames: []string{"Cockpit Tools"},
			RelativeExecutables: []string{`AppData\Local\Programs\Cockpit Tools\Cockpit Tools.exe`, `AppData\Local\Cockpit Tools\cockpit-tools.exe`},
			MinimumOS:           "Windows 10 1809", LatestVersion: "1.3.17"},
	}
}

type WindowsPackageEvidence struct {
	Installed bool
	Version   string
	Source    string
}

type WindowsPackageQuery interface {
	Evidence(context.Context, WindowsDesktopSpec) (WindowsPackageEvidence, error)
}

type RegistryPackageQuery struct{}

func (RegistryPackageQuery) Evidence(ctx context.Context, spec WindowsDesktopSpec) (WindowsPackageEvidence, error) {
	if err := ctx.Err(); err != nil {
		return WindowsPackageEvidence{}, err
	}
	for _, location := range uninstallRegistryLocations() {
		key, err := registry.OpenKey(location.root, location.path, registry.ENUMERATE_SUB_KEYS|location.view)
		if err != nil {
			if errors.Is(err, registry.ErrNotExist) {
				continue
			}
			return WindowsPackageEvidence{}, err
		}
		names, err := key.ReadSubKeyNames(-1)
		_ = key.Close()
		if err != nil {
			return WindowsPackageEvidence{}, err
		}
		sort.Strings(names)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return WindowsPackageEvidence{}, err
			}
			entry, err := registry.OpenKey(location.root, location.path+`\`+name, registry.QUERY_VALUE|location.view)
			if err != nil {
				continue
			}
			display, _, displayErr := entry.GetStringValue("DisplayName")
			version, _, _ := entry.GetStringValue("DisplayVersion")
			_ = entry.Close()
			if displayErr == nil && containsFold(spec.RegistryNames, strings.TrimSpace(display)) {
				return WindowsPackageEvidence{Installed: true, Version: cleanVersion(version), Source: "registry"}, nil
			}
		}
	}
	return appModelEvidence(ctx, spec)
}

type registryLocation struct {
	root registry.Key
	path string
	view uint32
}

func uninstallRegistryLocations() []registryLocation {
	const path = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	return []registryLocation{
		{registry.CURRENT_USER, path, registry.WOW64_64KEY},
		{registry.CURRENT_USER, path, registry.WOW64_32KEY},
		{registry.LOCAL_MACHINE, path, registry.WOW64_64KEY},
		{registry.LOCAL_MACHINE, path, registry.WOW64_32KEY},
	}
}

func appModelEvidence(ctx context.Context, spec WindowsDesktopSpec) (WindowsPackageEvidence, error) {
	const path = `Software\Classes\Local Settings\Software\Microsoft\Windows\CurrentVersion\AppModel\Repository\Packages`
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return WindowsPackageEvidence{}, nil
	}
	if err != nil {
		return WindowsPackageEvidence{}, err
	}
	defer key.Close()
	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return WindowsPackageEvidence{}, err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return WindowsPackageEvidence{}, err
		}
		for _, prefix := range spec.AppModelPrefixes {
			if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				return WindowsPackageEvidence{Installed: true, Version: appModelVersion(name), Source: "msix"}, nil
			}
		}
	}
	return WindowsPackageEvidence{}, nil
}

func appModelVersion(name string) string {
	parts := strings.Split(name, "_")
	if len(parts) > 1 {
		return cleanVersion(parts[1])
	}
	return "unknown"
}

func cleanVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 80 || strings.ContainsAny(value, "\x00\r\n") {
		return "unknown"
	}
	return value
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

type WindowsDesktopComponentProbe struct {
	Spec     WindowsDesktopSpec
	Packages WindowsPackageQuery
	Home     string
}

func (probe WindowsDesktopComponentProbe) Descriptor() domain.Component {
	return windowsDesktopComponent(probe.Spec, domain.StatusDetecting, nil, "")
}

func (probe WindowsDesktopComponentProbe) Detect(ctx context.Context, system domain.SystemInfo, paths []string) (domain.Component, error) {
	return DetectWindowsDesktop(ctx, probe.Spec, system, paths, probe.Packages, probe.Home)
}

func DetectWindowsDesktop(ctx context.Context, spec WindowsDesktopSpec, system domain.SystemInfo, paths []string, packages WindowsPackageQuery, home string) (domain.Component, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return windowsDesktopComponent(spec, domain.StatusBroken, nil, "检测已取消"), err
	}
	if !validWindowsDesktopSpec(spec) || packages == nil || !filepath.IsAbs(home) {
		return windowsDesktopComponent(spec, domain.StatusBroken, nil, "检测配置无效"), domain.NewPublicError(domain.ErrInvalidResult, "invalid Windows desktop detector", nil)
	}
	evidence, err := packages.Evidence(ctx, spec)
	if err != nil {
		return windowsDesktopComponent(spec, domain.StatusBroken, nil, "安装记录检测失败"), err
	}
	installations := windowsDesktopExecutables(ctx, spec, paths, filepath.Clean(home), evidence)
	if !system.Supported {
		return windowsDesktopComponent(spec, domain.StatusUnsupported, installations, "当前 Windows 版本不受支持"), nil
	}
	if len(installations) == 0 {
		if evidence.Installed {
			return windowsDesktopComponent(spec, domain.StatusBroken, nil, "发现安装记录，但未找到可执行文件"), nil
		}
		return windowsDesktopComponent(spec, domain.StatusMissing, nil, "未检测到安装"), nil
	}
	status, message := domain.StatusInstalled, "已安装"
	if len(installations) > 1 {
		status, message = domain.StatusConflict, "检测到多个安装位置"
	} else if spec.LatestVersion != "" && installations[0].Version != "unknown" && installations[0].Version != spec.LatestVersion {
		status, message = domain.StatusUpdateAvailable, "有可用更新"
	}
	return windowsDesktopComponent(spec, status, installations, message), nil
}

func windowsDesktopExecutables(ctx context.Context, spec WindowsDesktopSpec, paths []string, home string, packageEvidence WindowsPackageEvidence) []domain.Installation {
	candidates, seen := make([]string, 0), make(map[string]bool)
	add := func(path string) {
		if !filepath.IsAbs(path) {
			return
		}
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, path)
		}
	}
	for _, relative := range spec.RelativeExecutables {
		add(filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(relative, `\`, `/`))))
	}
	for _, directory := range paths {
		for _, name := range spec.ExecutableNames {
			add(filepath.Join(directory, name))
		}
	}
	result, resolvedSeen := make([]domain.Installation, 0), make(map[string]bool)
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			break
		}
		locked, err := platformwindows.OpenExecutableEvidence(candidate)
		if err != nil {
			continue
		}
		resolved := filepath.Clean(locked.ResolvedPath())
		_ = locked.Close()
		key := strings.ToLower(resolved)
		if resolvedSeen[key] {
			continue
		}
		resolvedSeen[key] = true
		source, version := "path", "unknown"
		if packageEvidence.Installed {
			source, version = packageEvidence.Source, packageEvidence.Version
		}
		result = append(result, domain.Installation{Path: candidate, ResolvedPath: resolved, Version: version, Source: source})
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Path) < strings.ToLower(result[j].Path) })
	return result
}

func validWindowsDesktopSpec(spec WindowsDesktopSpec) bool {
	return spec.ID != "" && spec.Name != "" && spec.Category != "" && spec.MinimumOS != "" &&
		len(spec.ExecutableNames) > 0 && len(spec.ExecutableNames) <= 8 && len(spec.RelativeExecutables) <= 12
}

func windowsDesktopComponent(spec WindowsDesktopSpec, status domain.ComponentStatus, installations []domain.Installation, message string) domain.Component {
	return domain.Component{ID: spec.ID, Name: spec.Name, Category: spec.Category, Status: status,
		Installations: installations, Message: message, MinimumOS: spec.MinimumOS}
}
