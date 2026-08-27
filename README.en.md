# Osverse

<p align="center">
  <img src="build/appicon.png" width="112" alt="Osverse logo">
</p>

<p align="center">
  <strong>Your local control center for AI development tools.</strong><br>
  Detect, install, update, launch, and safely remove Claude Code, Codex CLI, OpenCode, DeepSeek Harness, Qwen Code, Kimi Code, GitHub Copilot CLI, desktop apps, and third-party API profiles from one place.
</p>

<p align="center">
  <a href="README.md">简体中文</a> ·
  <a href="https://github.com/Oswald-Hao/Osverse/releases">Download</a> ·
  <a href="docs/testing/linux-v1-acceptance.md">Linux acceptance</a> ·
  <a href="docs/testing/windows-v1-acceptance.md">Windows acceptance</a> ·
  <a href="CONTRIBUTING.md">Contribute</a>
</p>

<p align="center">
  <a href="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/Oswald-Hao/Osverse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Oswald-Hao/Osverse?include_prereleases&sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Oswald-Hao/Osverse"></a>
  <img alt="Ubuntu 20.04 and 22.04" src="https://img.shields.io/badge/Ubuntu-20.04%20%7C%2022.04-E95420?logo=ubuntu&logoColor=white">
  <img alt="Windows 10 and 11" src="https://img.shields.io/badge/Windows-10%20%7C%2011-0078D4?logo=windows&logoColor=white">
  <img alt="Local first" src="https://img.shields.io/badge/local--first-no%20telemetry-159957">
</p>

> [!IMPORTANT]
> Osverse supports **Ubuntu 20.04/22.04 x86_64** and **Windows 10/11 x64**. The Windows build passes native tests plus installer, launch, and uninstall smoke tests. macOS follows after Windows stabilizes.

![Osverse environment and proxy overview](docs/assets/screenshots/连接状态检查.png)

## Why Osverse?

Moving to a new machine should not mean spending hours rediscovering runtimes, proxy settings, CLI locations, and API configuration formats. Osverse turns that setup work into an inspectable desktop workflow while preserving tools you already installed.

- **Find what is already there.** Claude Code, Codex CLI, OpenCode, Qwen Code, Kimi Code, and GitHub Copilot CLI are discovered from the user's effective command paths; they do not need to live inside an Osverse directory.
- **Run DeepSeek Harness without environment setup.** Osverse pins the official Harness and Node.js versions, verifies every locked dependency, runs no npm lifecycle scripts, launches the official web workspace, and can safely apply a verified third-party API profile to Harness.
- **Run Qwen Code without a system Node.js.** Osverse installs the official standalone archive with pinned version, length, and SHA-256, then uses its private Node runtime. Confirmed OpenAI-compatible API profiles can be applied directly.
- **Connect Kimi Code to third-party APIs natively.** Osverse verifies the official standalone binary and writes confirmed OpenAI Chat, OpenAI Responses, or Anthropic Messages profiles through Kimi's native provider format while preserving the vendor's exact model ID.
- **Install GitHub Copilot CLI from verified artifacts.** Osverse pins GitHub's official standalone package and disables upstream self-update so managed files cannot bypass Osverse's release verification. Authentication and subscription remain owned by GitHub Copilot.
- **Control every real installation.** Multiple locations are shown separately. Before launch, the backend rescans and verifies file identity instead of executing an arbitrary frontend-provided path.
- **Install without surrendering control.** Every supported CLI install starts with a backend-generated plan, verifies exact size and SHA-256, switches versions atomically, and can recover after interruption.
- **Preview and recover removals.** User and Osverse-managed files go to the desktop Trash, while system packages use a fixed privileged action. Config, credentials, and sessions are preserved by default.
- **Keep API credentials local.** Third-party API profiles are stored with AES-256-GCM encryption, can be edited safely, are probed for protocol compatibility, and are applied only after a second confirmation.
- **Use your proxy, not a global rewrite.** Enter one loopback port; Osverse detects HTTP, HTTPS CONNECT, and SOCKS5, remembers the verified protocol and port for the current user, and uses it only for its own downloads. It stores no proxy credentials and shows latency as green at `≤500ms`, yellow at `501–1000ms`, and red above `1000ms`.
- **Manage the surrounding desktop stack.** Install, update, and launch supported desktop clients and API-switching tools from the same dashboard.
- **Update Osverse in place.** Startup checks the official Release feed, shows the version and release notes, then downloads, verifies, and applies the matching package only after confirmation—no uninstall or manual GitHub trip.

