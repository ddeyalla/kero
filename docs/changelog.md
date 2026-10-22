# Shibu Code changelog

This file records personal-fork changes. For original Kero releases, see the
[upstream changelog](../CHANGELOG.md). The app version is defined in
[`mygo.json`](../mygo.json); numbering follows [Versioning](versioning.md).
Dates use the local calendar date. Releases from 2.0.5-local.6 on are
published on the fork as `shibu-v<version>`; earlier entries describe local
builds only. What each feature is and where it stands is tracked in the
[feature tracker](features.md).

## Unreleased

### Added

- An **Attention** pane: every session that needs the user, in one list,
  the most urgent first — what it is doing or asking, the session and its
  project, the agent, the line that came with it, and how long it has been
  so. A row puts the keyboard in the pane the state came from, where the
  decision is, without zooming. Chips narrow it to **Needs you**,
  **Working**, **Unread** or **Done** and count each; a plain-text search
  finds a row by its words. A Codex wait whose message the agent did not
  give says **Focus pane to approve** rather than nothing. `Cmd+Alt+A`,
  the New Pane chooser's `A`, or `kero split --attention`. It reads the
  attention state kerod already sends, so it needs no protocol change, and
  it keeps the filter and search while the pane is open.
- New icons are SF Symbols, drawn by macOS through AppKit and cached as
  bitmaps, so they match the system at any size and in the light and the
  dark (`symbols_darwin.go`). The Attention pane's mark and its rows'
  states are the first. No cgo.
- kerod keeps one attention state for every session — running,
  waiting-user, unread, done or idle — folded from its panes: the most
  urgent of them, one line of what it says, the pane it came from and the
  age of it. The state the app receives carries it (`Session.Attention`),
  so the attention pane and the notifications of the later work read one
  truth. Where an agent says nothing, the daemon reads the end of its own
  transcript on disk, locally and only as a fallback: a call that has run
  45 seconds with no result is taken for one waiting on the user. Hooks
  and the screen keep their say over it, and an end that came while the
  app was away keeps its summary across a restart. Codex has no
  permission-prompt hook, so this scan is its fallback for approvals.
  **Restart the terminal service once** (when agents are idle) for the
  state to carry it; until then nothing changes for the app.
