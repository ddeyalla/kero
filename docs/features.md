# Shibu Code feature tracker

Every feature Shibu Code adds to Kero, the release that shipped it, how to
reach it and where it stands. For what changed in each release see the
[Changelog](changelog.md); for how a feature works and where its code lives
see [Changes and implementation](changes.md).

Status:

- **Shipped**: released and tested.
- **Untried**: released and covered by tests, but not yet tried by hand in
  the installed app.

Current release: **2.0.7-local.1** (Kero 2.0.7), 2026-10-20.

> Updating to 2.0.7-local.1 needs one restart of the terminal service
> (daemon protocol 3), when agents are idle. Until then, Linear, task links
> and the Tasks, Files and Browser panes don't work; everything else does.

## Linear and tasks

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Linear workspace: board or list, search, project/milestone/state filters, issue detail with relations, attachments and comments | Sidebar **Linear** row, `Cmd+Shift+L` | 2.0.7-local.1 | Shipped |
| Linear API key in the personal data directory (`0600`), or `LINEAR_API_KEY` | Settings › Agents, `kero linear auth` | 2.0.7-local.1 | Shipped |
| Daemon keeps the workspace fresh (30 s changes, 5 min full read, last good read kept offline) | Automatic | 2.0.7-local.1 | Shipped |
| Session linked to its task, kept across restarts; issue shows the session and its agent resume id | `kero link <session> <BRU-25>`, issue detail | 2.0.7-local.1 | Shipped |
| Start a task: link the session, move the issue to In Progress, print its branch | `kero task start <BRU-25>` | 2.0.7-local.1 | Shipped |
| Project mapped to its Linear project (the one named like the repository offered first) | The project's menu in the sidebar, or the Tasks pane toolbar | 2.0.7-local.1 | Shipped |
| Linear from the CLI | `kero linear status\|board\|issue\|set-state\|comment` | 2.0.7-local.1 | Shipped |
| Linear and Tasks empty states act: Open Settings for the key or the integration turned off, Refresh Linear when it was not reached or has no issues, Clear Filters only for a filter the user set | The empty state's button | Unreleased | Untried |

## Panes

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| New Pane chooser: Terminal, Tasks, Files, Browser, Attention | `Cmd+Alt+N` or the pane header's **+**, then `T`/`L`/`E`/`B`/`A` | 2.0.7-local.1 | Shipped |
| Open the new pane below instead of beside | Hold Shift in the chooser | 2.0.7-local.1 | Shipped |
| Open a kind of pane directly | `Cmd+Alt+T` / `L` / `E` / `B` (and `A`) | 2.0.7-local.1 | Shipped |
| Turn a pane into another kind | Pane menu › **Show in This Pane** | 2.0.7-local.1 | Shipped |
| Attention pane: every session that needs you, one list, most urgent first, with chips (All, Needs you, Working, Unread, Done), counts and a plain-text search; a row opens the pane that needs you | `Cmd+Alt+A`, the chooser's `A`, or `kero split --attention` | Unreleased | Untried |
| New icons are SF Symbols, drawn by macOS and cached as bitmaps | Automatic; the Attention pane's mark and its rows' states | Unreleased | Untried |
| Agents open views beside themselves | `kero split --browser 5173`, `--files --open docs/plan.md`, `--tasks`, `--attention` | 2.0.7-local.1 | Shipped |
| A view's page or file is kept across restarts | Automatic | 2.0.7-local.1 | Shipped |
| Least pane size (280×150); a split without room is refused | Automatic | 2.0.7-local.1 | Shipped |
| Make panes even | Double-click a divider, `Cmd+Ctrl+=` | 2.0.7-local.1 | Shipped |
| Panes glide in and out; terminal sizes wait for the glide to end | Automatic | 2.0.7-local.1 | Shipped |
| The focused pane has a soft accent border among several panes (none alone or zoomed); unfocused panes keep the same card and text | Automatic; click or cycle panes | Unreleased | Untried |
| Panes open small and fade in; closing panes fade out the same way over 160 ms | Automatic | Unreleased | Untried |

### Files pane

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Folder tree with git changes, refreshed every few seconds | Files pane | 2.0.7-local.1 | Shipped |
| Markdown, code and images shown beside the tree | Click a file | 2.0.7-local.1 | Shipped |
| Markdown links to headings (`#section`) and other files open in the pane | Click the link | 2.0.7-local.1 | Shipped |
| Fuzzy Find across the project, indexed off the main thread | `Cmd+F` in the pane, arrows or `Ctrl+N`/`Ctrl+P`, Enter | 2.0.7-local.1 | Shipped |
| Drag a row into a terminal to type its path | Drag and drop | 2.0.7-local.1 | Shipped |
| Unfolded folders kept across restarts | Automatic | 2.0.7-local.1 | Shipped |

