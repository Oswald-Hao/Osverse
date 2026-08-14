# Changelog

Osverse follows semantic versioning while it is in prerelease. Published artifacts and exact dates are available on the [GitHub Releases](https://github.com/Oswald-Hao/Osverse/releases) page.

## 0.3.0-beta.1

- Add native Windows 10/11 x64 system, CLI, desktop application, and management-tool detection.
- Add identity-checked Windows launch with Job Object process-tree cleanup.
- Protect encrypted API profile master keys with current-user Windows DPAPI.
- Install pinned Claude Code, Codex CLI, and OpenCode CLI artifacts with checksum, size, extraction, and version verification.
- Install supported Windows desktop tools through fixed vendor artifacts, WinGet IDs, Store IDs, MSI ProductCodes, and trusted uninstall identities.
- Safely remove managed Windows CLIs into a recovery directory while preserving configs, credentials, and login sessions.
- Publish per-user NSIS, portable zip, and standalone exe packages after native Windows install/launch/uninstall smoke tests.
- Extend release checksums, signed update metadata, SBOM, and provenance to both Linux and Windows artifacts.

## 0.2.0-beta.1

- Launch every freshly verified CLI, desktop application, and management tool, with separate controls for multiple installation locations.
- Preview and safely remove installations using single-use plans, target identity checks, recoverable Trash moves, and fixed privileged package actions.
- Edit encrypted API profiles without revealing stored credentials.
- Correct OpenCode's OpenAI-compatible provider configuration and Base URL normalization.
- Run the complete CI gate once per pull request and enforce `feature → dev → beta → main` promotion.

## 0.1.1-beta.3

- Validate third-party API credentials against the model endpoint.
- Apply Anthropic-compatible, OpenAI-compatible, and OpenCode provider profiles with encrypted local credential storage.
- Detect existing installations from the user's effective command environment.
