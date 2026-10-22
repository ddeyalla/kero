# Shibu Code changelog

This file records personal-fork changes. For original Kero releases, see the
[upstream changelog](../CHANGELOG.md). The app version is defined in
[`mygo.json`](../mygo.json); numbering follows [Versioning](versioning.md).
Dates use the local calendar date. Releases from 2.0.5-local.6 on are
published on the fork as `shibu-v<version>`; earlier entries describe local
builds only. What each feature is and where it stands is tracked in the
[feature tracker](features.md).

## Unreleased

> **Restart the terminal service once.** This release changes the daemon
> protocol (an issue's assignee, priority and parent, and the workspace's
> viewer) and saved state: state version 3
> turns every saved Tasks pane into a Linear plugin pane. After updating,
> restart the terminal service when agents are idle. An older build cannot
> open the migrated panes as Tasks: do not go back to one after the new daemon
> has saved state.

### Added

- Linear is a built-in plugin. Its pane shows the team's issues as a list or a
  board, in the sections Current, Done, Todo and Backlog, each with nested
  folds (section, project, parent issue, sub-issues). Current follows the
  focused session: the issue linked to it first, then In Progress issues with a
  live session, then the rest assigned to you. Filters: assignee, project,
  label, priority, search and canceled issues. The issue detail has the full
  description in Markdown, properties, a status picker, sessions with Resume,
  sub-issues, relations and the activity feed. See
  [Linear plugin](linear-workspace.md).
- Sidebar pins: any plugin widget can have a row in the sidebar. A click
  focuses the plugin's pane in the selected session or splits one in. Pin from
  the pane header's menu, Settings › Plugins, the command palette, or
  `kero plugin pin <plugin> [widget]` (and `unpin`). Manifest widgets have a new
  `icon`. The old Linear row became the pin of the Linear plugin for installs
  that had it on.
- New plugin parts: `menu` (a pop-up button), `pressable` rows and columns with
  hover, press and context-menu states, `scrollX` rows, `markdown`, chips,
  and more icons (circle, circle-dashed, circle-half, check-circle,
  xmark-circle, filter, person, tag, flag, pin, priorities, and others). A
  plugin with the new permission `focus` hears which session is selected.
  A tree may also have a sticky `header` and `keys` (heard only while the pane
  holds the keyboard focus), and a pressable part a `contextMenu` with
  submenus.
- The Linear plugin is made to be driven from the keyboard and the mouse. The
  search and filters stay in a sticky header. J and K or the arrow keys move
  the selection, Enter opens the issue and Escape goes back, only while the
  Linear pane has the focus. A right click on an issue opens a menu: Open,
  Open in Linear, Copy ID, Copy Link, Move To (a submenu of states) and Start
  task. Move To shows the new state at once, before Linear confirms. Cards
  show the issue's labels in their Linear colours. Status and priority have
  screen-reader labels. Canceled issues have their own toggle.
- The plugin permission `clipboard` puts text on the clipboard (`clipboard.write`,
  up to 64 KB, write-only). The host accepts it only within about one second
  after it delivered a press, key or command to that plugin; any other write
  is refused and logged. The Linear plugin uses it for Copy ID and Copy Link.
- Links with a username (`https://user@host`) are refused in plugin views and
  the Files pane, as only plain `http` and `https` links open.

- The Spotify widget plays music itself, with no Spotify app open: after one
  sign-in with a Premium account, Shibu Code is a Spotify device of its own,
  through Spotify's Web Playback SDK in a hidden plugin web view. Sound starts
  with no click. The engine wakes the audio after a play, a stall watchdog
  fixes a track whose position stands still, the player reconnects with
  backoff (2, 5 and 15 seconds), and play and toggle retry on a 403 or 404.
  Errors read as plain text: Premium is required, sign in again, or Spotify's
  own message. The engine starts on the first play, and closes after 10
  minutes with nothing playing or when the last Spotify pane closes, which
  saves about 130 MB while idle. The web view runs with MyGo's new
  `PageOptions.RunWhenHidden`, so WebKit does not throttle or suspend the
  hidden page. `go.mod` pins the `ddeyalla/mygo` fork, which adds
  `AllowAutoplay` and `RunWhenHidden`. A sign-in from before this asks to
  sign in again, and so does the new scope `playlist-read-collaborative` (and
  `user-read-recently-played`).
