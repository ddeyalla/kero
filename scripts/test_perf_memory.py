"""Check workload safety and accounting without starting an app."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import os
import subprocess
import hashlib

spec = importlib.util.spec_from_file_location("perf_memory", Path(__file__).with_name("perf_memory.py"))
memory = importlib.util.module_from_spec(spec)
spec.loader.exec_module(memory)


class MemoryWorkloadTest(unittest.TestCase):
    def test_bundle_stages_only_verified_pinned_native_asset(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            data = b"pinned native fixture"
            digest = hashlib.sha256(data).hexdigest()
            cached = root / "Library/Caches/mygo/natives" / digest / "libghostty-vt.dylib"
            cached.parent.mkdir(parents=True)
            cached.write_bytes(data)
            manifest = root / "manifest.json"
            manifest.write_text(json.dumps({"libraries": [{"name": "libghostty-vt", "files": {
                "darwin-arm64": {"name": cached.name, "sha256": digest, "url": "https://unused.invalid"}}}]}))
            with patch.object(memory.platform, "machine", return_value="arm64"), patch.object(memory.Path, "home", return_value=root):
                memory.bundle_native(manifest, root / "Resources")
                self.assertEqual((root / "Resources" / cached.name).read_bytes(), data)
                cached.write_bytes(b"wrong asset")
                with self.assertRaisesRegex(ValueError, "SHA-256 mismatch"):
                    memory.bundle_native(manifest, root / "Rejected")
                self.assertFalse((root / "Rejected").exists())

    def test_gui_rejects_invalid_idle_before_starting_work(self):
        workload = object.__new__(memory.Workload)
        for value in ("0", "-1", "nan", "inf"):
            with self.subTest(value=value), patch.dict(os.environ, {"PERF_IDLE": value}):
                with self.assertRaisesRegex(ValueError, "PERF_IDLE"):
                    workload.gui()

    def test_window_evidence_requires_observations_and_visible_focus(self):
        focused = {"native_focused": True, "focused": True, "visible": True, "active": True}
        self.assertFalse(memory.stable_window([], True))
        self.assertFalse(memory.stable_window([None], True))
        self.assertTrue(memory.stable_window([focused], True))
        self.assertFalse(memory.stable_window([dict(focused, visible=False)], True))
        self.assertFalse(memory.stable_window([dict(focused, active=False)], True))
        self.assertFalse(memory.stable_window([focused], False))
        self.assertTrue(memory.stable_window([dict(focused, native_focused=False, focused=False)], False))

    def test_settle_override_allows_zero_but_rejects_nonfinite_values(self):
        for value, expected in (("0", 0), (".25", .25)):
            with patch.dict(os.environ, {"PERF_SETTLE": value}):
                self.assertEqual(memory.duration_env("PERF_SETTLE", 15, allow_zero=True), expected)
        for value in ("-1", "nan", "inf", "invalid"):
            with patch.dict(os.environ, {"PERF_SETTLE": value}):
                with self.assertRaisesRegex(ValueError, "PERF_SETTLE"):
                    memory.duration_env("PERF_SETTLE", 15, allow_zero=True)

    def test_readiness_retries_focus_after_control_registration(self):
        workload = object.__new__(memory.Workload)
        workload.sessions = ["fixture"]
        ready = {"control_ready": True, "selected": "fixture", "focus_pane": "pane",
                 "terms": {"pane": [80, 24]}, "native_focused": True,
                 "focused": True, "visible": True, "active": True}
        readings = iter([None, {"control_ready": False}, {"control_ready": True}, ready])
        workload.state = lambda: next(readings)
        calls = []
        def cli(*args):
            calls.append(args)
            if len(calls) == 1:
                raise subprocess.CalledProcessError(1, ["focus"])
        workload.cli = cli
        with patch.object(memory.time, "sleep"):
            self.assertEqual(workload.focus_ready(), ready)
        self.assertEqual(calls, [("focus", "fixture"), ("focus", "fixture")])

    def test_daemon_only_gui_conflict_is_rejected_before_build(self):
        root = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as directory:
            git = Path(directory) / "git"
            git.write_text("#!/bin/sh\necho unexpected-git >&2\nexit 99\n")
            git.chmod(0o755)
            result = subprocess.run(["/bin/sh", str(root / "scripts/perf.sh"), "1"],
                                    env=dict(os.environ, PERF_APP="0", PERF_GUI="1",
                                             PATH=directory+":/usr/bin:/bin"),
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 2)
            self.assertIn("PERF_APP=0", result.stderr)
            self.assertNotIn("unexpected-git", result.stderr)

    def test_latency_nearest_rank_and_invalid_samples(self):
        result = memory.latency_summary(list(range(1, 101)))
        self.assertEqual(result, {"count": 100, "p50_ms": 50, "p99_ms": 99, "max_ms": 100})
        for samples in ([], [-1], [float("nan")], [float("inf")]):
            with self.assertRaises(ValueError):
                memory.latency_summary(samples)

    def test_routing_must_stay_inside_run(self):
        with tempfile.TemporaryDirectory() as directory:
            environment = {key: str(Path(directory) / key) for key in
                           ("HOME", "TMPDIR", "KERO_HOME", "KERO_CONFIG", "KERO_SOCKET")}
            memory.validate_isolation(directory, environment)
            for key in environment:
                wrong = dict(environment, **{key: directory + "-sibling/file"})
                with self.subTest(key=key), self.assertRaisesRegex(ValueError, key):
                    memory.validate_isolation(directory, wrong)
            for key in ("KERO_PANE", "KERO_SESSION", "LINEAR_API_KEY", "XDG_RUNTIME_DIR"):
                with self.subTest(key=key), self.assertRaisesRegex(ValueError, key):
                    memory.validate_isolation(directory, dict(environment, **{key: "inherited"}))

    def test_total_and_incremental_are_distinct(self):
        mb = memory.perf_probe.MB
        cost = memory.memory_cost(700*mb, 200*mb, 25)
        self.assertEqual(cost["aggregate_bytes"], 700*mb)
        self.assertEqual(cost["baseline_bytes"], 200*mb)
        self.assertEqual(cost["incremental_per_pane_bytes"], 20*mb)
        self.assertEqual(cost["aggregate_budget_bytes"], 750*mb)
        self.assertTrue(cost["within_aggregate_budget"])
        self.assertFalse(memory.memory_cost(751*mb, 200*mb, 25)["within_aggregate_budget"])
        self.assertIsNone(memory.memory_cost(700*mb, None, 25)["incremental_per_pane_bytes"])
        self.assertIsNone(memory.memory_cost(100*mb, 100*mb, 25, 0)["incremental_per_pane_bytes"])
        first = memory.memory_cost(220*mb, 200*mb, 25, 1)
        self.assertEqual(first["incremental_per_pane_bytes"], 20*mb)
        self.assertEqual(first["aggregate_budget_bytes"], 750*mb)
        with self.assertRaises(ValueError):
            memory.memory_cost(700*mb, None, 0)
        with self.assertRaises(ValueError):
            memory.memory_cost(700*mb, None, 25, 26)

    def test_visit_requires_fresh_paint_of_requested_pane(self):
        state = {"selected": "session-id", "show": "session", "sequence": 8,
                 "terms": {"pane-id": [100, 30]},
                 "paints": {"pane-id": {"sequence": 12, "width": 900, "height": 700, "fixture_ready": True}}}
        self.assertTrue(memory.painted_visit(state, "session-id", "pane-id", 7, 11))
        self.assertFalse(memory.painted_visit(state, "session-id", "pane-id", 7, 12))
        pending = dict(state, paints={"pane-id": {"sequence": 12, "width": 900, "height": 700, "fixture_ready": False}})
        self.assertFalse(memory.painted_visit(pending, "session-id", "pane-id", 7, 11))
        self.assertFalse(memory.painted_visit(state, "session-id", "other-pane", 7, 0))
        self.assertFalse(memory.painted_visit(state, "other-session", "pane-id", 7, 11))
        self.assertFalse(memory.painted_visit(dict(state, show="session-id"), "session-id", "pane-id", 7, 11))
        self.assertFalse(memory.painted_visit(dict(state, terms={"pane-id": [100, 0]}), "session-id", "pane-id", 7, 11))
        self.assertFalse(memory.painted_visit(dict(state, paints={"pane-id": {"sequence": 12, "width": 0, "height": 700}}), "session-id", "pane-id", 7, 11))

    def test_split_traversal_unzooms_then_stops_at_each_pane(self):
        workload = object.__new__(memory.Workload)
        workload.panes = 3
        state = {"focus_pane": "a", "zoomed": ""}
        actions = []
        workload.state = lambda: dict(state)
        def cli(*args):
            actions.append(args)
            if args == ("do", "pane.next"):
                state["focus_pane"] = {"a": "b", "b": "c", "c": "a"}[state["focus_pane"]]
                if state["zoomed"]:
                    state["zoomed"] = state["focus_pane"]
            elif args == ("do", "pane.zoom"):
                state["zoomed"] = "" if state["zoomed"] else state["focus_pane"]
        workload.cli = cli
        workload.focus_split("c")
        self.assertEqual(state, {"focus_pane": "c", "zoomed": "c"})
        workload.focus_split("b")
        self.assertEqual(state, {"focus_pane": "b", "zoomed": "b"})
        self.assertEqual(actions.count(("do", "pane.zoom")), 3)
        with self.assertRaisesRegex(RuntimeError, "did not reach missing"):
            workload.focus_split("missing")
        self.assertLessEqual(len(actions), 12)

    def test_overlay_leaves_sources_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve() / "source"
            root.mkdir()
            main = 'package main\nimport (\n"runtime"\n)\n'
            view = 'package main\nfunc (a *App) view(c *ui.Context) {\n}\n'
            (root / "main.go").write_text(main)
            (root / "view.go").write_text(view)
            (root / "panes.go").write_text('termView := terminal.View(c.Key("term"), e.term).Fill().Bind(ref)')
            path = memory.prepare(root, Path(directory) / "overlay")
            self.assertEqual((root / "main.go").read_text(), main)
            self.assertEqual((root / "view.go").read_text(), view)
            replacements = json.loads(path.read_text())["Replace"]
            self.assertEqual(set(replacements), {str(root / name) for name in ("main.go", "view.go", "panes.go")})
            self.assertIn('"127.0.0.1:0"', Path(replacements[str(root / "main.go")]).read_text())
            self.assertIn('defer perfViewed(a)', Path(replacements[str(root / "view.go")]).read_text())
            (root / "view.go").write_text("seam changed")
            with self.assertRaisesRegex(ValueError, "seam changed"):
                memory.prepare(root, Path(directory) / "overlay")

    def test_overlay_matches_current_checkout_without_modifying_it(self):
        root = Path(__file__).resolve().parent.parent
        names = ("main.go", "view.go", "panes.go")
        before = {name: (root / name).read_bytes() for name in names}
        with tempfile.TemporaryDirectory() as directory:
            path = memory.prepare(root, Path(directory) / "overlay")
            replacements = json.loads(path.read_text())["Replace"]
            self.assertEqual(set(replacements), {str(root / name) for name in names})
            self.assertTrue(all(Path(value).is_file() for value in replacements.values()))
            panes = Path(replacements[str(root / "panes.go")]).read_text()
            self.assertIn("termView.DrawOver(", panes)
            self.assertIn("perfPainted(p.ID, r, text)", panes)
            self.assertIn("e.term.TailText(8)", panes)
        self.assertEqual(before, {name: (root / name).read_bytes() for name in names})

    def test_gui_overlay_does_not_snapshot_views_or_scan_inactive_paints(self):
        root = Path(__file__).resolve().parent.parent
        with tempfile.TemporaryDirectory() as directory:
            path = memory.prepare(root, Path(directory) / "overlay", gui=True)
            replacements = json.loads(path.read_text())["Replace"]
            self.assertNotIn(str(root / "view.go"), replacements)
            panes = Path(replacements[str(root / "panes.go")]).read_text()
            self.assertIn("if perfPaintEnabled.Load() {", panes)
            self.assertLess(panes.index("if perfPaintEnabled.Load() {"),
                            panes.index("termView.DrawOver("))
            self.assertLess(panes.index("if perfPaintEnabled.Load() {"),
                            panes.index("e.term.TailText(8)"))


if __name__ == "__main__":
    unittest.main()
