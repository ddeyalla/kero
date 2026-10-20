# Shibu Code changes and implementation

For release-by-release history see [Changelog](changelog.md). For daily use
and credential configuration see the [local edition guide](local-fork.md).

## Account usage

Claude and Codex use existing CLI login credentials. OpenCode Go uses its
workspace key; DeepSeek shows direct API balance. These are provider-specific
allowances and balances, not a combined subscription budget. Reads run in
the background every two minutes, preserve the last successful observation
after errors, and respect rate-limit backoff. Explicit provider-key overrides
are atomically stored with mode `0600` in the personal data directory.

The sidebar opens an in-window modal with allowance windows, reset times,
refresh and provider configuration. Cmd+Shift+U opens it; Cmd+R refreshes
while open; Esc/Cmd+W closes it. There is no extra tab or window.

Code: [`usage.go`](../usage.go), [`internal/usage/`](../internal/usage/).
Automation: `kero usage --json` and `kero do usage.open`.

## CPU and memory

Two equal-width sidebar meters show CPU busy percentage and RAM used. They
open details in the usage modal. Bars are clamped to 0–100%, turn amber at
90%, and stay empty when a measurement is unavailable.

macOS Mach calls sample every two seconds in the background without shell
processes. CPU covers all cores; RAM includes active, wired and compressed
physical memory, excluding reclaimable cache. These are whole-Mac readings,
not Shibu process usage or memory pressure.

Code: [`resources.go`](../resources.go),
[`internal/resources/`](../internal/resources/).
Automation: `kero resources --json`.

## Terminal responsiveness and attachments

The local build targets the host architecture: Apple Silicon is `arm64`;
Intel/AMD is `amd64`. Continuous status animation was removed. The daemon's
synchronized-redraw scheduling now advances completed frames past an older
50ms retry, with a fake-clock regression test. The measured isolated transport
median improved from 55.93ms to 16.90ms (15 samples per build). GPU rendering,
OS input delivery and full typing latency were not established by that probe.

Finder files paste as quoted absolute paths without submitting a command.
Clipboard screenshots are saved as private PNG files and inserted as paths;
native file/image drops use the same attachment flow. Text paste retains
normal terminal behavior. The receiving agent must support local files.
Attachment files persist until removed; remote shells require a separate
upload workflow. Native paste/drop still needs an interactive user trial.

Code: [`attachments.go`](../attachments.go),
[`internal/daemon/session.go`](../internal/daemon/session.go),
[`internal/daemon/frame_test.go`](../internal/daemon/frame_test.go).
Probe: [`scripts/measure-terminal-latency.py`](../scripts/measure-terminal-latency.py).
See [Herdr comparison](herdr-porting.md) for architecture and future candidates;
those candidate ports are not implemented.

## Identity and icon

- App name: **Shibu Code** (previously **Kero Local**).
- Bundle identifier: `dev.ddeyalla.kero`.
- Icon source: [`resources/shibu-icon.png`](../resources/shibu-icon.png),
  supplied `Desktop/Shibu Code.png` golden retriever terminal artwork, packaged into native ICNS.
- CLI/protocol names stay `kero`; data identity stays `kero-local`.
- Upstream binary auto-updates are disabled; source changes can still be merged.

Code: [`mygo.json`](../mygo.json),
[`internal/proto/proto.go`](../internal/proto/proto.go),
[`internal/proto/local_test.go`](../internal/proto/local_test.go).

## Updating the personal edition

Shibu consumes stable Kero releases through its own signed build pipeline.
Use the sidebar's circular-arrow button to check Kero now; conflicts/failures
can open an isolated background Codex repair session, with review before release.
See [Automatic updates](automatic-updates.md) for credentials, controls, tests
and the fork-only publishing boundary. No automatic window or daemon restart
is enabled.

## Sessions and data continuity

The official and personal editions have separate daemons and data:

| Data | Original Kero | Shibu Code |
| --- | --- | --- |
| macOS application | `/Applications/Kero.app` | `~/Applications/Shibu Code.app` |
| App data | `~/Library/Application Support/kero` | `~/Library/Application Support/kero-local` |
| Settings | `~/.config/kero/config` | `~/.config/kero-local/config` |

`KERO_HOME`, `KERO_CONFIG` and `KERO_SOCKET` can override defaults; development
builds use `kero-local-dev`. Keep those overrides consistent and never point
two independent daemons at the same mutable state directory.

On 2026-10-20, the requested one-time import copied three sessions / five panes
into Shibu, retaining its existing session for four sessions / six panes total.
Shibu's shells restarted and copied Claude/Codex panes resumed. Original Kero's
shell PIDs remained unchanged. Running shell processes cannot be copied; saved
state/history and agent resume IDs were copied instead.

The pre-import backup is local at
`~/Library/Application Support/kero-local/backups/before-kero-import-20261020-194341`.
This is not an automatic importer or a promise of ongoing synchronization.
Session contents, credentials and backups do not belong in the repository.
