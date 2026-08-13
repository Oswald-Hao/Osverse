# Osverse Linux v1 设计规格

日期：2026-08-13  
状态：设计已在对话中逐段确认，等待书面规格复核

## 1. 产品定义

Osverse 是一个独立、开源、跨平台的 AI 开发环境安装与配置面板。它解决用户更换电脑或进入全新系统后，逐项查找、安装和配置 AI CLI、桌面客户端及 API 管理工具耗时且容易出错的问题。

Osverse 是管理工具，不是 CLI 的常驻运行依赖。由 Osverse 安装的 `claude`、`codex` 和 `opencode` 在 Osverse 退出或卸载后仍可直接从新终端启动。

平台发布顺序为：

1. Linux
2. Windows
3. macOS

每个平台独立达到稳定版验收标准后才发布并开始下一平台。本文只定义 Linux v1。

## 2. Linux v1 范围

### 2.1 支持矩阵

| 项目 | Linux v1 承诺 |
| --- | --- |
| 发行版 | Ubuntu 20.04 LTS、22.04 LTS（Desktop/Server 不影响系统支持；GUI 会话可用性单独判断） |
| CPU | x86_64 / amd64 |
| 安装包 | `.deb`、AppImage |
| Shell | Bash、Zsh；其他 Shell 只检测并给出手动说明 |
| 桌面环境 | 以 Ubuntu 默认 GNOME 为主要验收环境 |

ARM64 不提供实验包，也不属于 Linux v1 的兼容承诺。

### 2.2 完整管理的 CLI

以下三个 CLI 必须支持检测、安装、升级、启动、版本验证、路径冲突提示和故障恢复：

- Claude Code CLI
- Codex CLI
- OpenCode CLI

### 2.3 桌面应用

Osverse 检测 Claude Desktop、ChatGPT Desktop（其中包含 Codex 桌面能力）和 OpenCode Desktop。安装按钮受上游官方兼容矩阵控制：

- Claude Desktop：Ubuntu 22.04 可按官方 apt 仓库方式安装；Ubuntu 20.04 显示最低版本要求并禁用安装。
- ChatGPT Desktop：Ubuntu 20.04 和 22.04 均显示上游最低系统要求并禁用安装，不提供非官方移植或强制安装。
- OpenCode Desktop：只有上游官方 `.deb` 明确兼容当前系统，且 `apt` 安装模拟无依赖冲突时才允许安装；否则仅提供官网入口和原因。

若上游兼容范围发生变化，签名清单可收紧或放宽按钮状态，但不得绕过发行版本身声明的依赖。

### 2.4 第三方管理工具

以下工具支持检测、从项目官方 GitHub Release 安装、版本检查、更新和启动：

- CC Switch（`farion1231/cc-switch`）
- Cockpit Tools（`jlcodes99/cockpit-tools`）

Osverse 不读取、修改或迁移这两个工具的内部数据库、账号或凭据。Osverse 自己的 API 配置档案也不依赖它们。

### 2.5 明确不在 Linux v1 内

- Windows 和 macOS 实现
- ARM64、Fedora、Arch、Debian 等额外平台
- 本地统一 API 转发网关
- 接管 CC Switch 或 Cockpit Tools 的账号数据
- 绕过上游系统限制的强制安装
- 自动登录官方账号或代替用户完成 OAuth
- 后台静默安装、静默更新或任意远程 Shell 执行

## 3. 用户体验

### 3.1 首次启动

首次启动自动执行只读环境扫描。基础安装流程中唯一要求用户输入的是本机代理端口；代理不是必填项，用户也可选择直连。API Key、Base URL 和模型名属于用户稍后主动创建 API 配置档案时的可选输入，不属于首次环境部署的必填信息。

代理默认主机固定为 `127.0.0.1`。输入端口后，Osverse 自动探测：

- HTTP 代理
- HTTPS CONNECT 能力
- SOCKS5 代理

界面显示成功协议、延迟和失败原因。探测失败时不把该端口用于后续下载，也不盲目继续。

### 3.2 仪表盘

首页采用已确认的“状态总览型”逻辑和 Cockpit Tools 风格的视觉语言：

