# Osverse

<p align="center">
  <img src="build/appicon.png" width="112" alt="Osverse 图标">
</p>

<p align="center">
  <strong>你的本地 AI 开发环境控制台。</strong><br>
  在一个界面里检测、安装、更新和配置 Claude Code、Codex CLI、OpenCode、桌面应用与第三方 API。
</p>

<p align="center">
  <a href="README.en.md">English</a> ·
  <a href="https://github.com/Oswald-Hao/Osverse/releases">下载</a> ·
  <a href="docs/testing/linux-v1-acceptance.md">验收矩阵</a> ·
  <a href="CONTRIBUTING.md">参与贡献</a>
</p>

<p align="center">
  <a href="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/Oswald-Hao/Osverse/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/Oswald-Hao/Osverse/releases"><img alt="Release" src="https://img.shields.io/github/v/release/Oswald-Hao/Osverse?include_prereleases&sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/Oswald-Hao/Osverse"></a>
  <img alt="Ubuntu 20.04 与 22.04" src="https://img.shields.io/badge/Ubuntu-20.04%20%7C%2022.04-E95420?logo=ubuntu&logoColor=white">
  <img alt="本地优先" src="https://img.shields.io/badge/本地优先-无遥测-159957">
</p>

> [!IMPORTANT]
> Osverse 目前是面向 **Ubuntu 20.04/22.04 x86_64** 的 Linux 预发布版。Linux 稳定后才会按 Windows、macOS 的顺序继续适配。

![Osverse 环境状态与代理检测](docs/assets/screenshots/连接状态检查.png)

## 为什么做 Osverse？

换一台电脑，不应该重新花几个小时找运行时、代理端口、CLI 安装位置和各不相同的 API 配置文件。Osverse 把这些工作收进一个可检查、可确认、可恢复的桌面流程，并且尊重你已经安装好的工具。

- **已有工具无需搬家。** Claude Code、Codex CLI、OpenCode 会从用户真实可用的命令路径中发现，不要求放进 Osverse 目录。
- **安装过程可解释。** 安装前显示后端生成的变更计划；下载同时校验固定长度和 SHA-256；版本原子切换，失败和异常退出都可恢复。
- **API Key 留在本地。** 第三方 API 档案使用 AES-256-GCM 加密，先探测协议兼容性，再由用户二次确认写入目标 CLI。
- **代理只服务 Osverse。** 只需输入 `127.0.0.1` 上的端口，软件自动识别 HTTP、HTTPS CONNECT、SOCKS5，不改终端或系统全局代理。
- **桌面生态统一管理。** 支持的桌面客户端与 API 切换工具可以在同一面板安装、更新和启动。

## Linux 预发布版能管理什么

| 范围 | 当前支持 |
| --- | --- |
| 核心 CLI | Claude Code、Codex CLI、OpenCode CLI 的检测与事务式安装/更新 |
| 桌面应用 | Claude Desktop（Ubuntu 22.04+）、OpenCode Desktop |
| API 管理工具 | CC Switch、Cockpit Tools |
| API 档案 | Anthropic/OpenAI 兼容协议、模型名、Base URL、加密 Key |
| 网络 | 直连或本机 HTTP / HTTPS CONNECT / SOCKS5 代理 |
| 可审计性 | 脱敏操作历史、校验和、SPDX SBOM、构建来源证明 |

Claude Desktop 官方 Linux 包的最低要求是 Ubuntu 22.04，所以在 Ubuntu 20.04 上会明确显示为不支持。ChatGPT Desktop 没有官方 Linux 版本，Osverse 不会提供来源不明的替代安装包。

## 安装

打开 [GitHub Releases](https://github.com/Oswald-Hao/Osverse/releases)，下载与你的 Ubuntu 版本匹配的文件。

### AppImage（免安装）

```bash
sha256sum --check SHA256SUMS --ignore-missing
chmod +x osverse-*-linux-amd64-ubuntuXX.XX.AppImage
./osverse-*-linux-amd64-ubuntuXX.XX.AppImage
```

### Debian 安装包

```bash
sha256sum --check SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

Release 还提供便携 `.tar.gz`、统一 `SHA256SUMS`、SPDX 2.3 SBOM、结构化更新清单和 GitHub 构建来源证明。可这样验证单个文件：

```bash
gh attestation verify ./osverse_*.deb --repo Oswald-Hao/Osverse
```

## 安全边界

- 扫描不会执行 Shell 启动文件或 `.desktop` 文件。
- 外部已安装 CLI 保留在原位置；同名外部命令不会被 Osverse 覆盖。
- 安装源来自内置白名单，并同时校验文件长度与 SHA-256。
- Osverse 管理的 CLI 版本位于 `~/.local/share/osverse/tools`，使用不可变版本目录、原子符号链接、即时回滚与权限为 `0600` 的崩溃恢复日志。
- API Key 不会出现在档案列表、历史或日志中；私网和保留地址必须明确确认。
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

以上截图均来自 Ubuntu 22.04 上真实运行的 Osverse 窗口，不是概念稿。

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

## 分支与贡献

代码只按 `dev` → `beta` → `main` 单向晋级：`dev` 做开发集成，`beta` 做候选版验收，`main` 只接收通过门禁的版本；发版标签必须属于 `main` 历史。

欢迎贡献。请先阅读 [CONTRIBUTING.md](CONTRIBUTING.md)，提交聚焦的问题或 PR，并保持现有安全边界。

## 许可证

Apache-2.0，见 [LICENSE](LICENSE)。

Osverse 是独立项目。Claude、Codex、OpenAI、OpenCode 等名称属于各自权利人；列出它们仅表示兼容，不代表背书或合作关系。
