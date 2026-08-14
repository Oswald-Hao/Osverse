# Osverse Linux

首个 Linux 版本面向 Ubuntu 20.04/22.04 x86_64，提供对应系统构建的 `.deb` 和便携 tar 包。

推荐下载与你的 Ubuntu 版本对应的 `.deb`，然后运行：

```bash
sha256sum -c SHA256SUMS --ignore-missing
sudo apt install ./osverse_*_amd64_ubuntuXX.XX.deb
osverse
```

主要功能：

- 检测 Claude Code、Codex CLI、OpenCode CLI 以及桌面/管理工具的现有安装；
- 使用固定版本、大小与 SHA-256 的事务安装器安装或更新 CLI 与 AppImage；
- 在 Ubuntu 22.04 上通过已验证的 Anthropic APT 签名密钥安装 Claude Desktop；
- 探测本机 HTTP、HTTPS CONNECT、SOCKS5 代理，代理只作用于 Osverse；
- 加密保存第三方 API 档案，探测协议兼容性并在确认后更新 CLI 配置；
- 保留最多 200 条脱敏本地操作记录。

ChatGPT Desktop 没有官方 Linux 客户端，因此在本版本中保持不可安装状态。Claude Desktop 官方要求 Ubuntu 22.04 或更高版本，因此 Ubuntu 20.04 不提供该安装按钮。
