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

有两种配置方式：

1. 打开 Harness Web 工作台的 Models 设置，直接使用 DeepSeek Provider 或添加 Custom Provider。
2. 在 Osverse 的“API 配置”中保存 Key、Base URL 和精确模型 ID，完成协议探测后勾选 **DeepSeek Harness**，预览并确认写入。

使用 Osverse 应用档案时：

- OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 任一协议确认后即可使用；多个协议同时可用时优先选择 Chat Completions，其次 Responses，最后 Anthropic Messages。
- Osverse 在 `$DSH_HOME/settings.yaml` 的 `llm-pi-ai.providers` 下维护唯一的 `osverse` Provider，并把模型设为 `agent-default-model`，因此新会话会直接使用它；已经发送过请求的既有会话仍保留自己的模型。
- Key 只写入 `$DSH_HOME/.credentials.yaml` 的 `OSVERSE_API_KEY`，`settings.yaml` 只保存凭据引用。模型 ID 不会添加 `osverse/` 前缀；Harness 界面显示的 `osverse/<模型 ID>` 只是 Provider 路由表示。
- 两份文件会在确认前展示，原内容打包备份到 Osverse 的当前用户私有备份目录。写入使用 Harness 官方的同名 `.lock` 文件约定、原子替换和失败回滚；目标、备份和凭据均限制为当前用户访问。
- 现有同名 `osverse` Provider 或孤立的 `OSVERSE_API_KEY` 如果不是 Osverse 创建的，操作会停止，不会覆盖。无关 Provider、设置、注释和凭据会保留。
- 默认使用 `~/.dsh`；如果启动 Osverse 时设置了绝对的 `$DSH_HOME`，它必须位于当前用户主目录内。相对路径或主目录外路径会被拒绝。

在 Harness 自己的 Models 页面配置时：

- 使用 DeepSeek 官方服务时，选择 DeepSeek Provider 并填写 API Key。
- 使用兼容服务时，添加 Custom Provider，填写服务给出的 Base URL、协议、API Key 和模型名。
- Base URL 来自 API 服务商的开发者文档或控制台，不是聊天网页地址。不要猜测路径；不同服务可能要求 `/v1`，也可能已经把版本路径包含在 URL 中。
- 模型名必须使用服务商 API 返回或文档列出的精确 ID。

Harness 将密钥以只写方式保存到 `$DSH_HOME/.credentials.yaml`，普通设置只保留引用。Osverse 只维护自己拥有的 `osverse` Provider、默认模型字段和 `OSVERSE_API_KEY`，并按固定的 `0.1.0-rc.6` 配置契约验证写回结果；不会接管其他 Provider 或工作区。卸载 Harness 时仍默认保留这些配置、凭据和会话。

### Osverse 如何安装

- Harness 固定为 `@deepseek-ai/dsh@0.1.0-rc.6`，Node.js 固定为 `22.23.2`。
- Node.js 归 Osverse 私有，不写入全局 npm，不影响电脑现有的 Node/npm。
- npm 依赖由嵌入的 lockfile 闭包确定，只从 `registry.npmjs.org` 下载，并逐包校验 SHA-512。
- Node.js 制品只从 `nodejs.org` 下载，同时校验精确长度和 SHA-256。
- 不执行任何 npm `preinstall`、`install` 或 `postinstall` 脚本。Linux x64 的 `node-pty` 原生模块由 Osverse 在 Ubuntu 20.04 基线中预构建并固定哈希。
- Linux/Windows 安装到当前用户目录，不要求管理员权限。已有外部 `dsh` 不会被删除；同名 Osverse 入口被其他程序占用时安装会停止。

Harness 的锁定依赖中包含按平台下载的 `sharp-libvips`（LGPL-3.0-or-later）和 `argparse`（Python-2.0）。这些依赖不嵌入 Osverse Release，而是在用户确认 Harness 安装后从其固定 npm 制品直接获取；包名、版本、摘要和许可证仍会进入 Osverse 的依赖扫描与 SBOM。`sharp-libvips` 的对应源码和构建脚本位于 [lovell/sharp-libvips](https://github.com/lovell/sharp-libvips)，许可证条款见 [GNU LGPL v3](https://www.gnu.org/licenses/lgpl-3.0.html)。

上游资料：[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness)、[Web UI](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md)、[Providers](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/providers.md)。

## English

### Install and launch

Find **DeepSeek Harness** under Core CLI, preview and confirm the install plan, refresh the scan, then select Launch. Osverse rescans the selected installation and invokes only the fixed `web` subcommand. The workspace listens on `127.0.0.1:3080` by default. You can also run `dsh web` in a new terminal.

### Configure a provider

Configure a provider in Harness's Models page, or save and probe an Osverse API profile and then select **DeepSeek Harness** in the compatibility matrix. Osverse accepts a confirmed OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages route, preferring them in that order when more than one is available. It transactionally updates `$DSH_HOME/settings.yaml` and `$DSH_HOME/.credentials.yaml`, owns only the `llm-pi-ai.providers.osverse` route and `OSVERSE_API_KEY`, and selects the exact model for new sessions. Existing sessions keep the model recorded in their logs.

The key is stored only in Harness's credential document; settings carry its reference. Unrelated providers, comments, and credentials are preserved. Osverse coordinates with a running Harness through the same `.lock` files used by the pinned release, writes atomically, rolls the first file back if the second commit fails, and refuses an unowned `osverse` route or credential. A configured `DSH_HOME` must resolve inside the current user's home. Harness removal continues to preserve provider settings, credentials, and sessions.

When configuring directly in Harness, choose the DeepSeek provider for the official service or add a Custom Provider with the exact protocol, Base URL, API key, and model ID supplied by the vendor. The Base URL comes from developer documentation or the vendor console, not a consumer chat page.

### Verified installation model

Osverse pins `@deepseek-ai/dsh@0.1.0-rc.6` and Node.js `22.23.2`. It downloads only from `registry.npmjs.org` and `nodejs.org`, verifies every package with the embedded lockfile's SHA-512 integrity, verifies the Node artifact's exact size and SHA-256, and runs no npm lifecycle scripts. The private runtime does not modify a global Node/npm installation.

The locked graph includes platform-selected `sharp-libvips` (LGPL-3.0-or-later) and `argparse` (Python-2.0). They are fetched from their pinned npm artifacts only after installation confirmation rather than embedded in the Osverse release; they remain visible to dependency scanning and SBOM generation. Corresponding libvips source and build scripts are available from [lovell/sharp-libvips](https://github.com/lovell/sharp-libvips).

Upstream references: [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness), [Web UI guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/index.md), and [Provider guide](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/providers.md).
