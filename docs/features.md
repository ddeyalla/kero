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

The next release (Unreleased) needs one more restart of the terminal service:
it changes the daemon protocol and saved state (Tasks panes become Linear
plugin panes), and an older build cannot open the migrated panes.

## Linear and tasks

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Linear plugin: list or board in the sections Current, Done, Todo and Backlog with nested folds; Current follows the focused session; filters for assignee, project, label, priority, search and canceled; full issue detail (Markdown, properties, status picker, runs with Resume, sub-issues, relations, activity) | The sidebar pin, the **Tasks** entry of the New Pane chooser (`Cmd+Alt+L`), `kero split --tasks` | Unreleased | Untried |
| Sidebar pins: a plugin widget has a row in the sidebar; a click focuses its pane in the selected session or splits one in | Pane header menu, Settings › Plugins, command palette, `kero plugin pin\|unpin <plugin> [widget]` | Unreleased | Untried |
| Linear plugin keyboard and menu: sticky search and filter header; J/K or arrows move, Enter opens, Escape goes back (only while the pane has the focus); right-click menu with Open, Open in Linear, Copy ID, Copy Link, Move To (shown at once) and Start task; label colours on cards; Canceled toggle; screen-reader labels for status and priority | Click in the Linear pane, then keys; right click an issue | Unreleased | Untried |
| Plugin permission `clipboard`: write-only, up to 64 KB, accepted only within about 1 s of a user event the host delivered; otherwise refused and logged | Manifest `permissions`; `Conn.Copy` in the SDK | Unreleased | Untried |
| The old built-in Linear screen (sidebar row, `Cmd+Shift+L`) and Tasks pane | Replaced by the Linear plugin | 2.0.7-local.1 | Removed in Unreleased |
| Linear API key in the personal data directory (`0600`), or `LINEAR_API_KEY` | Settings › Agents, `kero linear auth` | 2.0.7-local.1 | Shipped |
| Daemon keeps the workspace fresh (30 s changes, 5 min full read, last good read kept offline) | Automatic | 2.0.7-local.1 | Shipped |
| Daemon idle work cut: foreground program read as it changes, directory 1.5 s only for the focused pane, branch scan 30 s plus focus and a HEAD check every 1.6 s, `state.json` at most every 2 s, Linear in its own `linear.json`, written per Linear read, not per status change | Automatic | Unreleased | Untried |
| Session linked to its task, kept across restarts; issue shows the session and its agent resume id | `kero link <session> <BRU-25>`, issue detail | 2.0.7-local.1 | Shipped |
| Start a task: link the session, move the issue to In Progress, print its branch | `kero task start <BRU-25>` | 2.0.7-local.1 | Shipped |
| Project mapped to its Linear project (the one named like the repository offered first) | The project's menu in the sidebar | 2.0.7-local.1 | Shipped |
| Linear from the CLI | `kero linear status\|board\|issue\|set-state\|comment` | 2.0.7-local.1 | Shipped |
| The Linear plugin's empty states act: Refresh Linear when it was not reached or has no issues, Clear Filters when a filter hides them | The empty state's button | Unreleased | Untried |

