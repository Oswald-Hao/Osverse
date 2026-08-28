# Gemini CLI 接入指南 / Integration Guide

## 中文

### 安装与启动

1. 在 Osverse 的“核心 CLI”中找到 **Gemini CLI**。
2. 预览安装计划。计划必须只包含 Google 官方 Gemini CLI bundle、Node.js 官方运行时、Osverse 私有版本目录和 `gemini` 用户级入口。
3. 确认安装，等待任务显示“Gemini CLI 安装完成”，刷新扫描后点击“启动”。
4. 也可以在任意新终端直接运行 `gemini`。Osverse 写入的 PATH 块对重复安装保持幂等，不会覆盖其他 shell 配置。

Osverse 当前固定 Gemini CLI `0.57.0` 与 Node.js `22.23.2`。安装器只接受内置的 `registry.npmjs.org` 和 `nodejs.org` HTTPS 地址，要求响应长度与 SHA-256 完全一致，拒绝重定向、路径穿越、链接和特殊归档项。它不调用 npm、不运行 lifecycle script、不修改系统 Node/npm，也不会覆盖已有的外部 `gemini` 命令。

### 登录与 API

首次启动时按 Gemini CLI 提示使用 Google 账号、Gemini API Key 或组织提供的 Google Cloud 方式认证。凭据、设置和会话由 Gemini CLI 保存在自己的用户目录中。

Gemini CLI 当前不使用 Osverse 面向 OpenAI/Anthropic 兼容端点的通用 API 档案。Osverse 因此不会把第三方 Key、Base URL 或模型写入 `~/.gemini`，也不会把其他服务伪装成 Google Gemini。

### 更新、修复与移除

- 受管运行时位于 Linux 的 `~/.local/share/osverse/tools/gemini-cli` 或 Windows 的 `%LOCALAPPDATA%\Osverse\tools\gemini-cli`。
- 每次安装都先在临时目录完成下载、解包和真实 `gemini --version` 验证，再原子切换命令入口。
- 匹配 Osverse 身份但损坏的同版本运行时会先移入恢复区；新版本激活失败时恢复原运行时。
- 移除计划只处理经过重新扫描确认的用户级入口与 Osverse 管理目录。Google 登录、`~/.gemini` 设置、凭据和会话默认保留。

上游资料：[Gemini CLI](https://github.com/google-gemini/gemini-cli)、[安装说明](https://github.com/google-gemini/gemini-cli/blob/main/docs/get-started/installation.mdx)、[认证说明](https://github.com/google-gemini/gemini-cli/blob/main/docs/get-started/authentication.md)。

## English

### Install and launch

1. Find **Gemini CLI** under Core CLI.
2. Preview the plan. It must contain only Google's official Gemini CLI bundle, the official Node.js runtime, an Osverse private version directory, and the per-user `gemini` entry.
3. Confirm installation, wait for “Gemini CLI installation complete,” refresh the scan, and select Launch.
4. You can also run `gemini` directly from any new terminal. Osverse's PATH block is idempotent and preserves unrelated shell configuration.

Osverse currently pins Gemini CLI `0.57.0` and Node.js `22.23.2`. The installer accepts only built-in HTTPS URLs on `registry.npmjs.org` and `nodejs.org`, requires exact response lengths and SHA-256 digests, and rejects redirects, traversal, links, and special archive entries. It never invokes npm, executes lifecycle scripts, changes system Node/npm, or overwrites an external `gemini` command.

### Authentication and APIs

On first launch, follow Gemini CLI's prompt to use a Google account, Gemini API key, or an organization-provided Google Cloud authentication method. Gemini CLI owns its credentials, settings, and sessions in its user data directory.

Gemini CLI does not currently consume Osverse's generic OpenAI/Anthropic-compatible profiles. Osverse therefore does not write third-party keys, Base URLs, or models into `~/.gemini`, and does not present another service as Google Gemini.

### Update, repair, and removal

- Managed runtimes live under `~/.local/share/osverse/tools/gemini-cli` on Linux or `%LOCALAPPDATA%\Osverse\tools\gemini-cli` on Windows.
- Every install completes download, extraction, and a real `gemini --version` check in staging before atomically switching the command entry.
- A damaged same-version runtime with a matching Osverse identity is quarantined first; activation failure restores the previous runtime.
- Removal touches only a freshly rescanned per-user entry and Osverse-managed directories. Google authentication, `~/.gemini` settings, credentials, and sessions remain by default.

Upstream references: [Gemini CLI](https://github.com/google-gemini/gemini-cli), [installation](https://github.com/google-gemini/gemini-cli/blob/main/docs/get-started/installation.mdx), and [authentication](https://github.com/google-gemini/gemini-cli/blob/main/docs/get-started/authentication.md).
