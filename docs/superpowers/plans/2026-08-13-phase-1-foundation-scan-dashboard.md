# Osverse Phase 1 Foundation, Scan, and Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runnable, read-only Wails v2 desktop application that scans Ubuntu system details, detects the three core CLIs and selected desktop/management tools, and displays their state in the approved Cockpit-style dashboard.

**Architecture:** React never performs OS access. A Go scan service composes small Linux probes and component detectors behind interfaces, returns a stable frontend-safe snapshot, and exposes only `ScanEnvironment()` through the Wails bridge. Phase 1 is deliberately read-only: action buttons explain that installation and configuration arrive in later phases.

**Tech Stack:** Go 1.23, Wails v2.13.0, React 19, TypeScript 5.8, Vite 7, Vitest, Testing Library, CSS Modules/plain CSS, GitHub Actions.

## Global Constraints

- Supported systems are Ubuntu Desktop 20.04 LTS and 22.04 LTS on x86_64/amd64.
- Build against WebKitGTK ABI 4.0 using `-tags webkit2_40`; do not require WebKitGTK 4.1 at runtime.
- Support Bash and Zsh PATH discovery; other shells are reported without mutating configuration.
- Phase 1 performs no installation, package-manager mutation, shell-profile mutation, API configuration write, or credential storage.
- The Wails bridge must not expose generic command execution, arbitrary file access, or arbitrary URL download.
- A component is `installed` only when its executable exists, is executable, and its bounded version command succeeds.
- External installations are detected and displayed but never overwritten or removed.
- User-visible and logged command output must be bounded; environment-variable values are never returned to React.

---

## Planned file structure

```text
Osverse/
  main.go                              Wails bootstrap and production asset embedding
  app.go                               Narrow Wails-facing application methods
  go.mod                               Pinned Go/Wails dependencies
  wails.json                           Wails build configuration
  internal/domain/
    status.go                          Stable status/component/snapshot types
    errors.go                          Stable error codes and public errors
  internal/platform/
    platform.go                        Probe and command-runner interfaces
    linux/system.go                    Ubuntu/architecture/shell probe
    linux/system_test.go
    linux/path.go                      GUI/login-shell PATH candidate discovery
    linux/path_test.go
    linux/process.go                   Bounded read-only process runner
    linux/process_test.go
  internal/detect/
    command.go                         Executable candidate and version detection
    command_test.go
    desktop.go                         dpkg/.desktop/executable detection
    desktop_test.go
    catalog.go                         Phase-1 component specifications
    catalog_test.go
  internal/scan/
    service.go                         Concurrent scan orchestration
    service_test.go
  internal/bootstrap/
    linux.go                           Production Linux dependency assembly
    linux_test.go
  frontend/
    package.json                       Frontend commands and pinned dependencies
    package-lock.json
    tsconfig.json
    vite.config.ts
    vitest.config.ts
    index.html
    src/main.tsx
    src/App.tsx
    src/App.css
    src/domain.ts                      Frontend-safe mirror of Go view models
    src/services/osverse.ts            Typed Wails adapter with browser-test fallback
    src/hooks/useEnvironmentScan.ts    Scan lifecycle state
    src/hooks/useEnvironmentScan.test.tsx
    src/components/Sidebar.tsx
    src/components/SummaryCards.tsx
    src/components/ToolSection.tsx
    src/components/ToolCard.tsx
    src/components/StatusBadge.tsx
    src/components/Dashboard.test.tsx
    src/test/setup.ts
  build/                               Wails-generated icons/platform metadata
  .github/workflows/ci.yml             Unit, frontend, and build checks
  .gitignore
  README.md
```

---

### Task 1: Scaffold the pinned Wails application and test harness

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `app.go`
- Create: `wails.json`
- Create: `frontend/package.json`
- Create: `frontend/tsconfig.json`
- Create: `frontend/vite.config.ts`
- Create: `frontend/vitest.config.ts`
- Create: `frontend/index.html`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/App.test.tsx`
- Create: `frontend/src/test/setup.ts`
- Create: `.gitignore`

**Interfaces:**
- Produces: `NewApp() *App`
- Produces: `App.startup(context.Context)` for Wails lifecycle context capture
- Produces frontend scripts: `test`, `typecheck`, `build`

- [ ] **Step 1: Generate a temporary Wails v2.13.0 React/TypeScript reference scaffold**

Run outside the repository so generated defaults can be inspected without overwriting the committed design:

```bash
tmp_dir="$(mktemp -d)"
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 init -n OsverseReference -t react-ts -d "$tmp_dir"
find "$tmp_dir" -maxdepth 3 -type f | sort
```

Expected: exit 0 and a reference Wails project containing `main.go`, `app.go`, `wails.json`, `frontend/`, and `build/`.

- [ ] **Step 2: Write a failing Go construction test**

Create `app_test.go`:

```go
package main

