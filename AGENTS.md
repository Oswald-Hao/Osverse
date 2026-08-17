# Osverse repository rules for coding agents

## Protected-branch workflow

`dev`, `beta`, and `main` are protected integration branches. Never develop, commit, or push directly on any of them.

Before editing files:

1. Inspect the current branch and worktree.
2. For each bug, feature, dependency upgrade, documentation change, or maintenance task, create a new focused branch from the latest `origin/dev`.
3. Keep unrelated work on separate branches and in separate pull requests.

Only use this promotion path:

```text
fix/* | feat/* | test/* | docs/* | chore/*
                    ↓ pull request + complete CI
                   dev
                    ↓ promotion pull request
                   beta
                    ↓ promotion pull request
                   main
                    ↓ release tag
```

Feature branches may target only `dev`. Only `dev` may target `beta`, and only `beta` may target `main`. Do not cherry-pick around the promotion chain, synchronize protected branches with direct pushes, force-push them, or tag a release from any commit outside `main` history.

Automation may enable pull-request auto-merge after required checks pass. It must not bypass branch protection or required checks.
