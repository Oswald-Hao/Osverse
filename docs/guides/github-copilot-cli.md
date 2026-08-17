# GitHub Copilot CLI 接入指南 / Integration guide

## 中文

### 安装与登录

在 Osverse 的“核心 CLI”中找到 **GitHub Copilot CLI**，点击“安装”，检查下载来源、版本与变更路径后确认。Osverse 会下载 GitHub 官方 `v1.0.80` standalone 包，不要求本机预装 Node.js。

安装完成后可从 Osverse 点击“启动”，或在新终端运行：

```bash
copilot login
copilot
```

Copilot CLI 需要有效的 GitHub Copilot 订阅。登录、设备授权、会话与用量均由 GitHub Copilot 处理；Osverse 不读取登录令牌。

### 为什么不能应用第三方 API 档案

GitHub Copilot CLI 使用 GitHub 自己的认证与服务协议，不是一个可由任意 OpenAI/Anthropic 兼容 Base URL 替换的通用客户端。因此 Osverse 不会把 API 档案中的 Key、模型或 URL 写入 Copilot 配置。若需要第三方 API，请使用已明确支持对应协议的 Claude Code、Codex CLI、OpenCode CLI、Qwen Code 或 DeepSeek Harness。

### 更新与安全边界

- Linux x64/arm64、Windows x64、macOS x64/arm64 的官方制品 URL、字节数与 SHA-256 固定在后端。
- 下载只允许 GitHub 所需的一次官方 HTTPS 资产跳转；归档必须只含一个固定名称的普通可执行文件。
- 解压后必须精确返回 `GitHub Copilot CLI 1.0.80.`，否则不会激活。
- Osverse 管理的命令始终加入 `--no-auto-update`。升级由新的 Osverse 版本提供并重新经过回归测试。
- 如果电脑已有外部 `copilot`，Osverse 会检测并展示它，不要求迁移；也不会覆盖不属于 Osverse 的同名命令。
- 移除受管运行时时默认保留 Copilot 自己的登录、配置与会话。

## English

### Install and authenticate

Select **GitHub Copilot CLI** under Core CLI, review the backend-generated plan, and confirm. Osverse installs GitHub's official `v1.0.80` standalone package without requiring a system Node.js runtime.

Launch it from Osverse or open a new terminal and run:

```bash
copilot login
copilot
```

An active GitHub Copilot subscription is required. GitHub Copilot owns device authorization, tokens, sessions, and usage; Osverse does not read those credentials.

### Why generic API profiles are not applied

Copilot CLI uses GitHub authentication and service protocols. It is not a generic client whose backend can be replaced by an arbitrary OpenAI- or Anthropic-compatible Base URL. Osverse therefore never writes an API-profile key, model, or URL into Copilot configuration. Use a CLI with an explicitly supported third-party protocol when connecting another provider.

### Update and security boundary

- Official artifact URL, byte length, and SHA-256 are pinned for Linux x64/arm64, Windows x64, and macOS x64/arm64.
- Only GitHub's required single HTTPS handoff to its dedicated release-asset host is accepted; an archive must contain exactly one regular executable with the fixed platform name.
- The extracted executable must report the exact pinned version before activation.
- The managed command always adds `--no-auto-update`; upgrades arrive with a newly tested Osverse release.
- Existing external `copilot` commands are discovered in place and are never silently overwritten.
- Removing the managed runtime preserves Copilot-owned authentication, configuration, and sessions by default.
