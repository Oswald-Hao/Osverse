# Osverse

<p align="center">
  <img src="build/appicon.png" width="112" alt="Osverse 图标">
</p>

<p align="center">
  <strong>你的本地 AI 开发环境控制台。</strong><br>
  在一个界面里检测、安装、更新、启动和安全移除 Claude Code、Codex CLI、OpenCode、桌面应用与第三方 API。
</p>

<p align="center">
  <a href="README.en.md">English</a> ·
  <a href="https://github.com/Oswald-Hao/Osverse/releases">下载</a> ·
  <a href="docs/testing/linux-v1-acceptance.md">Linux 验收</a> ·
  <a href="docs/testing/windows-v1-acceptance.md">Windows 验收</a> ·
  <a href="CONTRIBUTING.md">参与贡献</a>
</p>

<p align="center">
  <a href="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/Oswald-Hao/Osverse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Oswald-Hao/Osverse?include_prereleases&sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Oswald-Hao/Osverse"></a>
  <img alt="Ubuntu 20.04 与 22.04" src="https://img.shields.io/badge/Ubuntu-20.04%20%7C%2022.04-E95420?logo=ubuntu&logoColor=white">
  <img alt="Windows 10 与 11" src="https://img.shields.io/badge/Windows-10%20%7C%2011-0078D4?logo=windows&logoColor=white">
  <img alt="本地优先" src="https://img.shields.io/badge/本地优先-无遥测-159957">
</p>

> [!IMPORTANT]
> Osverse 目前支持 **Ubuntu 20.04/22.04 x86_64** 与 **Windows 10/11 x64**。Windows 版已通过原生 Runner 的测试、安装、启动和卸载验证；macOS 将在 Windows 版稳定后适配。

![Osverse 环境状态与代理检测](docs/assets/screenshots/连接状态检查.png)

## 为什么做 Osverse？

换一台电脑，不应该重新花几个小时找运行时、代理端口、CLI 安装位置和各不相同的 API 配置文件。Osverse 把这些工作收进一个可检查、可确认、可恢复的桌面流程，并且尊重你已经安装好的工具。

- **已有工具无需搬家。** Claude Code、Codex CLI、OpenCode 会从用户真实可用的命令路径中发现，不要求放进 Osverse 目录。
- **每个真实安装都能控制。** 同一工具存在多个安装位置时会分别展示；启动前由后端重新扫描并核对文件身份，不会执行界面传入的任意路径。
- **安装过程可解释。** 安装前显示后端生成的变更计划；下载同时校验固定长度和 SHA-256；版本原子切换，失败和异常退出都可恢复。
- **移除可以预览和恢复。** 用户安装和 Osverse 管理的文件先进入系统回收站；系统软件包只通过固定提权动作移除，配置、凭据和会话默认保留。
- **API Key 留在本地。** 第三方 API 档案使用 AES-256-GCM 加密，支持安全编辑，先探测协议兼容性，再由用户二次确认写入目标 CLI。
- **代理只服务 Osverse。** 只需输入 `127.0.0.1` 上的端口，软件自动识别 HTTP、HTTPS CONNECT、SOCKS5，不改终端或系统全局代理。
- **桌面生态统一管理。** 支持的桌面客户端与 API 切换工具可以在同一面板安装、更新和启动。
- **Osverse 自身也能原地更新。** 启动时检查官方 Release，先展示版本与更新说明，再按当前安装方式下载、校验并启动更新，不必卸载或重新去 GitHub 找安装包。

## 能管理什么

| 范围 | Ubuntu 20.04/22.04 | Windows 10/11 x64 |
| --- | --- | --- |
| 核心 CLI | Claude Code、Codex CLI、OpenCode CLI 的检测与事务式安装/更新 | Claude Code、Codex CLI、OpenCode CLI 的检测与固定来源安装/更新 |
| 桌面应用 | Claude Desktop（Ubuntu 22.04+）、OpenCode Desktop | Claude Desktop、OpenCode Desktop、ChatGPT（含 Codex） |
| API 管理工具 | CC Switch、Cockpit Tools | CC Switch、Cockpit Tools |
| API 档案 | Anthropic/OpenAI 兼容协议、模型名、Base URL、加密 Key | 同左；主密钥由当前 Windows 用户的 DPAPI 保护 |
| 组件控制 | 启动所有已验证安装、按位置处理冲突、预览并安全移除 | 同左；受管 CLI 移入 Osverse 恢复区 |
| 网络 | 直连或本机 HTTP / HTTPS CONNECT / SOCKS5 代理 | 同左 |
| Osverse 更新 | AppImage 原子替换并重启；便携包安全解包后原子替换；`.deb` 交给系统安装器确认 | 校验 NSIS 安装包后启动并自动退出旧版本 |
| 可审计性 | 脱敏操作历史、校验和、SPDX SBOM、构建来源证明 | 同左 |

Claude Desktop 官方 Linux 包的最低要求是 Ubuntu 22.04，所以在 Ubuntu 20.04 上会明确显示为不支持。ChatGPT Desktop 没有官方 Linux 版本，Osverse 不会提供来源不明的替代安装包。

## 安装

