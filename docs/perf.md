# Performance budgets and measurement

What Shibu Code may cost, how to measure it with one command, and what it
cost when the measurement was added (BRU-223, the first step of BRU-222).
Every performance change records its numbers here, before and after.

## Delivery verification, 2026-10-23

The combined candidate was measured after integrating the UI, Usage, profile,
Attention and performance changes. These are separate workloads and counters:

| Workload | Result | Acceptance |
| --- | --- | --- |
| Final harness smoke, one pane, 100 native key dispatches | Key-to-paint p99 30.4 ms | Passes 50 ms; its two-second idle sample is not idle-budget evidence |
| Ordinary four-pane fixture, 60 seconds idle | App RSS 146.4 MiB; app/daemon interrupt wakeups 1,530/350 per minute; combined CPU 0.21%; 8 builds/min | Combined wakeups below 2,000, but app wakeups above 1,500 and RSS above 120 MiB. Window focus was not independently recorded in this noninjected run |
| Ordinary eight-pane fixture, 60 seconds idle | App RSS 143.9 MiB; app/daemon interrupt wakeups 1,340/371 per minute; combined CPU 0.19%; 8 builds/min | Numeric RSS/wakeup targets pass; window focus was not independently recorded |
| Eight visited terminals, all retained, 17 confirmed visits | Peak sampled app RSS 214.6 MiB; settled RSS 214.5 MiB; footprint 318.3 MiB streaming and 174.8 MiB settled | RSS below the 240 MiB eight-pane target; footprint is a separate counter. Forced diagnostic GC makes this a memory workload, not an idle benchmark |

These measurements do not close BRU-222, BRU-333 or the smaller-workload
acceptance gaps. The historical 1.22 GB daily-app growth has not reproduced;
native VM categories are recorded without inventing allocator ownership.
No unsupported GC or memory limit was added to make a measurement pass.

## Budgets

| Measure | Budget | How it is measured |
| --- | --- | --- |
| Footprint per pane, kerod | ≤ 25 MB | `scripts/perf.sh`: kerod's footprint with N panes, less its footprint with none, divided by N |
| Resident memory per pane, app | ≤ 30 MB | `scripts/perf.sh`: app RSS divided by the number of fixture panes; physical footprint is reported separately. Separate sessions retain up to eight client terminals; `PERF_LAYOUT=split` retains every pane in the selected session (see the limits below). |
| Idle wakeups, app + kerod | ≤ 2,000 a minute (kerod ≤ 500, app ≤ 1,500), window shown and unfocused, at 4 and 8 panes | `scripts/perf.sh`: interrupt wakeups over 60 s of idle. Agreed 2026-10-22 in place of 30, which a Go process with a MyGo window cannot reach (BRU-336, below) |
| Idle CPU, app + kerod | ≤ 1 % | `scripts/perf.sh`: CPU time over 60 s of idle, of one core |
| Idle frames, app | < 10 a minute that build the window | `scripts/perf.sh`: frames `MYGO_FRAME_STATS=all` logs over 60 s of idle. A frame that only paints the last one again ("painted again"), as a focused terminal's cursor blinks, is counted apart and not judged |
| p99 frame build | ≤ 4 ms | `scripts/perf.sh`: `MYGO_FRAME_STATS=all` while a shown pane streams output for 10 s |
| p99 key dispatch → paint | ≤ 50 ms | `scripts/perf.sh`: 100 native AppKit key events through the terminal, kerod and PTY echo to the next UI-thread turn after the matching frame; hardware/OS event-queue time and compositor presentation are not measured |
| Allocations of `screen()` | Flat with the scrollback; ≤ 16 KB a call | `TestScreenAllocsFlatWithScrollback` |
| Allocations of a key's echo | Flat with the scrollback; ≤ 52 KB in 1,400 objects (kerod), ≤ 20 KB in 350 objects (app) | `TestFrameAllocsFlatWithScrollback` |

The budgets are targets at 1, 4, 8 and 12 panes, each with a full scrollback
(BRU-222); the measurements below distinguish passes, misses and unmeasured
conditions. The last two rows are tests, which `go test ./...` runs, so a
regression fails the checks without the app.

MyGo's GPU builder keeps transformed clip masks for the current frame and
drops idle ones beyond 32 MiB. The pinned fork counts a shrunken mask's
retained backing capacity, not only its visible length, so stale capacity is
dropped. This is accounting and lifetime correctness, not a budget row, an app
RSS or footprint attribution, or evidence of 25-session savings.

## Measuring

```sh
sh scripts/perf.sh 4      # 4 panes
```

It takes about three minutes, and prints a table with each measure, its
budget, and `ok` or `OVER`. What it does:

