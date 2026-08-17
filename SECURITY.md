# Security policy

## Supported versions

During the Linux and Windows prerelease, security fixes are applied to the newest published prerelease and the active `dev` branch. Older prereleases may be replaced rather than patched in place.

## Report a vulnerability privately

Please do not open a public issue for a suspected vulnerability. Use [GitHub private vulnerability reporting](https://github.com/Oswald-Hao/Osverse/security/advisories/new) and include:

- the affected Osverse version and operating-system release;
- a minimal reproduction or proof of concept;
- the expected impact and required user interaction;
- whether credentials, filesystem writes, privilege boundaries, update metadata, or installer rollback are involved.

Remove real API keys, access tokens, private URLs, usernames, and unrelated personal data. You should receive an acknowledgement within 72 hours. A validated issue will be coordinated privately until a fix and release are available.

## Trust boundary

Osverse is local-first and has no telemetry. The frontend cannot choose download URLs or digests: installers use backend-owned allowlists and plans. API secrets are encrypted at rest and are not returned in lists or operation history. On Windows, the profile master key is additionally protected with current-user DPAPI. Privileged Linux Claude Desktop installation is restricted to a fixed PolicyKit helper action; Windows installs and removals use backend-owned artifact identities, Store/WinGet IDs, MSI ProductCodes, or trusted uninstaller paths.

Qwen Code standalone downloads are limited to an exact official release URL, byte length, and SHA-256 digest. Redirects are disabled. Archive extraction is confined to the fixed `qwen-code/` root and rejects traversal, links, special files, hazardous cross-platform names, excess entries, and excess expanded size. Osverse verifies the packaged version with the bundled Node.js runtime before atomically activating its own command wrapper. API profile application preserves unrelated Qwen settings, writes the exact model ID and endpoint, and stores the fallback key only in the current-user `0600` settings file and its protected backup.

GitHub Copilot CLI standalone downloads use the same fixed-origin, exact-length, SHA-256, no-redirect policy. Extraction accepts exactly one regular `copilot` or `copilot.exe` entry and rejects links, traversal, duplicate or extra entries, and oversized output. Osverse executes the extracted binary with auto-update disabled and requires the exact pinned version before activation. The managed wrapper keeps `--no-auto-update` enabled so later upstream downloads cannot bypass this boundary. Authentication tokens remain owned by Copilot CLI and are not read by Osverse.

Application self-update reads only the fixed `Oswald-Hao/Osverse` GitHub Release feed. It requires a valid semantic version, an exact repository/tag/channel manifest, a known platform filename and release path, a bounded byte length, and a lowercase SHA-256 digest. The frontend receives an opaque, short-lived, one-use plan ID and cannot replace any of those values. Windows and macOS hand off to a visible platform installer; Linux `.deb` updates require system confirmation, while AppImage and portable updates keep one rollback copy and restore it if restart fails. Release HTTPS and the GitHub repository account remain part of this trust boundary; GitHub provenance attestations provide an additional verification path for users and reviewers.

Release consumers should verify `SHA256SUMS` and GitHub build provenance:

```bash
sha256sum --check SHA256SUMS --ignore-missing
gh attestation verify ./osverse-asset --repo Oswald-Hao/Osverse
```

The repository CI scans reachable dependency vulnerabilities, licenses, Git history secrets, and validates an SPDX SBOM. These controls reduce risk but are not a substitute for responsible disclosure.
