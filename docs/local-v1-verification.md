# Local v1 verification

Verified on this Apple Silicon Mac, 2026-10-20, against upstream `a920e3a`
(Kero 2.0.5).

## Checks

- `CGO_ENABLED=0 go test ./...`: passed with the macOS system directories
  ahead of Homebrew on PATH. The original environment reproducibly fails
  two existing Homebrew-zsh subprocess tests before and after this change;
  the system-zsh run passes both.
- `go test -race ./internal/usage`: passed.
- `go tool mygo vet .` and `git diff --check`: passed.
- Native `darwin/arm64` build, ad hoc signing, installation in
  `~/Applications/Kero Local.app`, and launch: passed.
- Installed executable's `usage` command: live Claude, Codex and OpenCode Go
  allowance windows and reset times returned. DeepSeek has no configured
  direct API key and correctly reports not connected.
- Restarting the local GUI retained its daemon session and shell PID.
  The original Kero continued running.
- UI test renderer: light/dark sidebar, usage detail cards, stale/error states,
  compact window, and masked API-key editing checked. PNG screenshots were
  inspected locally under `/private/tmp/kero-local-shots`.
- Attachment checks: Finder path quoting, saved PNG integrity, private file
  permissions, normal-text rejection, and invalid PNG rejection passed.
- Provider fixtures cover both Claude response formats, Codex variable window
  durations, OpenCode legacy/current field names, DeepSeek currencies, invalid
  numeric data, authentication errors, 429 backoff and retained stale values.

## Review

Reviewed the current changes inline, following the repository instruction to
run subagent work in the main thread. Correctness, credential handling,
background work, failure states, UI lifetime, maintainability and test value
were checked. No independent reviewer or external peer ran.

The review caught and resolved the API-key field lifetime issue and OpenCode's
current response shape. No remaining blocker was found for this local preview.

## Verification limits

Computer Use did not have permission to inspect or operate the new application.
Native clipboard shortcuts and drag/drop therefore still need a user trial
inside Claude Code, Codex and OpenCode. The attachment conversion and input
binding tests pass, but do not establish end-to-end native event delivery.
Performance checks establish a native ARM64 process and removal of continuous
status animation, not a controlled before/after latency benchmark. New usage
strings are English in this first local version.

## Local.2 follow-up

Usage now renders in a `DialogBase` in the main window, with keyboard and
outside-click dismissal. Cmd+Shift+U opens it, Cmd+R refreshes while open, and
Esc/Cmd+W closes it without closing the underlying pane. The action is in the
menu, command palette and CLI. UI tests exercise open, dismiss, reopen, key
editing, and avoiding session rename/close actions behind the modal.

Native CPU/memory sampling and its CLI command were verified on this Mac.
The strip sits below account usage in the sidebar; its details are in the modal.
New monitoring tests and race checks pass, as do the existing full suite and
MyGo vet. Readings are machine-wide and do not claim memory-pressure or
per-process attribution.

The synchronized-redraw scheduler test failed before the fix and passes after
it. A live isolated transport probe measured median 55.93ms before and 16.90ms
after, with 15 samples per build. See [Herdr porting](herdr-porting.md) for the
workload, limits, sources, and reproduction command. This does not establish
keyboard-to-display latency or guarantee that all reported lag is resolved.

Kero Local 2.0.5-local.2 was built and installed as ARM64. Its daemon started
from the installed app, restored its saved idle session, and accepted
`kero do usage.open`. The original Kero was not replaced by this pass.

# Shibu Code branding verification — 2026-10-20

The personal app is installed at `~/Applications/Shibu Code.app`, with
display name Shibu Code, bundle identifier `dev.ddeyalla.kero`, and the supplied
terminal screenshot padded to a 1024px square as its icon. The installed binary
is ARM64; strict code-signature verification passed. The embedded icon matches
the built bundle. The version remains `2.0.5-local.2` for this branding change.

The data identity remains `kero-local`; a regression test covers continuity
from Kero Local to Shibu Code. All Go tests and MyGo vet passed. The existing
session/pane IDs, settings location, and provider credential file persisted.
Reopening the GUI also restarted the daemon and idle shell, so process IDs did
not persist. The local `bin/kero` link now targets Shibu Code. The prior bundle
was moved into a temporary rollback folder; the official app was untouched.

## Sidebar meters and copied sessions — 2026-10-20

Shibu Code `2.0.5-local.3` is installed and running as ARM64. CPU and RAM
use equal-width sidebar meters with values and proportional bars. Light,
dark and compact layouts were rendered with MyGo Tester; all Go tests and
MyGo vet passed. The supplied Golden Retriever Terminal Icon.png replaces
the earlier screenshot icon; installed and built ICNS hashes match, and
strict signature verification passed.