1. It builds this checkout into a temporary directory, as `shibu-perf`.
2. It starts a kerod of its own: a temporary `KERO_HOME`, `HOME`,
   `KERO_CONFIG` and socket, with every `KERO_*` variable of the caller,
   `LINEAR_API_KEY` and `XDG_RUNTIME_DIR` cleared. The daily app, its kerod
   and the Linear workspace are never reached.
3. It opens N panes and feeds each 16 MB of output (`PERF_FEED_MB`), past
   the default scrollback of 4 MB, so that every scrollback is full. The
   scrollback setting counts the emulator's memory, not text, so 4 MB of
   text would not fill it.
4. It packages the same build in a temporary app bundle with a unique
   `CFBundleIdentifier`, stages the pinned terminal library after SHA-256
   verification, and starts it through LaunchServices with every routing
   variable explicitly isolated, `MYGO_FRAME_STATS=all` and a
   `TMPDIR` of its own. Its name and identity are not the daily app's or a
   dev app's, and its single-instance lock is in that `TMPDIR`, so it
   neither finds nor disturbs them. It opens a window for the run.
   `PERF_APP=0` leaves it out.
5. It waits for its own GUI to subscribe to kerod and attach the selected
   fixture before focusing the native window. After 15 s (`PERF_SETTLE`) it
   samples 60 s of focused idle (`PERF_IDLE`). Fresh native focus, visibility
   and application state are checked independently every five seconds; missing
   observations or a changed window state invalidate acceptance. Native key
   focus requires an unlocked desktop. A locked login screen produces an
   explicit failure, cleans up only the test processes and records no pass.
6. It sends 100 native AppKit key-down/up pairs to that window. Each sample
   requires the corresponding PTY echo in the terminal's paint, and ends on
   the next UI-thread turn after the frame. The harness neither sends keys
   to another application nor requires Accessibility permission. The
   measurement excludes hardware and OS event-queue latency and does not
   establish compositor presentation. Queue/scheduler delay after drawing
   remains included. The paint-matching hook is enabled only during these
   key samples. It is then disabled before the test window resigns key focus
   for a separate visible/unfocused idle sample, with the same settle and idle
   durations. The harness refocuses only its own window before streaming
   output for 10 s (`PERF_BUSY`) for frame timings. Native focus queries still
   add small diagnostic overhead; neither idle phase nor streaming scans
   terminal tails, writes paint snapshots or matches keys on each frame.
7. It closes the panes, stops kerod and the app, and removes the directory.
   Anything still running from it is named instead (`PERF_KEEP=1` keeps
   the directory).

`scripts/perf_probe.py` reads each process with `proc_pid_rusage`, which
macOS answers for the user's own processes without root:

- **Footprint** is the physical footprint, what Activity Monitor's Memory
  column and `footprint(1)` show. RSS goes beside it.
- **CPU** is user and system time over the window, as a percentage of one
  core. The times come in Mach ticks on Apple silicon; the probe converts
  them.
- **Wakeups** are the interrupt wakeups: every time a thread of the process
  was woken. The package idle wakeups, those that woke an idle CPU package
  (Activity Monitor's "Idle Wake Ups", `top`'s `IDLEW`), go beside them.
  They depend on what else keeps the machine awake, and read 0 on a
  working Mac, so the budget uses the interrupt wakeups. `powermetrics`
  reads both too, but needs root.

The frame times are MyGo's (`ui/framestats.go`): build, layout, paint and
present of each frame, logged with `MYGO_FRAME_STATS=all`.

Shorter `PERF_IDLE` and `PERF_SETTLE` are for checking the script itself.
The numbers below use the defaults.

### Memory after visiting every terminal

```sh
PERF_MEMORY=1 PERF_KEEP=1 sh scripts/perf.sh 25
PERF_MEMORY=1 PERF_KEEP=1 PERF_LAYOUT=split sh scripts/perf.sh 25
```

The optional memory workload starts the same app with no sessions and verifies
that it has no selection or attached terminals before taking its baseline. It
then feeds every fixture, visits each terminal twice, streams every fixture,
and waits `PERF_SETTLE` seconds (15 by default) after output stops. `PERF_LAYOUT=split` keeps all panes in
one selected session instead of separate sessions. It uses the real `pane.next`
and `pane.zoom` actions to visit every pane zoomed, then expands the layout
before streaming. Minimum pane sizes clip part of a 25-tile layout in the
1180 × 760 window; this is not a claim that all 25 tiles were exposed at once.
The selected session is exempt from the eight-terminal cache limit, so this
case measures retained terminals that the ordinary one-shown-pane workload
misses.

