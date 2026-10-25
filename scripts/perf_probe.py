"""Measure processes and frame logs for scripts/perf.sh.

    perf_probe.py sample PID...            one JSON object of each process's counters
    perf_probe.py frames LOG [FROM [TO]]   the MYGO_FRAME_STATS lines of LOG, from
                                           line FROM to line TO, summarized as JSON
    perf_probe.py report ARGS              the table perf.sh prints (see --help)

A sample reads proc_pid_rusage(RUSAGE_INFO_V2), which macOS answers for the
user's own processes without root: the physical footprint (what Activity
Monitor and footprint(1) call memory), the resident size, CPU time and the
package idle wakeups (Activity Monitor's "Idle Wake Ups"), with the
interrupt wakeups beside them. It is macOS only; the parsing and the table
are not, so their tests run anywhere.
"""

import argparse
import json
import math
import re
import sys
import time

# A frame of MYGO_FRAME_STATS, as MyGo's ui/framestats.go logs it:
#   mygo: frame 12 took 9.1 ms: build 2.0 (2 passes), layout 0.3, paint 4.1,
#   present 1.2 (partial), other 1.5; meanwhile …
FRAME = re.compile(
    r"mygo: frame (?P<frame>\d+) took (?P<total>[\d.]+) ms: "
    r"build (?P<build>[\d.]+)(?: \((?:(?P<again>painted again)|\d+ passes)\))?, "
    r"layout (?P<layout>[\d.]+), paint (?P<paint>[\d.]+), present (?P<present>[\d.]+)"
)


def parse_frame(line):
    """The times of a logged frame, in milliseconds, or None. Its
    "repaint" is true for a frame that painted the last one again, without
    building or laying it out, as a cursor's blink does."""
    m = FRAME.search(line)
    if not m:
        return None
    f = {k: float(m.group(k)) for k in ("total", "build", "layout", "paint", "present")}
    f["repaint"] = m.group("again") is not None
    return f


def percentile(values, p):
    """The nearest-rank percentile p (0-100) of values, or None when empty."""
    if not values:
        return None
    ordered = sorted(values)
    rank = max(1, math.ceil(p / 100 * len(ordered)))
    return ordered[rank - 1]


def frames(lines):
    """A summary of the frames among lines: their count, how many of them
    only painted again, and the p50 and p99 of each part."""
    parsed = [f for f in (parse_frame(l) for l in lines) if f]
    out = {"count": len(parsed), "repaints": sum(f["repaint"] for f in parsed)}
    for part in ("total", "build", "paint"):
        values = [f[part] for f in parsed]
        out[part] = {"p50": percentile(values, 50), "p99": percentile(values, 99)}
    return out


RUSAGE_V2 = 2


def _rusage_reader():
    import ctypes
    import ctypes.util

    class RusageInfoV2(ctypes.Structure):
        _fields_ = [
            ("uuid", ctypes.c_uint8 * 16),
            ("user_time", ctypes.c_uint64),
            ("system_time", ctypes.c_uint64),
            ("pkg_idle_wkups", ctypes.c_uint64),
            ("interrupt_wkups", ctypes.c_uint64),
            ("pageins", ctypes.c_uint64),
            ("wired_size", ctypes.c_uint64),
            ("resident_size", ctypes.c_uint64),
            ("phys_footprint", ctypes.c_uint64),
            ("proc_start_abstime", ctypes.c_uint64),
            ("proc_exit_abstime", ctypes.c_uint64),
            ("child_user_time", ctypes.c_uint64),
            ("child_system_time", ctypes.c_uint64),
            ("child_pkg_idle_wkups", ctypes.c_uint64),
            ("child_interrupt_wkups", ctypes.c_uint64),
            ("child_pageins", ctypes.c_uint64),
            ("child_elapsed_abstime", ctypes.c_uint64),
            ("diskio_bytesread", ctypes.c_uint64),
            ("diskio_byteswritten", ctypes.c_uint64),
        ]

    class Timebase(ctypes.Structure):
        _fields_ = [("numer", ctypes.c_uint32), ("denom", ctypes.c_uint32)]

    libc = ctypes.CDLL(ctypes.util.find_library("c") or "libc.dylib", use_errno=True)
    libproc = ctypes.CDLL(ctypes.util.find_library("proc") or "libproc.dylib", use_errno=True)
    tb = Timebase()
    libc.mach_timebase_info(ctypes.byref(tb))
    # CPU times come in Mach absolute time units: nanoseconds on Intel,
    # ticks of 125/3 ns on Apple silicon.
    scale = tb.numer / tb.denom

    def read(pid):
        info = RusageInfoV2()
        if libproc.proc_pid_rusage(int(pid), RUSAGE_V2, ctypes.byref(info)) != 0:
            return None
        return {
            "cpu_ns": (info.user_time + info.system_time) * scale,
            "idle_wakeups": info.pkg_idle_wkups,
            "interrupt_wakeups": info.interrupt_wkups,
            "footprint": info.phys_footprint,
            "resident": info.resident_size,
        }

    return read


def sample(pids):
    read = _rusage_reader()
    return {"time": time.time(), "pids": {str(p): read(p) for p in pids}}


