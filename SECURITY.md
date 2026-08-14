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

Release consumers should verify `SHA256SUMS` and GitHub build provenance:

```bash
sha256sum --check SHA256SUMS --ignore-missing
gh attestation verify ./osverse-asset --repo Oswald-Hao/Osverse
```

The repository CI scans reachable dependency vulnerabilities, licenses, Git history secrets, and validates an SPDX SBOM. These controls reduce risk but are not a substitute for responsible disclosure.