`scripts/perf_memory.py` adds diagnostics through a temporary Go build overlay;
normal builds have no new listener or hook. A visit requires the requested
selection, a positive grid, a fresh pane-specific `DrawOver` callback with
positive geometry, the fixture's final `PERF-READY` marker in its bounded text
tail, and a newer completed frame. This proves paint traversal and that the
terminal consumed the fixture, not independent compositor presentation or
physical window exposure. Focus and activation come from the app's callbacks,
separately from frame counts.

Every stage preserves process counters, Go `MemStats` and a post-GC heap
profile. The first-shown, completed-sweep and settled stages additionally
capture a `vmmap` summary and individual mappings, and `footprint` JSON. Files
live under `memory/` in the temporary directory printed at exit;
`PERF_KEEP=1` keeps them.

Normal app runs use this GUI measurement by default. `PERF_GUI=0` keeps the
older uninjected workload; its window focus is not independently verified,
so it cannot establish focused or unfocused acceptance and reports key latency
as unmeasured. `PERF_APP=0` measures kerod alone. `PERF_MEMORY=1`
selects the separate memory workload instead. The native GUI code and
loopback listener exist only in the temporary build overlay, never in the
normal or released application. Detailed samples are preserved as `gui.json`
when `PERF_KEEP=1` is set.
Native tools have their own timestamps and totals, with process samples before
and after each tool. A category from one snapshot must not be subtracted from
a process total taken at another time. IOSurface region counts are mappings,
not a count of unique surfaces.

Sizes in this section use MiB (2^20 bytes), called MB by the harness. The
aggregate app target at 25 panes is 750 MiB. The report keeps that total
separate from the empty-app baseline and `(total - baseline) / distinct panes
confirmed so far`; the denominator is zero before any visit. These within-run
deltas can become negative as native graphics allocations change, and do not
measure a terminal's individual allocation. The ordinary harness's existing
30 MB row compares the whole app with a per-pane budget without an empty-app
baseline; it is not the aggregate 25-pane acceptance check.

Forced GC, bounded text-tail reads and native profiling perturb these runs.
Their CPU and frame rates are diagnostic data, not idle or streaming latency
acceptance. `GOMEMLIMIT`, `GOGC` and `GODEBUG`, when inherited, are recorded; this
mode sets none of them.

## Profiling kerod

With `KERO_PPROF` set to a loopback address, kerod serves Go's profiles
there while it runs, and stops with it:

```sh
PERF_PPROF=127.0.0.1:6060 sh scripts/perf.sh 4      # the run's kerod
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=30
go tool pprof -tagfocus kerod=pane.screen http://127.0.0.1:6060/debug/pprof/profile?seconds=30
curl -o trace.out 'http://127.0.0.1:6060/debug/pprof/trace?seconds=5' && go tool trace trace.out
```

Only a loopback address is served: `127.0.0.1`, `[::1]` or `localhost`.
An address of every interface (`:6060`, `0.0.0.0:6060`) or of another
machine is refused, and so is a port in use; kerod logs why in
`kerod.log` and runs without. While it serves, kerod labels its frequent
work `kerod=<name>`, so a profile can tell it apart: `pane.tick`,
`pane.screen`, `saveHistories` and `attachment.send`. Unset, the labels
cost nothing; `TestProfiledFree` checks that they allocate nothing.

Set `KERO_PPROF` on a kerod only when you start it yourself, as
`perf.sh` does. The daily app's kerod is started by the app.

## Baseline

Historical tables below retain the physical-footprint criterion used when
they were recorded. That retired criterion is not the current app RSS verdict.
The early GUI overlay also recorded every paint during idle and streaming;
its timings include that instrumentation. Entries labeled focused used cached
App flags, without independent native key-window verification. Their numbers
are preserved as diagnostic observations, not native-focused acceptance.
Current GUI runs query fresh native window state and isolate the costly
paint-matching hook to the native-key phase.

Measured 2026-10-22 on an Apple M4 Pro (24 GB, macOS 27.0.1), on
`shibu` at c8ac9bc with this harness (labels off: `KERO_PPROF` unset),
with the defaults: 16 MB fed to each pane, 15 s settle, 60 s idle, 10 s
streaming. The daily app and its agents ran beside it, as they always do.

