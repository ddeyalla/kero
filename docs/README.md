# Shibu Code documentation

Shibu Code is the personal Kero fork at [ddeyalla/kero](https://github.com/ddeyalla/kero).
It follows Kero's native UI and keeps its own application data. The current
application release is defined in [`mygo.json`](../mygo.json); the upstream
base is recorded in [`shibu-upstream.json`](../shibu-upstream.json).
These docs describe the local macOS edition.

## Start here

| Document | Contents |
| --- | --- |
| [Changelog](changelog.md) | Shibu release history and unreleased work |
| [Feature tracker](features.md) | Every Shibu feature, the release that shipped it, how to reach it and its status |
| [Keep your changes on your fork](keeping-your-fork.md) | Local commits, fork-only pushes, private settings and app releases |
| [Changes and implementation](changes.md) | Features, code locations, behavior and remaining limits |
| [Linear plugin and session links](linear-workspace.md) | Scope, setup, use, reliability and activation |
| [Security checks](security.md) | Secret scanning, fork-only push hooks and verification |
| [Build and install](build.md) | Toolchain, checks, local installation and rollback |
| [Versioning and releases](versioning.md) | Version source, numbering rules and release checklist |
| [Automatic updates](automatic-updates.md) | Daily upstream synchronization, signed releases, client behavior and recovery |
| [Local edition guide](local-fork.md) | Usage, shortcuts, credentials, attachments and upstream merges |
| [Verification record](local-v1-verification.md) | Completed checks, measurements and session import evidence |
| [Session recovery](session-recovery-2026-10-20.md) | Socket failure, authorized restart, restored panes and local backup |
| [Herdr comparison](herdr-porting.md) | Terminal architecture, latency findings and potential future ports |
| [Original v1 plan](plans/local-v1.md) | Initial implementation scope |

The [Kero user guide](index.md) and [upstream changelog](../CHANGELOG.md)
remain available. Upstream features and releases are separate from Shibu's
local changes. Update these docs alongside each relevant change; follow the
[release checklist](versioning.md#release-checklist) when shipping a build.
