# Qwen Code 接入指南 / Integration Guide

[中文](#中文) · [English](#english)

## 中文

### 安装与启动

1. 在 Osverse 的“核心 CLI”中找到 **Qwen Code**。
2. 点击“安装”，核对固定版本、官方下载来源、大小和写入位置后确认。
3. 安装完成后刷新扫描，再点击“启动”；也可以在新终端运行 `qwen`。

Osverse 当前固定 Qwen Code `v0.21.13`。使用官方 standalone 包和包内 Node.js，不要求电脑预装 Node/npm，也不会执行远端安装脚本。现有外部 `qwen` 会按真实路径显示；如果它占用了 Osverse 的命令入口，安装器会停止而不是覆盖。

### 应用第三方 API

Qwen Code 原生支持 `modelProviders`。在 Osverse 中保存 API 档案并完成协议探测；如果 OpenAI Chat Completions 路由已确认，可以勾选 **Qwen Code** 并预览应用计划。

确认后 Osverse 会：

- 备份 `~/.qwen/settings.json`；
- 保留无关设置和其他 provider；
- 添加 `modelProviders.osverse`，并用 `providerProtocol.osverse = "openai"` 声明协议；
- 写入精确模型 ID、规范化为 `/v1` 的 Base URL 和独立的 `OSVERSE_API_KEY`；
- 将 `security.auth.selectedType` 设为 Qwen 实际使用的 `openai` 协议，并同时写入 `model.name` 和 `model.baseUrl`，避免同名模型被路由到其他地址。

目标文件与备份均限制为当前用户可读。Key 不会出现在 Osverse 的档案列表、历史或日志里；移除 Qwen Code 时默认保留 `~/.qwen` 配置和会话。

### 安全校验

- 下载 URL、版本、精确字节数和 SHA-256 固定在应用内；只允许 GitHub 下载所需的一次官方 HTTPS 资产跳转，其他重定向全部拒绝。
- tar/zip 只允许固定的 `qwen-code/` 根目录，拒绝绝对路径、`..`、Windows 保留名、符号链接、硬链接和特殊文件。
- 文件数与解压总量有上限，写入使用不可覆盖的新文件。
- 提交前使用包内 Node 运行 `lib/cli-entry.js --version`，必须精确返回固定版本。
- 版本目录提交后才原子切换 `qwen` 入口；同版本复用会重新校验 Node、入口和 wrapper 的 SHA-256。

上游资料：[Qwen Code](https://github.com/QwenLM/qwen-code)、[官方安装说明](https://github.com/QwenLM/qwen-code/blob/main/scripts/installation/INSTALLATION_GUIDE.md)、[Model Providers](https://github.com/QwenLM/qwen-code/blob/main/docs/users/configuration/model-providers.md)。

## English

### Install and launch

Select **Qwen Code** under Core CLI, review the fixed source, version, size, and destination, then confirm. Osverse pins Qwen Code `v0.21.13` and uses its official standalone archive with a private Node.js runtime. No system Node/npm or remote installer script is required. Refresh the scan and launch it from Osverse, or run `qwen` in a new terminal.

### Apply a third-party API

After an Osverse API profile confirms the OpenAI Chat Completions route, select Qwen Code in the compatibility matrix. Osverse backs up and atomically updates `~/.qwen/settings.json`, preserves unrelated settings, adds an `osverse` model provider mapped to the OpenAI protocol, selects the built-in `openai` auth type, and pins both the exact model ID and endpoint. Config and backup files are current-user-only; removal preserves `~/.qwen` by default.

### Verification model

The artifact URL, version, exact length, and SHA-256 are built in. Only GitHub's required single HTTPS handoff to its dedicated release-asset host is accepted; every other redirect is rejected. Extraction is confined to the fixed `qwen-code/` root with entry-count and expanded-size limits. Links, special files, traversal, and cross-platform hazardous names are rejected. Before commit, Osverse runs the bundled Node.js against `lib/cli-entry.js --version`; only the exact pinned version can activate the command.

Upstream references: [Qwen Code](https://github.com/QwenLM/qwen-code), [official installation guide](https://github.com/QwenLM/qwen-code/blob/main/scripts/installation/INSTALLATION_GUIDE.md), and [Model Providers](https://github.com/QwenLM/qwen-code/blob/main/docs/users/configuration/model-providers.md).
