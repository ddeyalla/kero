# Herdr ideas for Kero

Reviewed 2026-10-20 against `penso/herdr-gpui` commit `0ca50aa` and the
current Herdr daemon source. Keep Kero's native Go/MyGo UI and libghostty-vt
terminal. These are recommendations, not an authorization to build the whole
Herdr feature set.

## What to borrow

| Priority | Feature | Kero-sized implementation | Decision |
| --- | --- | --- | --- |
| Now | Plan usage in a modal | Existing sidebar readout opens a scrollable dialog over the terminal | Implemented in local.2 |
| Now | Host CPU and memory | Quiet sidebar footer strip, details in the same dialog; background sampling | Implemented for this Mac |
| Now | Keyboard access | Account Usage action, Cmd+Shift+U; Cmd+R refresh and Esc/Cmd+W close in the dialog | Implemented |
| Next | Review Changes | A focused diff viewer for the current project, with selected lines sent to the agent as a draft | Best next feature |
| Next | File preview | Cmd+click a printed file path to preview text/images without leaving the terminal | High daily value |
| Next | Worktree actions | Show branch and dirty state; create/open a worktree from a project menu | Add after review/preview |
| Later | Listening ports | A small project menu listing local dev servers with Open in Browser | Useful, bounded |
| Later | Actionable notifications | Existing Kero notification opens the exact pane needing attention | Refine what already exists |
| Later | Remote machines | Separate SSH connections, status, file upload and recovery | Significant scope; defer |

Avoid copying Herdr's full browser, pull-request client, cloud-host control,
teleport, or orchestration system into this first Kero fork. Each adds its own
state, failure modes and maintenance load. Kero already has persistent sessions,
agent status, a command palette, split panes, CLI control, themes and notifications.

If source code is copied later, preserve Herdr's Apache-2.0 license and applicable
NOTICE attribution. The current metrics and dialog changes are Go implementations
using Kero's own UI patterns; no Rust renderer was copied.

## Which terminal Herdr uses

Herdr's daemon uses its `ghostty-vt` crate, a binding to vendored
**libghostty-vt**. Herdr GPUI does not start another emulator in the GUI: it
receives the daemon's cells and paints them with Rust/GPUI. Kero also uses
libghostty-vt, through MyGo. Sharing a terminal core does not imply matching
input scheduling, parsing overhead, rendering cost, or frame presentation.

Sources:

- https://github.com/herdrdev/herdr/blob/master/crates/ghostty-vt/Cargo.toml
- https://github.com/penso/herdr-gpui/blob/main/README.md
- https://github.com/penso/herdr-gpui/blob/main/crates/herdr-gpui/PERFORMANCE.md
- https://github.com/penso/herdr-gpui/blob/main/crates/herdr-gpui/src/terminal_painter.rs
- https://github.com/penso/herdr-gpui/blob/main/crates/herdr-gpui/src/system_load.rs

## Optimization lessons

Herdr's painter caches shaped glyph/style results, batches cell backgrounds,
limits painting to the requested areas, and distinguishes terminal repaint from
whole-window updates. Its performance fixture uses a dense 160x50 terminal and
40 workspaces/agents, validates cache correctness, and measures CPU event-to-scene
work. Its published numbers do not measure GPU completion or key-to-screen
latency and should not be presented as a benchmark of this Mac or Kero.

MyGo already caches terminal rows and shaped text. Replacing it wholesale with
GPUI would be a rewrite, not a small optimization. Profile the actual bottleneck
and retain targeted regression coverage instead.

## Kero typing-delay finding and fix

An agent can start a synchronized redraw (`CSI ?2026h`), keep it open beyond
the initial 4ms output timer, and finish (`CSI ?2026l`) shortly afterwards.
Previously the first timer scheduled a 50ms retry; `framing=true` then prevented
the completed redraw from advancing that retry. The screen could stay stale for
tens of milliseconds even though the agent had finished drawing.

The local.2 scheduler keeps the earliest deadline. New output advances a later
hold retry to the usual 4ms coalescing delay. It preserves synchronized-output
atomicity and prevents bursts from continually postponing a frame. A fake-clock
regression test failed before the fix and passes after it, without a flaky
wall-clock threshold.

The isolated mirror-plus-terminal-view benchmark on this M4 Pro measured about
0.24ms for 80x24 and 1.17ms for 280x70 before the scheduler fix. This benchmark
excludes socket, OS input delivery, window composition and GPU presentation;
it does not prove end-to-end typing latency. The scheduler fix removes the
reproduced hold delay, not every possible source of lag.

A separate live transport probe ran the same raw-PTY program in isolated
daemons of the installed upstream ARM64 build and local.2. The program holds
each synchronized redraw for 10ms, then completes it. Fifteen samples per build:

| Input to mirrored output frame | Upstream 2.0.5 | Local.2 |
| --- | ---: | ---: |
| Median | 55.93ms | 16.90ms |
| p95 | 69.32ms | 18.15ms |
| Minimum | 55.24ms | 14.75ms |

This is a short transport probe, not a statistically broad typing benchmark.
It includes the intentional 10ms program hold and excludes GUI/GPU presentation.
Reproduce with `python3 scripts/measure-terminal-latency.py <before-executable>
<after-executable>`. The script creates and cleans up private daemons, sessions
and shell configuration; it never sends input to the user's sessions.

Two remaining profiling candidates are full-scrollback text extraction during
agent status detection and cached, hidden terminals invalidating their former
window on output. Both require workload-specific evidence before changing the
terminal library or session caching policy.

## Architecture

Apple Silicon = **arm64/aarch64**. Intel/AMD x86-64 = **amd64/x86_64**.
The earlier installed Kero process was translated Intel. A fresh process sampled
during this pass was already ARM64; architecture alone cannot explain remaining
lag. Kero Local builds explicitly for the host and remains separate from the
original application.