At the user's request, three Kero sessions / five panes were copied into
the separate Shibu data directory, retaining projects, working directories,
layouts, pane IDs, saved histories, and agent resume IDs. The current Codex
resume ID was verified against its local session metadata because Kero had
not recorded it. The existing Shibu session remains: four sessions / six
panes total. Shibu's daemon and shells restarted after the build; the
copied agent panes report Claude/Codex processes. Original Kero daemon and
shell PIDs were verified unchanged.

Pre-import state, preferences and histories are backed up under
`~/Library/Application Support/kero-local/backups/before-kero-import-20261020-194341`.
Provider keys were left in place. Session contents and backups are local
only, outside the repository.

## Desktop icon update — 2026-10-20

Shibu Code `2.0.5-local.4` was built for ARM64 and installed using the exact
`Desktop/Shibu Code.png` source. The source PNG matches the repository asset;
built and installed ICNS files match. Strict bundle signature verification
passed. Launch Services was refreshed and the Dock restarted to refresh icons.

The installed bundle was replaced while retaining the running GUI and daemon
(PIDs 95310 / 95322); neither was restarted for this asset change. Their runtime
version remains local.3 until a future normal restart. Original Kero's daemon
PID 41840 was unchanged. The prior bundle is backed up in the personal app's
`backups/app-20261020-195940/` directory. No session state was migrated or reset.

This asset-only release was verified by building, checking the packaged image
and signature, and checking process continuity; the Go suite was not rerun.

## Revised Desktop icon — 2026-10-20

Installed ARM64 `2.0.5-local.5` using the revised `Desktop/Shibu Code.png`.
Source/repository PNG equality, packaged/installed ICNS equality and strict
bundle signature verification passed. Launch Services and Dock were refreshed.
Running GUI/daemon PIDs 95310/95322 were retained; their runtime version remains
unchanged until a normal restart. No shell restart or state migration occurred.
Previous bundle backup: `app-20261020-200116/Shibu Code.app` in the personal data
backup directory. This asset-only update was build-verified; tests were not rerun.

## Signed update pipeline — 2026-10-20

Published and installed `2.0.5-local.6` from the fork's signed GitHub release.
The public-feed probe rejected an incorrect signing key without replacing its
app, then downloaded/installed the correct release into a temporary app bundle.
The archive signature, installed identity and strict native bundle signature
were checked. Existing settings/provider-key fingerprints, GUI/daemon process
IDs and all six observed shell PIDs were retained during bundle replacement.
Three current sessions remained available. No active process was restarted.

Hosted checks passed:

- [No-change upstream check](https://github.com/ddeyalla/kero/actions/runs/39443674445).
- [Non-publishing build trial](https://github.com/ddeyalla/kero/actions/runs/39443854019).
- [Signed release publication](https://github.com/ddeyalla/kero/actions/runs/39444838453).

Automatic checks/downloads are enabled in this Mac's updater preferences. The
running older GUI does not gain updater code until the next normal launch.

A separate accessory app with no coding window exercised MyGo's native
`App.Relaunch` against a disposable Kero daemon and shell. The GUI process
changed and daemon/shell PIDs stayed identical; the test daemon was then stopped.
This does not establish full-window focus restoration or guarantee survival
across arbitrary protocol/data changes. Automatic GUI relaunch remains off.

## Manual update control and repair isolation — 2026-10-20

`2.0.5-local.7` passed the [hosted release pipeline](https://github.com/ddeyalla/kero/actions/runs/39447980816)
and was installed from its signed public archive. The actual updater probe
again rejected a wrong key and installed the correct release into a temporary
bundle. ARM64, bundle identity and strict native signature verification passed.
The sidebar control was rendered and inspected with MyGo Tester.

A [live manual workflow trigger](https://github.com/ddeyalla/kero/actions/runs/39447195716)
passed through the new maintenance client. A separate real-fork clone/worktree
test retained dirty source files and the source branch while preparing a repair
branch. Unit tests cover fixed account/host/destination, secret error redaction,
monitor cancellation, upstream-origin rejection and prompt boundaries. Go tests,
MyGo vet and maintenance race checks passed in an isolated checkout; unrelated
in-progress code in the shared checkout was excluded. No paid Codex repair was
started for the test; interactive CLI trust/approval remains a first-use check.

The installed bundle is local.7, while the pre-existing GUI/daemon remain running
without a forced relaunch. Settings/provider-key fingerprints are unchanged and
the original six shell processes remain alive.

The final RPC session comparison was blocked by a separate local test daemon:
`/tmp/kerobin/kero` used `KERO_HOME=/tmp/kerotest` while inheriting the personal
`KERO_SOCKET`, replacing the live socket pathname. The original GUI, daemon and
shells remained alive on existing connections. No daemon was stopped to repair
this; the user was asked to pause the other test task before a safe recovery.
The existing saved state/history was backed up again. Do not interpret this
record as successful reconnect validation or an enabled automatic window handoff.

Tests must isolate or unset **all** of KERO_HOME, KERO_CONFIG, KERO_SOCKET,
KERO_PANE and KERO_SESSION. Isolating KERO_HOME alone is insufficient.
