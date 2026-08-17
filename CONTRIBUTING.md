# Contributing to Osverse

Thanks for helping make AI development environments easier to reproduce. Osverse accepts focused bug fixes, tests, documentation improvements, and carefully scoped platform support.

## Before opening code

1. Search existing issues and releases.
2. Open an issue for behavior changes or a new managed tool. Describe the upstream source, installation contract, rollback plan, and security boundary.
3. Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Development flow

Every change reaches a protected branch through a pull request. Before changing files, update `origin/dev` and create a new focused branch for that one bug, feature, related dependency upgrade, documentation change, or maintenance task. Never develop or commit directly on `dev`, `beta`, or `main`, and never reuse one feature branch for unrelated tasks. Use this fixed promotion path:

1. Create a focused branch and open a pull request into `dev`.
2. When a tested development set is ready, open a `dev` → `beta` pull request.
3. After beta acceptance, open a `beta` → `main` pull request.
4. Create release tags only from `main`.

Feature pull requests into `dev` run the complete CI suite: tests, static analysis, supply-chain checks, and native Linux and Windows builds. Promotion pull requests (`dev` → `beta` and `beta` → `main`) run only the required promotion-path gate because they reuse the source branch content that already passed the complete suite. They never rebuild, push, or synchronize branches. GitHub performs the branch update only when a maintainer merges the pull request. A maintainer can still start a complete manual diagnostic run with `workflow_dispatch`, but it does not replace a required feature pull-request run.

The `Validate promotion path` required check enforces that chain. Do not open feature pull requests directly against `beta` or `main`, and do not push commits directly to `dev`, `beta`, or `main`.

The authoritative bilingual policy is [docs/governance/branch-policy.md](docs/governance/branch-policy.md). Coding agents must also follow [AGENTS.md](AGENTS.md).

Use the pinned versions in the README. Before submitting, run:

```bash
go mod verify
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend ci
npm --prefix frontend test
npm --prefix frontend run typecheck
npm --prefix frontend run build
npm --prefix frontend audit --audit-level=high
npm --prefix frontend run audit:responsive
```

Keep commits small and descriptive. Conventional prefixes such as `feat:`, `fix:`, `test:`, `docs:`, and `ci:` are preferred.

## Safety expectations

- Detection must remain read-only and bounded. Do not execute shell profiles or desktop entries during a scan.
- Installers need an allowlisted source, exact size and digest verification, an explicit plan, cancellation, rollback, and tests for interrupted state.
- Never put API keys, access tokens, private endpoints, or unredacted user data in fixtures, logs, screenshots, issues, or history entries.
- Do not overwrite externally managed commands or configurations without an exact ownership check and user confirmation.
- Network changes must stay local to Osverse unless a future design explicitly adds and reviews a broader setting.
- New privileged actions require a fixed command surface, strict argument validation, and a dedicated security review.

## Generated files and UI

Wails bindings under `frontend/wailsjs` are generated from the Go API. Regenerate them with Wails 2.13.0 when the public backend contract changes, and include only intentional output. Keep the 1280×800 default layout usable at every breakpoint covered by the responsive audit.

By contributing, you agree that your contribution is licensed under Apache-2.0 and that you will follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