import "testing"

func TestNewApp(t *testing.T) {
	if NewApp() == nil {
		t.Fatal("NewApp() returned nil")
	}
}
```

- [ ] **Step 3: Run the construction test and verify failure**

Run: `go test ./...`

Expected: FAIL because `NewApp` is undefined before the scaffold is implemented.

- [ ] **Step 4: Create the pinned scaffold and minimal app constructor**

Use module `github.com/Oswald-Hao/Osverse`, pin `github.com/wailsapp/wails/v2 v2.13.0`, copy only required `build/` assets from the reference scaffold, and implement:

```go
type App struct{ ctx context.Context }

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}
```

In `main.go`, instantiate `NewApp()`, pass `app.startup` as `OnStartup`, and bind only `app`. Configure `wails.json` frontend commands as `npm install`, `npm run build`, and `npm run dev`; set product name to `Osverse` and output filename to `osverse`.

- [ ] **Step 5: Add deterministic frontend commands**

Pin React 19, TypeScript 5.8, Vite 7, Vitest, jsdom, and Testing Library in `frontend/package.json`. Configure:

```json
{
  "scripts": {
    "dev": "vite",
    "test": "vitest run",
    "typecheck": "tsc --noEmit",
    "build": "tsc && vite build"
  }
}
```

Create `frontend/src/App.test.tsx` as a smoke test that renders `<App />` and finds the visible `Osverse` heading. This ensures `vitest run` has a real test from the first commit.

- [ ] **Step 6: Run baseline verification**

Run:

```bash
go mod tidy
npm --prefix frontend install
npm --prefix frontend run test
npm --prefix frontend run typecheck
npm --prefix frontend run build
go test ./...
```

Expected: all commands exit 0 and the frontend smoke test passes. The frontend build runs before `go test ./...` because `main.go` embeds `frontend/dist`.

- [ ] **Step 7: Commit the scaffold**

```bash
git add go.mod go.sum main.go app.go app_test.go wails.json build frontend .gitignore
git commit -m "chore: scaffold Wails application"
```

---

### Task 2: Define stable scan domain types and public errors

**Files:**
- Create: `internal/domain/status.go`
- Create: `internal/domain/status_test.go`
- Create: `internal/domain/errors.go`
- Create: `internal/domain/errors_test.go`

**Interfaces:**
- Produces: `type ComponentStatus string`
- Produces constants: `StatusDetecting`, `StatusMissing`, `StatusInstalled`, `StatusUpdateAvailable`, `StatusConflict`, `StatusUnsupported`, `StatusBroken`, `StatusInstalling`, `StatusFailed`
- Produces: `type SystemInfo struct { Distribution, Version, Architecture, Shell string; Supported bool; UnsupportedReason string }`
- Produces: `type Installation struct { Path, ResolvedPath, Version, Source string; Managed bool }`
- Produces: `type Component struct { ID, Name, Category string; Status ComponentStatus; Installations []Installation; Message, MinimumOS string }`
- Produces: `type EnvironmentSnapshot struct { ScannedAt time.Time; System SystemInfo; Components []Component; Ready, Total, NeedsAttention int }`
- Produces: `type PublicError struct { Code ErrorCode; Message string }`

- [ ] **Step 1: Write failing status validation tests**

Create table-driven tests asserting that all nine defined statuses are valid, `"ready"` is invalid, and `EnvironmentSnapshot.Recount()` counts only `installed` and `update_available` as ready while `missing`, `conflict`, `unsupported`, `broken`, and `failed` need attention.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/domain -run 'Test(ComponentStatusValid|EnvironmentSnapshotRecount)' -v`

Expected: FAIL because the domain package does not exist.

- [ ] **Step 3: Implement immutable JSON-facing domain types**

Use explicit JSON tags such as `json:"scannedAt"` and implement:

```go
func (s ComponentStatus) Valid() bool
func (s *EnvironmentSnapshot) Recount()
```