| Measure | 1 pane | 4 panes | 8 panes | Budget |
| --- | --- | --- | --- | --- |
| kerod footprint | 28.8 MB | 40.2 MB | 57.7 MB | |
| kerod footprint per pane (over 13.1–13.5 MB with none) | 15.3 MB | 6.8 MB | 5.6 MB | ≤ 25 MB: ok |
| kerod RSS | 31.7 MB | 64.3 MB | 74.4 MB | |
| kerod idle CPU | 0.03 % | 0.05 % | 0.08 % | |
| kerod idle wakeups (interrupt) | 870/min | 1,005/min | 1,521/min | |
| app footprint, one pane shown | 157.3 MB | 297.8 MB | 271.0 MB | retired footprint criterion: over; not the current RSS verdict |
| app RSS | 116.0 MB | 169.7 MB | 153.0 MB | |
| app idle CPU | 0.24 % | 0.52 % | 0.40 % | |
| app idle wakeups (interrupt) | 2,300/min | 10,060/min | 8,626/min | |
| app + kerod idle CPU | 0.27 % | 0.58 % | 0.48 % | ≤ 1 %: ok |
| app + kerod idle wakeups | 3,170/min | 11,066/min | 10,148/min | ≤ 30/min: over, by 100× and more |
| app idle frames | 31/min | 127/min | 70/min | < 10/min: over |
| frame build p50 / p99, streaming | 0.1 / 0.8 ms | 0.1 / 0.8 ms | not measured | p99 ≤ 4 ms: ok |
| frame paint p50 / p99, streaming | 0.3 / 0.5 ms | 0.3 / 0.5 ms | not measured | |
| frame total p50 / p99, streaming | 0.4 / 1.2 ms (837 frames) | 0.4 / 1.2 ms (820 frames) | not measured | |
| key press → paint p99 | not measured | not measured | not measured | ≤ 50 ms |

The package idle wakeups read 0 to 246 a minute in these runs. The tests'
allocation costs on the same commit: `screen()` 4.4 KB in 37 objects a
call at 1 MB and at 16 MB of scrollback (the whole text: 85 KB and 1.5 MB);
a key's echo 35 KB in 913 objects on kerod's side and 12.9 KB in 230
objects on the app's, at 1 MB and at 16 MB.

What the numbers say:

- kerod is within its memory budget: a full 4 MB scrollback costs it 6 to
  15 MB a pane. The first pane costs the most, probably because it
  also carries what kerod sets up once for any pane.
- The app is far over its memory budget with a single pane shown: 110 to
  300 MB, and it moves by 150 MB from run to run. That is BRU-333's work.
- Idle CPU is within budget, but wakeups are not: kerod wakes about 15 to
  25 times a second (its 400 ms tick, and Go's runtime), and the app 40 to
  170 times. Idle frames are over budget too: the cursor blink, every
  600 ms, alone accounts for about 100 frames a minute (BRU-225,
  BRU-331). The 1-pane run's 31 a minute suggests its window was hidden
  for part of the minute; a shown window logged 135 to 143 a minute in
  shorter trial runs.
- Frames are cheap when they come: a p99 build under 1 ms while a pane
  streams.
- At 8 panes the app's window was hidden during each run (no frame at
  all, even while its pane streamed; two more runs read the same), so its
  frame times there are missing and its idle frames count too few. The
  harness says so rather than judge them.

### After the cursor blink paints (20dc41c)

Two runs at 4 panes on `shibu` at 20dc41c, which pins MyGo 1c5a2cea: a
focused terminal's cursor blinks by painting the last frame again,
without building the window (BRU-225). Same machine and defaults as
above. The second run counts the frames that build the window apart from
those that only paint it again.

| Measure | c8ac9bc | 20dc41c, run 1 | 20dc41c, run 2 | Budget |
| --- | --- | --- | --- | --- |
| app idle frames that build the window | 127/min (all frames) | 104/min (all frames) | 25/min | < 10/min: over |
| app idle frames painted again | — | — | 100/min | not judged |
| app + kerod idle wakeups | 11,066/min | 5,595/min | 11,697/min | ≤ 30/min: over |
| app footprint, one pane shown | 297.8 MB | 162.8 MB | 279.8 MB | retired footprint criterion: over; not the current RSS verdict |
| frame total p99, streaming | 1.2 ms | 1.2 ms | 1.5 ms | |

- The blink no longer builds the window: its 100 frames a minute are now
  paint-only. About 25 frames a minute still build it while idle, which
  is over budget and not yet traced.
- Wakeups did not move: the two runs differ by a factor of two with no
  change between them, so a single run says little. Cutting them is
  BRU-336.
- The app's footprint moves as much between runs as before.

### Idle wakeups (BRU-336)

Measured 2026-10-22 on the same Mac, defaults as above, on
`fix/bru-336-wakeups` off `shibu` at 179327e. Each app run is classed by
its `app.log`: **unfocused** (frames built, none painted again: the
window shows, without the focus, so the cursor does not blink),
**focused** (about 100 frames a minute painted again: the cursor blinks)
or **hidden** (no frame). `perf.sh` does not choose which; macOS does, as
the window opens. Wakeups are interrupt wakeups a minute.