- The Spotify pane is a compact player that fits any size from 280 by 150
  points. With 440 points of width or more it has two rows: 56 point art,
  title and artist, shuffle, previous, play, next and repeat, then elapsed
  time, the seek bar, duration, mute and an 80 point volume. Narrower, it has
  three rows. The mute icon becomes `speaker.slash.fill` when muted. The
  player shows only once music plays in this session, and stays while paused;
  with nothing playing, the pane shows just the library, and there is no Sign
  In flash on open. Shuffle and repeat keep their value until Spotify
  confirms it, and the library refreshes when it is older than 60 seconds.
- The Spotify pane smooths its work. The plugin SDK skips a tree identical to
  the last one it sent for a pane. The seek bar and the elapsed clock move in
  the host (`Rate` and `At` on sliders and text, `sdk.Clock`,
  `Slider.Playing`), with no one-second tick. A drag renders only when it
  ends. The picture cache is keyed by URL. SF Symbols are drawn as templates
  and tinted at draw time, with no flash of a fallback. The grid shows at
  most 48 tiles, with a **Show More** tile, using 300 point covers.
- Browse in the Spotify pane: in the large layout, a playlist or album tile
  opens its track list, with a back button, the name, **Play** and numbered
  rows; the playing row is highlighted. Pressing a row plays it in its
  context (`PlayFrom`, also on the desktop player through AppleScript). The
  drill-in is saved in the pane's target. A 403 shows "Spotify doesn't share
  this playlist's tracks", with **Sign In Again** when the collaborative scope
  is missing. A new **Up next** tab reads `/me/player/queue`. The medium and
  small layouts still play on a tile press.
- **Sign Out** for Spotify, in Settings › Plugins › Spotify and in the command
  palette as "Sign Out of Spotify". It keeps the Client ID and stops the
  engine. For this, a plugin manifest setting may have the new kind `action`,
  which names a declared command, and the host has `Host.Act`.
- Colour from music: a plugin with the new `ambient` permission lends its
  pane's tint to the whole app: the accent, the focus edge, the terminals'
  cursor and selection, and a soft wash of the card. The colour is capped to
  3:1 contrast. Turn it off in Settings › Appearance › **Colour from music**
  (`ambient-color` in the config; on by default). The Spotify pane tints only
  while a track plays, so the colour fades on pause.
- Plugin protocol additions, all optional: `Node.Active`, `Item.Active` and
  `Item.Leading`; `Rate` and `At` on sliders and text; the icons
  `volume-off`, `shuffle`, `repeat` and `volume`; `(*sdk.Node).Label` and
  `.Active`; the permissions `web` and `ambient`; a `headline` text role, a
  round `iconButton` size and a `grid` list; and `Tree.Scroll`
  (`ScrollingTree()`) for a pane that scrolls as one page. A horizontal
  slider with a `Width` keeps that width, and one without grows.
- Plugin icon buttons draw real SF Symbols.
- Optional memory diagnostics visit every terminal twice, stream all panes,
  and preserve app/daemon footprint, Go heaps and native VM accounting.
  `PERF_MEMORY=1 PERF_KEEP=1 sh scripts/perf.sh N`; add `PERF_LAYOUT=split`
  for N panes in one selected session. See [Performance](perf.md) (BRU-333).
- `scripts/perf.sh N` measures Shibu Code with N panes against the
  performance budgets: kerod's and the app's footprint, idle CPU, idle
  wakeups and idle frames over 60 s, and frame build and paint times
  while a pane streams. It runs its own kerod and app, isolated from the
  daily app, needs no root, and cleans up after itself. kerod serves Go's
  profiles on a loopback address with `KERO_PPROF=127.0.0.1:6060`, with
  its pane ticks, screen reads, history saves and frame sends labelled.
  Two tests fail if a screen read or a key's echo starts to allocate with
  the scrollback. Budgets and the first numbers: [Performance](perf.md)
  (BRU-223).
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
- Plugin panes: a plugin runs as its own process and draws a native widget in
  a pane. The app hosts it over JSON-RPC on stdin and stdout, one message per
  line, protocol version 1, and checks every call the plugin makes against the
  permissions in its manifest, `kero-plugin.json`. A plugin pane keeps its
  plugin, widget and state in the pane's target, so it comes back after a
  restart. Open one from the New Pane chooser, the command palette,
  `Cmd+Alt+P`, or `kero split --plugin <id> [--widget <name>]`. This release changes the daemon protocol. Restart the terminal service once after you update.
- The parts the host draws for a plugin: rows and columns, text (title, body,
  caption, mono), images, buttons and icon buttons, sliders, progress, lists,
  a search field (with an optional query from the plugin), an empty state,
  spacers and dividers, in the app's palette,
  fonts and motion. Focus and scroll stay with a part's id across renders,
  images load off the main thread and are cached, and progress moves locally
  from a rate and a time.
