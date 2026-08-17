# Linux v1 release acceptance

此清单是 Osverse Linux v1 从 `dev` 晋级 `beta`、再晋级 `main` 的发布门禁。自动化测试、Ubuntu 20.04/22.04 构建、包结构检查和至少一次 Ubuntu 22.04 真实 Wails 冒烟测试必须全部通过。

## 自动化门禁

- [x] `go test ./...`
- [x] `go test -race ./...`
- [x] `go vet ./...`
- [x] 前端全部测试、TypeScript 检查、生产构建、依赖审计和响应式审计
- [x] Ubuntu 20.04 `webkit2_36` 原生构建及动态库解析
- [x] Ubuntu 22.04 `webkit2_40` 原生构建及动态库解析
- [x] 两种构建均成功生成 `.deb`、AppImage、tar 和 SHA-256 清单
- [x] `.deb` 只包含声明的二进制、图标、desktop entry、文档和 control 元数据，无 maintainer script
- [x] 两种 `.deb` 均完成真实安装/卸载；两种 AppImage 均在 Xvfb 中出现可见 Osverse 窗口
- [x] OSV 可达性漏洞扫描、许可证白名单、Gitleaks 历史扫描和 SPDX SBOM 校验通过
- [x] Release 更新元数据、SBOM、校验和及全部安装包均包含 GitHub OIDC 构建来源证明
- [x] 发布工作流语法通过 actionlint，所有第三方 Action 使用完整提交 SHA

## 真实界面与扫描

- [x] 窗口以 1280×800 打开，320/650/900/901/960/1024/1053/1440 宽度无横向溢出
- [x] 扫描结果与 `command -v`、对应绝对路径的 `--version`、`dpkg-query` 一致
- [x] PATH 中任意正常位置的 Claude Code 都显示已安装，不要求放入 Osverse 目录
- [x] 刷新保留旧快照直到新扫描完成，时间戳随后更新
- [x] 总览、API 配置、安装记录和设置四个导航页均可访问

## 安装与回滚

- [x] Claude Code、Codex CLI、OpenCode CLI 三个既有核心 CLI 都先展示固定计划，再安装到 `~/.local/share/osverse/tools`
- [x] DeepSeek Harness 从嵌入式 lockfile 重建完整运行时，不执行 npm 生命周期脚本；`dsh --version` 与 `dsh web` 真实启动通过
- [x] 取消、下载截断、SHA 不匹配、版本校验失败时当前命令不改变
- [x] 进程在链接/配置提交中断后，下次启动从权限为 `0600` 的事务日志恢复旧状态
- [x] 外部同名命令存在时不覆盖，并显示安全错误
- [x] OpenCode Desktop、CC Switch、Cockpit Tools AppImage 安装后可扫描、启动和更新
- [x] AppImage 被篡改后 Osverse 拒绝启动
- [x] Ubuntu 20.04 不提供 Claude Desktop 安装；Ubuntu 22.04 计划明确显示管理员授权与 APT 变更
- [x] Claude 官方密钥指纹为 `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`

## 代理与 API

- [x] 直连与本机 HTTP、HTTPS CONNECT、SOCKS5 代理探测行为正确
- [x] 更换或失败的代理探测不会继续使用旧端口
- [x] API Key 保存后立即从表单清除，列表和历史中仅出现掩码
- [x] 公网 API 协议探测不产生付费生成请求；重定向和私网 SSRF 默认被拒绝
- [x] 用户明确确认私网端点后才允许探测
- [x] Claude/Codex/OpenCode 配置在二次确认后原子写入，非 Osverse 字段保持不变且保留备份
- [x] Harness Provider 凭据由 Harness 自己保存在 `$DSH_HOME`；Osverse 启动、更新和移除均不读取或删除

## 发布证据

记录候选提交、CI URL、两个二进制及包的 SHA-256、运行系统版本、WebKitGTK 版本、截图和测试日期。预发布可用于收集额外反馈；只有所有阻断项关闭后才把同一候选提交晋级 `main` 并创建稳定标签。

### 2026-08-14 候选提交 `57b9685`

