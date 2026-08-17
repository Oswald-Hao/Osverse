# Osverse 保护分支规则 / Protected branch policy

## 中文

`dev`、`beta`、`main` 是 Osverse 的保护分支。仓库不使用 `master`；稳定发布分支为 `main`。

每发现一个问题、添加一个功能、升级一组相关依赖或修改一项文档，都必须先从最新 `origin/dev` 创建一个新的聚焦分支。禁止直接在 `dev`、`beta`、`main` 上开发、提交或推送，也禁止在一个功能分支里混入无关任务。

允许的唯一晋级链路为：

```text
问题/功能/测试/文档/维护分支 → dev → beta → main → Release 标签
```

- 功能分支只能通过 PR 合入 `dev`，并运行完整 Linux、Windows、前端和供应链 CI。
- `dev` 只能通过晋级 PR 合入 `beta`。
- `beta` 只能通过晋级 PR 合入 `main`。
- 晋级 PR 复用功能 PR 已验证的源码，只验证分支来源，不由 CI 直接推送或同步。
- Release 标签必须指向 `main` 历史中的提交。
- 不允许绕过 PR、必需检查、未解决对话或管理员分支保护；不允许强推或删除保护分支。
- 可以在所有必需检查通过后启用 PR 自动合并，不要求维护者重复手动点击。

GitHub 已对三个保护分支启用严格状态检查、必须通过 PR、管理员同样受限、禁止强推和删除、必须解决对话。仓库内 `Validate promotion path` 检查继续验证 PR 的来源与目标关系。

## English

`dev`, `beta`, and `main` are protected Osverse branches. The repository does not use `master`; `main` is the stable release branch.

Every bug, feature, related dependency upgrade, documentation change, or maintenance task starts on a new focused branch created from the latest `origin/dev`. Direct development, commits, or pushes on `dev`, `beta`, or `main` are forbidden, and unrelated tasks must not share a feature branch.

The only permitted promotion chain is:

```text
fix/feature/test/docs/maintenance branch → dev → beta → main → release tag
```

- A feature branch may enter only `dev`, through a pull request that runs the complete Linux, Windows, frontend, and supply-chain CI suite.
- Only `dev` may be promoted to `beta`.
- Only `beta` may be promoted to `main`.
- Promotion pull requests reuse the source already verified on the feature pull request; CI validates the branch relationship and does not push or synchronize protected branches.
- Release tags must point to commits in `main` history.
- Pull requests, required checks, resolved conversations, and administrator-enforced branch protection may not be bypassed. Force pushes and deletion are disabled.
- Pull-request auto-merge may be enabled after all required checks pass; a redundant manual merge click is not required.

GitHub protection currently enforces strict status checks, pull requests, conversation resolution, administrator inclusion, and force-push/deletion denial on all three branches. The repository's `Validate promotion path` check additionally enforces the permitted source and target relationship.
