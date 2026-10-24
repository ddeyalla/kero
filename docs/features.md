# Shibu Code feature tracker

Every feature Shibu Code adds to Kero, the release that shipped it, how to
reach it and where it stands. For what changed in each release see the
[Changelog](changelog.md); for how a feature works and where its code lives
see [Changes and implementation](changes.md).

Status:

- **Shipped**: released and tested.
- **Untried**: released and covered by tests, but not yet tried by hand in
  the installed app.
- **Tested**: automated checks passed; installed-app manual acceptance is
  not claimed. Rows that say **Measured** give their remaining limits explicitly.

Current release: **2.0.7-local.5** (Kero 2.0.7), 2026-10-24.

> Updating from a build before 2.0.7-local.2 requires one terminal-service
> restart when agents are idle (protocol 4, saved state 3). Tasks panes become
> Linear plugin panes; an older build cannot open the migrated panes.
> The capsule-outline fix is UI-only and requires no further service restart
> when 2.0.7-local.2 or later is already running.

## Control boundaries

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Complete glass capsule outlines over white content, including Reduce Transparency | Browser toolbar and plugin filter controls | 2.0.7-local.3 | Tested with native tester pixel regressions and shots |

## Delivery recovery

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Spotify cached-list warning, pending retry suppression and retired-player callback isolation | Spotify pane | 2.0.7-local.2 | Fake HTTP/event/race regressions pass |

## Linear and tasks

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Agent guide explains Attention state, pane creation, focus and session silence | `kero skill show`; `kero skill setup` installs the guide | 2.0.7-local.2 | Tested |
| Performance verification with native input and independently observed focus | `sh scripts/perf.sh 1`, `4` or `8`; `PERF_MEMORY=1` profiles retained terminals | 2.0.7-local.2 | Harness validated; native focus/idle acceptance awaits an unlocked desktop; native memory ownership remains open |
| Release validation permits Spotify automation consent while guarding the app identity; CI runs all local delivery checks | `scripts/check.sh`, Shibu checks and Shibu updates workflows | 2.0.7-local.2 | Tested |
| Linear plugin: list or board in the sections Current, Done, Todo and Backlog with nested folds; Current follows the focused session; filters for assignee, project, label, priority, search and canceled; full issue detail (Markdown, properties, status picker, runs with Resume, sub-issues, relations, activity) | The sidebar pin, the **Tasks** entry of the New Pane chooser (`Cmd+Alt+L`), `kero split --tasks` | 2.0.7-local.2 | Untried |
| Sidebar pins: a plugin widget has a row in the sidebar; a click focuses its pane in the selected session or splits one in | Pane header menu, Settings › Plugins, command palette, `kero plugin pin\|unpin <plugin> [widget]` | 2.0.7-local.2 | Untried |
| Linear plugin keyboard and menu: sticky search and filter header; J/K or arrows move, Enter opens, Escape goes back (only while the pane has the focus); right-click menu with Open, Open in Linear, Copy ID, Copy Link, Move To (shown at once) and Start task; label colours on cards; Canceled toggle; screen-reader labels for status and priority | Click in the Linear pane, then keys; right click an issue | 2.0.7-local.2 | Untried |
| Plugin permission `clipboard`: write-only, up to 64 KB, accepted only within about 1 s of a user event the host delivered; otherwise refused and logged | Manifest `permissions`; `Conn.Copy` in the SDK | 2.0.7-local.2 | Untried |
| The old built-in Linear screen (sidebar row, `Cmd+Shift+L`) and Tasks pane | Replaced by the Linear plugin | 2.0.7-local.1 | Removed in Unreleased |
| Linear API key in the personal data directory (`0600`), or `LINEAR_API_KEY` | Settings › Agents, `kero linear auth` | 2.0.7-local.1 | Shipped |
| Daemon keeps the workspace fresh (30 s changes, 5 min full read, last good read kept offline) | Automatic | 2.0.7-local.1 | Shipped |
| Daemon idle work cut: foreground program read as it changes, directory 1.5 s only for the focused pane, branch scan 30 s plus focus and a HEAD check every 1.6 s, `state.json` at most every 2 s, Linear in its own `linear.json`, written per Linear read, not per status change | Automatic | 2.0.7-local.2 | Untried |
| Session linked to its task, kept across restarts; issue shows the session and its agent resume id | `kero link <session> <BRU-25>`, issue detail | 2.0.7-local.1 | Shipped |
| Start a task: link the session, move the issue to In Progress, print its branch | `kero task start <BRU-25>` | 2.0.7-local.1 | Shipped |
| Project mapped to its Linear project (the one named like the repository offered first) | The project's menu in the sidebar | 2.0.7-local.1 | Shipped |
| Linear from the CLI | `kero linear status\|board\|issue\|set-state\|comment` | 2.0.7-local.1 | Shipped |
| The Linear plugin's empty states act: Refresh Linear when it was not reached or has no issues, Clear Filters when a filter hides them | The empty state's button | 2.0.7-local.2 | Untried |