- 浅色渐变背景
- 悬浮胶囊侧栏
- 大圆角白色卡片
- 蓝色与青色作为主强调色
- 清晰区分正常、缺失、可更新、不兼容和失败状态

首页展示：

- 系统版本、CPU 架构和最近扫描时间
- 环境健康度、已就绪项目数、代理状态和待处理数量
- 三个核心 CLI 的安装位置、版本、来源和操作按钮
- 桌面应用及第三方管理工具状态
- 当前 API 配置档案及其应用范围
- “重新检测”和“修复全部缺失项”入口

侧栏至少包含：总览、工具、API 配置、安装记录、设置。

### 3.3 状态模型

每个受管组件使用统一状态：

- `detecting`：检测中
- `missing`：未安装且允许安装
- `installed`：已安装且版本可用
- `update_available`：有可验证的新版本
- `conflict`：发现多个同名命令或外部安装冲突
- `unsupported`：当前系统不满足上游要求
- `broken`：存在安装痕迹但验证失败
- `installing`：安装事务进行中
- `failed`：最近操作失败，可查看脱敏日志和恢复动作

界面不能把“检测到文件”当作安装成功；`installed` 必须同时通过路径、可执行权限和版本命令验证。

## 4. 技术架构

### 4.1 技术栈

- 桌面框架：Wails v2
- 前端：React + TypeScript
- 核心后端：Go
- Linux WebView：系统 WebKitGTK 4.0
- 本地状态：结构化 JSON；敏感档案单独加密
- CI/CD：GitHub Actions

选择 Wails v2 是因为其 Linux 运行依赖可覆盖 Ubuntu 20.04 的 WebKitGTK 4.0，同时 Go 适合实现跨平台检测、下载、校验、文件事务和进程管理。Tauri 2 依赖 WebKitGTK 4.1，不能作为 Ubuntu 20.04 的可靠基线。

### 4.2 模块边界

#### React 控制面板

负责显示状态、收集用户输入、预览变更计划和展示进度。前端不得直接拼接或执行 Shell 命令，也不得获得明文凭据的长期副本。

#### Go 应用核心

由以下独立模块组成：

- `detector`：系统、架构、PATH、版本、桌面文件、包管理器状态检测
- `proxy`：HTTP、HTTPS CONNECT 和 SOCKS5 探测，为单次任务生成网络配置
- `planner`：把期望状态转换为可预览的安装或配置步骤
- `installer`：下载、校验、暂存、提交、验证、取消和恢复
- `manifest`：加载内置清单与签名远程清单，执行模式和来源校验
- `credentials`：系统密钥环与 AES-GCM 回退存储
- `profiles`：API 配置档案的增删改查和兼容性状态
- `adapters`：Claude Code、Codex、OpenCode 的检测、安装和配置适配器
- `desktop`：apt/`.deb` 桌面包检测、兼容判断和提权安装
- `externaltools`：CC Switch、Cockpit Tools 的 Release 检测和安装
- `auditlog`：结构化、脱敏的任务日志与诊断报告
- `platform`：Linux、Windows、macOS 平台接口；v1 只实现 Linux

所有操作系统 I/O 通过接口封装，使规划逻辑能够用虚拟文件系统、假 HTTP 服务和假进程执行器测试。

### 4.3 Wails Bridge 约束

前端只能调用明确绑定的高层方法，例如：

- `ScanEnvironment()`
- `ProbeProxy(port)`
- `BuildInstallPlan(componentIDs)`
- `ExecutePlan(planID)`
- `CancelTask(taskID)`
- `SaveAPIProfile(input)`
- `ProbeAPIProfile(profileID)`
- `ApplyAPIProfile(profileID, targets)`

Bridge 不暴露通用 `Exec(command)`、任意文件写入或任意 URL 下载能力。

## 5. 检测设计

### 5.1 CLI 检测

对每个命令执行以下步骤：

1. 从 GUI 进程环境、登录 Shell 和常见用户目录构造候选 PATH；不能假设 GUI 自动继承 `.bashrc`。
2. 查找全部同名可执行文件，而非只取第一个。
3. 解析真实路径、文件所有者和安装来源。
4. 以超时和受控环境执行官方版本参数。
5. 将 Osverse 托管安装与系统/用户外部安装分开显示。

