# Security policy

## Supported versions

During the Linux prerelease, security fixes are applied to the newest published prerelease and the active `dev` branch. Older prereleases may be replaced rather than patched in place.

## Report a vulnerability privately

Please do not open a public issue for a suspected vulnerability. Use [GitHub private vulnerability reporting](https://github.com/Oswald-Hao/Osverse/security/advisories/new) and include:

- the affected Osverse version and Ubuntu release;
- a minimal reproduction or proof of concept;
- the expected impact and required user interaction;
- whether credentials, filesystem writes, privilege boundaries, update metadata, or installer rollback are involved.

Remove real API keys, access tokens, private URLs, usernames, and unrelated personal data. You should receive an acknowledgement within 72 hours. A validated issue will be coordinated privately until a fix and release are available.

## Trust boundary

Osverse is local-first and has no telemetry. The frontend cannot choose download URLs or digests: installers use backend-owned allowlists and plans. API secrets are encrypted at rest and are not returned in lists or operation history. Privileged Claude Desktop installation is restricted to a fixed PolicyKit helper action.

Release consumers should verify `SHA256SUMS` and GitHub build provenance:

```bash
sha256sum --check SHA256SUMS --ignore-missing
gh attestation verify ./osverse-asset --repo Oswald-Hao/Osverse
```

The repository CI scans reachable dependency vulnerabilities, licenses, Git history secrets, and validates an SPDX SBOM. These controls reduce risk but are not a substitute for responsible disclosure.
