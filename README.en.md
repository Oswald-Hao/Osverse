# Osverse

<p align="center">
  <img src="build/appicon.png" width="112" alt="Osverse logo">
</p>

<p align="center">
  <strong>Your local control center for AI development tools.</strong><br>
  Detect, install, update, launch, and safely remove Claude Code, Codex CLI, OpenCode, desktop apps, and third-party API profiles from one place.
</p>

<p align="center">
  <a href="README.md">简体中文</a> ·
  <a href="https://github.com/Oswald-Hao/Osverse/releases">Download</a> ·
  <a href="docs/testing/linux-v1-acceptance.md">Test matrix</a> ·
  <a href="CONTRIBUTING.md">Contribute</a>
</p>

<p align="center">
  <a href="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/Oswald-Hao/Osverse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Oswald-Hao/Osverse?include_prereleases&sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Oswald-Hao/Osverse"></a>
  <img alt="Ubuntu 20.04 and 22.04" src="https://img.shields.io/badge/Ubuntu-20.04%20%7C%2022.04-E95420?logo=ubuntu&logoColor=white">
  <img alt="Local first" src="https://img.shields.io/badge/local--first-no%20telemetry-159957">
</p>

> [!IMPORTANT]
> Osverse is currently a **Linux prerelease** for Ubuntu 20.04/22.04 on x86_64. Windows and macOS are planned only after the Linux release is stable.

![Osverse environment and proxy overview](docs/assets/screenshots/连接状态检查.png)

## Why Osverse?

Moving to a new machine should not mean spending hours rediscovering runtimes, proxy settings, CLI locations, and API configuration formats. Osverse turns that setup work into an inspectable desktop workflow while preserving tools you already installed.

- **Find what is already there.** Claude Code, Codex CLI, and OpenCode are discovered from the user's effective command paths; they do not need to live inside an Osverse directory.
- **Control every real installation.** Multiple locations are shown separately. Before launch, the backend rescans and verifies file identity instead of executing an arbitrary frontend-provided path.
- **Install without surrendering control.** Every supported CLI install starts with a backend-generated plan, verifies exact size and SHA-256, switches versions atomically, and can recover after interruption.
- **Preview and recover removals.** User and Osverse-managed files go to the desktop Trash, while system packages use a fixed privileged action. Config, credentials, and sessions are preserved by default.
- **Keep API credentials local.** Third-party API profiles are stored with AES-256-GCM encryption, can be edited safely, are probed for protocol compatibility, and are applied only after a second confirmation.
- **Use your proxy, not a global rewrite.** Enter one loopback port; Osverse detects HTTP, HTTPS CONNECT, and SOCKS5 and uses the result only for its own downloads.
- **Manage the surrounding desktop stack.** Install, update, and launch supported desktop clients and API-switching tools from the same dashboard.

## What it manages

| Area | Supported in Linux prerelease |
| --- | --- |
| Core CLI | Claude Code, Codex CLI, OpenCode CLI detection and transactional install/update |
| Desktop apps | Claude Desktop on Ubuntu 22.04+, OpenCode Desktop |
| API tools | CC Switch, Cockpit Tools |
| API profiles | Anthropic-compatible and OpenAI-compatible endpoints, model, Base URL, encrypted key |
| Component controls | Launch every verified installation, handle per-location conflicts, preview and safely remove |
| Networking | Direct connection or loopback HTTP / HTTPS CONNECT / SOCKS5 proxy |
| Auditability | Redacted local operation history, release checksums, SPDX SBOM, build provenance |

Claude Desktop's official Linux package requires Ubuntu 22.04 or newer, so Osverse reports it as unsupported on Ubuntu 20.04. ChatGPT Desktop has no official Linux build and is not installed by Osverse.

## Install

Open the [latest GitHub prerelease](https://github.com/Oswald-Hao/Osverse/releases) and download the artifact matching your Ubuntu version.

### AppImage

```bash
sha256sum --check SHA256SUMS --ignore-missing
chmod +x osverse-*-linux-amd64-ubuntuXX.XX.AppImage
./osverse-*-linux-amd64-ubuntuXX.XX.AppImage
```

### Debian package

```bash
sha256sum --check SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

A portable `.tar.gz` is also published. Every release includes one unified checksum file, an SPDX 2.3 SBOM, structured update metadata, and GitHub build-provenance attestations. Verify an artifact with:

```bash
gh attestation verify ./osverse_*.deb --repo Oswald-Hao/Osverse
```

## Safety model

Osverse is deliberately narrower than a generic package manager.

- Scans do not execute shell startup files or desktop entries.
- Existing commands are detected at their real locations and never need to be moved into an Osverse-managed path.
- Launch and removal rescan and verify target identity; the frontend cannot supply an arbitrary executable path or system package name.
- User files are moved transactionally to the Freedesktop Trash, while config, API credentials, and login sessions remain untouched by default.
- Downloads come from a built-in allowlist and must match both an exact byte count and SHA-256 digest.
- Managed CLI versions live under `~/.local/share/osverse/tools`; an external command with the same name is not overwritten.
- CLI commits use immutable version directories, atomic symlink replacement, immediate rollback, and a `0600` crash-recovery journal.
- API keys are never returned in profile lists, history, or logs. Private and reserved endpoints require explicit acknowledgement.
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

All screenshots above are from a real Osverse session on Ubuntu 22.04, not design mockups.

## Build from source

The pinned toolchain is Go 1.25.12, Node.js 22.23.2, and Wails 2.13.0.

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
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_40

# Ubuntu 20.04 (WebKitGTK 2.38 baseline)
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_36
```

Release packaging is checksum-pinned and produces `.deb`, `.AppImage`, portable tar, checksums, SBOM, and update metadata:

```bash
build/linux/fetch-appimage-tools.sh build/tools/appimage
build/linux/package.sh 0.1.0 build/bin/osverse ubuntu22.04 build/release/ubuntu22.04 build/tools/appimage
```

## Project workflow

Changes move in one direction: `feature branch` → `dev` → `beta` → `main`. The full CI suite runs once for each pull request and is not duplicated by a post-merge push run. `main` receives only promoted candidates, and release tags must point to `main` history.

Contributions are welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md), open a focused issue, and keep platform expansion consistent with the safety boundaries above.

## License

Apache-2.0. See [LICENSE](LICENSE).

Osverse is an independent project. Claude, Codex, OpenAI, OpenCode, and other product names belong to their respective owners; inclusion indicates interoperability, not endorsement.
