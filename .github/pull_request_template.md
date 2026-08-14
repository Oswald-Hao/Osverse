## Summary

<!-- What user-visible problem does this solve? -->

## Verification

- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `npm --prefix frontend test`
- [ ] `npm --prefix frontend run typecheck`
- [ ] Relevant Ubuntu/package/manual checks are documented below.

## Safety review

- [ ] No credentials, private endpoints, or personal data are included.
- [ ] Detection remains read-only and bounded.
- [ ] Filesystem/network/privilege effects are explicit and rollback-safe.
- [ ] External commands and user-managed configuration retain ownership protection.

## Evidence

<!-- Paste concise test output or attach redacted screenshots. -->
