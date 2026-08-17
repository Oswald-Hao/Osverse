# Kimi Code 接入指南 / Integration Guide

[中文](#中文) · [English](#english)

## 中文

### 安装与启动

1. 在 Osverse 的“核心 CLI”中找到 **Kimi Code**。
2. 点击“安装”，核对版本、官方下载来源、大小和写入位置后确认。
3. 安装完成后刷新扫描并点击“启动”；也可以在新终端运行 `kimi`。

Osverse 当前固定官方 Kimi Code `0.36.1` 独立二进制，不要求 Node/npm。已有外部 `kimi` 会按真实路径显示，不需要移动到 Osverse 目录；外部命令占用受管入口时，安装器会停止而不是覆盖。受管包装器设置 `KIMI_CODE_NO_AUTO_UPDATE=1`，因此 Kimi 不会在后台绕过 Osverse 的校验与回滚流程更新自身。

### 应用第三方 API

在 Osverse 中保存 API 档案并完成凭据与协议探测。OpenAI Chat Completions、OpenAI Responses 或 Anthropic Messages 任一协议确认后，都可以勾选 **Kimi Code** 并预览写入计划。

确认后 Osverse 会：

- 备份并原子更新 `~/.kimi-code/config.toml`（设置了安全的主目录内 `KIMI_CODE_HOME` 时使用该目录）；
- 保留无关注释、根设置、Provider、模型与工具配置；
- 创建带所有权标记的 `[providers.osverse]` 和 `[models.osverse]`；
- 根据实测协议选择 Kimi 原生 `openai`、`openai_responses` 或 `anthropic` Provider 类型；
- OpenAI 地址缺少版本段时规范化为 `/v1`，Anthropic 地址保持服务商提供的根地址；
- 将服务商提供的模型名原样写入 `model`，例如 `deepseek/deepseek-v4-flash`，不会生成 `osverse/deepseek/...`。

`osverse` 只是 Kimi 配置内部的模型别名与 Provider 名。Kimi 发给第三方 API 的 `model` 字段仍是用户填写的原始值。配置和备份仅允许当前用户读取；Key 不会出现在 Osverse 的档案列表、历史或日志中。移除 Kimi Code 时默认保留 `~/.kimi-code` 的配置与会话。

### 校验边界

- Linux x64/arm64、Windows x64/arm64、macOS x64/arm64 的官方 URL、精确字节数和 SHA-256 固定在应用内。
- 只接受 GitHub 官方 Release 到专用资产域名的一次 HTTPS 跳转。
- ZIP 必须只包含一个固定名称的普通可执行文件；拒绝额外条目、符号链接、路径穿越和超过上限的解压内容。
- 提交前运行 `kimi --version`，输出必须精确等于 `0.36.1`。
- 版本目录不可变，命令入口原子切换；失败会恢复原入口与 PATH 状态。
- API 适配器已经用官方 `0.36.1` Linux 二进制完成真实流式请求回归，验证了 Bearer Key、Base URL 和精确模型字段。

上游资料：[Kimi Code](https://github.com/MoonshotAI/kimi-code)、[配置文件](https://github.com/MoonshotAI/kimi-code/blob/main/docs/zh/configuration/config-files.md)、[Provider 配置](https://github.com/MoonshotAI/kimi-code/blob/main/docs/zh/configuration/providers.md)。

## English

### Install and launch

Select **Kimi Code** under Core CLI, review the fixed version, official source, exact size, and destination, then confirm. Osverse pins the official Kimi Code `0.36.1` standalone binary and requires no Node/npm. Existing external `kimi` commands remain in place and are discovered through the user's effective command paths. The managed wrapper sets `KIMI_CODE_NO_AUTO_UPDATE=1`, keeping upgrades inside Osverse's verified and recoverable workflow.

### Apply a third-party API

After an Osverse API profile confirms OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages, select Kimi Code in the compatibility matrix. Osverse backs up and atomically updates `~/.kimi-code/config.toml`, preserves unrelated TOML, and creates owned `[providers.osverse]` and `[models.osverse]` tables using Kimi's native provider type.

The `osverse` name is only a local provider/model alias. The actual request keeps the vendor's exact model ID, such as `deepseek/deepseek-v4-flash`, with no `osverse/` prefix. OpenAI-compatible unversioned endpoints receive `/v1`; Anthropic endpoints retain their supplied root. Config and backups are current-user-only, secrets are excluded from UI lists and history, and removal preserves Kimi configuration and sessions by default.

### Verification model

Official URL, exact byte length, and SHA-256 metadata are pinned for Linux x64/arm64, Windows x64/arm64, and macOS x64/arm64. Only GitHub's required single HTTPS release-asset handoff is accepted. The ZIP must contain exactly one regular `kimi` or `kimi.exe`; links, traversal, extra entries, and oversized expansion are rejected. The extracted binary must report exactly `0.36.1` before an immutable version directory and command entry are activated atomically. The profile adapter is covered by an end-to-end streaming request through the official Linux binary.

Upstream references: [Kimi Code](https://github.com/MoonshotAI/kimi-code), [configuration files](https://github.com/MoonshotAI/kimi-code/blob/main/docs/en/configuration/config-files.md), and [providers](https://github.com/MoonshotAI/kimi-code/blob/main/docs/en/configuration/providers.md).
