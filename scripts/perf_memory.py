"""Opt-in, isolated memory workload for perf.sh (PERF_MEMORY=1).

The Go diagnostics exist only in a temporary build overlay. They snapshot
terminal state on the UI thread and expose profiles on an ephemeral loopback
port; normal builds gain neither a listener nor instrumentation.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

import importlib.util
_spec = importlib.util.spec_from_file_location("perf_probe", Path(__file__).with_name("perf_probe.py"))
perf_probe = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(perf_probe)

HOOK = r'''
var perfLastView atomic.Value
var perfPaints sync.Map
var perfPaintSequence atomic.Uint64

func perfPainted(id string, r ui.Rect, fixtureReady bool) {
    perfPaints.Store(id, map[string]any{
        "sequence": perfPaintSequence.Add(1), "width": r.W, "height": r.H, "fixture_ready": fixtureReady,
    })
}
var perfViewSequence uint64

func perfViewed(a *App) {
    perfViewSequence++
    terms := map[string][2]int{}
    for id, e := range a.terms {
        if e.term != nil {
            cols, rows := e.term.Size()
            terms[id] = [2]int{cols, rows}
        }
    }
    focusedPane, zoomed := "", ""
    if s := a.session(a.selected); s != nil { focusedPane, zoomed = a.focusOf(s), s.Zoomed }
    perfLastView.Store(map[string]any{
        "selected": a.selected, "show": a.show, "sequence": perfViewSequence,
        "focus_pane": focusedPane, "zoomed": zoomed,
        "terms": terms, "focused": a.focused, "active": a.active, "go_version": runtime.Version(),
    })
}

func init() {
    dir := os.Getenv("SHIBU_PERF_DIAGNOSTICS")
    if dir == "" || len(os.Args) != 1 { return }
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { panic(err) }
    mux := http.NewServeMux()
    mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
        value := perfLastView.Load()
        if value == nil { json.NewEncoder(w).Encode(nil); return }
        state := map[string]any{}
        for key, value := range value.(map[string]any) { state[key] = value }
        paints := map[string]any{}
        perfPaints.Range(func(key, value any) bool { paints[key.(string)] = value; return true })
        state["paints"] = paints
        json.NewEncoder(w).Encode(state)
    })
    mux.HandleFunc("/memstats", func(w http.ResponseWriter, r *http.Request) {
        var m runtime.MemStats
        runtime.ReadMemStats(&m)
        json.NewEncoder(w).Encode(m)
    })
    mux.HandleFunc("/debug/pprof/", httppprof.Index)
    mux.HandleFunc("/debug/pprof/profile", httppprof.Profile)
    srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5*time.Second}
    if err := os.WriteFile(filepath.Join(dir, "address"), []byte("http://"+ln.Addr().String()), 0600); err != nil { panic(err) }
    go srv.Serve(ln)
}
'''


def prepare(root, directory):
    """Overlay two existing files, leaving the checkout unchanged."""
    root, directory = Path(root).resolve(), Path(directory).resolve()
    directory.mkdir(parents=True, exist_ok=True)
    main = (root / "main.go").read_text()
    if main.count('import (') != 1 or '"runtime"' not in main:
        raise ValueError("main.go import seam changed")
    main = main.replace('import (', '''import (
    "encoding/json"
    "net"
    "net/http"
    httppprof "net/http/pprof"
    "sync"
    "sync/atomic"
    "time"''', 1) + HOOK
    view = (root / "view.go").read_text()
    seam = 'func (a *App) view(c *ui.Context) {'
    if view.count(seam) != 1:
        raise ValueError("view.go instrumentation seam changed")
    view = view.replace(seam, seam + '\n defer perfViewed(a)', 1)
    panes = (root / "panes.go").read_text()
    paint_seam = 'termView := terminal.View(c.Key("term"), e.term).Fill().Bind(ref)'
    if panes.count(paint_seam) != 1:
        raise ValueError("panes.go paint seam changed")
    if '"strings"' not in panes:
        panes = panes.replace('import (', 'import (\n "strings"', 1)
    panes = panes.replace(paint_seam, paint_seam + '\n termView.DrawOver(func(_ *ui.Painter, r ui.Rect) { text, _ := e.term.TailText(8); perfPainted(p.ID, r, strings.Contains(text, "PERF-READY")) })', 1)
    replacements = {}
    for name, content in [("main.go", main), ("view.go", view), ("panes.go", panes)]:
        overlay = directory / name
        overlay.write_text(content)
        replacements[str(root / name)] = str(overlay)
    overlay_path = directory / "overlay.json"
    overlay_path.write_text(json.dumps({"Replace": replacements}))
    return overlay_path


def validate_isolation(directory, environment):
    """Refuse CLI work when any routing path reaches outside this run."""
    directory = Path(directory).resolve()
    for key in ("HOME", "TMPDIR", "KERO_HOME", "KERO_CONFIG", "KERO_SOCKET"):
        value = environment.get(key)
        if not value or not Path(value).resolve().is_relative_to(directory):
            raise ValueError(f"{key} must be inside {directory}")
    for key in ("KERO_PANE", "KERO_SESSION", "LINEAR_API_KEY", "XDG_RUNTIME_DIR"):
        if environment.get(key):
            raise ValueError(f"{key} must be cleared")


def wait_for(read, accepts, description, timeout=30):
    end = time.monotonic() + timeout
    last = None
    while time.monotonic() < end:
        try:
            last = read()
            if accepts(last):
                return last
        except (OSError, ValueError):
            pass
        time.sleep(0.1)
    raise RuntimeError(f"timed out: {description}; last={last}")


def memory_cost(footprint, baseline, panes, confirmed_panes=None):
    if panes < 1:
        raise ValueError("pane denominator must be positive")
    denominator = panes if confirmed_panes is None else confirmed_panes
    if denominator < 0 or denominator > panes:
        raise ValueError("confirmed panes must be within the workload")
    return {
        "aggregate_bytes": footprint,
        "workload_panes": panes,
        "incremental_pane_denominator": denominator,
        "baseline_bytes": baseline,
        "incremental_per_pane_bytes": None if baseline is None or denominator == 0 else (footprint - baseline) / denominator,
        "aggregate_budget_bytes": panes * 30 * perf_probe.MB,
        "within_aggregate_budget": footprint <= panes * 30 * perf_probe.MB,
    }


def painted_visit(state, session, pane, sequence, paint_sequence):
    """Selection and a fresh paint of this pane must agree, not just any frame."""
    if not state:
        return False
    paint = state.get("paints", {}).get(pane, {})
    return (state["selected"] == session and state["show"] == "session"
            and state["sequence"] > sequence
            and all(n > 0 for n in state["terms"].get(pane, [0, 0]))
            and paint.get("sequence", 0) > paint_sequence
            and paint.get("width", 0) > 0 and paint.get("height", 0) > 0
            and paint.get("fixture_ready") is True)


class Workload:
    def __init__(self, directory, app, daemon, panes, busy):
        self.directory = Path(directory).resolve()
        validate_isolation(self.directory, os.environ)
        self.app, self.daemon, self.panes, self.busy = app, daemon, panes, busy
        self.bin = str(self.directory / "shibu-perf")
        # A caller's HTTP proxy must not receive this run's diagnostics.
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.address = wait_for(
            lambda: (self.directory / "diagnostics/address").read_text(),
            lambda x: x.startswith("http://127.0.0.1:"), "diagnostic listener")
        self.sessions = (self.directory / "sessions").read_text().split()
        self.targets = (self.directory / "targets").read_text().split()
        self.baseline = None
        baseline_path = self.directory / "memory/empty/sample.json"
        if baseline_path.exists():
            self.baseline = json.loads(baseline_path.read_text())["pids"][str(self.app)]["footprint"]
        self.evidence = []
        self.samples = []

    def get(self, path):
        with self.http.open(self.address + path, timeout=10) as response:
            return response.read()

    def state(self):
        return json.loads(self.get("/state"))

    def frames(self):
        return perf_probe.frames((self.directory / "app.log").read_text(errors="replace").splitlines())["count"]

    def cli(self, *args):
        return subprocess.check_output([self.bin, *args], text=True, timeout=30)

    def focus_split(self, pane):
        # Native windows cannot expose 25 minimum-sized tiles at once.
        # Use the real pane actions to paint each one zoomed, retaining all
        # terminals through the selected-session cache exemption.
        state = self.state()
        if state["zoomed"]:
            self.cli("do", "pane.zoom")
            wait_for(self.state, lambda x: x and not x["zoomed"], "unzoom before traversal")
        for _ in range(self.panes + 1):
            state = self.state()
            if state["focus_pane"] == pane:
                break
            previous = state["focus_pane"]
            self.cli("do", "pane.next")
            wait_for(self.state, lambda x: x and x["focus_pane"] != previous,
                     "pane.next changes the app focus")
        else:
            raise RuntimeError(f"pane.next did not reach {pane}")
        if state["zoomed"] != pane:
            self.cli("do", "pane.zoom")
            wait_for(self.state, lambda x: x and x["zoomed"] == pane, f"zoom {pane}")

    def visit(self, target):
        before = self.state() or {}
        sequence, frames = before.get("sequence", 0), self.frames()
        # A focus request is answered only by the app connected to this socket.
        if os.environ.get("PERF_LAYOUT") != "split":
            self.cli("focus", target)
        sessions = json.loads(self.cli("ls", "--json"))
        if isinstance(sessions, dict):
            sessions = sessions.get("sessions", [])
        session = next(s for s in sessions if s["id"] == target or any(p["id"] == target for p in s["panes"]))
        pane = target if target != session["id"] else session["focus"]
        paint_before = before.get("paints", {}).get(pane, {}).get("sequence", 0)
        if os.environ.get("PERF_LAYOUT") == "split":
            if before.get("selected") != session["id"]:
                self.cli("focus", session["id"])
                wait_for(self.state, lambda x: x and x["selected"] == session["id"], "selected split session")
            self.focus_split(pane)
        def ready(state):
            return (painted_visit(state, session["id"], pane, sequence, paint_before)
                    and self.frames() > frames)
        # Refocusing the current pane may make no frame, so force a harmless
        # visible input into its fixture's read; it is not yet given Enter.
        self.cli("send", target, " ")
        state = wait_for(self.state, ready, f"rendered terminal {pane}")
        self.evidence.append({"target": target, "pane": pane, "state": state, "frames": self.frames()})
        return state

    def capture(self, name, native=True):
        directory = self.directory / "memory" / name
        directory.mkdir(parents=True, exist_ok=True)
        sample = perf_probe.sample([self.app, self.daemon])
        sample["stage"] = name
        sample["view"] = wait_for(self.state, lambda x: x is not None, "completed app frame")
        sample["go_before_gc"] = json.loads(self.get("/memstats"))
        (directory / "sample.json").write_text(json.dumps(sample, indent=2))
        # Capture native accounting before requesting a diagnostic GC.
        if native:
            for label, command in [
                ("vmmap-summary", ["vmmap", "-summary", str(self.app)]),
                ("vmmap-regions", ["vmmap", "-noCoalesce", str(self.app)]),
                ("footprint", ["footprint", "-p", str(self.app), "-j", str(directory / "footprint.json")]),
            ]:
                before = perf_probe.sample([self.app, self.daemon])
                result = subprocess.run(command, capture_output=True, text=True, timeout=60)
                after = perf_probe.sample([self.app, self.daemon])
                (directory / (label + ".txt")).write_text(result.stdout + result.stderr)
                sample[label + "_exit"] = result.returncode
                sample[label + "_process_before"] = before
                sample[label + "_process_after"] = after
        (directory / "heap.pb.gz").write_bytes(self.get("/debug/pprof/heap?gc=1"))
        sample["go_after_gc"] = json.loads(self.get("/memstats"))
        sample["process_after_gc"] = perf_probe.sample([self.app, self.daemon])
        sample["cost"] = memory_cost(sample["pids"][str(self.app)]["footprint"], self.baseline, self.panes,
                                     len({visit["pane"] for visit in self.evidence}))
        (directory / "sample.json").write_text(json.dumps(sample, indent=2))
        self.samples.append(sample)
        print(f"memory: {name}: app {sample['cost']['aggregate_bytes']/perf_probe.MB:.1f} MB; "
              f"Go heap before/after GC {sample['go_before_gc']['HeapAlloc']/perf_probe.MB:.1f}/"
              f"{sample['go_after_gc']['HeapAlloc']/perf_probe.MB:.1f} MB; "
              f"retained terminals {len(sample['view']['terms'])}", flush=True)

    def run(self):
        self.visit(self.targets[0])
        self.capture("first-shown")
        for sweep in range(1, 3):
            for target in self.targets:
                self.visit(target)
            self.capture(f"visited-{sweep}")
        if os.environ.get("PERF_LAYOUT") == "split":
            self.cli("do", "pane.zoom")
            wait_for(self.state, lambda x: x and not x["zoomed"], "expanded split layout")
        start = self.frames()
        t0 = perf_probe.sample([self.app, self.daemon])
        for target in self.targets:
            self.cli("send", "--enter", target, "go")
        time.sleep(max(1, self.busy / 2))
        self.capture("streaming", native=False)
        for target in self.targets:
            wait_for(lambda: self.cli("read", target), lambda text: "PERF-STREAMED" in text,
                     f"fixture {target} completes", self.busy + 30)
        end = self.frames()
        time.sleep(15)
        self.capture("settled")
        t1 = perf_probe.sample([self.app, self.daemon])
        output = {"panes": self.panes, "layout": os.environ.get("PERF_LAYOUT", "sessions"),
                  "baseline_bytes": self.baseline,
                  "revision": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
                  "dirty_source": subprocess.check_output(["git", "status", "--short"], text=True),
                  "feed_bytes_per_pane": (self.directory / "feed.txt").stat().st_size,
                  "runtime_environment": {key: os.environ[key] for key in
                                          ("GOMEMLIMIT", "GOGC", "GODEBUG") if key in os.environ},
                  "baseline_note": "same app, before any session was created or terminal attached",
                  "visits": self.evidence, "samples": self.samples,
                  "stream_frames": end-start, "rates_including_profiling": {
                      "app": perf_probe.idle_rates(t0, t1, self.app),
                      "daemon": perf_probe.idle_rates(t0, t1, self.daemon)}}
        (self.directory / "memory/results.json").write_text(json.dumps(output, indent=2))
        print(f"memory: {len(self.evidence)} confirmed visits; {end-start} frames during streaming; "
              f"evidence {self.directory}/memory", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    prepare_args = commands.add_parser("prepare")
    prepare_args.add_argument("root")
    prepare_args.add_argument("directory")
    for name in ("run", "baseline"):
        run_args = commands.add_parser(name)
        run_args.add_argument("directory")
        run_args.add_argument("app", type=int)
        run_args.add_argument("daemon", type=int)
        run_args.add_argument("panes", type=int)
        run_args.add_argument("busy", type=int)
    args = parser.parse_args()
    if args.command == "prepare":
        print(prepare(args.root, args.directory))
    else:
        workload = Workload(args.directory, args.app, args.daemon, args.panes, args.busy)
        if args.command == "baseline":
            state = wait_for(workload.state, lambda x: x is not None, "empty app frame")
            if state["terms"] or state["selected"]:
                raise RuntimeError(f"baseline already has terminals or selection: {state}")
            workload.capture("empty", native=False)
        else:
            workload.run()


if __name__ == "__main__":
    main()