**What woke them.**

- *The Go runtime.* A Go program with nothing to do wakes about 2 times a
  minute. A `time.Ticker` costs about 3 wakeups a fire: 450 a minute at
  400 ms, 90 at 2 s, 38 at 5 s, 19 at 10 s (a 60 s probe of a test
  program). A syscall while the runtime's sysmon sleeps wakes sysmon too,
  which then polls every 20 µs until every P is idle again.
- *kerod.* A 20 s `runtime/trace` of an idle kerod with 4 panes
  (`PERF_PPROF`) shows the 400 ms `tickLoop` as nearly all its scheduled
  work: 50 ticks, each with about 5 syscalls (`TIOCGPGRP` for each pane,
  and some `procName` and `procCwd` reads). The 10 s history save fired
  twice, the Linear poll once (no key: it returns at once), and the GC
  did not run. 150 ticks a minute, with their syscalls, account for the
  1,000 a minute.
- *The app.* A 20 s trace of the app shows little Go work: the main
  thread ran 105 times and the Metal presenter 38, of which the blink was
  33 paints; the meters' reader ran 7 times. Most of the app's wakeups are
  in AppKit, Core Animation and Metal threads, which run for each frame.
  A minimal MyGo window (`examples/counter-native` from the pinned MyGo,
  its own `HOME` and `TMPDIR`) wakes 39 times a minute at rest. Each frame
  of Shibu's window costs about 100 to 150 wakeups, and each blink paint
  about 85. So the app's wakeups at rest are its frames:
  - unfocused, 13 to 16 frames built a minute, 12 of them for the CPU and
    RAM meters (a reading every 5 s whose labels change);
  - focused, the same, plus 90 to 100 blink paints a minute.

  With the meters turned off (a test build only), the app read 564 a
  minute unfocused, at 1 frame built a minute.

**What changed.** kerod ticks every 400 ms only while something happens
in a pane, and every 10 s after 10 s with nothing. It writes the
histories only after their text changed. The meters read every 30 s while
the window does not have the focus. See
[Changes › Daemon idle work](changes.md#daemon-idle-work).

kerod alone (`PERF_APP=0`):

| Run | Panes | kerod idle wakeups | kerod idle CPU |
| --- | --- | --- | --- |
| 179327e | 1 | 1,104/min | 0.05 % |
| 179327e | 4 | 973/min | 0.05 % |
| after | 1 | 67/min | 0.01 % |
| after | 4 | 99/min | 0.01 % |
| after | 4 | 201/min | 0.02 % |
| after, `GODEBUG=gctrace=1,scavtrace=1` | 4 | 161/min | |

The run with the GC trace logged no GC and no scavenger work during the
idle minute.

App and kerod, 4 panes unless the run says otherwise:

| Run | Window | kerod | app | app + kerod | app idle CPU | frames built / painted again |
| --- | --- | --- | --- | --- | --- | --- |
| 179327e, 1 | unfocused | 1,069 | 1,999 | 3,068/min | 0.35 % | 13 / 0 |
| 179327e, 2 | unfocused | 1,090 | 2,487 | 3,577/min | 0.35 % | 14 / 0 |
| kerod change only | focused | 271 | 10,437 | 10,707/min | 0.65 % | 16 / 90 |
| after, 1 | unfocused | 148 | 751 | 899/min | 0.10 % | 4 / 0 |
| after, 2 | unfocused | 230 | 922 | 1,152/min | 0.11 % | 4 / 0 |
| after, 3 | focused | 126 | 9,410 | 9,536/min | 0.60 % | 13 / 99 |
| after, 1 pane | focused for part of the minute | 281 | 2,900 | 3,182/min | 0.20 % | 8 / 12 |
| after, 8 panes | unfocused | 110 | 669 | 779/min | 0.07 % | 4 / 0 |

The second run on 179327e was built with a trace hook that does nothing
unless an environment variable is set; it was not. The "kerod change
only" run had that hook on, for the app's trace.

- In the case the issue judges, 4 panes with the window shown and
  unfocused, app + kerod went from 3,068 to 3,577 a minute down to 899 to
  1,152: about a third. kerod went from about 1,000 to 100 to 230, and
  the app from 2,000 to 2,500 to 750 to 920. The app's idle CPU went from
  0.35 % to 0.10 %.
- Focused, the blink still costs about 8,500 a minute: 9,536 in all.
- kerod alone reads 67 a minute with 1 pane and 99 to 201 with 4. The
  spread at 4 panes is not explained: the trace of a run beside the app
  shows the tick waking every 10 s, as it should. Beside the app kerod
  reads 110 to 281: each focus change and request of the app makes its
  tick fast for 10 s.

**What is achievable.** 30 a minute is not, with this design, for these
reasons:

- A bare MyGo window wakes 39 times a minute at rest.
- A Go process with even one timer every 10 s wakes about 20 times a
  minute.
- kerod must poll each pane's foreground group (`TIOCGPGRP`): macOS has no
  event for it.
- Each frame of the window costs 100 or more wakeups. So does any rebuild
  the app shows, such as a time-ago label that moves once a minute.

The unfocused runs after the change read 779 to 1,152 a minute for app +
kerod (kerod 110 to 230 beside the app, 67 to 281 in all runs; the app 669
to 922), and runs vary by up to two times. A budget with room for that
would be **≤ 2,000 a minute** for app + kerod at rest, window shown and
unfocused, at 4 and 8 panes (kerod ≤ 500, app ≤ 1,500). The user agreed
to it on 2026-10-22, and the budget table above now says so. Going further
would need these steps:

- Done after this run: a focused terminal's cursor stops blinking 10 s
  after the last key (MyGo `81afe923`), which was about 8,500 of the
  focused window's 9,500 a minute. Not yet measured.
- Stop the meters' readings while the window does not have the focus,
  not only slow them (about 300 a minute).
- Follow the shell's children with `kqueue` (`EVFILT_PROC`,
  `NOTE_FORK`/`NOTE_EXEC`) instead of a slow poll.
