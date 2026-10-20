# Shibu Code changelog

This file records personal-fork changes. For original Kero releases, see the
[upstream changelog](../CHANGELOG.md). The app version is defined in
[`mygo.json`](../mygo.json); numbering follows [Versioning](versioning.md).
Dates use the local calendar date. Entries below describe installed builds;
they do not imply published GitHub releases or existing release tags.

## Unreleased

### Upstream

- Merged Kero 2.0.6 and 2.0.7 by hand: program logos in pane titles, image
  previews, pixel mouse reporting, theme colors for programs and agent-named
  sessions, with Shibu's views, task runs and attachment drops kept. The
  daemon protocol is now 3, so the app asks to restart the terminal service
  once after updating; saved sessions keep their format.

### Documentation

- Added the fork-only source-save guide, separating local commits, GitHub pushes,
  signed app releases and private Mac session/configuration data.
- Recorded the authorized session recovery and remaining socket-guard integration.
- Clarified workflow-owned version increments and retained unfinished Linear work
  outside these documentation commits.

### Added

- A Linear workspace in the app: the issues of the team as a board or a
  list, with a search and filters for the project, the milestone and the
  state, and the issue open beside them with its description, its
  relations, its attachments and its comments.
- A session linked to the task it works on: `kero link <session> <BRU-25>`,
  the link kept across restarts, the issue showing the session's name, and
  its detail offering the agent's resume id to copy and the session to open.
- The daemon reads the Linear workspace and keeps it fresh, so the app, the
  CLI and the agents see the same one; a request is bounded and a workspace
  that cannot be read is shown as such, not as an absence.
- A Linear personal API key in Settings › Agents, kept at mode `0600` in
  the personal data directory, and read from `LINEAR_API_KEY` when set.
- `kero linear status|board|issue|set-state|comment|auth` and `kero link`.
- `kero task start`, and a project mapped to its Linear project, so a
  session started from a task is linked to it.
- Panes that show anything: a terminal, **Tasks** (the project's issues),
  **Files** (tree, Markdown, code, images, a fuzzy Find box) or **Browser**
  (the dev server in the system web view). A New Pane chooser
  (`Cmd+Alt+N`, Shift for below), `Cmd+Alt+T/L/E/B`, Show in This Pane, and
  `kero split --browser/--files/--tasks` for agents.
- Files rows drag into a terminal as their paths; unfolded folders and the
  page or file a view shows are kept across restarts.
- In a focused Browser pane, `Cmd+L` selects the address, `Cmd+=`/`-`/`0`
  zoom the page and `Cmd+R` reloads it.
- A least pane size, double-click a divider (or `Cmd+Ctrl+=`) to make panes
  even, and panes that glide in and out, with terminal sizes held until the
  glide ends.
- The `linear.open` and `linear.refresh` actions, and the `Cmd+Shift+L`
  shortcut for the workspace.
- Local Gitleaks commit/push guards, a fork-only push destination allowlist,
  GitHub secret-scan CI and ordinary feature-branch test CI.

### Fixed

- Panes split downward no longer share their layout state with a stray
  element, which warned and could misplace their divider.
- Paginated Linear reads, dependency decoding, fractional state ordering,
  offline cache preservation, serialized refreshes and rate-limit backoff.
- Linear navigation survives daemon broadcasts; stale detail responses are
  rejected and failed reads show Retry. Existing sessions can be linked and
  unlinked directly from issue detail without writing to Linear.
- API keys no longer pass through CLI arguments; credential inputs clear,
  removal is supported, and remote response bodies/redirects cannot echo or
  forward the key. Private and malformed credential stores are preserved.
- Test daemons refuse to replace an existing live daemon socket, including
  when a different data directory inherits the same socket address.

Scope and verification: [Linear v1](linear-workspace.md),
[secret protection](security.md). This is unreleased feature-branch work;
the installed daily app remains on its current release.

## [2.0.5-local.7] - 2026-10-20

- Includes Kero 2.0.5 (`a920e3a0cc25`) and Shibu customizations.
- Preserves the Shibu app identity, icon, usage features and separate data folders.
- Signed background updates apply on the next launch; no automatic restart.

### Added

- Manual sidebar update control, command-palette action and `kero do updates.sync`.
- Existing fork-owner GitHub CLI authentication and guarded workflow monitoring.
- Background Codex repair sessions in isolated worktrees after failed update builds;
  repair commits require review before release and never replace the daily checkout.
- Tests for account/host isolation, credential error redaction, cancellation,
  upstream-origin rejection, retained active selection and dirty-worktree preservation.

## [2.0.5-local.6] - 2026-10-20

- Includes Kero 2.0.5 (`a920e3a0cc25`) and Shibu customizations.
- Preserves the Shibu app identity, icon, usage features and separate data folders.
- Signed background updates apply on the next launch; no automatic restart.

### Added

- Daily stable Kero release checks with guarded merges, tests and signed ARM64 Shibu builds.
- Shibu-only background update checks/downloads, applying on the next launch without automatic restarts.
- Real updater probes for signature rejection and installation into temporary app bundles.
- Replaced the inherited upstream release workflow with the personal Shibu pipeline.
- Retained the separately merged upstream session-naming change (`ad99d14`); reviewed its additive JSON fields and unchanged protocol/schema versions.

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
