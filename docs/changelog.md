# Shibu Code changelog

This file records personal-fork changes. For original Kero releases, see the
[upstream changelog](../CHANGELOG.md). The app version is defined in
[`mygo.json`](../mygo.json); numbering follows [Versioning](versioning.md).
Dates use the local calendar date. Entries below describe installed builds;
they do not imply published GitHub releases or existing release tags.

## Unreleased

No pending changes.

## 2.0.5-local.5 — 2026-10-20

### Changed

- Refreshed the Mac app icon from the revised `Desktop/Shibu Code.png`.
- Retained the running GUI, daemon and terminal sessions during installation.

## 2.0.5-local.4 — 2026-10-20

### Changed

- Updated the app icon to the supplied `Desktop/Shibu Code.png` artwork,
  keeping its exact source image in `resources/shibu-icon.png`.

### Documentation

- Added a root documentation index, consolidated change details, build and
  rollback instructions, and a versioning/release maintenance checklist.
- The preceding documentation-only update retained `2.0.5-local.3`.

## 2.0.5-local.3 — 2026-10-20

### Changed

- Balanced the sidebar CPU and RAM readouts into two equal-width meters with
  proportional bars, numeric values and an amber state at 90% or higher.
- Replaced the temporary screenshot app icon with the supplied golden
  retriever terminal artwork.

### Local installation

- Copied three original Kero sessions / five panes into the separate Shibu
  data directory, preserving layouts, working directories, saved histories
  and agent resume IDs. Retained the existing Shibu session.
- Restarted Shibu's daemon and shells and resumed Claude/Codex panes;
  original Kero shell processes remained running. This was a one-time local
  operation, not a shipped migration command.

### Verified

- Full Go suite, MyGo vet, light/dark/compact UI renders, ARM64 build,
  strict bundle signature and installed icon match. See the
  [verification record](local-v1-verification.md).

## 2.0.5-local.2 — 2026-10-20

### Added

- Usage opens as a modal inside the current window, accessible through the
  sidebar, menu, command palette, CLI and Cmd+Shift+U.
- Modal shortcuts: Cmd+R refreshes; Esc or Cmd+W closes it.
- Native whole-machine CPU and RAM sampling, sidebar readouts, modal details
  and `kero resources --json`.

### Fixed

- Completed synchronized redraws can advance a pending 50ms retry to the
  usual 4ms coalescing deadline. The isolated transport probe improved from
  55.93ms to 16.90ms median; this is not a keyboard-to-display measurement.

### Branding follow-up at the same version

- Renamed Kero Local to Shibu Code with its first supplied icon. Preserved
  bundle ID `dev.ddeyalla.kero`, data identity `kero-local` and CLI `kero`.
  This historical rebuild reused local.2; future installed app changes must
  receive a new version.

## 2.0.5-local.1 — 2026-10-20

### Added

- First Kero Local build, with separate app identity/data and upstream binary
  updates disabled for the personal edition.
- Claude, Codex, OpenCode Go and DeepSeek usage integration, background
  refresh, stale/error states, rate-limit backoff and `kero usage --json`.
- Optional private provider-key storage; existing CLI credentials remain
  read-only.
- Clipboard/Finder attachment paths and native file/image drop handling.
- Native ARM64 local build script and removal of continuous status animation.

### Limits

- Native clipboard and drop behavior still needs an interactive trial in
  the installed app. Conversion/input tests pass.
- Provider entitlement and credentials determine available usage data;
  unsupported or unavailable responses are not shown as zero usage.