## Panes

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| New Pane chooser: Terminal, Tasks (the Linear plugin), Files, Browser, Attention | `Cmd+Alt+N` or the pane header's **+**, then `T`/`L`/`E`/`B`/`A` | 2.0.7-local.1 | Shipped |
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
| Plugin panes: a plugin's widget in a pane, with plugin, widget and state kept in the pane target across a restart | New Pane chooser, palette, `Cmd+Alt+P`, `kero split --plugin <id> [--widget <name>]` | Unreleased | Untried |
| The plugin platform: manifest, JSON-RPC process host, one process per plugin, permission checks, crash with **Restart** | Automatic when a plugin pane opens | Unreleased | Untried |
| Plugin parts: rows, columns, text, images, buttons, sliders, progress, lists, a search field, an empty state, spacers and dividers, in the app's palette and motion | A plugin's widget pane | Unreleased | Untried |
| Plugin settings and secrets: Settings › Plugins lists plugins, versions, permissions and fields; secrets in a 0600 file | Settings › Plugins | Unreleased | Untried |
| Plugin commands: each manifest command in the palette and as a `kero do` action; runs on a pane of its plugin, opening one if needed | Command palette, `kero do plugin.<id>.<command>`, `kero actions` | Unreleased | Untried |
| Marquee text only when it overflows; static ellipsis with Reduce Motion | `marquee` on a text part | Unreleased | Untried |
| Image tint: a soft gradient of the picture's own color behind the pane, crossfaded and contrast clamped | `tint` on an image part | Unreleased | Untried |
| A plugin's stderr log, rolled at 1 MB, and a failed plugin's error with a **Restart** button | Automatic | Unreleased | Untried |
| An old terminal service without the plugin view shows one line asking to restart it | Automatic | Unreleased | Untried |
| Spotify widget: plays in the app with no Spotify app open (Premium), through Spotify's Web Playback SDK; engine starts on the first play and closes after 10 idle minutes; sign in again for the new scopes | New Pane chooser › Spotify, palette, `kero split --plugin spotify`; Client ID in Settings › Plugins | Unreleased | Untried |
| Spotify compact player: two rows from 440 points wide (art, title, controls; seek, time, mute, volume), three rows narrower; shown once music plays | Spotify pane | Unreleased | Untried |
| Spotify browse: a playlist or album tile opens its numbered track list, a row plays in context, the drill-in survives a restart; **Up next** tab | Spotify pane, large layout | Unreleased | Untried |
| Sign Out of Spotify (keeps the Client ID, stops the engine) | Settings › Plugins › Spotify; palette › Sign Out of Spotify | Unreleased | Untried |
| Colour from music: a plugin with the `ambient` permission tints the accent, focus edge, cursor, selection and the cards, capped to 3:1 contrast | Settings › Appearance › Colour from music; `ambient-color = false` off | Unreleased | Untried |
| Plugin panes that scroll as one page (`Tree.Scroll`), plugin icon buttons drawn as SF Symbols, and a smooth pane: unchanged trees skipped, seek bar and clock moved by the host | Automatic; Spotify uses all | Unreleased | Untried |
| Usage widget: a Cost card (Today, Yesterday, 30 Days; donut and legend, from local Claude Code, Codex and OpenCode logs) and a section per provider with each window's bar, pace tick, "% left", reset and "Limit in …" flame, and Today and Last 30 Days in dollars and tokens; compact in small panes | New Pane chooser › Usage, palette, `kero split --plugin usage` | Unreleased | Untried |