- In MyGo, make a frame of an idle window cheaper.

The focused numbers above precede MyGo `81afe923`, which now stops the
blink after 10 seconds without a key or focus change and rests the cursor
shown. The next key or focus change starts it again. These older numbers
are the baseline, not a measurement of the timeout's saving. A focused
window still reads its meters every five seconds; an unfocused one reads
them every 30 seconds, so their remaining costs need not be equal.

After the timeout, zero blink paints no longer distinguishes a focused
window from an unfocused one. Record focus independently of the frame log.
A hidden window is not evidence for either visible-window case.


### After the cursor blink timeout (03a94a6)

Measured on the same Mac on 2026-10-22–23, with the default feed, settle,
idle and streaming durations. The checkout was fixed at `03a94a6` in a
separate worktree while other sessions continued working. macOS window
metadata and the frontmost application PID were sampled every five
seconds to check visibility and focus independently of the frame log.
The activation happens before the 15-second settle, so the cursor's
10-second timeout has expired before the idle sample starts. The daily
app and its terminal service were left running.

| Panes | Window | kerod wakeups/min | app wakeups/min | Combined/min | Combined CPU | Builds / blink paints per minute | Streaming build p99 | App footprint, one pane shown |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | visible, unfocused | 63 | 1,133 | 1,196 | 0.17 % | 4 / 0 | 1.1 ms | 253.7 MB |
| 4 | visible, unfocused | 61 | 730 | 790 | 0.08 % | 4 / 0 | 1.5 ms | 254.5 MB |
| 8 | visible, unfocused | 100 | 1,395 | 1,494 | 0.17 % | 4 / 0 | 1.8 ms | 234.5 MB |
| 4, repeat | visible, unfocused | 104 | 979 | 1,083 | 0.08 % | 3 / 0 | 1.0 ms | 121.8 MB |

The visible, unfocused 4- and 8-pane samples meet the agreed combined
2,000/min and individual 500/min kerod and 1,500/min app budgets. They
also meet the CPU, rebuild and streaming build budgets. The repeat
4-pane sample had 12 independent window-state observations during its
60.07-second idle interval: all visible and unfocused. These are samples
on a working Mac, not a statistical estimate of an improvement. App
footprint still exceeds the 30 MB budget with one pane shown; BRU-333
remains open. Key press to paint and the 12-pane case remain unmeasured.

Hidden-window and mixed-focus runs were excluded. No focused attempt
completed without focus changes, so the focused wakeup saving and the
focused idle-frame acceptance remain unverified.

`sh scripts/check.sh` also passed on the integrated `shibu` code at
`3c05209`: all Go tests, MyGo vet, the 24 Python tests and the diff check.
This includes the allocation guards; no new performance code was added
for these measurements.

The pinned MyGo regression test, `TestBlinkPaintsOnly`, was rerun on
2026-10-23: 0 builds and 17 paints through the first 10 seconds, then
0 builds and 0 paints over an idle minute after focus, after a key, and
after focus returned. This proves the terminal timer behavior, separately
from the whole app's native wakeups and meter rebuilds.

### Visited-terminal measurements (BRU-333)