- A plugin that crashes shows an error and a **Restart** button once; the app
  does not restart it in a loop. A plugin that stops reading its input is
  stopped after five seconds rather than freezing the app. Its stderr goes to
  `logs/plugins/<id>.log`, rolled at 1 MB.
- Plugin settings and secrets: Settings › Plugins lists the built-in and
  installed plugins with their version and permissions, and draws each
  plugin's settings fields from its manifest, with their saved values, help
  and link. Secret fields go to `plugin-data/<id>/secrets.json` under the data
  folder (0600 in a 0700 folder), apart from the plugin's code; the other
  values go to `settings.json` there. The app sends the non-secret values to
  the plugin on start and after every change, and the plugin reads its secrets
  itself.
- A plugin's commands are command palette items and `kero do` actions
  (`kero do plugin.<id>.<command>`; `kero actions` lists them). A command runs
  on a pane of its plugin, and opens one when there is none. A click on a
  plugin's notification shows a pane of the plugin.
- An old terminal service that answers `no view "plugin"` is caught: the pane
  asks you to restart the terminal service instead of showing the raw error.
- Text in a plugin part can marquee: it stays still when it fits, and
  otherwise pauses, scrolls slowly, pauses and resets. Reduce Motion shows a
  static ellipsis instead.
- An image in a plugin part can tint its pane: the app samples the picture's
  dominant color once per picture, off the main thread, draws a soft gradient
  behind the card, crossfades it, and clamps the lightness for contrast in
  light and dark.
- Two built-in plugins use the same contract. The Spotify widget plays,
  pauses, skips and seeks the desktop app, changes the volume, and searches
  albums and playlists, with a compact player and a large one that adds the
  seek bar, volume, shuffle and repeat, and Search, Playlists and Albums tabs
  above them; it scrolls a title that does not fit, tints the pane with the
  album art, keeps the track position moving between reads, and tells Spotify
  not running, Spotify not installed (with a link to the download page), an
  Automation refusal (with a button to Privacy › Automation in System
  Settings), signed out, a refused sign-in, a lapsed Premium account, a rate
  limit and an offline read apart; offline and rate-limited, it keeps the last
  results of the same query with a note. A failed command says so in every
  layout until a command works, and the search field shows the saved query
  after a restart. Sign in once from Settings › Plugins with a Spotify Client
  ID, after you add `http://127.0.0.1:43017/callback` as the redirect URI of
  your Spotify app; the tokens stay in the plugin's secrets file. The plugin
  process ends when the app does. Its commands are in
  the command palette and are `kero do` actions, with no key bindings. The Usage
  widget shows each account's usage in a bar and a percent (or its balance),
  and the machine's CPU and RAM; the large one adds the reset time, the plan
  and every window. It reads the same data as the Usage tab, needs no
  permission, and refreshes only while it is visible, at most every two
  minutes unless you press refresh.
- The Usage pane is redesigned. A Cost card has a Today, Yesterday and 30 Days
  segmented control, a donut with the total in its middle (the text shrinks to
  fit), and a legend of a provider dot, its name and its amount. An amount
  made with an estimated price shows "≈", and a line "Some amounts are
  estimates" has a tooltip that lists the models priced by estimate. Below it,
  each provider has a section with its mark, name and plan. Each window
  (Session, Weekly, Monthly) shows a bar with a tick for the pace, "% left"
  and "Resets in …". When the current pace would reach the limit before the
  reset, a red flame and "Limit in …" tell when. A chevron opens Today and Last
  30 Days as dollars and tokens. A small pane gets a compact form. The "This
  Mac" CPU and memory section is removed. The range and the open providers are
  saved per pane.
- Cost comes from the logs on this Mac: Claude Code (`~/.claude/projects`, or
  `CLAUDE_CONFIG_DIR`), Codex (`~/.codex/sessions`, or `CODEX_HOME`) and
  OpenCode (`opencode.db`, read-only through the system `sqlite3`, with the
  cost OpenCode recorded). Prices come from a table in the binary
  (`internal/usage/cost/price.go`): Claude uses Anthropic's list, and the
  prices of other models are estimates. Reads are incremental. A compact
  binary cache, `plugin-data/usage/cost-cache.bin` in the data folder, is
  saved at most every 30 minutes and when the pane closes. The plugin reads
  only while a pane is visible: every 5 minutes, at midnight and on refresh.
  Nothing reads or stores the text of a message.