def idle_rates(before, after, pid):
    """CPU %, idle and interrupt wakeups a minute of pid between two
    samples, or None when either misses it."""
    a, b = before["pids"].get(str(pid)), after["pids"].get(str(pid))
    seconds = after["time"] - before["time"]
    if not a or not b or seconds <= 0:
        return None
    return {
        "cpu": (b["cpu_ns"] - a["cpu_ns"]) / (seconds * 1e9) * 100,
        "idle_wakeups": (b["idle_wakeups"] - a["idle_wakeups"]) / seconds * 60,
        "interrupt_wakeups": (b["interrupt_wakeups"] - a["interrupt_wakeups"]) / seconds * 60,
        "seconds": seconds,
    }


MB = 1 << 20


def _mb(n):
    return f"{n / MB:.1f} MiB"


def _ms(v):
    return "n/a" if v is None else f"{v:.1f} ms"


def _verdict(value, limit):
    if value is None or limit is None:
        return ""
    return "ok" if value <= limit else "OVER"


def validate_daemon_fixture(panes, sessions):
    """The isolated fixture must contain N distinct terminal PTYs, not views."""
    if type(panes) is not int or panes < 1:
        raise ValueError("fixture panes must be a positive integer")
    ids = []
    for session in sessions:
        for pane in session["panes"]:
            if pane.get("view") or not pane.get("id"):
                raise ValueError("fixture must contain only terminal panes with IDs")
            ids.append(pane["id"])
    if len(ids) != panes or len(set(ids)) != panes:
        raise ValueError("fixture terminal count does not match requested panes")
    return len(ids)


def report(panes, fed, kerod, base, t0, t1, app=None, app_reason="", idle_frames=None, busy_frames=None, key_latency=None, idle_window=None):
    """The table perf.sh prints, as lines. base is a sample of kerod with
    no pane, t0 and t1 bound the idle window; app is the app's pid.

    Wakeups are the interrupt wakeups: every time the process was woken.
    The package idle wakeups, those that woke an idle CPU package (Activity
    Monitor's "Idle Wake Ups"), go beside them; they depend on what else
    keeps the machine awake, and are 0 on a busy one."""
    if type(panes) is not int or panes < 1:
        raise ValueError("fixture panes must be a positive integer")
    rows = []

    def row(name, value, budget="", verdict=""):
        rows.append((name, value, budget, verdict))

    def wakeups(r):
        return f"{r['interrupt_wakeups']:.0f}/min  ({r['idle_wakeups']:.0f}/min package idle)"

    k1 = t1["pids"].get(str(kerod))
    k0 = base["pids"].get(str(kerod))
    if k1:
        row("kerod footprint", _mb(k1["footprint"]))
        row("kerod resident (RSS)", _mb(k1["resident"]))
        per_rss = k1["resident"] / panes
        row("kerod resident (RSS) per pane", _mb(per_rss), "<= 25 MiB",
            _verdict(per_rss, 25 * MB))
        if k0:
            per = (k1["footprint"] - k0["footprint"]) / panes
            row("kerod footprint per pane", _mb(per) + f"  (over {_mb(k0['footprint'])} with none; diagnostic)")
    r = idle_rates(t0, t1, kerod)
    if r:
        row("kerod idle CPU", f"{r['cpu']:.2f} %")
        row("kerod idle wakeups", wakeups(r), "<= 500/min" if idle_window == "unfocused" else "",
            _verdict(r["interrupt_wakeups"], 500) if idle_window == "unfocused" else "")
    if app:
        a1 = t1["pids"].get(str(app))
        if a1:
            row("app footprint (1 pane shown)", _mb(a1["footprint"]))
            row("app resident (RSS)", _mb(a1["resident"]))
            # Fixture sessions are not retained client terminals. The ordinary
            # workload shows only one, so dividing by panes can invent a pass.
            row("app retained-terminal RSS budget", "not measured (use PERF_MEMORY=1)", "<= 30 MiB/pane")
        ra = idle_rates(t0, t1, app)
        if ra:
            row("app idle CPU", f"{ra['cpu']:.2f} %")
            judge_wakeups = idle_window == "unfocused"
            row("app idle wakeups", wakeups(ra), "<= 1500/min" if judge_wakeups else "",
                _verdict(ra["interrupt_wakeups"], 1500) if judge_wakeups else "")
            if r:
                cpu = ra["cpu"] + r["cpu"]
                woke = ra["interrupt_wakeups"] + r["interrupt_wakeups"]
                row("app + kerod idle CPU", f"{cpu:.2f} %", "<= 1 %", _verdict(cpu, 1))
                scope = "focused: reported separately" if idle_window == "focused" else "focus unverified: not judged"
                row("app + kerod idle wakeups", f"{woke:.0f}/min", "<= 2000/min" if judge_wakeups else scope,
                    _verdict(woke, 2000) if judge_wakeups else "")
        if idle_frames is not None and r:
            # The budget is for frames that build the window. A frame that
            # only paints the last one again, as a focused terminal's
            # cursor blinks, is counted apart and not judged.
            per_min = 60 / r["seconds"]
            again = idle_frames.get("repaints", 0)
            n = (idle_frames["count"] - again) * per_min
            if busy_frames is not None and not busy_frames["count"]:
                # Not even a streaming pane painted: the window was hidden,
                # and its idle frames say nothing.
                row("app idle frames built", f"{n:.0f}/min  (not judged: no frame while streaming)", "< 10/min")
            else:
                row("app idle frames built", f"{n:.0f}/min", "< 10/min", "ok" if n < 10 else "OVER")
            row("app idle frames painted again", f"{again * per_min:.0f}/min  (cursor blink)")
        if busy_frames is not None:
            if busy_frames["count"]:
                build, paint, total = busy_frames["build"], busy_frames["paint"], busy_frames["total"]
                row("frame build p50 / p99 (streaming)", f"{_ms(build['p50'])} / {_ms(build['p99'])}", "p99 <= 4 ms",
                    _verdict(build["p99"], 4))
                row("frame paint p50 / p99 (streaming)", f"{_ms(paint['p50'])} / {_ms(paint['p99'])}")
                row("frame total p50 / p99 (streaming)", f"{_ms(total['p50'])} / {_ms(total['p99'])}  ({busy_frames['count']} frames)")
            else:
                row("frames (streaming)", "n/a (0 frames: the window was hidden, or the pane did not stream)")
    else:
        if r:
            row("kerod idle CPU, without the app", f"{r['cpu']:.2f} %", "<= 1 %", _verdict(r["cpu"], 1))
            row("kerod idle wakeups, without the app", f"{r['interrupt_wakeups']:.0f}/min", "<= 500/min",
                _verdict(r["interrupt_wakeups"], 500))
        row("app", f"not measured ({app_reason or 'not started'})")
    if key_latency:
        row("key -> paint p99", f"{key_latency['p99_ms']:.1f} ms ({key_latency['count']} native dispatch samples)",
            "<= 50 ms", _verdict(key_latency['p99_ms'], 50))
    else:
        row("key -> paint p99", "not measured (use PERF_GUI=1)", "<= 50 ms")

    seconds = r["seconds"] if r else 0
    head = f"perf: {panes} pane(s), {fed / MB:.0f} MiB fed to each, idle window {seconds:.0f} s"
    widths = [max(len(x[i]) for x in rows + [("measure", "value", "budget", "")]) for i in range(3)]
    lines = [head, ""]
    lines.append(f"{'measure'.ljust(widths[0])}  {'value'.ljust(widths[1])}  budget")
    lines.append("  ".join("-" * w for w in widths))
    for n, v, b, ok in rows:
        lines.append(f"{n.ljust(widths[0])}  {v.ljust(widths[1])}  {b.ljust(widths[2])}  {ok}".rstrip())
    return lines