### Browser pane

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| A web page in the system web view, most often the dev server | Browser pane, `kero split --browser 5173` | 2.0.7-local.1 | Shipped |
| Select the address | `Cmd+L` | 2.0.7-local.1 | Shipped |
| Zoom the page (Safari's steps); the zoom chip resets it | `Cmd+=`, `Cmd+-`, `Cmd+0` | 2.0.7-local.1 | Shipped |
| Reload the page (instead of renaming the session) | `Cmd+R` | 2.0.7-local.1 | Shipped |

## Agents

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude Code and Codex report working, waiting, done and error from their hooks; Esc and granted permissions still read from the screen | `kero skill setup`; `agent-hooks` in the configuration | Unreleased | Untried |
| Session status icons: a turning ring while working (a filling one when the program reports progress), an amber ring with a dot when it needs you, a filled check when done and unseen, a faint check once read, a red disc when a program fails, a crossed ring when the shell exits with an error, a half moon when read and idle for 24 hours; kept across a terminal service restart | Sidebar rows and the footer's count chips | Unreleased | Untried |
| Each session carries one attention state — running, waiting-user, unread, done or idle — with one line of what it says, the pane it comes from and its age; a transcript scan fills in where an agent reports nothing (Codex's approvals) | Automatic: it rides on the state kerod sends | Unreleased | Untried |
| Notifications from that attention: a session that needs you or ends unseen tells once, a turn that begins again clears it, and the same session and state inside `notify-throttle-minutes` is counted silently; every session kerod holds tells, in any profile or window | Automatic; `notify-on-waiting`, `notify-on-done`, `notify-on-error` and `notify-throttle-minutes` in the configuration, Settings › Notifications | Unreleased | Untried |
| Silence one session's notifications, remembered across restarts | Session › Silence Notifications, the session's context menu, `kero do session.silence`; `silence-session` in the configuration | Unreleased | Untried |

## Usage and the machine

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude, Codex, OpenCode Go and DeepSeek usage, refreshed in the background | Sidebar, `kero usage --json` | 2.0.5-local.1 | Shipped |
| Usage as a modal in the window | `Cmd+Shift+U`; `Cmd+R` refreshes, Esc closes | 2.0.5-local.2 | Shipped |
| Optional private provider keys | Usage modal | 2.0.5-local.1 | Shipped |
| Sidebar usage as a ring per installed harness (found on `PATH` or in the installers' folders), a currency sign for a balance, a dashed ring when not connected, an amber dot when a refresh failed; hovering highlights the harness and shows the plan, every window, its percentage and reset time, a balance or why it is not connected | Hover a ring; click it or press `Cmd+Shift+U` for the modal | Unreleased | Untried |
| CPU and RAM meters for the whole Mac | Sidebar, `kero resources --json` | 2.0.5-local.2 | Shipped |
| Idle window paints less: CPU and RAM meters redraw only when what they show changes; times ago tick once a minute, only while shown | Automatic | Unreleased | Untried |
| The loading.dev ring wherever the app itself is busy: connecting, attaching a pane, reading Files or Linear; it and a working session's ring turn at 15 frames a second | Automatic | Unreleased | Untried |

## Settings

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Settings as a modal over the main window, not a separate window | `Cmd+,` toggles it, or the sidebar's gear; Esc, a click beside it or `Cmd+W` closes it | Unreleased | Untried |
| Accent presets: Blue, Plum, Rose, Sea, Moss, Graphite — an accent color, the wash that comes with it, the terminals' selection | Settings › Appearance › Accent (with its wash); `accent = <preset>` in the config (the accent alone) | Unreleased | Untried |
| Grain over the background wash | Settings › Appearance › Grain; `grain = false` off | Unreleased | Untried |

## Terminal

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Finder files and clipboard screenshots paste as paths | `Cmd+V` | 2.0.5-local.1 | Untried |
| Files and images dropped on a terminal insert their paths | Drag and drop | 2.0.5-local.1 | Untried |
| Faster redraws after a synchronized frame (55.9 ms → 16.9 ms transport median) | Automatic | 2.0.5-local.2 | Shipped |
| Native ARM64 build, no constant status animation | Automatic | 2.0.5-local.1 | Shipped |

## Window

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Compact title bar (40 pt): the traffic lights where macOS puts them, the sidebar's buttons centered on them | Automatic | Unreleased | Untried |
| With the sidebar hidden, the first header's title centered on the traffic lights; the sidebar's first row 30 pt below them, aligned with the close light | Automatic; `Cmd+B` hides the sidebar | Unreleased | Untried |
| macOS's own traffic lights in sight in full screen, where they are in the window, with their hover glyphs: close, minimize (greyed, as macOS has it) and exit full screen; the bar that slides down with the menu bar is empty and lets clicks through | `Ctrl+Cmd+F`, or the green light | Unreleased | Untried |
| The app moves as one (motion tokens, a few literals left); a project collapsing folds its rows away animated, the chosen row never leaving | Automatic; collapse by clicking a project header | Unreleased | Untried |
| A project's hover chevron and add button wait 80 ms after the pointer leaves | Automatic; hover a project header | Unreleased | Untried |
| First run: a ~900 ms reveal of the real shell, covering the window from its first frame, skipped after 2 s or on a connection error, cancellable, Reduce Motion aware, silent, shown once | Automatic, on the first launch | Unreleased | Untried |

## Updates and releases

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Daily check for stable Kero releases, merged, tested and signed | Automatic (GitHub Actions on the fork) | 2.0.5-local.6 | Shipped |
| Signed background updates that apply on the next launch | Settings › General › Updates | 2.0.5-local.6 | Shipped |
| Check Kero now | Sidebar update button, palette, `kero do updates.sync` | 2.0.5-local.7 | Shipped |
| Codex repair session in its own worktree when an update build fails | Automatic, review before release | 2.0.5-local.7 | Shipped |

## Identity and safety

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Shibu Code name and icon, separate app and data folders from Kero | `~/Applications/Shibu Code.app`, `kero-local` data | 2.0.5-local.2 | Shipped |
| Gitleaks commit and push guards, fork-only push allowlist, secret-scan CI | `.githooks`, GitHub Actions | 2.0.7-local.1 | Shipped |
| Test daemons refuse to take over a live socket | Automatic | 2.0.7-local.1 | Shipped |

## Not done yet

- Native paste and drop of attachments still need a hand trial in the
  installed app.
- The Home screen and composer are separate work, not started in a release.
- Herdr ports are candidates only; see [Herdr comparison](herdr-porting.md).
- Dev servers do not survive a terminal service restart; restart them
  yourself.

When a feature ships, add its row here with the release, and its entry in the
[Changelog](changelog.md).
