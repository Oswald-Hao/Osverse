# DeepSeek Harness 接入指南 / Integration Guide

[中文](#中文) · [English](#english)

## 中文

### 安装与启动

1. 在 Osverse 的“核心 CLI”中找到 **DeepSeek Harness**。
2. 点击“安装”，检查计划中的版本、下载来源和写入位置，再确认。
3. 安装完成后刷新扫描，点击“启动”。Osverse 只会启动后端重新验证过的 `dsh` 安装，并使用固定参数 `web`。
4. Harness 默认监听 `127.0.0.1:3080`。如果端口已占用，请先关闭占用程序；Osverse 不会自动暴露到局域网。

你也可以在新终端中运行：

```bash
dsh web
```

### 配置 DeepSeek 或第三方 API

打开 Harness Web 工作台的 Provider 设置：

- 使用 DeepSeek 官方服务时，选择 DeepSeek Provider 并填写 API Key。
- 使用兼容服务时，添加 Custom Provider，填写服务给出的 Base URL、协议、API Key 和模型名。
- Base URL 来自 API 服务商的开发者文档或控制台，不是聊天网页地址。不要猜测路径；不同服务可能要求 `/v1`，也可能已经把版本路径包含在 URL 中。
- 模型名必须使用服务商 API 返回或文档列出的精确 ID。

Harness 将密钥以只写方式保存到 `$DSH_HOME/.credentials.yaml`，普通设置只保留引用。Osverse 不解析这份快速演进的 Developer Preview 配置，以免破坏 Harness 的凭据和工作区；卸载 Harness 时也默认保留它。

### Osverse 如何安装

- Harness 固定为 `@deepseek-ai/dsh@0.1.0-rc.6`，Node.js 固定为 `22.23.2`。
- Node.js 归 Osverse 私有，不写入全局 npm，不影响电脑现有的 Node/npm。
- npm 依赖由嵌入的 lockfile 闭包确定，只从 `registry.npmjs.org` 下载，并逐包校验 SHA-512。
- Node.js 制品只从 `nodejs.org` 下载，同时校验精确长度和 SHA-256。
- 不执行任何 npm `preinstall`、`install` 或 `postinstall` 脚本。Linux x64 的 `node-pty` 原生模块由 Osverse 在 Ubuntu 20.04 基线中预构建并固定哈希。
- Linux/Windows 安装到当前用户目录，不要求管理员权限。已有外部 `dsh` 不会被删除；同名 Osverse 入口被其他程序占用时安装会停止。

上游资料：[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)、[Web UI](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md)、[Providers](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/providers.md)。

## English

### Install and launch

Find **DeepSeek Harness** under Core CLI, preview and confirm the install plan, refresh the scan, then select Launch. Osverse rescans the selected installation and invokes only the fixed `web` subcommand. The workspace listens on `127.0.0.1:3080` by default. You can also run `dsh web` in a new terminal.

### Configure a provider

Use Harness's Provider settings. Choose the DeepSeek provider for the official service, or add a Custom Provider with the exact protocol, Base URL, API key, and model ID supplied by your API vendor. A Base URL comes from the vendor's developer documentation or console; it is not the consumer chat page and should not be guessed.

Harness stores keys write-only in `$DSH_HOME/.credentials.yaml`. Osverse deliberately does not parse this fast-changing Developer Preview format and preserves it during Harness removal.

### Verified installation model

Osverse pins `@deepseek-ai/dsh@0.1.0-rc.6` and Node.js `22.23.2`. It downloads only from `registry.npmjs.org` and `nodejs.org`, verifies every package with the embedded lockfile's SHA-512 integrity, verifies the Node artifact's exact size and SHA-256, and runs no npm lifecycle scripts. The private runtime does not modify a global Node/npm installation.

Upstream references: [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness), [Web UI guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md), and [Provider guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/providers.md).