## What it manages

| Area | Ubuntu 20.04/22.04 | Windows 10/11 x64 |
| --- | --- | --- |
| Core CLI | Claude Code, Codex CLI, OpenCode CLI, DeepSeek Harness, Qwen Code, Kimi Code, and GitHub Copilot CLI detection plus transactional install/update | Same; Harness, Qwen Code, Kimi Code, and Copilot CLI use verified private runtimes |
| Desktop apps | Claude Desktop on Ubuntu 22.04+, OpenCode Desktop | Claude Desktop, OpenCode Desktop, ChatGPT Desktop, and Codex Desktop (detected separately) |
| API tools | CC Switch, Cockpit Tools | CC Switch, Cockpit Tools |
| API profiles | Anthropic/OpenAI-compatible endpoint, model, Base URL, encrypted key | Same; master key protected by current-user DPAPI |
| Component controls | Launch verified installations, resolve location conflicts, preview and safely remove | Same; managed CLIs move to an Osverse recovery directory |
| Networking | Direct or loopback HTTP / HTTPS CONNECT / SOCKS5 proxy | Same |
| Osverse update | Atomic AppImage/portable replacement and system-confirmed `.deb` updates | Verified NSIS installer launch with automatic handoff from the old version |
| Auditability | Redacted history, checksums, SPDX SBOM, provenance | Same |

Claude Desktop's official Linux package requires Ubuntu 22.04 or newer, so Osverse reports it as unsupported on Ubuntu 20.04. ChatGPT Desktop has no official Linux build and is not installed by Osverse.

### DeepSeek Harness

Select **DeepSeek Harness** under Core CLI, preview the plan, and confirm installation. Osverse installs the official `@deepseek-ai/dsh`, a private Node.js runtime, and the target's native dependency; no preinstalled Node/npm or global package change is required. Launching it runs:

```bash
dsh web
```

On Linux, the workspace defaults to `http://127.0.0.1:3080`; on Windows, Osverse selects a free loopback port and opens the default browser only after the real page is reachable. The terminal opened at launch hosts the Harness service, so close it before updating or removing Harness. You can configure a provider in Harness's Models page, or save and probe an Osverse API profile and select **DeepSeek Harness** in the compatibility matrix. Osverse chooses a confirmed OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages route; backs up and transactionally updates `$DSH_HOME/settings.yaml` plus `$DSH_HOME/.credentials.yaml`; creates a dedicated `osverse` provider; and selects the exact model for new sessions. The key is written only to Harness's credential document, while settings retain the `OSVERSE_API_KEY` reference. Unrelated providers, comments, and credentials survive. Writes coordinate with a running Harness through its official `.lock` convention, and removal still preserves the Harness home. See the [DeepSeek Harness integration guide](docs/guides/deepseek-harness.md) for details.

Harness is currently an upstream Developer Preview. Osverse pins the tested contract instead of silently following npm `latest`; upgrades arrive with a tested Osverse release. Linux x64 and Windows x64 are wired into the app today. The same verified installer is ready for macOS x64/arm64 and will be enabled with the Osverse macOS release.

### Qwen Code

Select **Qwen Code** under Core CLI and confirm the install plan. Osverse downloads the official `v0.21.13` standalone archive, which includes Node.js and target-native modules, so it never changes a global Node/npm installation. Official metadata is pinned for Linux x64/arm64, Windows x64, and macOS x64/arm64; installation is enabled in the current Linux x64 and Windows x64 apps.

After an API profile confirms OpenAI Chat Completions compatibility, it can also be applied to Qwen Code. Osverse backs up and atomically updates `~/.qwen/settings.json`, adds a dedicated `osverse` provider with the exact model ID and Base URL, selects that exact pair through Qwen's built-in `openai` protocol, and preserves unrelated providers. See the [Qwen Code integration guide](docs/guides/qwen-code.md).

### Kimi Code