### Files pane

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Folder tree with git changes, refreshed every few seconds | Files pane | 2.0.7-local.1 | Shipped |
| Markdown, code and images shown beside the tree | Click a file | 2.0.7-local.1 | Shipped |
| Markdown links to headings (`#section`) and other files open in the pane | Click the link | 2.0.7-local.1 | Shipped |
| Markdown web links open in the default browser (fixed: they were handed over as a file's path) | Click the link | Unreleased | Untried |
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
| Open the page in the default browser (fixed: it was handed over as a file's path) | The pane bar's **Open in Default Browser** | Unreleased | Untried |

## Agents

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude Code and Codex report working, waiting, done and error from their hooks; Esc and granted permissions still read from the screen | `kero skill setup`; `agent-hooks` in the configuration | Unreleased | Untried |
| Session status icons: a turning ring while working (a filling one when the program reports progress), an amber ring with a dot when it needs you, a filled check when done and unseen, a faint check once read, a red disc when a program fails, a crossed ring when the shell exits with an error, a half moon when read and idle for 24 hours; kept across a terminal service restart | Sidebar rows and the footer's count chips | Unreleased | Untried |
| Session title marks (SF Symbols, derived, never typed): needs you, bug (linked task labeled Bug), working, done and seen; the title truncates, not the mark | Automatic; sidebar rows and the command palette | Unreleased | Untried |
| Sidebar project groups: room above each header, header close to its sessions, fixed order and places for the count, "needs you", chevron and add button; emoji in project titles kept and drawn whole | Sidebar; rename a project from its header's context menu | Unreleased | Untried |
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
| Hidden terminal output updates its screen without redrawing the window; revealing a pane shows its latest output | Automatic when switching sessions, zooming, or replacing a terminal with another pane view | Unreleased | Automated checks; not yet tried in the installed app |
| Visited-terminal memory diagnostics: empty-app baseline, two sweeps, all-pane output and settled footprint, Go heaps and native accounting; separate-session and single-session split workloads | `PERF_MEMORY=1 PERF_KEEP=1 sh scripts/perf.sh N`; add `PERF_LAYOUT=split`; see [Performance](perf.md) | Unreleased | Automated probe checks; synthetic measurements recorded separately from the daily app |
| Performance budgets and a one-command measurement: footprint, idle CPU, wakeups and frames, and frame times while streaming, for N panes, in an isolated kerod and app; kerod profiles on a loopback address; allocation tests for screen reads and a key's echo | `sh scripts/perf.sh N`, `KERO_PPROF=127.0.0.1:6060`; see [Performance](perf.md) | Unreleased | Measured at 1, 4 and 8 panes; key to paint remains open |
| Idle window paints less: CPU and RAM meters redraw only when what they show changes; times ago tick once a minute, only while shown | Automatic | Unreleased | Untried |
| Fewer idle wakeups: kerod checks quiet panes every 10 s instead of every 400 ms and writes histories only after a change; the CPU and RAM meters read every 30 s while the window does not have the focus | Automatic; numbers in [Performance](perf.md) | Unreleased | Measured with 4 and 8 panes, visible and unfocused |
| A focused terminal's cursor blinks by painting the last frame again, then rests shown after 10 seconds without a key or focus change; input starts the blink again | Automatic when cursor blinking is enabled | Unreleased | Automated checks; whole-app results in [Performance](perf.md) |
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
| Status detection reads the last rows, not the whole scrollback (0.75 ms → 10 µs a look at 4 MB) | Automatic | Unreleased | Untried |
| Startup: immediate connection, sessions restored in the background, four at a time | Automatic, after a restart of the terminal service | Unreleased | Untried |
| Scrollback budget: the app's terminals keep the Scrollback setting (not 10 MB), 8 stay attached (every pane of the shown session kept), the 10 s history save writes the last 256 KB | Settings › Shell › Scrollback; automatic | Unreleased | Untried |

## Window

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Compact title bar (40 pt): the traffic lights where macOS puts them, the sidebar's buttons centered on them | Automatic | Unreleased | Untried |
| With the sidebar hidden, the first header's title centered on the traffic lights; the sidebar's first row 30 pt below them, aligned with the close light | Automatic; `Cmd+B` hides the sidebar | Unreleased | Untried |
| macOS's own traffic lights in sight in full screen, where they are in the window, with their hover glyphs: close, minimize (greyed, as macOS has it) and exit full screen; the bar that slides down with the menu bar is empty and lets clicks through | `Ctrl+Cmd+F`, or the green light | Unreleased | Untried |
| The app moves as one (motion tokens, a few literals left); a project collapsing folds its rows away animated, the chosen row never leaving | Automatic; collapse by clicking a project header | Unreleased | Untried |
| A project's hover chevron and add button wait 80 ms after the pointer leaves | Automatic; hover a project header | Unreleased | Untried |
| First run: a ~900 ms reveal of the real shell, covering the window from its first frame, skipped after 2 s or on a connection error, cancellable, Reduce Motion aware, silent, shown once | Automatic, on the first launch | Unreleased | Untried |
| Floating panels on Liquid Glass: the command palette, the confirmation dialog, the New Pane chooser, Settings and a pane's drag chip; no dimming under them except a confirmation; concentric corners; opaque under Reduce Transparency | `Cmd+P`, `Cmd+,`, `Cmd+Alt+N` (New Pane chooser), closing a busy session, dragging a pane's title | Unreleased | Untried |

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
- Installing a plugin from a folder or a git URL, the public plugin SDK and
  the plugin index are later rounds (BRU-206 and BRU-207); the sidebar plugin
  slot is BRU-165.
- Dev servers do not survive a terminal service restart; restart them
  yourself.

When a feature ships, add its row here with the release, and its entry in the
[Changelog](changelog.md).