Sort is not part of `Recount`; scan orchestration owns deterministic ordering.

- [ ] **Step 4: Write failing error-redaction tests**

Assert `NewPublicError(ErrCommandFailed, "codex version failed", errors.New("token=sk-secret"))` exposes only code and public message through `Error()` and JSON serialization, never the cause text.

- [ ] **Step 5: Implement stable public errors**

Define error codes `SCAN_FAILED`, `COMMAND_TIMEOUT`, `COMMAND_FAILED`, `UNSUPPORTED_SYSTEM`, and `INVALID_RESULT`. Store the wrapped cause in an unexported field and implement `Unwrap()` for backend logs/tests.

- [ ] **Step 6: Run domain and repository tests**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit domain contracts**

```bash
git add internal/domain app.go
git commit -m "feat: define environment scan domain"
```

---

### Task 3: Implement bounded Linux system and process probes

**Files:**
- Create: `internal/platform/platform.go`
- Create: `internal/platform/linux/system.go`
- Create: `internal/platform/linux/system_test.go`
- Create: `internal/platform/linux/process.go`
- Create: `internal/platform/linux/process_test.go`

**Interfaces:**
- Produces: `type SystemProbe interface { Probe(context.Context) (domain.SystemInfo, error) }`
- Produces: `type CommandRequest struct { Path string; Args []string; Env []string; Timeout time.Duration; OutputLimit int }`
- Produces: `type CommandResult struct { ExitCode int; Stdout, Stderr string; TimedOut, Truncated bool }`
- Produces: `type CommandRunner interface { Run(context.Context, CommandRequest) (CommandResult, error) }`
- Produces: `linux.NewExecRunner() platform.CommandRunner`
- Produces: `linux.NewSystemProbe() platform.SystemProbe`
- Produces: `linux.ProbeSystem(osRelease []byte, goarch, shell string) domain.SystemInfo`

- [ ] **Step 1: Write failing Ubuntu parsing tests**

Use inline `/etc/os-release` fixtures for Ubuntu 20.04, Ubuntu 22.04, Ubuntu 24.04, Debian 12, malformed input, `amd64`, and `arm64`. Assert `Supported` is true only for Ubuntu 20.04/22.04 on amd64 and every unsupported result includes a concrete reason.

- [ ] **Step 2: Run system tests and verify failure**

Run: `go test ./internal/platform/linux -run TestProbeSystem -v`

Expected: FAIL because `ProbeSystem` is undefined.

- [ ] **Step 3: Implement pure system parsing**

Parse `ID`, `VERSION_ID`, and `PRETTY_NAME` without invoking a shell. Normalize Go `amd64` to display `x86_64`. Normalize the login shell to its basename (`bash`, `zsh`, or the detected alternative).

Implement `linux.NewSystemProbe()` as a thin production adapter that reads `/etc/os-release`, uses `runtime.GOARCH` and `SHELL`, checks context cancellation, then delegates all parsing to `ProbeSystem`. A read failure returns a redacted `SCAN_FAILED` public error.

- [ ] **Step 4: Write failing process-runner tests**

Use `os.Args[0] -test.run=TestHelperProcess --` helper-process fixtures to verify:

- successful stdout and exit code
- non-zero exit with bounded stderr
- timeout kills the process
- combined stdout/stderr each truncate at `OutputLimit`
- supplied environment contains only inherited `HOME`, `PATH`, `LANG`, `LC_ALL`, and `TERM` values that are present, plus request overrides; an unrelated parent variable is absent

- [ ] **Step 5: Implement the process runner without a shell**

Use `exec.CommandContext(req.Path, req.Args...)`; reject empty paths, enforce default timeout `3s` and default per-stream limit `64 KiB`, construct `Cmd.Env` from the five-name allowlist plus request overrides, and never call `bash -c` or `sh -c`.

- [ ] **Step 6: Run probe tests**

Run: `go test ./internal/platform/linux -v`

Expected: PASS with no leaked helper-process output.

- [ ] **Step 7: Commit Linux probes**

```bash
git add internal/platform
git commit -m "feat: add bounded Linux probes"
```

---

### Task 4: Discover login-shell PATH candidates without executing profiles

**Files:**
- Modify: `internal/platform/platform.go`
- Create: `internal/platform/linux/path.go`
- Create: `internal/platform/linux/path_test.go`