## Panes

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| New Pane chooser: Terminal, Tasks (the Linear plugin), Files, Browser, Attention | `Cmd+Alt+N` or the pane header's **+**, then `T`/`L`/`E`/`B`/`A` | 2.0.7-local.1 | Shipped |
| Open the new pane below instead of beside | Hold Shift in the chooser | 2.0.7-local.1 | Shipped |
| Open a kind of pane directly | `Cmd+Alt+T` / `L` / `E` / `B` (and `A`) | 2.0.7-local.1 | Shipped |
| Turn a pane into another kind | Pane menu › **Show in This Pane** | 2.0.7-local.1 | Shipped |
| Attention pane: every session that needs you, one list, most urgent first, with chips (All, Needs you, Working, Unread, Done), counts and a plain-text search; filter, search and scroll survive restart; a row opens the pane that needs you, and an Open issue button on a session with an issue opens it in a Linear plugin pane | `Cmd+Alt+A`, the chooser's `A`, or `kero split --attention` | 2.0.7-local.2 | Untried |
| New icons are SF Symbols, drawn by macOS and cached as bitmaps | Automatic; the Attention pane's mark and its rows' states | 2.0.7-local.2 | Untried |
| Agents open views beside themselves | `kero split --browser 5173`, `--files --open docs/plan.md`, `--tasks`, `--attention` | 2.0.7-local.1 | Shipped |
| A view's page or file is kept across restarts | Automatic | 2.0.7-local.1 | Shipped |
| Least pane size (280×150); a split without room is refused | Automatic | 2.0.7-local.1 | Shipped |
| Make panes even | Double-click a divider, `Cmd+Ctrl+=` | 2.0.7-local.1 | Shipped |
| Panes glide in and out; terminal sizes wait for the glide to end | Automatic | 2.0.7-local.1 | Shipped |
| The focused pane has a soft accent border among several panes (none alone or zoomed); unfocused panes keep the same card and text | Automatic; click or cycle panes | 2.0.7-local.2 | Untried |
| Panes open small and fade in; closing panes fade out the same way over 160 ms | Automatic | 2.0.7-local.2 | Untried |
| Plugin panes: a plugin's widget in a pane, with plugin, widget and state kept in the pane target across a restart | New Pane chooser, palette, `Cmd+Alt+P`, `kero split --plugin <id> [--widget <name>]` | 2.0.7-local.2 | Untried |
| The plugin platform: manifest, JSON-RPC process host, one process per plugin, permission checks, crash with **Restart** | Automatic when a plugin pane opens | 2.0.7-local.2 | Untried |
| Plugin parts: rows, columns, text, images, buttons, sliders, progress, lists, a search field, an empty state, spacers and dividers, in the app's palette and motion | A plugin's widget pane | 2.0.7-local.2 | Untried |
| Plugin settings and secrets: Settings › Plugins lists plugins, versions, permissions and fields; secrets in a 0600 file | Settings › Plugins | 2.0.7-local.2 | Untried |
| Plugin commands: each manifest command in the palette and as a `kero do` action; runs on a pane of its plugin, opening one if needed | Command palette, `kero do plugin.<id>.<command>`, `kero actions` | 2.0.7-local.2 | Untried |
| Marquee text only when it overflows; static ellipsis with Reduce Motion | `marquee` on a text part | 2.0.7-local.2 | Untried |
| Image tint: a soft gradient of the picture's own color behind the pane, crossfaded and contrast clamped | `tint` on an image part | 2.0.7-local.2 | Untried |
| A plugin's stderr log, rolled at 1 MB, and a failed plugin's error with a **Restart** button | Automatic | 2.0.7-local.2 | Untried |
| An old terminal service without the plugin view shows one line asking to restart it | Automatic | 2.0.7-local.2 | Untried |
| Spotify widget: plays in the app with no Spotify app open (Premium), through Spotify's Web Playback SDK; engine starts on the first play and closes after 10 idle minutes; sign in again for the new scopes | New Pane chooser › Spotify, palette, `kero split --plugin spotify`; Client ID in Settings › Plugins | 2.0.7-local.2 | Untried |
| Spotify command deadlines: a play waiting for a cold or reconnecting player stops at its five-second deadline with a "taking too long" notice instead of being sent late; an older failed play, including a late page error, does not revert or dim a newer selection, even when the same row is pressed again; a superseded pending play is canceled before admission, a coalesced click keeps the accepted play's owner, and a retry after a current failure is sent | Automatic; Spotify pane | Unreleased | Fake-engine/race tests; real account acceptance pending |
| Spotify compact player: two rows from 440 points wide (art, title, controls; seek, time, mute, volume), three rows narrower; shown once music plays | Spotify pane | 2.0.7-local.2 | Untried |
| Spotify browse: a playlist or album tile opens its numbered track list, a row plays in context, the drill-in survives a restart; **Up next** tab | Spotify pane, large layout | 2.0.7-local.2 | Untried |
| Sign Out of Spotify (keeps the Client ID, stops the engine) | Settings › Plugins › Spotify; palette › Sign Out of Spotify | 2.0.7-local.2 | Untried |
| Colour from music: a plugin with the `ambient` permission tints the accent, focus edge, cursor, selection and the cards, capped to 3:1 contrast | Settings › Appearance › Colour from music; `ambient-color = false` off | 2.0.7-local.2 | Untried |
| Plugin panes that scroll as one page (`Tree.Scroll`), plugin icon buttons drawn as SF Symbols, and a smooth pane: unchanged trees skipped, seek bar and clock moved by the host | Automatic; Spotify uses all | 2.0.7-local.2 | Untried |
| Usage widget: a Cost card (Today, Yesterday, 30 Days; total and provider list, from local Claude Code, Codex and OpenCode logs) and a section per provider with each window's bar, pace tick, "% left", reset and "Limit in …" flame, and Today and Last 30 Days in dollars and tokens; compact in small panes | New Pane chooser › Usage, palette, `kero split --plugin usage` | 2.0.7-local.2 | Untried |

