# Versioning and releases

## Source of truth

[`mygo.json`](../mygo.json) is the single source for the app version. MyGo
embeds it in the application bundle. Do not maintain a second VERSION file
or change `go.mod` to bump the application version.

Read the current release from `mygo.json` and its upstream base from
`shibu-upstream.json`. Local releases use:

```text
<upstream-major>.<upstream-minor>.<upstream-patch>-local.<revision>
2.0.5-local.3
```

- The first three numbers identify the upstream release base.
- `local.N` is a positive integer, incremented for every newly shipped
  personal app build that changes runtime behavior, branding or assets.
- For the same base, increment the current local revision, for example
  `2.0.5-local.5` becomes `2.0.5-local.6`. The release workflow does this.
- Documentation-only commits do not bump the app version. Record them under
  **Unreleased** until the next app release.
- Once a newer upstream release is merged and verified, adopt its base and
  start at `local.1`, for example `2.0.6-local.1`. Merging an arbitrary upstream
  commit does not by itself change the base; record that commit in release notes.
- Never reuse a shipped version for a different app build. The local.2
  branding rebuild predates this policy and is documented in the changelog.

This is SemVer-compatible prerelease notation; `2.0.5-local.3` sorts below
upstream `2.0.5`. Keep upstream binary auto-updates disabled for the personal
edition; Shibu binary updates use the separate signed `shibu-v` feed.
Do not interpret upstream update ordering as Shibu release ordering.

## Release checklist

1. Review the changes since the previous personal release. Update
   [change details](changes.md) and the [local guide](local-fork.md) as needed.
2. Choose the release path. The guarded workflow in **mode: release** increments
   `version` in `mygo.json` itself; do not increment it manually first. For a
   separately managed local build, increment it once before building. Keep the
   name, bundle ID and `kero-local` data identity stable.
3. Maintain the relevant **Unreleased** entries in [Shibu changelog](changelog.md).
   The workflow moves them into a dated release section; do that manually only
   for the separately managed local path. Record features, fixes, migration
   needs, verification and known limits; do not claim an install before it happens.
4. Follow [Build and install](build.md): run checks, build, confirm embedded
   version/architecture/signature, install, and smoke-test the app. Record
   the results in [verification history](local-v1-verification.md). Keep
   credentials, local session IDs and private histories out of new reports.
5. Keep the release/base in `mygo.json`/`shibu-upstream.json` accurate.
   Preserve previous changelog and verification entries.
6. The workflow commits the release changes, creates the annotated
   `shibu-v<version>` tag and publishes after its gates pass. For a separately
   managed release, commit/tag that exact verified build yourself and push only
   when publishing is part of the task. Never push a release to `egoist/kero`.

The `shibu-v` prefix distinguishes personal tags from upstream `v2.0.5` tags.
The inherited upstream release workflow is replaced by the Shibu workflow.
No personal release tags were created retroactively for local.1–local.5.
The [automatic update workflow](automatic-updates.md) publishes signed update
archives after checks pass; a tag by itself does not notarize a macOS build.

## Incorporating upstream

Keep `origin` as `ddeyalla/kero` and `upstream` as `egoist/kero`, with upstream
pushes disabled. Prefer the guarded stable-release workflow. For a manual
upstream merge, use an isolated feature branch, resolve conflicts, then verify
the personal identity, Shibu-only binary update feed and all local features
before shipping. See [source update instructions](local-fork.md#incorporate-upstream-changes)
and [fork-only source saves](keeping-your-fork.md).
Retain the root upstream changelog and add personal changes to
`docs/changelog.md` to reduce merge conflicts.