**Interfaces:**
- Produces: `type PathInputs struct { ProcessPath, Home, Shell string; ProfileFiles map[string][]byte }`
- Produces: `func DiscoverPaths(PathInputs) []string`
- Produces: `type PathProbe interface { Paths(context.Context) ([]string, error) }` in `internal/platform`
- Produces: `linux.NewPathProbe() platform.PathProbe`
- Consumes: no command runner; parsing is pure and read-only

- [ ] **Step 1: Write failing PATH discovery tests**

Cover process PATH, the Osverse stable shim directory `~/.local/bin`, simple literal exports in `.profile`, `.bash_profile`, `.bashrc`, `.zprofile`, and `.zshrc`, duplicate removal, relative-entry rejection, and unsafe constructs such as `$()`, backticks, redirections, command separators, and globbing.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/platform/linux -run TestDiscoverPaths -v`

Expected: FAIL because `DiscoverPaths` is undefined.

- [ ] **Step 3: Implement conservative PATH parsing**

Always include normalized absolute entries from the GUI process PATH and `~/.local/bin`. Accept only literal `PATH=...` or `export PATH=...` assignments whose segments contain `$HOME`, `${HOME}`, or existing PATH placeholders; never execute or fully interpret shell syntax.

Implement `linux.NewPathProbe()` as a thin production adapter using `os.UserHomeDir`, `PATH`, and `SHELL`. It reads only `.profile`, `.bash_profile`, `.bashrc`, `.zprofile`, and `.zshrc` under that home, ignores `fs.ErrNotExist`, returns a redacted `SCAN_FAILED` error for other read failures, and delegates parsing to `DiscoverPaths`.

- [ ] **Step 4: Run PATH and all Go tests**

Run:

```bash
go test ./internal/platform/linux -run TestDiscoverPaths -v
go test ./...
```

Expected: PASS.

- [ ] **Step 5: Commit PATH discovery**

```bash
git add internal/platform/platform.go internal/platform/linux/path.go internal/platform/linux/path_test.go
git commit -m "feat: discover user command paths safely"
```

---

### Task 5: Detect CLI executables, versions, and conflicts

**Files:**
- Create: `internal/detect/command.go`
- Create: `internal/detect/command_test.go`

**Interfaces:**
- Produces: `type CommandSpec struct { ID, Name string; ExecutableNames []string; VersionArgs []string; VersionPattern *regexp.Regexp; MinimumOS string }`
- Produces: `type CommandDetector struct { Runner platform.CommandRunner }`
- Produces: `func (d CommandDetector) Detect(ctx context.Context, spec CommandSpec, paths []string) domain.Component`
- Produces: `type CommandComponentProbe struct { Detector CommandDetector; Spec CommandSpec }` with `Descriptor() domain.Component` and `Detect(context.Context, domain.SystemInfo, []string) (domain.Component, error)`
- Consumes: `platform.CommandRunner.Run`

- [ ] **Step 1: Write failing command detection tests**

Create temporary executable fixtures and a fake runner. Cover:

- no candidate yields `missing`
- one executable with a parsable version yields `installed`
- a non-executable file is ignored
- two valid paths yield `conflict` with both installations preserved
- version timeout or unparsable output yields `broken`
- a path under `~/.local/share/osverse/tools/` is marked `Managed: true`
- final installation ordering is deterministic by path

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/detect -run TestCommandDetector -v`

Expected: FAIL because `CommandDetector` is undefined.

- [ ] **Step 3: Implement candidate and version detection**

Resolve symlinks for deduplication and store the target in `ResolvedPath`, retain the invoked candidate in `Path`, use only the spec's fixed version arguments, parse the first regex capture as the version, and set `Source` to `osverse`, `path`, or `unknown` without package-manager guesses.

Implement `CommandComponentProbe.Descriptor` from its compiled spec. Implement `Detect` as the narrow scan adapter: ignore system details, delegate to `CommandDetector.Detect`, and return a nil error because detector failures are represented by the component's `broken` status.

- [ ] **Step 4: Run detector tests**

Run: `go test ./internal/detect -run TestCommandDetector -v`

Expected: PASS.

- [ ] **Step 5: Commit command detection**

```bash
git add internal/detect/command.go internal/detect/command_test.go
git commit -m "feat: detect CLI versions and conflicts"
```

---

