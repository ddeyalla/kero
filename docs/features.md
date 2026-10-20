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

## Panes

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| New Pane chooser: Terminal, Tasks, Files, Browser | `Cmd+Alt+N` or the pane header's **+**, then `T`/`L`/`E`/`B` | 2.0.7-local.1 | Shipped |
| Open the new pane below instead of beside | Hold Shift in the chooser | 2.0.7-local.1 | Shipped |
| Open a kind of pane directly | `Cmd+Alt+T` / `L` / `E` / `B` | 2.0.7-local.1 | Shipped |
| Turn a pane into another kind | Pane menu › **Show in This Pane** | 2.0.7-local.1 | Shipped |
| Agents open views beside themselves | `kero split --browser 5173`, `--files --open docs/plan.md`, `--tasks` | 2.0.7-local.1 | Shipped |
| A view's page or file is kept across restarts | Automatic | 2.0.7-local.1 | Shipped |
| Least pane size (280×150); a split without room is refused | Automatic | 2.0.7-local.1 | Shipped |
| Make panes even | Double-click a divider, `Cmd+Ctrl+=` | 2.0.7-local.1 | Shipped |
| Panes glide in and out; terminal sizes wait for the glide to end | Automatic | 2.0.7-local.1 | Shipped |

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

## Usage and the machine

| Feature | How to use it | Since | Status |
| --- | --- | --- | --- |
| Claude, Codex, OpenCode Go and DeepSeek usage, refreshed in the background | Sidebar, `kero usage --json` | 2.0.5-local.1 | Shipped |
| Usage as a modal in the window | `Cmd+Shift+U`; `Cmd+R` refreshes, Esc closes | 2.0.5-local.2 | Shipped |
| Optional private provider keys | Usage modal | 2.0.5-local.1 | Shipped |
| CPU and RAM meters for the whole Mac | Sidebar, `kero resources --json` | 2.0.5-local.2 | Shipped |

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
| Traffic lights in sight in full screen, where they are in the window: close, minimize (greyed, as macOS has it) and exit full screen; the bar that slides down with the menu bar is empty and lets clicks through | `Ctrl+Cmd+F`, or the green light | Unreleased | Untried |

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
