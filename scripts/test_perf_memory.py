"""Check workload safety and accounting without starting an app."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("perf_memory", Path(__file__).with_name("perf_memory.py"))
memory = importlib.util.module_from_spec(spec)
spec.loader.exec_module(memory)


class MemoryWorkloadTest(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