### Task 6: Define and verify the Phase-1 component catalog

**Files:**
- Create: `internal/detect/catalog.go`
- Create: `internal/detect/catalog_test.go`

**Interfaces:**
- Produces: `func CoreCLISpecs() []CommandSpec`
- Produces component IDs: `claude-code`, `codex-cli`, `opencode-cli`
- Produces fixed version calls: `claude --version`, `codex --version`, `opencode --version`

- [ ] **Step 1: Write failing catalog tests**

Assert exactly three unique CLI IDs, non-empty names, exactly one executable name per v1 tool, `[]string{"--version"}` fixed args, anchored/non-empty version patterns, and catalog order Claude Code → Codex CLI → OpenCode CLI.

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/detect -run TestCoreCLISpecs -v`

Expected: FAIL because `CoreCLISpecs` is undefined.

- [ ] **Step 3: Implement the explicit catalog**

Do not load a remote manifest in Phase 1. Keep specs compiled into Go and document regex examples in test cases using representative upstream version output.

- [ ] **Step 4: Run catalog tests**

Run: `go test ./internal/detect -run TestCoreCLISpecs -v`

Expected: PASS.

- [ ] **Step 5: Commit the catalog**

```bash
git add internal/detect/catalog.go internal/detect/catalog_test.go
git commit -m "feat: define core CLI catalog"
```

---

### Task 7: Detect desktop applications and management tools read-only

**Files:**
- Create: `internal/detect/desktop.go`
- Create: `internal/detect/desktop_test.go`

**Interfaces:**
- Produces: `type PackageQuery interface { InstalledVersion(context.Context, string) (string, bool, error) }`
- Produces: `type DesktopSpec struct { ID, Name, Category, PackageName, ExecutableName, DesktopFileName, MinimumUbuntu string }`
- Produces: `func DesktopSpecs() []DesktopSpec`
- Produces: `func DetectDesktop(ctx context.Context, spec DesktopSpec, system domain.SystemInfo, paths []string, packages PackageQuery, fsys fs.FS, home string) (domain.Component, error)`
- Produces: `type DpkgQuery struct { Runner platform.CommandRunner }`
- Produces: `type DesktopComponentProbe struct { Spec DesktopSpec; Packages PackageQuery; FS fs.FS; Home string }` with `Descriptor() domain.Component` and `Detect(context.Context, domain.SystemInfo, []string) (domain.Component, error)`

- [ ] **Step 1: Write failing compatibility/detection tests**

Cover:

- Claude Desktop is `unsupported` on Ubuntu 20.04 when absent and `missing` on 22.04
- ChatGPT Desktop is `unsupported` on both supported Phase-1 systems when absent
- installed package plus executable yields `installed` even if upstream minimum later rises
- package without an executable yields `broken`
- CC Switch and Cockpit Tools have no OS minimum beyond Phase-1's supported system
- a `.desktop` file and executable can identify an AppImage-style external install
- `DpkgQuery` uses only the fixed `dpkg-query` arguments, maps “not installed” to `false`, and rejects malformed success output

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/detect -run 'TestDetectDesktop|TestDesktopSpecs' -v`

Expected: FAIL because desktop detection is undefined.

- [ ] **Step 3: Implement package and desktop-file evidence merging**

The catalog must include `claude-desktop`, `chatgpt-desktop`, `opencode-desktop`, `cc-switch`, and `cockpit-tools`. Check each fixed desktop filename only under `/usr/share/applications`, `/usr/local/share/applications`, and `<home>/.local/share/applications`; strip the leading slash before opening it through the root `fs.FS`. Return `unsupported` only when absent and disallowed; preserve an already-installed unsupported component as `installed` with a warning message so the dashboard reports reality without offering installation.

- [ ] **Step 4: Implement the real dpkg query adapter**

Call `/usr/bin/dpkg-query` directly with fixed arguments `-W -f=${Status}\t${Version} <package>` through `CommandRunner`. Treat “not installed” as `(false, nil)` and malformed output as `INVALID_RESULT`.

Implement `DesktopComponentProbe.Descriptor` from its compiled spec. Implement `Detect` by delegating to `DetectDesktop`; return package-query or filesystem failures as errors so scan orchestration can isolate them as a `failed` component.

- [ ] **Step 5: Run desktop detection tests**

Run: `go test ./internal/detect -v`

Expected: PASS.

- [ ] **Step 6: Commit desktop detection**

