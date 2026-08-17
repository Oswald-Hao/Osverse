# Windows v1 release acceptance

此清单是 Osverse Windows 10/11 x64 从功能分支晋级到 `dev`、`beta`、`main` 并发布的门禁。自动化证据来自 GitHub 托管的 `windows-2022` 原生 Runner，不使用 Wine 代替 Windows 验证。

## 自动化门禁

- [x] Go 1.25.12、Node.js 22.23.2、Wails 2.13.0 工具链固定。
- [x] Windows 原生 `go test ./...`、`go test -race ./...`、`go vet ./...`。
- [x] 前端测试、类型检查与高危依赖审计。
- [x] Wails Windows amd64 原生构建，内嵌 WebView2 bootstrapper。
- [x] 当前用户范围 NSIS 安装包生成。
- [x] 三个 Windows 产物的 SHA-256 重新计算并校验。
- [x] 静默安装、启动存活检查、强制退出、静默卸载与残留二进制检查。
- [x] CI 构建产物上传；正式标签发布时生成统一校验和、SBOM、更新清单和构建来源证明。

首轮通过证据：PR #12，GitHub Actions run `31786053589`，Windows job `94722086808`，日期 2026-08-14。

## 原生能力验收

- [x] 系统版本、架构、Shell 和支持状态来自 Windows 原生信息源。
- [x] CLI 发现覆盖当前用户有效 PATH；不要求已有工具迁移到 Osverse 管理目录。
- [x] 桌面应用证据来自固定安装目录、注册表或精确 Microsoft Store 包身份。
- [x] 命令运行器使用 Job Object 清理超时或取消后的进程树。
- [x] 启动前重新扫描并验证身份，前端不能提交任意可执行路径。
- [x] API 档案存储在 `%LOCALAPPDATA%\Osverse`，AES 主密钥由当前用户 DPAPI 保护。
- [x] CLI 下载使用固定 URL、精确字节长度和 SHA-256；归档解压拒绝路径穿越。
- [x] DeepSeek Harness 的 Windows Node ZIP 与全部平台适用 npm 包完成真实制品展开；逐包 SHA-512、Node 大小/SHA-256 和 Windows 命令入口均由自动化校验。
- [x] 桌面安装仅使用固定 WinGet、Store、MSI 或受信任安装器身份。
- [x] 移除不会删除 API 配置、凭据或登录会话；受管 CLI 移入恢复目录。

## 发布产物

- [x] `osverse-<version>-windows-amd64-setup.exe`：推荐的每用户安装包。
- [x] `osverse-<version>-windows-amd64-portable.zip`：免安装目录。
- [x] `osverse-<version>-windows-amd64.exe`：单文件应用。
- [x] Release 统一 `SHA256SUMS` 包含 Windows 与 Linux 文件。

## 人工补充检查

以下项目不阻断已完成的原生 CI 预发布，但在宣布 Windows 稳定版前应分别在 Windows 10 与 Windows 11 实机复核：

- [ ] 100%、125%、150%、200% 缩放下窗口布局与文字无截断。
- [ ] 中文与英文系统区域设置下安装器、程序和卸载器可用。
- [ ] 已安装真实 Claude Code、Codex CLI、OpenCode CLI 的机器均显示正确位置与版本。
- [ ] 真实第三方 API 在确认后应用到目标 CLI，重启 CLI 后仍生效。
- [ ] Windows Defender/SmartScreen 对未签名预发布包的提示被发布说明清楚解释。
- [ ] 在 Windows 10 与 11 各完成一次 Harness 安装，确认 `dsh --version`、`dsh web`、Provider 保存、Osverse 启动和保留凭据的安全移除。

正式代码签名证书不在当前仓库自动化范围内；预发布产物依靠 SHA-256、GitHub OIDC provenance 与可复现的公开 CI 记录建立来源链。