def _load(path):
    with open(path) as f:
        return json.load(f)


def _frames_of(spec):
    """frames of LOG:FROM:TO, lines FROM (1-based) to TO, inclusive."""
    path, start, end = spec.rsplit(":", 2)
    with open(path, errors="replace") as f:
        lines = f.readlines()
    return frames(lines[int(start) - 1:int(end)])


def main(argv):
    if argv and argv[0] == "sample":
        print(json.dumps(sample([int(p) for p in argv[1:]])))
        return 0
    if argv and argv[0] == "frames":
        path = argv[1]
        with open(path, errors="replace") as f:
            lines = f.readlines()
        start = int(argv[2]) if len(argv) > 2 else 1
        end = int(argv[3]) if len(argv) > 3 else len(lines)
        print(json.dumps(frames(lines[start - 1:end])))
        return 0
    if argv and argv[0] == "validate-fixture":
        p = argparse.ArgumentParser(prog="perf_probe.py validate-fixture")
        p.add_argument("panes", type=int)
        p.add_argument("sessions", help="isolated daemon ls --json output")
        a = p.parse_args(argv[1:])
        validate_daemon_fixture(a.panes, _load(a.sessions))
        return 0
    if argv and argv[0] == "report":
        p = argparse.ArgumentParser(prog="perf_probe.py report")
        p.add_argument("--panes", type=int, required=True)
        p.add_argument("--fed", type=int, required=True, help="bytes fed to each pane")
        p.add_argument("--kerod", type=int, required=True)
        p.add_argument("--base", required=True, help="sample of kerod with no pane")
        p.add_argument("--t0", required=True)
        p.add_argument("--t1", required=True)
        p.add_argument("--app", type=int)
        p.add_argument("--app-reason", default="")
        p.add_argument("--idle-frames", help="LOG:FROM:TO")
        p.add_argument("--busy-frames", help="LOG:FROM:TO")
        a = p.parse_args(argv[1:])
        lines = report(
            a.panes, a.fed, a.kerod, _load(a.base), _load(a.t0), _load(a.t1),
            app=a.app, app_reason=a.app_reason,
            idle_frames=_frames_of(a.idle_frames) if a.idle_frames else None,
            busy_frames=_frames_of(a.busy_frames) if a.busy_frames else None,
        )
        print("\n".join(lines))
        return 0
    print(__doc__, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