```bash
git add internal/detect/desktop.go internal/detect/desktop_test.go
git commit -m "feat: detect desktop and management tools"
```

---

### Task 8: Orchestrate deterministic concurrent environment scans

**Files:**
- Create: `internal/scan/service.go`
- Create: `internal/scan/service_test.go`
- Create: `internal/bootstrap/linux.go`
- Create: `internal/bootstrap/linux_test.go`
- Modify: `app.go`
- Modify: `app_test.go`
- Modify: `main.go`

**Interfaces:**
- Consumes: `platform.SystemProbe` and `platform.PathProbe`
- Produces: `type ComponentProbe interface { Descriptor() domain.Component; Detect(context.Context, domain.SystemInfo, []string) (domain.Component, error) }`
- Produces: `func NewService(system platform.SystemProbe, paths platform.PathProbe, components []ComponentProbe, clock func() time.Time) *Service`
- Produces: `func (s *Service) Scan(context.Context) (domain.EnvironmentSnapshot, error)`
- Produces: `func (s *Service) ComponentCount() int`
- Produces: `bootstrap.NewLinuxScanner() *scan.Service`
- Produces: `App.ScanEnvironment() (domain.EnvironmentSnapshot, error)` as the only Phase-1 Wails operation
- Consumes: domain contracts and Phase-1 detectors

- [ ] **Step 1: Write failing orchestration tests**

Use fake probes with stable descriptors for service behavior, plus a production-factory contract test, to assert:

- system failure returns `SCAN_FAILED` and does not start component probes
- PATH probe failure returns `SCAN_FAILED` and does not start component probes
- component probes run concurrently (barrier-based test, not wall-clock guessing)
- a component error or panic becomes a `failed` component without aborting other results
- result order follows catalog order regardless of completion order
- `ScannedAt` uses the injected clock
- counts are recalculated after all components finish
- `bootstrap.NewLinuxScanner()` returns non-nil and reports exactly eight production components through `ComponentCount()`

- [ ] **Step 2: Run tests and verify failure**

Run: `go test ./internal/scan ./internal/bootstrap -v`

Expected: FAIL because `Service` and the Linux bootstrap factory are undefined.

- [ ] **Step 3: Implement scan orchestration**

Probe system and PATH once, then use one goroutine per bounded catalog entry. Pass the same immutable system value and copied path slice to each component, recover at the component boundary, and use `Descriptor()` to retain ID/name/category when an error or panic becomes `failed`. Collect by original index and honor context cancellation. Do not create a general worker pool in Phase 1 because the catalog has only eight entries.

- [ ] **Step 4: Wire real Linux probes in `main.go`**

In `internal/bootstrap/linux.go`, construct one bounded exec runner, `linux.NewSystemProbe()`, `linux.NewPathProbe()`, a `detect.CommandComponentProbe` for each `CoreCLISpecs()` entry, and a `detect.DesktopComponentProbe` for each `DesktopSpecs()` entry using `detect.DpkgQuery`, `os.DirFS("/")`, and the best-effort result of `os.UserHomeDir()`. Preserve catalog order: three CLI probes followed by five desktop/management probes. Pass all eight to `scan.NewService` with `time.Now`.

In `internal/bootstrap/linux_test.go`, assert the factory returns non-nil and its exported `ComponentCount()` is exactly eight; this catches catalog entries that were accidentally omitted from production wiring without executing host probes.

In `main.go`, instantiate `App` with `bootstrap.NewLinuxScanner()`, pass `app.startup` as `OnStartup`, and bind only `app` in `wails.Run`.

- [ ] **Step 5: Implement the Wails-facing method**

```go
type Scanner interface {
	Scan(context.Context) (domain.EnvironmentSnapshot, error)
}

type App struct {
	ctx     context.Context
	scanner Scanner
}

func NewApp(scanner Scanner) *App {
	if scanner == nil {
		return nil
	}
	return &App{scanner: scanner}
}

func (a *App) ScanEnvironment() (domain.EnvironmentSnapshot, error) {
	if a == nil || a.scanner == nil {
		return domain.EnvironmentSnapshot{}, domain.NewPublicError(domain.ErrScanFailed, "环境扫描服务不可用", nil)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.scanner.Scan(ctx)
}
```

