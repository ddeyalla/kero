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
| Idle frames, app | < 10 a minute | `scripts/perf.sh`: frames `MYGO_FRAME_STATS=all` logs over 60 s of idle |
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