发现多个命令时进入 `conflict` 状态，展示每个路径和版本。用户可选择 Osverse shim 的优先级，但 Osverse不得删除或覆盖外部安装。

### 5.2 桌面应用检测

综合使用：

- `dpkg-query`
- `.desktop` 文件
- 已知可执行路径
- 官方包名

只有包已登记且启动入口存在时才标记为已安装。

### 5.3 依赖检测

检测至少包括：

- Ubuntu 版本与架构
- Bash/Zsh 与 `~/.local/bin` PATH 状态
- `curl`、CA 证书、`git` 等任务依赖
- 系统 Node/npm，仅用于报告和外部安装识别
- Osverse 托管 Node 运行时
- 可用磁盘空间
- 系统密钥环服务状态
- `pkexec`/PolicyKit 能力

Osverse 不依赖系统 Node/npm 安装核心 CLI。系统 Node 过旧不会阻塞托管安装。

## 6. CLI 安装与独立运行

### 6.1 目录布局

```text
~/.local/share/osverse/
  runtime/node/<version>/
  tools/<tool>/<version>/
  tools/<tool>/current -> <version>/
  staging/<task-id>/
  manifests/
  logs/
  state/

~/.local/bin/
  claude
  codex
  opencode
```

每个 CLI 版本安装在不可变版本目录中。`~/.local/bin` 中的稳定 shim 指向 Osverse 当前选择的版本。shim 使用绝对路径找到托管 Node 和 CLI 入口，因此不依赖 Osverse 进程或系统 Node。

### 6.2 PATH 管理

若 `~/.local/bin` 不在登录 Shell PATH 中，Osverse 在用户确认后向相应 Shell 配置写入带明确起止标记的最小片段。写入前创建备份，重复执行保持幂等。

Osverse 不覆盖用户已有的 PATH 语句。卸载桌面程序时默认保留 PATH 片段，因为 CLI 仍被保留；用户选择“同时清理托管工具”时才移除由 Osverse 自己写入且内容未被用户修改的片段。

### 6.3 安装来源

内置或签名清单为每个组件声明：

- 组件 ID
- 稳定版本
- 安装类型（npm 包、官方压缩包、官方 `.deb`、GitHub Release）
- 官方包名或仓库
- 允许的下载主机
- SHA-256
- 支持的系统和架构
- 版本验证命令及安全参数
- 所需磁盘空间

Linux v1 的 CLI 默认通过 Osverse 托管 Node LTS 和官方 npm 包安装：

- `@anthropic-ai/claude-code`
- `@openai/codex`
- `opencode-ai`

清单可在上游官方安装方式变化时切换为官方独立二进制，但不能包含任意 Shell 文本。

### 6.4 安装事务

每次安装或更新遵循：

1. 预检系统、网络、磁盘、冲突和权限。
2. 生成不可变计划，列出所有 URL、版本、文件和系统改动。
3. 用户确认计划。
4. 在 `staging/<task-id>` 下载，支持取消和断点续传。
5. 验证 HTTPS、允许域名、签名清单和 SHA-256。
6. 解包或安装到新的版本目录。
7. 在隔离环境执行版本和启动冒烟验证。
8. 原子切换当前版本和 shim。
9. 重新扫描并记录脱敏审计结果。

下载或校验失败时不触碰当前版本。安装中断时清理或保留可续传 staging。验证失败时自动切回旧版本。应用崩溃后，下次启动从任务日志判断是继续清理还是回滚。

### 6.5 提权边界

核心 CLI 安装不使用 `sudo`。只有 apt 桌面应用或系统包操作需要管理员权限，并通过 `pkexec` 执行固定、参数化的辅助程序。UI 必须在授权前显示具体包、仓库和文件改动。

提权辅助程序：

- 不接受通用 Shell 字符串
- 只接受已知动作枚举和经过校验的参数
- 不读取或接收 API Key
- 操作完成立即退出

### 6.6 卸载