Update `app_test.go` with a fake scanner. Assert `NewApp(nil)` returns nil, `startup` supplies its context to the scanner, `ScanEnvironment()` returns the fake snapshot, and the pre-startup fallback context is non-nil.

- [ ] **Step 6: Run all Go tests and race detection**

Run:

```bash
go test ./...
go test -race ./internal/scan ./internal/detect ./internal/platform/...
```

Expected: PASS with no race report.

- [ ] **Step 7: Commit scan orchestration**

```bash
git add internal/scan internal/bootstrap app.go app_test.go main.go
git commit -m "feat: orchestrate environment scans"
```

---

### Task 9: Build the typed frontend scan adapter and lifecycle hook

**Files:**
- Create (generated): `frontend/wailsjs/go/main/App.js`
- Create (generated): `frontend/wailsjs/go/main/App.d.ts`
- Create (generated): `frontend/wailsjs/go/models.ts`
- Create: `frontend/src/domain.ts`
- Create: `frontend/src/services/osverse.ts`
- Create: `frontend/src/hooks/useEnvironmentScan.ts`
- Create: `frontend/src/hooks/useEnvironmentScan.test.tsx`

**Interfaces:**
- Produces: `scanEnvironment(): Promise<EnvironmentSnapshot>`
- Produces: `useEnvironmentScan(): { snapshot; phase; error; refresh }`
- Phase union: `'idle' | 'scanning' | 'ready' | 'error'`

- [ ] **Step 1: Generate the narrow Wails bindings**

After Task 8 compiles, run:

```bash
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 generate module
```

Expected: generated files under `frontend/wailsjs/`; `App.d.ts` exposes `ScanEnvironment(): Promise<domain.EnvironmentSnapshot>` and no generic command or filesystem method.

- [ ] **Step 2: Write failing hook tests**

Mock `scanEnvironment` and assert initial automatic scan, scanning state, successful snapshot, public error display, and a manual `refresh()` replacing the previous result without clearing it during refresh.

- [ ] **Step 3: Run the focused test and verify failure**

Run: `npm --prefix frontend test -- useEnvironmentScan.test.tsx`

Expected: FAIL because the hook and service do not exist.

- [ ] **Step 4: Add frontend-safe domain types**

Mirror the JSON field names from Go exactly. Define `ComponentStatus` as the nine-value string union and avoid frontend-only aliases.

- [ ] **Step 5: Implement the narrow Wails adapter**

Import `ScanEnvironment` from `../../wailsjs/go/main/App` in production and map its generated model into the frontend-safe domain type. For browser tests, accept an injected function through a module-level test seam; do not expose `window.runtime` or generic invocation to components.

- [ ] **Step 6: Implement the lifecycle hook**

Use an `AbortController`/generation counter so a slower earlier refresh cannot overwrite a newer result. Preserve the last good snapshot during refresh and set an accessible error string on failure.

- [ ] **Step 7: Run hook tests and typecheck**

Run:

```bash
npm --prefix frontend test -- useEnvironmentScan.test.tsx
npm --prefix frontend run typecheck
```

Expected: PASS.

- [ ] **Step 8: Commit frontend data flow**

```bash
git add frontend/wailsjs frontend/src/domain.ts frontend/src/services frontend/src/hooks
git commit -m "feat: connect dashboard to environment scan"
```

---

### Task 10: Implement the approved Cockpit-style dashboard

**Files:**
- Modify: `frontend/src/App.tsx`
- Create: `frontend/src/App.css`
- Create: `frontend/src/components/Sidebar.tsx`
- Create: `frontend/src/components/SummaryCards.tsx`
- Create: `frontend/src/components/ToolSection.tsx`
- Create: `frontend/src/components/ToolCard.tsx`
- Create: `frontend/src/components/StatusBadge.tsx`
- Create: `frontend/src/components/Dashboard.test.tsx`

**Interfaces:**
- Consumes: `useEnvironmentScan()` and Phase-1 domain types
- Produces: accessible dashboard sections and buttons
- Phase-1 action behavior: install/update/configure buttons are disabled with the visible text `将在下一阶段开放`; refresh is functional

- [ ] **Step 1: Write failing dashboard tests**

Render representative snapshots and assert:

- system/version/architecture and scan time are visible
- ready, total, and attention counts render
- CLI, desktop, and management categories render separately
- installed, missing, conflict, unsupported, and broken use distinct text labels
- every installation path is visible for a conflict
- refresh calls the hook action
- unavailable mutation actions are disabled and explain the phase boundary
- loading and error states use `role="status"` and `role="alert"`