Select **Kimi Code** under Core CLI to install the official `0.36.1` standalone binary. Osverse pins the official URL, exact byte length, and SHA-256 for Linux x64/arm64, Windows x64/arm64, and macOS x64/arm64; installation is enabled in the current Linux x64 and Windows x64 apps. The managed command sets `KIMI_CODE_NO_AUTO_UPDATE=1`, keeping runtime upgrades inside Osverse's verified release path.

After a profile confirms OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages, select Kimi Code in the compatibility matrix. Osverse backs up and atomically updates `~/.kimi-code/config.toml`, preserves unrelated TOML, creates an owned `osverse` provider and model alias, and passes the vendor's original model ID—such as `deepseek/deepseek-v4-flash`—to Kimi without adding an `osverse/` prefix. See the [Kimi Code integration guide](docs/guides/kimi-code.md).

### GitHub Copilot CLI

Select **GitHub Copilot CLI** under Core CLI to install the official `v1.0.80` standalone package. Official URL, exact length, and SHA-256 metadata are pinned for Linux x64/arm64, Windows x64, and macOS x64/arm64; installation is currently enabled in the Linux x64 and Windows x64 apps. The managed command always adds `--no-auto-update`, so runtime upgrades arrive only through a tested Osverse release.

Follow the prompt on first launch or run `copilot login` in a terminal. An active GitHub Copilot subscription is required. Copilot CLI does not consume Osverse's generic third-party API profiles, so no profile key, Base URL, or model is written into its configuration. See the [GitHub Copilot CLI integration guide](docs/guides/github-copilot-cli.md).

## Install

