# Osverse Linux v1 Delivery Roadmap

> **For agentic workers:** Each phase requires its own implementation plan and review gate. Do not start a later phase until the prior phase is merged and its acceptance checks pass.

**Goal:** Deliver Osverse Linux v1 as five independently testable increments, ending with signed `.deb` and AppImage releases for Ubuntu 20.04/22.04 x86_64.

## Phase order

1. **Foundation, read-only scan, and dashboard**
   - Wails v2 + React/TypeScript application shell
   - Linux system, PATH, CLI, desktop app, and management-tool detection
   - Cockpit-style status dashboard
   - No installation or configuration writes
   - Detailed plan: `docs/superpowers/plans/2026-08-13-phase-1-foundation-scan-dashboard.md`

2. **Proxy probe and transactional CLI installation**
   - HTTP/HTTPS CONNECT/SOCKS5 probe
   - Signed component manifest model
   - Managed Node runtime, immutable CLI versions, stable shims, PATH integration
   - Plan/confirm/download/checksum/install/verify/rollback task engine

3. **Encrypted API profiles and CLI configuration adapters**
   - Secret Service with AES-GCM local-key fallback
   - Base URL normalization and protocol capability probe
   - Claude Code, Codex, and OpenCode config merge adapters
   - External-change detection, backup, atomic replace, and redacted diagnostics

4. **Desktop apps and third-party management tools**
   - PolicyKit fixed-action helper
   - Compatibility-gated desktop installation
   - CC Switch and Cockpit Tools official GitHub Release installation/update/launch

5. **Packaging, supply-chain controls, and release qualification**
   - Ubuntu 20.04 WebKitGTK 4.0 build baseline
   - `.deb` and AppImage packaging
   - SBOM, SHA-256, signed update metadata, dependency/secret/license scans
   - Ubuntu 20.04/22.04 x86_64 VM acceptance matrix and prerelease promotion

## Cross-phase review gates

- A phase must leave `go test ./...`, frontend tests, typecheck, and production build green.
- New operating-system writes require a previewable plan and a rollback test.
- No phase may expose a generic command-execution or arbitrary-download Wails binding.
- Sensitive values must be represented by redacted view models before reaching the frontend.
- Every phase ends with a focused commit and a pushed `main` only after local verification.