卸载 Osverse 桌面应用时默认保留：

- 三个 CLI 及托管 Node
- CLI 官方配置
- API 配置档案
- shell PATH 片段

卸载界面提供单独的“同时清理 Osverse 托管工具”选项，并逐项列出删除目标。外部安装永远不在清理范围内。

## 7. 代理设计

代理设置只作用于 Osverse 发起的探测、下载和安装任务，不默认修改系统全局代理。

探测过程：

1. 验证端口范围和本机 TCP 可达性。
2. 分别尝试 HTTP、HTTPS CONNECT 和 SOCKS5 握手。
3. 使用固定的小型 HTTPS 目标验证证书链和实际出网。
4. 保存成功协议及最近验证时间；不保存探测产生的临时连接状态。

执行任务前再次快速检查代理。代理中途断开时暂停或失败并保留可续传数据，不自动切换到直连，避免绕过用户网络预期。

## 8. API 配置档案

### 8.1 用户输入

每个档案包含：

- 配置名称
- API Key
- Base URL
- 模型名

Base URL 旁提供“在哪里获取”说明：应从服务商控制台的 API 文档或接入信息中复制，不能填写普通聊天网页地址。界面展示常见 `/v1` 格式示例，但不假定所有服务必须使用同一路径。

### 8.2 URL 规范化与协议探测

保存后先规范化 URL：

- 只允许 `https`；仅 `localhost`/环回地址允许用户确认后使用 `http`
- 移除重复斜杠和明显的完整资源端点
- 保留服务商必需的路径前缀
- 拒绝 URL 中的用户名、密码、片段和控制字符
- 阻止远程地址解析到环回、链路本地或私网的意外 SSRF；用户明确配置的局域网服务需要额外确认

Osverse 探测三类协议：

- OpenAI Responses
- OpenAI Chat Completions
- Anthropic Messages

默认探测使用模型列表、OPTIONS/HEAD、预期错误结构等不产生模型推理费用的方式。若这些信号不足，状态为“未确认”，而不是误报兼容。只有用户主动点击“发送测试请求”并确认可能产生少量费用后，才发送最小推理请求。

### 8.3 兼容矩阵

协议探测结果映射为：

- Claude Code：需要 Anthropic Messages 兼容能力
- Codex CLI：需要 OpenAI Responses 兼容能力
- OpenCode：根据其 provider 配置支持 OpenAI 或 Anthropic 兼容能力

只有兼容状态明确成立时才启用应用按钮。不兼容或未确认时显示协议缺失、URL 问题或模型问题。

### 8.4 写入目标 CLI

每个 CLI 由独立适配器生成其官方配置格式。通用写入规则：

1. 读取并解析现有配置。
2. 备份原文件。
3. 只更新 Osverse 管理的 provider/model 字段，保留无关设置。
4. 写入同目录临时文件并同步磁盘。
5. 将临时文件原子替换目标文件。
6. 含凭据文件在 Linux 上强制为 `0600`，目录为 `0700`。
7. 重新读取并验证结构。

Osverse 记录应用时的文件哈希和自己管理的字段快照。如果 CLI、CC Switch、Cockpit Tools 或用户之后修改了目标文件，下一次应用必须显示差异并重新合并，不能静默覆盖。

### 8.5 档案更新

用户修改 API Key、Base URL 或模型名后，Osverse 先重新探测，再列出将受影响的 CLI。只有用户确认后才批量更新。某个 CLI 更新失败不回滚其他已成功目标，但必须保留该 CLI 的原始配置并给出可重试状态；每个目标自身是原子事务。

## 9. 凭据安全

### 9.1 主存储

优先使用 Linux Secret Service（通常由 GNOME Keyring 提供）保存 Osverse 主加密密钥。API 配置档案内容使用 AES-256-GCM 加密，每条记录使用随机 nonce 并携带格式版本。

### 9.2 回退存储

系统密钥环不可用或用户选择不使用时：

- 生成随机 256 位本地密钥
- 密钥文件权限设为 `0600`
- 数据目录权限设为 `0700`
- UI 明确显示当前使用“本地文件保护”而非系统密钥环