- Notifications now follow that attention: a session that comes to need you
  (`waiting-user`) or ends unseen (`unread`) tells once, a return to work
  tells nothing, and the same session and state inside five minutes is
  counted silently, so a chatty agent's loop does not fill Notification
  Center (`notify-throttle-minutes`). Every session kerod holds is watched,
  so a session outside the window or profile you look at still tells.
  Session › Silence Notifications (or a session's context menu) turns one
  session's notifications off until you resume it (`kero do
  session.silence`); the choice is saved (`silence-session`). Unseen ends
  that failed keep the error's title and `notify-on-error`.
- Accent presets: six hand-tuned themes of the app's own colors — Blue (the
  one it has had), Plum, Rose, Sea, Moss and Graphite. Each is an accent
  color in the light and in the dark, the background wash that comes with
  it, and the terminals' selection in its tint; the neutrals stay as they
  are. Settings › Appearance › Accent; picking one takes its wash, and the
  background stays yours after. The dark blue deepens (#6b93ff → #4a71d4)
  so that the white text on a primary button keeps a 4.5-to-1 contrast.
  `accent = plum` in the configuration sets the accent and the selection
  alone; the wash is `background`'s.
- A faint grain over the background wash, a speckle of a few percent that
  stops large flat gradients reading as flat, drawn under the sidebar and
  the cards and lighter where the desktop shows through. Settings ›
  Appearance › Grain; `grain = false` turns it off.
- The app moves as one: motion tokens hold its durations and curves in one
  place (`press` 90 ms, `hover` 120, `row` 160, `focus` 160, `slide` 260,
  and the two springs), in place of most of the scattered literals; a few
  stay where no token fits, as a switch's knob spring. A project
  collapsing now folds its rows away animated — each row folds to nothing
  along the list and its neighbours make room — instead of the rows popping
  in and out, and the chosen session's row stays in view, as it did. The
  rows keep up with the sidebar's divider as it drags.
- The pane the user is in now has a soft accent-tinted border when the
  session shows several panes (a pane alone or zoomed has none), and the
  panes around it are no longer dimmed or veiled: every pane keeps the
  same card and text, and only the focus edge moves.
- New panes open a hair small and fade in over 160 ms, and closing ones
  fade out the same way. A project's chevron and add button wait 80 ms
  after the pointer leaves before they go, so moving diagonally off the
  header does not flicker them.
- The Tasks and Linear empty states now say what to do: **Open Settings**
  for a missing or refused key or the integration turned off, **Refresh
  Linear** when Linear was not reached, **Clear Filters** when a filter you
  set hides everything, **Show Done** in a Tasks pane whose issues are all
  done, and **Refresh Linear** when nothing is filtered (a Tasks pane's
  linked project is not a filter). A workspace not read says why, and a
  turned-off integration says so. Error copy around Linear,
  the browser, files, saving the settings and installing the CLI says what
  went wrong and, where there is one, the next step.
- A first run shows a short reveal of the real shell: an accent wash and
  the mark from the window's first frame, with the shell hidden until its
  state is in, then the shell sliding 8 points and fading in, a capped row
  stagger, and the overlay gone by 900 ms. A state slower than 2 seconds,
  or kerod out of reach, skips it, and the shell fades in plainly. A key,
  click or scroll in the window cancels it to the end, and the key types
  nothing; window shortcuts such as ⌘1–9 still act during it, and a
  dialog opened during it, as the palette, ends it and keeps the
  keyboard. After it, the keyboard is the selected session's pane's. Reduce
  Motion turns it into a 150 ms fade with no movement; it is silent. Later
  launches do not show it.
- Claude Code and Codex report their turns: Kero's hook now also runs as a
  turn starts, asks for permission, runs a tool and ends, and the pane shows
  working, waiting or done at once instead of waiting for the screen to
  change. Esc and a granted permission are still read from the screen, so a
  pane no longer sticks on working or waiting. Run `kero skill setup` (or
  update from Settings) to add the new events; in Codex, approve them with
  `/hooks`. `agent-hooks = false` turns it off. **Restart the terminal
  service once** (when agents are idle) for the reports to reach the panes;
  until then the hooks do nothing. No protocol change.
- `agent-hooks` configuration key, `true` by default.
- Account usage in the sidebar is a ring for each harness this Mac has
  installed — Claude, Codex, OpenCode Go or DeepSeek — with the first
  allowance as its arc and amber once any window reaches 90%, its name
  beside it where the sidebar has room and rows of rings where it does not.
  A balance shows its currency's sign, a harness not connected a dashed
  ring, and a ring whose refresh failed an amber dot. Hovering one makes its
  name stand out and shows the plan, every allowance window with the
  percentage and its reset, a balance, why it is not connected, or "Last
  known usage; refresh failed"; clicking it, or `Cmd+Shift+U`, opens the
  full modal as before. The CPU and RAM meters stay below the bar.
- Session states read at a glance: a working session turns a loading ring
  (or fills it when its program reports progress), one that needs you shows
  an amber ring with a dot, one done and unseen a filled check, one done and
  read a faint check, a failed one a red disc, one whose shell exited with
  an error a crossed ring, red until read, and an inactive one — done, read
  and unchanged for 24 hours — a half moon with its name dimmed.
- The app's own busy states use the same loading.dev ring from MyGo's
  loading plugin — connecting, a pane attaching, Files reading, Linear
  reading — instead of the generic spinner. It follows Reduce Motion and
  turns only while it shows, at 15 frames a second.

### Fixed

- The Browser pane's **Open in Default Browser** button and a Markdown
  link in the Files pane open the page again. The app handed the address
  to the system as a file's path, so the page never opened.
- A restart of the terminal service no longer loses the sessions' marks:
  what was unread stays unread, the ages keep their times, and a failure
  stays failed, as does a shell that crashed unseen ("Exited with code N").
  A pane that was working or waiting comes back idle and unread, as of the
  restart, as its program is gone. The state file gains optional fields; older ones load as
  before. **Restart the terminal service once** (when agents are idle) to
  run this version: that first restart still loads the old file, without
  the marks, and they survive every restart after it. No protocol change.
- The usage bar finds Claude Code, Codex and OpenCode when the app is
  opened from the Dock or the Finder, whose `PATH` lacks the installers'
  folders, such as `~/.local/bin`.
- A weekly allowance resetting six or more days out says its date ("resets
  Oct 28"), not a weekday that reads as today's.
- Plan names show capitalized ("Max", "Plus") and the weekly window as
  "Week" in the usage card.
- The loading ring, on a working session or a busy view, no longer paints
  the window at the display's rate for as long as it shows: it turns at 15
  frames a second.
- Every session mark has its accessible label, a read one too, and the
  Files view's loading rings are named.

- The title bar is lower: macOS's compact title bar and toolbar, 40 points
  (it was 60), with the traffic lights where macOS puts them in it and the
  sidebar's buttons centered on them. The panes' headers keep their own
  height, 36, for their titles and buttons. With the sidebar hidden, the
  first header comes up so that its title centers on the traffic lights,
  as the sidebar's buttons do, instead of sitting 6 points below them.
- The sidebar's first row is no longer crowded under the shorter bar: it
  starts 30 points below the traffic lights' line, as it did under the
  taller bar, and the rows line up with the close light's left edge.
- In full screen the traffic lights stay in sight, where they are in the
  window. They are macOS's own buttons, with their hover glyphs: close
  closes the window, the green light leaves full screen, and minimize is
  greyed, as macOS has it in full screen. The bar that slides down over
  the top of the window with the menu bar still comes, but empty, without
  a second set of lights or its background, and clicks go through it.
  The sidebar's buttons no longer move going in or out of full screen.
- The traffic lights follow the window's actual state whenever it resizes
  or comes to the front, so that a missed full-screen change can no
  longer leave them hidden in the window, as reported once when hiding
  the sidebar.
- Settings opens over the main window as a modal, a card on a dimmed
  backdrop, instead of in a window of its own. `Cmd+,` opens and closes it;
  Esc, a click beside the card and `Cmd+W` close it. In a small window the
  sidebar shows its pages' icons and the search moves into the page's header.

### Documentation

- `scripts/dev-env.sh` puts the bundled Go on the shell's path from any
  worktree, sharing one toolchain and one warm build cache, and
  `scripts/check.sh` runs the commit checks, or the tests of chosen packages,
  without reaching the daily app. See [Build](build.md).
- Added the [feature tracker](features.md): every Shibu feature, the release
  that shipped it, how to reach it and its status.
- Noted the one-time service restart for 2.0.7-local.1 and the Files Find
  and Markdown heading links in its release notes.
- Added test-daemon isolation and socket recovery guidance to
  [Automatic updates](automatic-updates.md) and the socket guard's
  verification record.
- Rewrote `AGENTS.md` and added `CLAUDE.md`: branches, checks, docs with
  every change, releases, upstream merges, the daily app and private files.
- Set the product direction in `AGENTS.md`: everything is a pane or a plugin,
  and plans that work against it get pushed back on.

## [2.0.7-local.1] - 2026-10-20

- Includes Kero 2.0.7 (`7b60cc3f5090`) and Shibu customizations.
- Preserves the Shibu app identity, icon, usage features and separate data folders.
- Signed background updates apply on the next launch; no automatic restart.
- After updating, restart the terminal service once when agents are idle.
  Sessions, layouts, scrollback, views and task links come back and Claude
  Code and Codex resume; running dev servers must be started again. Linear,
  task links and the Tasks, Files and Browser panes need the new service.

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
- The Files pane's Find box (`Cmd+F` in the pane) matches any file of the
  project loosely, indexed off the main thread; the tree and the viewer share
  one header line.
- Markdown links to a heading (`#section`, `other.md#section`) scroll the
  Files pane to it.
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
[secret protection](security.md).

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