Measured on the same M4 Pro on 2026-10-23, with 16 MiB fed per fixture,
10 seconds of output and 15 seconds settling after output. Baseline
`1368b83` pins MyGo `81afe923`; candidate `aa6f9a5` pins `43944857`.
The 1- and 8-session references use `724fddc`, which adds usage-test
synchronization without changing production code from `aa6f9a5`. All visit snapshots
reported the app active and focused. No `GOMEMLIMIT`, `GOGC` or `GODEBUG` was
set. These are instrumented memory workloads, not idle-budget measurements.

App physical footprint and Go heap are in MiB:

| Workload / code | Empty app | First shown | Sweep 1 | Sweep 2 | Streaming | Settled | Settled live Go heap | Retained terminals |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 session (`724fddc`) | 231.5 | 253.7 | 253.8 | 248.1 | 258.7 | 209.5 | 14.2 | 1 |
| 8 sessions (`724fddc`) | 106.9 | 269.0 | 298.2 | 296.0 | 324.6 | 252.2 | 26.4 | 8 |
| 25 sessions, before (`1368b83`) | 106.4 | 271.8 | 294.9 | 293.7 | 307.5 | 281.3 | 14.3 | 8 |
| 25 sessions, after (`aa6f9a5`) | 231.6 | 272.5 | 293.7 | 293.0 | 305.1 | 155.6 | 14.1 | 8 |
| 25 panes, one session (`aa6f9a5`) | 106.2 | 403.0 | 418.1 | 427.1 | 431.7 | 409.6 | 38.3 | 25 |

Both 25-session runs and the selected 25-pane run completed 51 confirmed
visits each: one first visit and two complete sweeps of 25 distinct panes.
Separate sessions retained eight terminals after each sweep; the selected
multi-pane session retained all 25, including after output stopped. The
1- and 8-session references completed 3 and 17 confirmed visits respectively.
The split workload visited every pane zoomed; the expanded streaming layout
had clipped tiles, so it does not prove all 25 were exposed simultaneously.

The 25-pane aggregate criterion is met in these synthetic runs: their stage
samples and native-tool totals stayed below 750 MiB. This is not an all-budget
pass. The ordinary whole-app 30 MiB row is still over with one pane; the
8-session reference also exceeds 240 MiB in these samples. The references
characterize fixed overhead and retention, not a redefinition of those budgets.

There is no demonstrated causal memory reduction from BRU-331: the two
25-session sweep samples are essentially equal. Empty-app footprint changed
from 106.4 to 231.6 MiB between runs, and the candidate settled sample is lower
than its own empty baseline. Its signed within-run delta is -3.04 MiB per
visited pane; that is unstable subtraction, not a negative terminal allocation
or an optimization saving.

Native accounting drifts too. The split settled process sample was 409.6 MiB;
its later `footprint` snapshot at 01:34:06 IST reported 298.1 MiB. That native
snapshot labeled about 103 MiB `App-Specific Tag 1`, 98 MiB `Untagged`, and
13.7 MiB `IOSurface`. These labels do not establish allocator ownership or
unique surface counts. They must not be combined with the earlier 409.6 MiB
total as a precise breakdown. The separate-session settled native snapshot
reported 155.2 MiB, with 48.8 MiB untagged and 31.5 MiB app-specific.

The split post-GC heap was 38.3 MiB; `Sys - HeapReleased`, a Go-runtime estimate
rather than OS accounting, was 87.8 MiB. Its sampled heap profile mainly
identified text shaping (`ui.Shape` about 18.6 MiB and terminal `shapeRun` about
3.5 MiB), with about 2 MiB in the terminal LRU. These bounded samples do not
show Go heap dominance or an unbounded cache. No memory limit or new cache
policy was added.

BRU-333 remains open for the historical 1.22 GB daily-app observation: this
synthetic workload did not reproduce or fully attribute it. The daily app's memory was
not inspected, and the app was not restarted. Unknown native/untagged memory remains unknown;
it is not assigned to libghostty by subtracting Go heap from footprint.

### Completion measurements (BRU-223, BRU-225, BRU-226, BRU-228, BRU-333)

Measured on 2026-10-23 in an isolated worktree at `7dcc432` with the
completion changes, on the same M4 Pro. The meters now read every ten
seconds while focused (six possible rebuilds per minute), rather than
every five (twelve). They still read every thirty seconds unfocused and
immediately when focus returns. This fixes the focused rebuild floor;
the resource card now states those actual intervals.

HEAD checks now run separately every 1.6 seconds. A branch switched by
another process must not wait for the expensive pane loop's quiet ten-second
tick. A regression that performs an atomic HEAD replacement while that loop
is quiet failed its two-second deadline before the change and passes after.
The daemon retains its quiet pane/sysctl cadence and thirty-second full
repository refresh.

