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
| [Changes and implementation](changes.md) | Features, code locations, behavior and remaining limits |
| [Build and install](build.md) | Toolchain, checks, local installation and rollback |
| [Versioning and releases](versioning.md) | Version source, numbering rules and release checklist |
| [Automatic updates](automatic-updates.md) | Daily upstream synchronization, signed releases, client behavior and recovery |
| [Local edition guide](local-fork.md) | Usage, shortcuts, credentials, attachments and upstream merges |
| [Verification record](local-v1-verification.md) | Completed checks, measurements and session import evidence |
| [Herdr comparison](herdr-porting.md) | Terminal architecture, latency findings and potential future ports |
| [Original v1 plan](plans/local-v1.md) | Initial implementation scope |

The [Kero user guide](index.md) and [upstream changelog](../CHANGELOG.md)
remain available. Upstream features and releases are separate from Shibu's
local changes. Update these docs alongside each relevant change; follow the
[release checklist](versioning.md#release-checklist) when shipping a build.