### Files pane

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Folder tree with git changes, refreshed every few seconds | Files pane | 2.0.7-local.1 | Shipped |
| Markdown, code and images shown beside the tree | Click a file | 2.0.7-local.1 | Shipped |
| Markdown links to headings (`#section`) and other files open in the pane | Click the link | 2.0.7-local.1 | Shipped |
| Markdown web links open in the default browser (fixed: they were handed over as a file's path) | Click the link | 2.0.7-local.2 | Untried |
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
| Open the page in the default browser (fixed: it was handed over as a file's path) | The pane bar's **Open in Default Browser** | 2.0.7-local.2 | Untried |

## Work profiles

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Profiles saved with the state: a name, an accent + background preset, the ids of the projects and sessions it brings forward, its env bundle's name, and which profile is active — thin references, resolved when applied, not copies; nothing user-facing yet, the switcher's children build on this | Automatic (saved state); no switching surface yet | 2.0.7-local.2 | Untried |

## Agents

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude Code and Codex report working, waiting, done and error from their hooks; Esc and granted permissions still read from the screen | `kero skill setup`; `agent-hooks` in the configuration | 2.0.7-local.2 | Untried |
| Session status icons: a turning ring while working (a filling one when the program reports progress), an amber ring with a dot when it needs you, a filled check when done and unseen, a faint check once read, a red disc when a program fails, a crossed ring when the shell exits with an error, a half moon when read and idle for 24 hours; kept across a terminal service restart | Sidebar rows and the footer's count chips | 2.0.7-local.2 | Untried |
| Session title marks (SF Symbols, derived, never typed): needs you, bug (linked task labeled Bug), working, done and seen; the title truncates, not the mark | Automatic; sidebar rows and the command palette | 2.0.7-local.2 | Untried |
| Sidebar project groups: room above each header, header close to its sessions, fixed order and places for the count, "needs you", chevron and add button; emoji in project titles kept and drawn whole | Sidebar; rename a project from its header's context menu | 2.0.7-local.2 | Untried |
| Each session carries one attention state — running, waiting-user, unread, done or idle — with one line of what it says, the pane it comes from and its age; a transcript scan fills in where an agent reports nothing (Codex's approvals) | Automatic: it rides on the state kerod sends | 2.0.7-local.2 | Untried |
| Go to the next session waiting on you, longest wait first, with the pane that needs you in front (a zoom follows it); the issue detail's session opens there too | Session › **Next Waiting Session**, the command palette, `kero do attention.next-blocked` | 2.0.7-local.2 | Untried |
| Notifications from that attention: a session that needs you or ends unseen tells once, a turn that begins again clears it, and a short return within 30 seconds of leaving the same state is counted silently inside `notify-throttle-minutes`; only native-accepted delivery starts the throttle; every session kerod holds tells, in any profile or window | Automatic; `notify-on-waiting`, `notify-on-done`, `notify-on-error` and `notify-throttle-minutes` in the configuration, Settings › Notifications | 2.0.7-local.2 | Untried |
| Silence one session's notifications, remembered across restarts and removed after a confirmed app close | Session › Silence Notifications, the session's context menu, `kero do session.silence`; `silence-session` in the configuration | 2.0.7-local.2 | Untried |

## Usage and the machine

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude, Codex, OpenCode Go and DeepSeek usage, refreshed in the background | Usage icon popover, Usage modal, `kero usage --json` | 2.0.5-local.1 | Shipped |
| Usage as a modal in the window | `Cmd+Shift+U`; `Cmd+R` refreshes, Esc closes | 2.0.5-local.2 | Shipped |
| Optional private provider keys | Usage modal | 2.0.5-local.1 | Shipped |
| Usage icon popover: the Usage plugin's list of providers, allowances, percentages or balances, reset times, cost ranges and CPU and RAM, opened from one compact icon in every navigation layout; hover opens, click pins, Enter or Space opens for the keyboard, keyboard focus inside keeps it open when the pointer leaves, Escape or an outside click closes, Open in Pane keeps the normal Usage pane, and the footer fits at short window heights while the list scrolls | Hover or click the usage icon in the sidebar footer or the tab row; `Cmd+Shift+U` still opens the Usage modal | 2.0.7-local.4 | Installed; fixture and installed-app smoke verified; native pointer/feel hand test pending |
| CPU and RAM meters for the whole Mac | Usage icon popover, `kero resources --json` | 2.0.5-local.2 | Shipped |
| Hidden terminal output updates its screen without redrawing the window; revealing a pane shows its latest output | Automatic when switching sessions, zooming, or replacing a terminal with another pane view | 2.0.7-local.2 | Automated checks; not yet tried in the installed app |
| Visited-terminal memory diagnostics: empty-app baseline, two sweeps, all-pane output and settled footprint, Go heaps and native accounting; separate-session and single-session split workloads | `PERF_MEMORY=1 PERF_KEEP=1 sh scripts/perf.sh N`; add `PERF_LAYOUT=split`; see [Performance](perf.md) | 2.0.7-local.2 | Automated probe checks; synthetic measurements recorded separately from the daily app |
| Performance budgets and a one-command measurement: footprint, idle CPU, wakeups and frames, and frame times while streaming, for N panes, in an isolated kerod and app; kerod profiles on a loopback address; allocation tests for screen reads and a key's echo | `sh scripts/perf.sh N`, `KERO_PPROF=127.0.0.1:6060`; see [Performance](perf.md) | 2.0.7-local.2 | Measured at 1, 4 and 8 panes; key to paint remains open |
| Idle window paints less: CPU and RAM meters redraw only when what they show changes; times ago tick once a minute, only while shown | Automatic | 2.0.7-local.2 | Untried |
| Fewer idle wakeups: kerod checks quiet panes every 10 s instead of every 400 ms and writes histories only after a change; the CPU and RAM meters read every 30 s while the window does not have the focus | Automatic; numbers in [Performance](perf.md) | 2.0.7-local.2 | Measured with 4 and 8 panes, visible and unfocused |
| A focused terminal's cursor blinks by painting the last frame again, then rests shown after 10 seconds without a key or focus change; input starts the blink again | Automatic when cursor blinking is enabled | 2.0.7-local.2 | Automated checks; whole-app results in [Performance](perf.md) |
| The loading.dev ring wherever the app itself is busy: connecting, attaching a pane, reading Files or Linear; it and a working session's ring turn at 15 frames a second | Automatic | 2.0.7-local.2 | Untried |

## Settings

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Settings as a modal over the main window, not a separate window | `Cmd+,` toggles it, or the sidebar's gear; Esc, a click beside it or `Cmd+W` closes it | 2.0.7-local.2 | Untried |
| Accent presets: Blue, Plum, Rose, Sea, Moss, Graphite — an accent color, the wash that comes with it, the terminals' selection | Settings › Appearance › Accent (with its wash); `accent = <preset>` in the config (the accent alone) | 2.0.7-local.2 | Untried |
| Shared keyboard focus ring in the chosen accent, including generic controls and sliders; Increase Contrast keeps its full opacity | Settings › Appearance › Accent; Tab to a slider or control | Unreleased | Tested |
| Grain over the background wash: a repeating 300-point tile at 3.3% light and 4% dark | Settings › Appearance › Grain; `grain = false` off | 2.0.7-local.2 | Pixel strength and tiling regressions pass |
| Spotify list errors retain the open album or playlist and offer Retry; offline libraries and Up next can retry in place | Retry in a failed Spotify list | 2.0.7-local.2 | Fake HTTP recovery and race tests |

## Terminal

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Finder files and clipboard screenshots paste as paths | `Cmd+V` | 2.0.5-local.1 | Untried |
| Files and images dropped on a terminal insert their paths | Drag and drop | 2.0.5-local.1 | Untried |
| Faster redraws after a synchronized frame (55.9 ms → 16.9 ms transport median) | Automatic | 2.0.5-local.2 | Shipped |
| Native ARM64 build, no constant status animation | Automatic | 2.0.5-local.1 | Shipped |
| Status detection reads the last rows, not the whole scrollback (0.75 ms → 10 µs a look at 4 MB) | Automatic | 2.0.7-local.2 | Untried |
| Startup: immediate connection, sessions restored in the background, four at a time | Automatic, after a restart of the terminal service | 2.0.7-local.2 | Untried |
| Scrollback budget: the app's terminals keep the Scrollback setting (not 10 MB), 8 stay attached (every pane of the shown session kept), the 10 s history save writes the last 256 KB | Settings › Shell › Scrollback; automatic | 2.0.7-local.2 | Untried |

## Window

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Compact title bar (40 pt): the traffic lights where macOS puts them, the sidebar's buttons centered on them | Automatic | 2.0.7-local.2 | Untried |
| With the sidebar hidden, the first header's title centered on the traffic lights; the sidebar's first row 30 pt below them, aligned with the close light | Automatic; `Cmd+B` hides the sidebar | 2.0.7-local.2 | Untried |
| macOS's own traffic lights in sight in full screen, where they are in the window, with their hover glyphs: close, minimize (greyed, as macOS has it) and exit full screen; the bar that slides down with the menu bar is empty and lets clicks through | `Ctrl+Cmd+F`, or the green light | 2.0.7-local.2 | Untried |
| The app moves as one (motion tokens, a few literals left); a project collapsing folds its rows away animated, the chosen row never leaving | Automatic; collapse by clicking a project header | 2.0.7-local.2 | Untried |
| A project's hover chevron and add button wait 80 ms after the pointer leaves | Automatic; hover a project header | 2.0.7-local.2 | Untried |
| First run: a ~900 ms reveal of the real shell, covering the window from its first frame, skipped after 2 s or on a connection error, cancellable, Reduce Motion aware, silent, shown once | Automatic, on the first launch | 2.0.7-local.2 | Untried |
| Floating panels on Liquid Glass: the command palette, the confirmation dialog, the New Pane chooser, Settings and a pane's drag chip; no dimming under them except a confirmation; concentric corners; opaque under Reduce Transparency | `Cmd+P`, `Cmd+,`, `Cmd+Alt+N` (New Pane chooser), closing a busy session, dragging a pane's title | 2.0.7-local.2 | Untried |
| Glass Browser/Files toolbar groups, capsule fields and controls, native SF Symbols, held slider/switch lenses and soft scrolling edges; sidebar and pane-header icon buttons retain their previous plain style; opaque under Reduce Transparency | Sidebar, pane headers, Browser/Files/Attention toolbars, Settings and plugin controls | 2.0.7-local.2 | Automated checks and isolated dev app; hand acceptance pending |
| Increase Contrast strengthens app-drawn muted and faint text, card and field edges, dividers and control hairlines, including the opaque Reduce Transparency fallback; the normal appearance and the selected or ambient accent stay as they are | Automatic from the desktop's Increase Contrast | Unreleased | Tested |

## Navigation

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Four saved navigation placements: left or right sidebar, or session tabs across the top or bottom; horizontal tabs replace the sidebar; Cmd+B hides and reveals the current surface without changing its placement | Settings › General › Placement, View menu, command palette, `kero do nav.placement.<placement>` (replace `<placement>` with `left`, `right`, `top` or `bottom`); shortcuts are bindable in Settings › Keyboard | 2.0.7-local.4 | Installed; MyGo tester and installed-app smoke verified; native pointer/drag hand test pending |
| Compact navigation utilities: sidebar footer keeps Usage, Settings, attention and connection/restart usable at the 200-point minimum; when it overflows, attention is reserved before optional project open, search, working and done actions, which move into a labelled Sidebar Actions menu; horizontal tabs keep pinned plugin icons and utilities at the trailing edge only while the same measured compact-title floors used by tab allocation fit, moving optional pins into the labeled plugin overflow without wide-window duplicates | Sidebar footer, Sidebar Actions menu, tab-row icons | 2.0.7-local.5 | Original compact utilities introduced in 2.0.7-local.4; refinement MyGo tester verified; native pointer/drag hand test pending |
| Project-grouped session tabs: projects in saved order and pins, Other last; each project header and its sessions sit in a glass capsule with 2-point padding/internal gaps and 6 points between capsules, opaque under Reduce Transparency; a project header folds its sessions inline and shows its count and attention when folded; the selected session's panes stay in view; a folded active project marks its header; a session whose project is unknown or empty is grouped under Other and expands Other when selected explicitly | Top or bottom placement; click a project header; the project context menu | 2.0.7-local.5 | Original tabs introduced in 2.0.7-local.4; refinement MyGo tester verified; native pointer/folding hand test pending |
| Session tabs: status, content-measured title with the whole title in its tooltip and accessibility name, actual light/dark border budgeting, and pin space only when the cached SF Symbol is available; close on hover or keyboard focus, rename, context menu, session and project drag; preferred widths cap at 220 points, compact titles at or below 120 points stay whole, only longer tabs shrink proportionally toward the 80-point minimum, and the row scrolls when the floors exceed its room; project-grouped overflow and selection from the overflow, palette, shortcut, attention jump or plugin notification opens the folded project and reveals the tab, including an attention jump to an already-selected session | Top or bottom placement; right-click a tab; drag tabs or project headers; the overflow button | 2.0.7-local.5 | Original tabs introduced in 2.0.7-local.4; refinement MyGo tester verified; native drag/folding hand test pending |

## Session organization

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Pin sessions inside each project; reorder pinned or unpinned sessions with a glass preview and neutral placement cue; palette and navigation share saved order | Session context menu, drag, palette, `kero do session.pin\|session.unpin\|session.moveUp\|session.moveDown` | 2.0.7-local.2 | Installed in 2.0.7-local.4; native drag/reorder hand test pending |
| Reorder whole project groups without changing session membership | Drag project headers, project context menu, Session menu, palette, `kero do project.moveUp\|project.moveDown` | 2.0.7-local.2 | Installed in 2.0.7-local.4; combined native drag hand test pending |

## Updates and releases

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Daily check for stable Kero releases, merged, tested and signed | Automatic (GitHub Actions on the fork) | 2.0.5-local.6 | Shipped |
| Signed background updates that apply on the next launch | Settings › General › Updates | 2.0.5-local.6 | Shipped |
| Check Kero now, with progress and result; separate from published app updates | Settings › General › Updates › Check Upstream & Build, palette, `kero do updates.sync` | Unreleased (moved from sidebar; action since 2.0.5-local.7) | Tested locally; not installed |
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
