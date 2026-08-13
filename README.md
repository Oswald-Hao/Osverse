# Osverse

Osverse 是面向 Linux AI 开发环境的桌面状态面板。它把系统兼容性、CLI、桌面应用和管理工具的检测结果集中到一个界面中，帮助你在开始工作前看清本机环境。Osverse is a desktop dashboard for understanding the state of a Linux AI-development environment.

## Phase 1：只读环境扫描 / Read-only scan

当前版本只检测，不安装、不更新，也不修改工具配置。面板会显示：

- Ubuntu 版本、CPU 架构和当前 Shell；
- Claude Code、Codex CLI 和 OpenCode CLI 的安装路径与版本状态；
- Claude Desktop、ChatGPT Desktop、OpenCode Desktop、CC Switch 和 Cockpit Tools 的本机安装证据；
- 缺失、冲突、版本输出异常或不受支持等需要注意的状态。

支持范围为 **Ubuntu 20.04 或 22.04、x86_64（amd64）**。其他发行版、Ubuntu 版本和架构会显示为不受支持；这不代表应用已经在这些平台上通过验证。

截图：尚未提供公开截图资源，后续版本补充。

## 安全边界 / Security boundaries

Phase 1 的扫描范围有意保持狭窄：

- 不使用 shell 执行命令，也不执行 shell 配置文件；只从进程环境和固定名称的 Bash/Zsh 配置文件中解析受限的绝对 PATH 条目。
- 仅在已发现的固定 CLI 候选路径上直接执行 `--version`，单次执行有 3 秒超时和 64 KiB 输出上限。
- 桌面应用只查询固定的 dpkg 包名、固定位置的 `.desktop` 文件是否存在，以及对应可执行文件；不会读取或执行 `.desktop` 文件内容。
- 扫描不会安装、卸载、更新、重命名或写入任何被检测工具，也不包含网络探测。
- 外部可执行文件的行为不由 Osverse 控制；请只把可信目录加入 PATH。

## 开发 / Development

开发工具版本：

- Go 1.25.x（CI 使用 1.25.12）
- Node.js 22.x（CI 使用 22.23.2）与 npm
- Wails CLI 2.13.0（通过 `go run` 固定版本调用）
- Ubuntu 原生依赖：GTK 3、WebKitGTK 4.0、pkg-config 和 C/C++ 构建工具

在 Ubuntu 20.04/22.04 上安装原生依赖：

```bash
sudo apt-get update
sudo apt-get install build-essential libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
```

安装依赖并运行自动化检查：

```bash
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
go test ./...
go test -race ./internal/scan ./internal/detect ./internal/platform/...
go vet ./...
npm --prefix frontend audit --audit-level=high
```

Wails 的标签表示 WebKitGTK **库版本下限**，不是 4.0/4.1 ABI 名称。Ubuntu 20.04 当前公开更新提供 WebKitGTK 2.38，因此使用 `webkit2_36`；已更新的 Ubuntu 22.04 提供 2.40 以上版本，因此使用 `webkit2_40`。

启动 Wails 开发模式：

```bash
# Ubuntu 20.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_36

# Updated Ubuntu 22.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_40
```

生成生产二进制：

```bash
# Ubuntu 20.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -tags webkit2_36

# Updated Ubuntu 22.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 build -tags webkit2_40

test -x build/bin/osverse
```

详细的人工验收矩阵见 [`docs/testing/phase-1-acceptance.md`](docs/testing/phase-1-acceptance.md)。

## 分支策略 / Branch policy

变更按 `dev` → `beta` → `main` 单向推进：`dev` 用于日常集成，`beta` 用于候选版本验收，`main` 保持已验收的稳定版本。每次晋级都应通过代码审查、CI 和目标 Ubuntu 版本的人工验收，不直接跳过阶段。

## 公开路线图 / Public roadmap

- Phase 1：只读环境状态扫描与响应式桌面面板。
- Phase 2：在明确确认和可回滚边界内提供安装与配置引导。
- Phase 3：扩展工具生命周期管理、诊断和更多 Linux 环境支持。

后续阶段不会改变 Phase 1 的默认安全原则：先展示证据，再由用户决定是否执行修改。