- CI：[31766216435](https://github.com/Oswald-Hao/Osverse/actions/runs/31766216435)，四个 job 全部成功。
- Ubuntu 20.04 二进制 SHA-256：`0a2b7391478a687b6d2c7c2335080d941a0488d6a811ec8029bf5e921288e6c5`。
- Ubuntu 22.04 二进制 SHA-256：`3a76b2f94da60bef15968f0f0335508ad75f74849fa13154236e62d8a8272237`。
- Ubuntu 20.04 `.deb` SHA-256：`69617561543abdd55e97c7fbff6ef9493275671041d218559bd50f45fbc36e94`。
- Ubuntu 22.04 `.deb` SHA-256：`a0a66824b7c82251241f0374339aedee781a9a5cf3993955a1996e4bd37e9b20`。
- CI 使用 Go 1.25.12、Node.js 22.23.2；Ubuntu 20.04 原生构建使用 `webkit2_36`，Ubuntu 22.04 原生构建使用 `webkit2_40`。
- 两个 tar 和两个 `.deb` 的内置清单均重新通过 `sha256sum --check`；两个 ELF 均为动态链接 x86-64，当前 Ubuntu 22.04 主机无缺失动态库。
- `.deb` 内容检查通过：仅包含 `/usr/bin/osverse`、desktop entry、图标、README 和 control 元数据，无 maintainer script；desktop entry 权限为 0644。
- 实机环境：Ubuntu 22.04.5 LTS、x86_64、GNOME/X11、Bash。两个系统构建均以 1280×800 打开并正常退出。
- 总览扫描与直接命令一致：Claude Code 在两个 PATH 位置均为 2.1.114，界面状态为已安装；Codex CLI 为 0.147.0；OpenCode CLI 缺失。当前验证了“外部路径即有效安装”的产品要求。
- 总览、API 配置、安装记录、设置四页均完成真实窗口渲染；API 表单不含预填凭据，设置页显示 AES-256-GCM、loopback 代理作用域、固定清单与无遥测策略。
- Anthropic 官方密钥已从 `downloads.claude.ai` 只读下载并由 GnuPG 实测，指纹精确匹配 `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`。
- 未在真实主机执行 CLI/AppImage/Claude Desktop 安装或写入真实 API 凭据；这些写路径由隔离临时目录、伪 HTTP 服务、失败注入、全量 race 测试和事务回滚测试覆盖，避免为验收改变用户现有环境。

### 2026-08-14 预发布 `v0.1.1-beta.1`

- 分支晋级：`dev` 候选 `74f5dd8` → `beta` 合并 `8890bed` → `main` 合并 `7baf587`；带注释标签 `v0.1.1-beta.1` 精确指向 `7baf587`。
- CI：[`dev` 31768674094](https://github.com/Oswald-Hao/Osverse/actions/runs/31768674094)、[`beta` 31769085201](https://github.com/Oswald-Hao/Osverse/actions/runs/31769085201)、[`main` 31769241470](https://github.com/Oswald-Hao/Osverse/actions/runs/31769241470) 的五个 job 均成功。
- Release：[31769392519](https://github.com/Oswald-Hao/Osverse/actions/runs/31769392519) 从标签源码重新构建；标签来源、两套源码测试、原生构建、`.deb` 安装/卸载、AppImage 可见窗口、SBOM、更新清单、来源证明与发布步骤全部成功。
- [GitHub prerelease](https://github.com/Oswald-Hao/Osverse/releases/tag/v0.1.1-beta.1) 为非草稿且 `prerelease=true`，包含 6 个安装文件、`SHA256SUMS`、SPDX 2.3 SBOM 和结构化更新清单。
- Ubuntu 20.04 AppImage：`386ba0a926745fe30acc97f959b87bc8c192c47fc32c053c131ae3f0b8b70232`；`.deb`：`6ab0267fa0642c5ae22a3240cf9a82d372d0f94ed16a0d9dc2233538fb17fc50`。
- Ubuntu 22.04 AppImage：`a86805747a071fcfe53aebb0b6a6f345fcafa88b3ca8f9648efd0a77c2e450c2`；`.deb`：`c6639c1846d8f450165c5abed53dc4e77534ca068c38e0c68806883bb00a4d25`。
- `SHA256SUMS`、Release API 摘要和更新清单中的 8 个受校验文件逐项一致；更新清单列出 6 个安装文件及其大小、目标、URL 和 SHA-256。
- `gh attestation verify` 验证成功：证书身份为仓库的 `release-linux.yml@refs/tags/v0.1.1-beta.1`，源提交为 `7baf587`，9 个 subject 均写入 SLSA v1 provenance 并带 Rekor 时间戳。
- OSV-Scanner 2.5.0 的 Go 可达性与 npm 依赖扫描为 0 个已知漏洞；许可证白名单、Gitleaks 全历史扫描、Syft 1.51.0 SPDX 文档校验均通过。
- README 使用 Ubuntu 22.04 上真实运行得到的 1280×800 总览、CLI 检测与 API 配置截图；其中 Claude Code 在两个外部 PATH 位置均正确显示为已安装。
