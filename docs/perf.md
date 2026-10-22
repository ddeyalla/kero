# Performance budgets and measurement

What Shibu Code may cost, how to measure it with one command, and what it
cost when the measurement was added (BRU-223, the first step of BRU-222).
Every performance change records its numbers here, before and after.

## Budgets

| Measure | Budget | How it is measured |
| --- | --- | --- |
| Footprint per pane, kerod | ≤ 25 MB | `scripts/perf.sh`: kerod's footprint with N panes, less its footprint with none, divided by N |
| Footprint per pane, app | ≤ 30 MB | `scripts/perf.sh`: the app's footprint, one pane shown (see the limits below) |
| Idle wakeups, app + kerod | ≤ 30 a minute | `scripts/perf.sh`: interrupt wakeups over 60 s of idle |
| Idle CPU, app + kerod | ≤ 1 % | `scripts/perf.sh`: CPU time over 60 s of idle, of one core |
| Idle frames, app | < 10 a minute that build the window | `scripts/perf.sh`: frames `MYGO_FRAME_STATS=all` logs over 60 s of idle. A frame that only paints the last one again ("painted again"), as a focused terminal's cursor blinks, is counted apart and not judged |
| p99 frame build | ≤ 4 ms | `scripts/perf.sh`: `MYGO_FRAME_STATS=all` while a shown pane streams output for 10 s |
| p99 key press → paint | ≤ 50 ms | Not measured yet: it needs keys sent to the window |
| Allocations of `screen()` | Flat with the scrollback; ≤ 16 KB a call | `TestScreenAllocsFlatWithScrollback` |
| Allocations of a key's echo | Flat with the scrollback; ≤ 52 KB in 1,400 objects (kerod), ≤ 20 KB in 350 objects (app) | `TestFrameAllocsFlatWithScrollback` |

The budgets hold at 1, 4, 8 and 12 panes, each with a full scrollback
(BRU-222). The last two rows are tests, which `go test ./...` runs, so a
regression fails the checks without the app.

## Measuring

```sh
sh scripts/perf.sh 4      # 4 panes
```

It takes about two minutes, and prints a table with each measure, its
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
4. It starts the app, the same build, with `MYGO_FRAME_STATS=all` and a
   `TMPDIR` of its own. Its name and identity are not the daily app's or a
   dev app's, and its single-instance lock is in that `TMPDIR`, so it
   neither finds nor disturbs them. It opens a window for the run.
   `PERF_APP=0` leaves it out.
5. It waits 15 s (`PERF_SETTLE`), past kerod's 10 s history save, then
   samples kerod and the app over 60 s of idle (`PERF_IDLE`).
6. The pane the app shows streams output for 10 s
   (`PERF_BUSY`), for the frame times.
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
| app footprint, one pane shown | 157.3 MB | 297.8 MB | 271.0 MB | ≤ 30 MB a pane: over |
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
| app footprint, one pane shown | 297.8 MB | 162.8 MB | 279.8 MB | ≤ 30 MB a pane: over |
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
unfocused, at 4 and 8 panes (kerod ≤ 500, app ≤ 1,500). This is a
proposal, provisional on more runs, and it needs the user's agreement;
until then the table above still says 30. Going further would need these
steps:

- Stop the meters' readings while the window does not have the focus,
  not only slow them (about 300 a minute).
- Follow the shell's children with `kqueue` (`EVFILT_PROC`,
  `NOTE_FORK`/`NOTE_EXEC`) instead of a slow poll.
- In MyGo, make a frame of an idle window cheaper.

Focused, the blink is the cost: a cursor that stops blinking after some
seconds without a key, as GTK's `gtk-cursor-blink-timeout` (10 s) does,
would take the focused case to about the unfocused one. That is a change
of MyGo's terminal plugin, and of what the Blink setting means, so it is
not done here.

## Limits

- **Key press → paint** is not measured: it needs keys sent to the app's
  window, which the harness does not do. The transport part, from a key
  sent to kerod to the frame it sends back, is what
  `scripts/measure-terminal-latency.py` measures.
- **The app's footprint** is measured with one pane shown. The app keeps
  only the terminals of panes it showed (`maxTerms`), so its footprint with
  N panes is close to its footprint with one, and the per-pane budget
  applies to the panes shown. A run that visits every pane is not done yet.
- **The app's window** opens behind or in front of the user's windows, as
  macOS places it. Frames of a hidden window may cost less than those of a
  shown one.
- The numbers vary from run to run by what else the Mac does, most of all
  the idle CPU and wakeups. Compare runs on the same Mac, with the same
  apps open.