The following sixty-second idle samples observed cached App focus and
activation flags every second. A later native-window check found that these
flags could stay true on a locked desktop. These samples therefore do not
establish native focus acceptance. The corrected harness checks the native
window every five seconds and fails closed while the desktop is locked.
Each key sample used direct native AppKit dispatch, the real PTY echo and the
next UI-thread turn after the matching frame. Hardware, OS queue and
compositor presentation remain outside the measurement.

| Fixtures | Window | Idle builds/min | App + kerod CPU | App / kerod interrupt wakeups/min | Native key p50 / p99 / max, 100 samples | App RSS / footprint |
| --- | --- | --- | --- | --- | --- | --- |
| 4 | cached focused/active flags only | 7 | 0.263 % | 1,745 / 440 | 11.60 / 20.55 / 45.91 ms | 106.1 / 255.5 MiB |
| 8 | cached focused/active flags only | 5 | 0.226 % | 1,428 / 426 | 12.21 / 16.93 / 25.31 ms | 144.0 / 126.1 MiB |

The numeric rebuild, CPU and direct-dispatch values are below their limits,
but native focused acceptance remains unverified. Wakeups in these diagnostic
samples cannot establish the agreed 2,000/min visible, unfocused budget. Both daemon samples remain
under its 500/min limit after the independent HEAD check. The ordinary GUI
fixture has one attached client terminal; it does not establish eight
retained terminals' cost.

The earlier eight-terminal visited workload at `724fddc` retained all eight
and measured RSS 222.9 MiB, below the literal BRU-226 aggregate RSS target of
240 MiB. Its physical footprint was 252.2 MiB. Those are distinct OS
counters: the previous report's footprint comparison did not establish an
RSS failure. The harness now reports both counters and judges the app's
per-pane RSS against the issue's RSS budget.

A fresh selected twenty-five-pane memory run retained all twenty-five
terminals and completed fifty-one confirmed visits. Physical footprint was
389.0 MiB first shown, 408.9/413.4 MiB after the two sweeps, 414.6 MiB after
streaming, and 267.3 MiB settled; every stage is below the 750 MiB aggregate
footprint target. Settled RSS was 289.5 MiB. Post-GC live Go heap was
33.4 MiB; `Sys - HeapReleased` was 65.8 MiB, a runtime estimate rather than
OS attribution. The sampled heap profile primarily identified `ui.Shape`
(18.6 MiB), terminal `read` (3.2 MiB), `shapeRun` (2.5 MiB) and the terminal
LRU (2.0 MiB).

The settled `footprint` snapshot independently totaled 267.3 MiB: 100.3 MiB
App-Specific Tag 1, 84.9 MiB Untagged, 13.7 MiB IOSurface, 4.3 MiB owned
unmapped graphics and 2.3 MiB IOAccelerator graphics; the other OS categories
account for the remaining 61.8 MiB. IOSurface had two mappings, not a proven
count of unique surfaces. These tags do not identify a native allocator;
neither App-Specific Tag 1 nor Untagged is claimed to be libghostty. The Go
heap snapshot is separate and must not be subtracted from this OS snapshot
to invent an exact native ownership breakdown.

The historical daily-app 1.22 GB observation remains unreproduced. The
released `2.0.7-local.1` predates the shared scrollback, eight-terminal cache,
32 MiB image cap and hidden-terminal redraw fixes. An isolated historical
comparison reached 397.6 MiB first shown, then was stopped to give the
combined release gate exclusive native-renderer access; it is incomplete.
No reduction is attributed to a particular fix from that partial sample.

The measured live Go heap and runtime commitment do not support a new
`GOGC` or `GOMEMLIMIT` value as a fix for unexplained native/GPU memory.
The app and daemon retain their explicit scrollback/image/cache budgets and
respect the Go runtime's existing environment controls. BRU-333 remains
open for exact native ownership and a justified memory policy; the passing
synthetic aggregate result does not close that investigation.

## Limits

- **Native key dispatch → paint** is measured by the default GUI workload.
  Hardware/OS event-queue latency and physical/compositor presentation remain
  outside its scope. `scripts/measure-terminal-latency.py` measures only the
  separate daemon transport part.
- **The ordinary app workload** shows one pane. Use the opt-in visited-terminal
  workload above for retained terminals, with its separate baseline and
  profiling limits. It does not reproduce every workload of the daily app.
- **The app's window** opens behind or in front of the user's windows, as
  macOS places it. Frames of a hidden window may cost less than those of a
  shown one.
- The numbers vary from run to run by what else the Mac does, most of all
  the idle CPU and wakeups. Compare runs on the same Mac, with the same
  apps open.
