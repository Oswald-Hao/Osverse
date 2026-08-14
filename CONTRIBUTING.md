# Contributing to Osverse

Thanks for helping make AI development environments easier to reproduce. Osverse accepts focused bug fixes, tests, documentation improvements, and carefully scoped platform support.

## Before opening code

1. Search existing issues and releases.
2. Open an issue for behavior changes or a new managed tool. Describe the upstream source, installation contract, rollback plan, and security boundary.
3. Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).

## Development flow

Pull requests target `dev`. Changes are promoted by maintainers from `dev` to `beta`, then from `beta` to `main`; do not open feature pull requests directly against `main`.

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