- [ ] **Step 2: Run dashboard tests and verify failure**

Run: `npm --prefix frontend test -- Dashboard.test.tsx`

Expected: FAIL because dashboard components do not exist.

- [ ] **Step 3: Implement semantic components**

Use buttons, headings, lists, and status text rather than clickable `div` elements. Keep the left sidebar icons accompanied by `aria-label`; preserve visible navigation labels at normal desktop widths.

- [ ] **Step 4: Implement the confirmed visual system**

In `App.css`, define tokens for:

```css
:root {
  --accent-blue: #2e72e5;
  --accent-teal: #16a7a2;
  --surface: rgba(255, 255, 255, 0.92);
  --text: #1d2940;
  --muted: #8390a6;
  --success: #18a976;
  --warning: #e68a27;
  --danger: #cc5e57;
  --radius-card: 18px;
}
```

Apply the approved pale blue/teal radial background, floating pill sidebar, large white cards, restrained shadows, and responsive collapse below 900px. Respect `prefers-reduced-motion` and maintain WCAG AA contrast for status text.

- [ ] **Step 5: Run frontend verification**

Run:

```bash
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
```

Expected: all commands exit 0.

- [ ] **Step 6: Run a development UI smoke check**

Run `go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_40`, confirm the window opens, trigger refresh, and verify the rendered values match `claude --version`, `codex --version`, `opencode --version`, and `command -v` results on the test machine. Record discrepancies as issues before continuing.

- [ ] **Step 7: Commit the dashboard**

```bash
git add frontend/src
git commit -m "feat: add environment status dashboard"
```

---

### Task 11: Add CI, documentation, and Phase-1 acceptance checks

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `README.md`
- Create: `docs/testing/phase-1-acceptance.md`
- Modify: `.gitignore`

**Interfaces:**
- Produces CI jobs: `go-test`, `frontend-test`, `linux-build-webkit40`
- Produces contributor commands documented in README

- [ ] **Step 1: Write the Phase-1 acceptance checklist**

Document exact manual cases for Ubuntu 20.04 and 22.04 x86_64: no CLI installed, one external CLI, multiple conflicting CLI paths, broken version command, desktop apps absent/present, Bash, Zsh, and unsupported architecture display.

- [ ] **Step 2: Add least-privilege CI**

Set workflow permissions to `contents: read`. Because [GitHub retired its hosted `ubuntu-20.04` runner on 2025-04-15](https://github.blog/changelog/2025-01-15-github-actions-ubuntu-20-runner-image-brownout-dates-and-other-breaking-changes/), run the compatibility build as a container job with `runs-on: ubuntu-24.04` and `container: ubuntu:20.04`; install `git ca-certificates build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.0-dev`. Pin the verified current action releases (`actions/checkout@v7.0.1`, `actions/setup-go@v7.0.0`, `actions/setup-node@v7.0.0`) and pin Go `1.23.x` plus Node `22.x`. Run:

```bash
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
go test ./...
go test -race ./internal/scan ./internal/detect ./internal/platform/...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -tags webkit2_40
```

- [ ] **Step 3: Write README setup and safety boundaries**

Include supported systems, current read-only Phase-1 capability, development dependencies, exact test/build commands, repository roadmap links, and an explicit statement that install/configure buttons are not active yet.

- [ ] **Step 4: Run fresh full verification**

Run:

```bash
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
go test ./...
go test -race ./internal/scan ./internal/detect ./internal/platform/...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -tags webkit2_40
git diff --check
```

Expected: every command exits 0 and `build/bin/osverse` exists.

- [ ] **Step 5: Review against the Phase-1 acceptance boundary**

Confirm no code path invokes npm installation, apt mutation, shell-profile writes, credential storage, API calls, arbitrary command execution, or arbitrary downloads. Confirm the only bound application operation is `ScanEnvironment`.

- [ ] **Step 6: Commit Phase-1 delivery infrastructure**

```bash
git add .github README.md docs/testing .gitignore
git commit -m "ci: verify Phase 1 environment scanner"
```

- [ ] **Step 7: Push only after verification and review**

Run:

```bash
git status --short
git log --oneline --decorate -12
git push origin main
```

Expected: clean working tree and remote `main` updated to the locally reviewed Phase-1 head.
