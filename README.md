# Osverse

Osverse 是面向 Ubuntu AI 开发环境的本地优先桌面管理器。它在一个面板中检测 CLI、桌面应用和 API 接入状态，并在用户确认后完成受校验、可回滚的安装与配置。

## Linux 功能

- 检测 PATH 中现有的 Claude Code、Codex CLI、OpenCode CLI，不要求工具位于 Osverse 目录；
- 检测 Claude Desktop、OpenCode Desktop、CC Switch、Cockpit Tools 等桌面与管理工具；
- 探测一个本机代理端口的 HTTP、HTTPS CONNECT 和 SOCKS5 能力，代理只用于 Osverse 的网络请求；
- 使用固定版本、下载长度和 SHA-256 清单事务式安装或更新三个核心 CLI；
- 在用户目录事务式安装、更新并启动 OpenCode Desktop、CC Switch 和 Cockpit Tools AppImage；
- 在 Ubuntu 22.04 上验证 Anthropic 官方签名密钥后，通过 APT 安装或升级 Claude Desktop；
- 加密保存第三方 API 档案，探测 Anthropic/OpenAI 协议兼容性，并在二次确认后更新 CLI 配置；
- 在权限为 `0600` 的本地文件中保留最多 200 条脱敏操作记录。

支持范围为 **Ubuntu 20.04 或 22.04、x86_64（amd64）**。Claude Desktop 的官方 Linux 最低要求是 Ubuntu 22.04，因此在 Ubuntu 20.04 上会显示为不支持。ChatGPT Desktop 没有官方 Linux 客户端，本版本不会提供非官方安装包。

## 安装

从 [GitHub Releases](https://github.com/Oswald-Hao/Osverse/releases) 下载与你的 Ubuntu 版本匹配的 `.deb` 与 `SHA256SUMS`：

```bash
sha256sum -c SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

也可以解压对应的便携 tar 包后直接运行 `./osverse`。默认窗口为 1280×800，与 Cockpit Tools 的默认桌面尺寸一致。

## 安全边界

- 扫描不会执行 shell 或 `.desktop` 文件；PATH 配置只解析受限的绝对路径语法。
- 已发现 CLI 的版本命令具有超时、输出上限、文件身份固定或执行前后重验。
- 所有安装都先展示后端生成的固定变更计划，只有用户确认后才写入。
- CLI 与 AppImage 下载只接受内置来源，并同时校验长度和 SHA-256；外部同名命令或桌面文件不会被覆盖。
- Claude Desktop 的提权助手只接受一个固定动作和经过验证的 loopback 代理参数，不能执行用户提供的命令。
- API 探测禁止重定向并默认阻止私网、链路本地与保留地址；私网端点必须由用户明确确认。
- API Key 不会返回到前端列表、历史记录或日志；本地档案采用 AES-256-GCM 加密。
- Osverse 不包含遥测。代理设置不会修改终端或系统的全局代理。

## 开发

固定工具链：Go 1.25.12、Node.js 22.23.2、Wails 2.13.0。

```bash
sudo apt-get update
sudo apt-get install build-essential libgtk-3-dev libwebkit2gtk-4.0-dev pkg-config
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend audit --audit-level=high
```

Ubuntu 20.04 的 WebKitGTK 为 2.38，使用 Wails `webkit2_36` 标签；Ubuntu 22.04 更新后的 WebKitGTK 为 2.40 以上，使用 `webkit2_40`：

```bash
# Ubuntu 20.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_36

# Ubuntu 22.04
go run github.com/wailsapp/wails/v2/cmd/wails@v2.13.0 dev -tags webkit2_40
```

打包脚本接受已构建二进制并生成可复现 tar、无 maintainer script 的 `.deb` 及 SHA-256 清单：

```bash
build/linux/package.sh 0.1.0 build/bin/osverse ubuntu22.04 build/release/ubuntu22.04
```

完整 Linux v1 验收矩阵见 [`docs/testing/linux-v1-acceptance.md`](docs/testing/linux-v1-acceptance.md)。

## 分支策略

变更按 `dev` → `beta` → `main` 单向推进：`dev` 用于开发集成，`beta` 用于候选版本验收，`main` 只接收已通过门禁的版本。Linux 发布工作流只接受指向 `main` 历史的语义版本标签，并为发布文件生成 SHA-256 和 GitHub 构建来源证明。