打开 [GitHub Releases](https://github.com/Oswald-Hao/Osverse/releases)，下载与你的平台匹配的文件。

### Windows（推荐安装包）

下载 `osverse-*-windows-amd64-setup.exe`，双击后按提示安装。安装范围仅为当前用户，不需要管理员权限；安装包内置 WebView2 bootstrapper。也可使用 `*-portable.zip` 免安装版或单文件 `.exe`。

PowerShell 校验：

```powershell
(Get-FileHash .\osverse-*-windows-amd64-setup.exe -Algorithm SHA256).Hash
Get-Content .\SHA256SUMS
```

### Ubuntu

#### AppImage（免安装）

```bash
sha256sum --check SHA256SUMS --ignore-missing
chmod +x osverse-*-linux-amd64-ubuntuXX.XX.AppImage
./osverse-*-linux-amd64-ubuntuXX.XX.AppImage
```

#### Debian 安装包

```bash
sha256sum --check SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

Release 还提供便携 `.tar.gz`、统一 `SHA256SUMS`、SPDX 2.3 SBOM、结构化更新清单和 GitHub 构建来源证明。可这样验证单个文件：

```bash
gh attestation verify ./osverse_*.deb --repo Oswald-Hao/Osverse
```

安装一次之后，Osverse 会在启动时静默检查新版本；发现更新时显示 Release 更新说明并等待确认。更新同样使用当前已验证的本机代理，前端不能指定下载地址、文件路径或摘要。

## 安全边界

- 扫描不会执行 Shell 启动文件或 `.desktop` 文件。
- 外部已安装 CLI 保留在原位置；同名外部命令不会被 Osverse 覆盖。
- 启动和移除前会重新扫描并验证目标身份；前端不能提交任意可执行路径或系统包名。
- 用户文件使用可回滚操作移入 Freedesktop 回收站；配置、API 凭据和登录会话默认不删除。
- Windows 上受管 CLI 使用锁定文件身份移入 `%LOCALAPPDATA%\Osverse\recovery`，卸载桌面应用只允许固定的 WinGet ID、Microsoft Store ID、MSI ProductCode 或受信任卸载器路径。
- 安装源来自内置白名单，并同时校验文件长度与 SHA-256。
- Osverse 自更新只接受固定仓库的语义化版本、匹配平台的结构化清单和精确发布路径；稳定版默认不接收预发布版，测试版可升级到更新的测试版或稳定版。
- Osverse 管理的 CLI 版本位于 `~/.local/share/osverse/tools`，使用不可变版本目录、原子符号链接、即时回滚与权限为 `0600` 的崩溃恢复日志。
- API Key 不会出现在档案列表、历史或日志中；Linux 使用权限受限的 AES-256-GCM 密钥，Windows 再以当前用户 DPAPI 保护主密钥；私网和保留地址必须明确确认。
- Claude Desktop 的提权助手只能执行固定动作，不能运行用户输入的命令。
- 软件不包含遥测，也不会修改系统或终端的全局代理。

漏洞报告方式与完整信任边界见 [SECURITY.md](SECURITY.md)。

## 更多真实截图

<details>
<summary><strong>发现外部安装的 CLI——包括两个不同路径的 Claude Code</strong></summary>

![Osverse 检测 Claude Code 与 Codex CLI](docs/assets/screenshots/cli工具.png)

</details>

<details>
<summary><strong>桌面应用检测与平台兼容性</strong></summary>

![Osverse 桌面应用管理](docs/assets/screenshots/桌面应用.png)

</details>

<details>
<summary><strong>CC Switch 与 Cockpit Tools 管理</strong></summary>

![Osverse 管理第三方 API 工具](docs/assets/screenshots/管理工具.png)

</details>

<details>
<summary><strong>加密的第三方 API 档案</strong></summary>

![Osverse API 档案编辑器](docs/assets/screenshots/api.png)

</details>

以上截图均来自 Ubuntu 22.04 上真实运行的 Osverse 窗口，不是概念稿。Windows 使用同一套前端界面；平台能力由原生后端提供。

## 从源码运行

固定工具链为 Go 1.25.12、Node.js 22.23.2、Wails 2.13.0。

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

# Ubuntu 20.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_36
```

Windows 10/11 x64（PowerShell，需 Go、Node.js、WebView2 与 Wails 构建环境）：

```powershell
npm --prefix frontend ci
go test ./...
go test -race ./...
go vet ./...
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev

# 生成每用户范围的 NSIS 安装包
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -platform windows/amd64 -nsis -webview2 embed -installscope user -trimpath
```

## 分支与贡献

代码只按 `功能分支 → dev → beta → main` 单向晋级：每个 PR 运行一次完整 CI，合并后不会再重复运行同一套 push 流程；`main` 只接收通过门禁的候选版，发版标签必须属于 `main` 历史。

欢迎贡献。请先阅读 [CONTRIBUTING.md](CONTRIBUTING.md)，提交聚焦的问题或 PR，并保持现有安全边界。

## 许可证

Apache-2.0，见 [LICENSE](LICENSE)。

Osverse 是独立项目。Claude、Codex、OpenAI、OpenCode 等名称属于各自权利人；列出它们仅表示兼容，不代表背书或合作关系。