- Plugin trees have new parts: `segmented` and `donut` nodes; `surface: "card"`
  on a row or column; `marker` on a progress bar; `icon` and `mark` on text;
  a `strong` text role; the colour roles danger, warning, success, orange,
  green, teal, blue, purple and gray; the icons flame, dot, warning and info;
  and a `label` on icon-only text. Plugin text uses tabular digits, so numbers
  do not jump.

### Changed

- A focused terminal's cursor stops blinking 10 seconds after the last key,
  or after the terminal takes the focus, and rests shown, as GTK's cursor
  does (MyGo `81afe923`). The next key starts it again. A blinking cursor
  paints the window every 600 ms, which woke the app some 8,500 times a
  minute while it sat focused and idle.

- The sidebar's CPU and RAM meters read the Mac every 5 seconds, as Activity
  Monitor does by default, instead of every 2. Their changes rebuilt the
  window up to 30 times a minute while it was otherwise idle; now at most 12.
- While the window does not have the focus, the CPU and RAM meters read the
  Mac every 30 seconds instead of every 5, and read again at once when the
  window gets the focus back. Each reading that changed them rebuilt the
  window, which cost the idle app most of its wakeups (BRU-336).
- kerod follows a quiet pane less often. While a pane has output, a key, a
  resize or a request, kerod checks it every 400 ms, as before. After 10
  seconds with none of these, it checks every 10 seconds, until the next one.
  Pane histories are written 10 seconds after their text changes, not every
  10 seconds. At rest, kerod wakes about 70 to 280 times a minute, not
  1,000 (BRU-336). A branch switched outside Shibu, or an agent transcript
  written without screen output, can now show up to 10 seconds later in a
  quiet pane.

- The Tasks entry of the New Pane chooser, `Cmd+Alt+L` and `kero split
  --tasks` open the Linear plugin pane. The daemon still accepts the view
  `tasks` in create, split and replace, and makes the Linear plugin pane of it,
  so older clients and scripts work.
- The settings for the sidebar row, empty columns and card density left the
  Linear page of Settings. Empty columns and density are settings of the
  plugin, in Settings › Plugins. The key and the refresh interval stay.

### Removed

- The built-in Linear screen, its sidebar row, the `Cmd+Shift+L` shortcut and
  the Tasks pane. A saved window that showed the Linear screen opens the
  selected session.

- Session titles carry a mark at their head that follows the work, an SF
  Symbol the app derives and never stores in the name: `hand.raised`
  (amber) when the session needs you, `ladybug` when its linked Linear task
  is labeled Bug, `arrow.triangle.2.circlepath` while it works, and
  `checkmark.circle` once an agent's work has ended and you have seen it —
  the Attention pane's symbols for the same states. Needs you comes first, then the bug,
  then work, then done; a plain shell back at its prompt, a turn ended unseen, a failure or an
  exit have none. The title truncates, not the mark, and the time and the
  status glyph keep their places. The sidebar rows and the command palette's
  session rows show it. It reads what the app already receives, so it needs
  no protocol change.

- The command palette, the confirmation dialog, the New Pane chooser,
  Settings and a pane's drag chip float on Liquid Glass, as macOS 26 and
  later draw their popovers, alerts and sheets: regular glass that frosts
  the terminals under it, tinted in the dark so that muted text keeps 4.5
  to 1, with its lit rim and a soft shadow. The window
  under them is no longer dimmed, as macOS dims no window under a sheet,
  except under a confirmation, which asks before something that cannot be
  undone.
  Settings is a sheet of glass around its opaque page, its corner
  concentric with the page's, and the chooser's rows are rounded
  concentric with it too. Under Reduce Transparency every panel is
  opaque again, over the dimmed window, as before.
- Liquid Glass follows Reduce Transparency and Increase Contrast (MyGo
  `1c5a2cea`): opaque with a hairline rim under the first, a stronger rim
  and less show-through under the second. The same MyGo blinks a focused
  terminal's cursor by painting the window again, without building it.

### Fixed

- Output from hidden terminal panes no longer redraws the window. Switching
  sessions or unzooming a pane shows its latest output immediately (BRU-331).
- The app connects at once after a terminal service restart. Sessions appear
  as they come back, in their saved order, with up to four restoring at a
  time. The saved state stays intact until restoration finishes (BRU-229).
- The terminal service no longer sends the Linear workspace with every state,
  and a pane's title no longer writes the saved state. An agent's spinner,
  renaming its pane many times a second, rebuilt the app's window and its
  Linear views for each name, with the whole workspace encoded over the
  socket again each time. The workspace now has an event of its own, sent only
  when it changes; the app ignores a state equal to the one it shows; and a
  title change is broadcast without a save. The daemon protocol is 4:
  **restart the terminal service once** after updating (BRU-332).