此方案不要求主密码，保护目标是防止无意读取和普通文件泄露；它不能抵御已经取得当前用户会话及全部文件权限的攻击者。

### 9.3 必要的明文配置

若目标 CLI 的官方格式要求把 API Key 写入配置文件，Osverse 在首次应用时明确提示。该文件使用 `0600`，备份同样使用 `0600`。界面和日志只显示 Key 后四位，剪贴板复制需要显式动作。

### 9.4 日志脱敏

日志层在写盘前统一移除或掩码：

- API Key、OAuth Token、Authorization/Cookie 头
- URL 中的凭据和敏感查询参数
- 代理用户名与密码
- 请求或命令环境中的秘密值

用户可复制脱敏诊断报告。普通错误信息不得包含完整环境变量或配置文件内容。

## 10. 远程清单与更新安全

Osverse 内置一份可离线工作的组件清单。联网后可拉取新的版本清单，要求：

- 使用 HTTPS
- 使用内置 Ed25519 公钥验证签名
- 清单版本单调递增并具有过期时间
- 下载来源必须匹配内置允许域名策略
- 每个制品必须声明 SHA-256
- 清单只允许声明结构化安装动作，绝不允许下发 Shell 脚本或任意命令

签名失败、清单过期或字段不受支持时继续使用最后一份有效清单，并在界面告警。

Osverse 自身更新只提示，不静默安装。用户确认后下载对应 `.deb` 或 AppImage，验证签名和 SHA-256 后再进入安装流程。

## 11. 错误处理与恢复

所有长任务提供阶段、当前步骤、字节/步骤进度、取消按钮和脱敏日志。

| 故障 | 行为 |
| --- | --- |
| 网络或代理中断 | 停止当前下载，保留安全的可续传数据，不自动改用直连 |
| SHA-256 或签名失败 | 隔离制品，禁止安装，提示来源与期望值不匹配 |
| 磁盘不足 | 在写入前预检；中途发生时清理 staging 并保持旧版本 |
| 安装验证失败 | 自动恢复旧 current/shim，保留失败版本用于诊断或清理 |
| 应用崩溃/断电 | 下次启动读取事务日志并完成清理或回滚 |
| PATH 配置冲突 | 不自动重写；显示冲突文件和手动/自动修复选项 |
| 外部配置发生变化 | 停止覆盖，显示差异并要求重新合并 |
| 密钥环锁定 | 提示用户解锁；可选择本地文件保护回退，但不自动降级 |
| 管理员授权取消 | 标记操作取消，不影响用户空间 CLI |
| 上游不兼容 | 禁用安装，显示最低版本和官方链接 |

任务错误使用稳定错误码和本地化消息分离，便于 UI 展示、测试和诊断。

## 12. 测试设计

### 12.1 自动化测试

#### Go 单元测试

覆盖：

- Ubuntu 版本与架构识别
- PATH 合并和命令冲突检测
- 语义版本比较
- 代理协议探测与超时
- 安装计划生成和权限边界
- 清单签名、过期、来源允许列表和哈希验证
- 下载恢复、提交和回滚状态机
- Base URL 规范化和 SSRF 防护
- API 兼容矩阵
- 三个 CLI 配置解析与保留式合并
- AES-GCM 存储和密钥回退
- 日志脱敏

#### Go 集成测试

使用虚拟文件系统、假 HTTP/代理服务、假进程执行器和临时目录验证完整事务，不在开发机上真实修改用户配置。

#### React 测试

覆盖：

- 统一状态卡和健康度计算
- 安装计划确认与进度
- 取消、错误和恢复提示
- 路径冲突选择
- API 协议兼容矩阵
- Key 脱敏显示
- 不兼容系统的禁用状态和说明

#### 打包冒烟测试

- `.deb` 安装、启动和卸载
- AppImage 启动
- React 到 Go Bridge 调用
- 桌面入口、图标和单实例行为
- `~/.local/bin` shim 在全新登录 Shell 中运行

#### 安全与供应链检查

- Go、npm 依赖漏洞扫描
- 许可证检查
- Secret 扫描
- 生成 SBOM
- Release 制品 SHA-256
- GitHub Actions 最小权限和固定 Action 版本