Open the [latest GitHub prerelease](https://github.com/Oswald-Hao/Osverse/releases) and download the artifact matching your platform.

### Windows (recommended installer)

Download `osverse-*-windows-amd64-setup.exe` and follow the installer. It installs only for the current user and does not need administrator access; the WebView2 bootstrapper is embedded. A portable zip and a standalone exe are also available.

```powershell
(Get-FileHash .\osverse-*-windows-amd64-setup.exe -Algorithm SHA256).Hash
Get-Content .\SHA256SUMS
```

### Ubuntu

#### AppImage

```bash
sha256sum --check SHA256SUMS --ignore-missing
chmod +x osverse-*-linux-amd64-ubuntuXX.XX.AppImage
./osverse-*-linux-amd64-ubuntuXX.XX.AppImage
```

#### Debian package

```bash
sha256sum --check SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

A portable `.tar.gz` is also published. Every release includes one unified checksum file, an SPDX 2.3 SBOM, structured update metadata, and GitHub build-provenance attestations. Verify an artifact with:

```bash
gh attestation verify ./osverse_*.deb --repo Oswald-Hao/Osverse
```

After the first installation, Osverse checks the official Release feed quietly at startup and prompts only when a newer release is available. Update downloads reuse the verified, persisted loopback proxy without depending on GitHub's anonymous REST rate limit for shared proxy exits; the frontend cannot provide an artifact URL, path, or digest.

## Safety model

Osverse is deliberately narrower than a generic package manager.

- Scans do not execute shell startup files or desktop entries.
- Existing commands are detected at their real locations and never need to be moved into an Osverse-managed path.
- Launch and removal rescan and verify target identity; the frontend cannot supply an arbitrary executable path or system package name.
- User files are moved transactionally to the Freedesktop Trash, while config, API credentials, and login sessions remain untouched by default.
- On Windows, locked managed-CLI identities move to `%LOCALAPPDATA%\Osverse\recovery`; desktop removal accepts only fixed WinGet IDs, Microsoft Store IDs, MSI ProductCodes, or trusted uninstaller paths.
- Downloads come from a built-in allowlist and must match both an exact byte count and SHA-256 digest. Qwen Code, Kimi Code, and Copilot CLI extraction reject traversal, links, special entries, and unexpected archive layouts. DeepSeek Harness additionally verifies every npm tarball against an embedded lockfile with SHA-512 and executes no package install scripts.
- Runtime commit, command entry activation, and PATH updates form one final installation transaction. Failures remove only a newly written version, preserve a reverified existing version, and explicitly report residue that could not be cleaned automatically.
- Osverse self-update accepts only semantic versions and platform artifacts from the fixed official repository and release-manifest path. Stable builds ignore prereleases; prerelease builds can advance to newer prereleases or stable releases.
- Managed CLI versions live under `~/.local/share/osverse/tools`; an external command with the same name is not overwritten.
- CLI commits use immutable version directories, atomic symlink replacement, immediate rollback, and a `0600` crash-recovery journal.
- API keys are never returned in profile lists, history, or logs. Windows protects the AES-256-GCM master key with current-user DPAPI. Private and reserved endpoints require explicit acknowledgement.
- Privileged Claude Desktop installation uses a fixed PolicyKit helper; it cannot execute user-supplied commands.
- Osverse has no telemetry and does not modify the terminal or operating system's global proxy settings.

See [SECURITY.md](SECURITY.md) for vulnerability reporting and the full trust boundary.

## Screenshots

<details>
<summary><strong>Existing CLI discovery — including two external Claude Code paths</strong></summary>

![Osverse detects existing Claude Code and Codex CLI installations](docs/assets/screenshots/cli工具.png)

</details>

<details>
<summary><strong>Desktop application detection and platform compatibility</strong></summary>

![Osverse desktop application management](docs/assets/screenshots/桌面应用.png)

</details>

<details>
<summary><strong>CC Switch and Cockpit Tools management</strong></summary>

![Osverse third-party API tool management](docs/assets/screenshots/管理工具.png)

</details>

<details>
<summary><strong>Encrypted third-party API profiles</strong></summary>

![Osverse encrypted API profile editor](docs/assets/screenshots/api.png)

</details>

All screenshots above are from a real Osverse session on Ubuntu 22.04, not design mockups. Windows uses the same frontend with native platform services underneath.

## Build from source

The pinned toolchain is Go 1.25.12, Node.js 22.23.2, and Wails 2.15.0.

```bash
sudo apt-get update
sudo apt-get install build-essential libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
go test ./...
go test -race ./...
go vet ./...

# Ubuntu 22.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.15.0 dev -tags webkit2_40

# Ubuntu 20.04 (WebKitGTK 2.38 baseline)
go run github.com/wailsapp/wails/v2/cmd/wails@v2.15.0 dev -tags webkit2_36
```

On Windows 10/11 x64 (PowerShell, with Go, Node.js, WebView2, and the Wails build environment):

```powershell
npm --prefix frontend ci
go test ./...
go test -race ./...
go vet ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.15.0 dev

# Build a per-user NSIS installer
go run github.com/wailsapp/wails/v2/cmd/wails@v2.15.0 build -platform windows/amd64 -nsis -webview2 embed -installscope user -trimpath
```

Release packaging is checksum-pinned and produces Windows NSIS/portable artifacts plus Linux `.deb`, `.AppImage`, portable tar, checksums, SBOM, and update metadata:

```bash
build/linux/fetch-appimage-tools.sh build/tools/appimage
build/linux/package.sh 0.1.0 build/bin/osverse ubuntu22.04 build/release/ubuntu22.04 build/tools/appimage
```

## Project workflow

Every bug, feature, dependency upgrade, or documentation task starts on a separate branch from the latest `origin/dev`. Direct development or commits on `dev`, `beta`, and `main` are forbidden, and unrelated tasks must not share a branch. Changes move in one direction: `task branch` → `dev` → `beta` → `main`. Feature pull requests run the complete CI suite before entering `dev`; promotion pull requests from `dev` to `beta` and from `beta` to `main` reuse that verified result and run only the promotion-path gate. CI never rebuilds solely for promotion and never pushes or synchronizes branches. `main` receives only gated candidates, and release tags must point to `main` history. See the [protected branch policy](docs/governance/branch-policy.md).

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), open a focused issue, and keep platform expansion consistent with the safety boundaries above.

## License

Apache-2.0. See [LICENSE](LICENSE).

Osverse is an independent project. Claude, Codex, OpenAI, OpenCode, DeepSeek, Qwen, Kimi, Moonshot AI, GitHub, Copilot, and other product names belong to their respective owners; inclusion indicates interoperability, not endorsement. A copy of the upstream Copilot CLI license is installed with every managed runtime.