- An old Tasks pane that was not migrated yet now shows the Linear plugin even
  before the terminal service restarts, and a new Tasks pane opens as a Linear
  plugin pane directly.
- The first-run intro tests no longer fail now and then.
- A data race in the tests of the Spotify plugin's lazy engine is gone: the
  wake timeout is a field of the engine, not a package variable.
- Dragging the Spotify seek bar no longer breaks playback. The slider of the
  plugin parts now commits once, when you let go, even if the pointer left
  the thin track, and the Spotify engine runs its commands one at a time and
  drops a double play of the same item.
- The Spotify player is no longer silent after sign-in, and no longer stalls
  while hidden. Plugin web views start audio without a click and run when
  hidden. The Browser pane keeps WebKit's default.
- The Browser pane's **Open in Default Browser** button and a Markdown
  link in the Files pane open the page again. The app handed the address
  to the system as a file's path, so the page never opened.
- An idle window paints less. The CPU and RAM meters still read every two
  seconds, but rebuild the window only when a label or a bar would change;
  the bars fill in whole percents, as the labels read. The sidebar's
  "5m ago" times move on once a minute instead of every 20 seconds, and
  not at all while the sidebar is hidden; the Attention pane and a working
  pane's chip keep their own times moving then. A time ago may now read up
  to a minute late ("now" for up to two minutes). The cursor blink is
  unchanged (BRU-225, in part).
- Status detection no longer reads a pane's whole scrollback several times
  a second. kerod reads the text of the last rows of the terminal alone, so
  each look at an agent's screen costs the same with any scrollback: about
  10 µs and 4 KB rather than 0.75 ms and 387 KB with the default 4 MB, or
  12.6 ms and 6.2 MB with 64 MB, while the session's lock is held. `kero
  read` reads its lines the same way. Detection sees the same text, byte for
  byte. It needs MyGo's `Terminal.TailText`, from the fork.
- The terminal service does less while nothing happens. It asks the
  kernel for a pane's foreground program as the program changes, not every
  400 ms; it reads the arguments only for a program whose name is not an
  agent's; it follows the directory of the pane the user is in every 1.5 s
  and of the others every 10 s; and it reads the branches agents work on
  every 30 seconds and when the user focuses a pane, with a cheap check of
  each HEAD file every 1.6 s, so a branch switch still links its session
  within 2 seconds. It writes `state.json` at most once every 2 seconds
  under a storm of status changes, and keeps the Linear workspace in a
  file of its own, `linear.json`, written when a read of Linear brings a
  new copy (about every 30 seconds while a key is set) instead of with
  every status change.
  An existing `state.json` with the workspace in it loads as before, and
  the next save moves the workspace out. Going back to an older release
  loses only the cached workspace until it is read from Linear again.
  The protocol did not change.
- A session's scrollback costs less memory and disk. The app's terminals
  keep the scrollback that Settings › Shell › Scrollback sets, as the
  terminal service does, instead of MyGo's 10 MB each; at the default of
  4 MB that is up to 6 MB less per attached pane. The app keeps 8 terminals
  attached instead of 12, and counts every pane of a session, not only the
  one in focus, so the panes of sessions shown long ago are let go; the
  panes of the session on screen always stay. The terminal service writes
  at most the last 256 KB of a pane's history every 10 seconds, and the
  whole of it only as it ends, so a crash restores the last screen and a
  little above it. A history file of any length, or an empty one, still
  restores.
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

- The sidebar's project groups read as groups: each header has room above
  it and sits close to its own sessions, the first header has the same room
  under the Linear row, and the header's name lines up with its sessions'
  marks. The count and "needs you" no longer shrink, and the add button's
  place is kept while it is hidden, so the header does not move when the
  pointer comes over it. A project title with an emoji, such as
  `🐛 kero-deyalla`, keeps it through a rename and a restart, shows it in
  color, and is cut short only between whole characters.

### Documentation

- Recorded visible, unfocused idle measurements at 1, 4 and 8 panes,
  including a repeat at 4 panes, and the remaining memory, focused-idle and
  input-latency limits. Clarified the cursor blink timeout, its code and
  regression checks, and independent focus verification after the timeout
  (BRU-223, BRU-225, BRU-336).
- Rewrote [Linear plugin and session links](linear-workspace.md) around the
  plugin, and updated the plugin guide (new parts, the `focus` permission,
  widget icons and pins), the security notes and the feature tracker.
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