### 12.2 真实虚拟机验收矩阵

每个候选版本在干净的 Ubuntu 20.04 和 22.04 x86_64 虚拟机上验证：

- 未安装 Node、系统 Node 过旧、系统 Node 合格
- 无代理、HTTP、SOCKS5、错误端口、代理中途断开
- 三个 CLI 从缺失到安装成功，并能在新终端直接运行
- 已有同名 CLI 时不覆盖并正确显示全部路径
- 下载中断、校验失败、磁盘不足和版本验证失败
- Secret Service 可用、不可用和锁定
- API 协议兼容、不兼容、未确认、错误 Key、错误模型
- CLI 配置被外部修改后的合并保护
- Claude Desktop 在 20.04 禁止安装，在 22.04 可按官方方式安装
- ChatGPT Desktop 在两版系统上均禁止强装并显示官方要求
- CC Switch 与 Cockpit Tools 检测、安装、更新和启动
- 卸载 Osverse 后默认保留 CLI 和配置

## 13. 发布流程

GitHub Actions 分别构建 `.deb` 与 AppImage，并输出：

- 版本化安装包
- `SHA256SUMS`
- SBOM
- 签名后的更新元数据
- Release notes

构建基线必须与 Ubuntu 20.04 兼容；发布前在 Ubuntu 20.04 和 22.04 的真实 VM 运行打包冒烟测试。GitHub Release 先进入预发布渠道，完成人工验收后再标记稳定版。

Linux 稳定版发布的硬标准：用户在干净的受支持系统中，最多只需选择直连或输入代理端口，即可安装三款 CLI；重新打开终端后 `claude`、`codex`、`opencode` 均可执行；任何失败不会破坏操作前的可用状态。

## 14. 后续平台扩展原则

Windows 和 macOS 复用 React UI、领域模型、安装状态机、API 适配器及大部分 Go 核心，只替换平台接口：

- 路径和 Shell/PATH 集成
- 系统密钥存储
- 提权机制
- 桌面包格式
- 进程和应用检测
- Release 制品

新增平台不得通过大量条件分支侵入核心模块；平台差异必须由接口和能力声明隔离。

## 15. 完成定义

Linux v1 只有在以下条件全部满足时才视为完成：

1. Ubuntu 20.04/22.04 x86_64 的 `.deb` 和 AppImage 均通过发布验收。
2. 三个核心 CLI 支持完整检测、事务式安装、升级、冲突处理和验证。
3. CLI 脱离 Osverse 可直接从新终端运行。
4. 代理自动探测和失败保护按设计工作。
5. API 配置档案可探测协议并安全应用到所有兼容 CLI。
6. CC Switch 与 Cockpit Tools 可从官方 Release 安装、更新和启动。
7. 桌面应用严格遵循上游系统兼容要求。
8. 安装和配置失败不会破坏操作前状态。
9. 自动化测试、安全检查和真实 VM 矩阵全部通过。
10. Release 包含校验值、SBOM、更新元数据和使用说明。

## 16. 上游资料基线

以下资料用于确定本设计在 2026-08-13 的技术与兼容边界。实现阶段必须把上游变化作为清单和兼容测试输入，不能假定这些页面永久不变。

- Wails Linux 发行版支持：<https://wails.io/docs/guides/linux-distro-support/>
- Tauri v2 Linux 依赖：<https://v2.tauri.app/start/prerequisites/>
- Claude Code 安装与系统要求：<https://docs.anthropic.com/en/docs/claude-code/getting-started>
- Claude Desktop Linux 安装与系统要求：<https://support.claude.com/en/articles/10065433-install-claude-desktop>
- ChatGPT Desktop Linux 系统要求：<https://learn.chatgpt.com/docs/linux/linux-app>
- Codex CLI：<https://learn.chatgpt.com/docs/codex/cli>
- OpenCode 下载与安装：<https://dev.opencode.ai/download>
- CC Switch Releases：<https://github.com/farion1231/cc-switch/releases>
- Cockpit Tools Releases：<https://github.com/jlcodes99/cockpit-tools/releases>
