"""Test the parsing and the table of scripts/perf_probe.py."""
import importlib.util
import os
from pathlib import Path
import sys
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("perf_probe", SOURCE / "scripts/perf_probe.py")
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)

# Lines as MyGo's ui/framestats.go logs them, with log's date in front.
PLAIN = ("2026/10/22 18:14:49 mygo: frame 166 took 0.6 ms: build 0.1, layout 0.0, paint 0.2, "
         "present 0.2 (drawn on the GPU: 2380 instances in 81 batches), other 0.0; meanwhile the "
         "process made about 577 allocations, 39.4 KB, 0 text layouts; no GC")
PASSES = ("2026/10/22 18:14:50 mygo: frame 167 took 9.5 ms: build 4.5 (2 passes), layout 1.0, "
          "paint 2.0, present 1.5 (partial), other 0.5; meanwhile the process made about 10 "
          "allocations, 1.0 KB, 3 text layouts; GC: 1 cycles ended, 0.2 ms of assists")
AGAIN = ("2026/10/22 18:14:51 mygo: frame 168 took 1.0 ms: build 0.0 (painted again), layout 0.0, "
         "paint 0.6, present 0.4 (not shown), other 0.0; meanwhile the process made about 0 "
         "allocations, 0.0 KB, 0 text layouts; no GC")

MB = 1 << 20


def sample(at, pids):
    return {"time": at, "pids": {str(k): v for k, v in pids.items()}}


def counters(cpu_s, idle, intr, footprint_mb, resident_mb):
    return {"cpu_ns": cpu_s * 1e9, "idle_wakeups": idle, "interrupt_wakeups": intr,
            "footprint": footprint_mb * MB, "resident": resident_mb * MB}


class PerfProbeTest(unittest.TestCase):
    def test_focused_wakeups_are_separate_from_unfocused_acceptance(self):
        before = sample(0, {7: counters(0, 0, 0, 20, 20), 9: counters(0, 0, 0, 100, 100)})
        after = sample(60, {7: counters(0, 0, 500, 20, 20), 9: counters(0, 0, 2000, 100, 100)})
        focused = "\n".join(probe.report(4, 16*MB, 7, before, before, after, app=9, idle_window="focused"))
        unfocused = "\n".join(probe.report(4, 16*MB, 7, before, before, after, app=9, idle_window="unfocused"))
        self.assertIn("focused: reported separately", focused)
        focused_rows = {line.split("  ")[0]: line for line in focused.splitlines()}
        unfocused_rows = {line.split("  ")[0]: line for line in unfocused.splitlines()}
        self.assertFalse(focused_rows["app idle wakeups"].endswith("OVER"))
        self.assertTrue(unfocused_rows["app idle wakeups"].endswith("OVER"))
        self.assertTrue(unfocused_rows["app + kerod idle wakeups"].endswith("OVER"))
        unknown = "\n".join(probe.report(4, 16*MB, 7, before, before, after, app=9))
        unknown_rows = {line.split("  ")[0]: line for line in unknown.splitlines()}
        self.assertFalse(unknown_rows["app idle wakeups"].endswith("OVER"))
        self.assertFalse(unknown_rows["app + kerod idle wakeups"].endswith("OVER"))
        self.assertIn("focus unverified", unknown_rows["app + kerod idle wakeups"])

    def test_daemon_wakeup_limit_is_judged_independently_with_app(self):
        before = sample(0, {7: counters(0, 0, 0, 20, 20), 9: counters(0, 0, 0, 100, 100)})
        after = sample(60, {7: counters(0, 0, 600, 20, 20), 9: counters(0, 0, 1000, 100, 100)})
        for phase in ("unfocused", "focused", None):
            with self.subTest(phase=phase):
                lines = {line.split("  ")[0]: line for line in probe.report(
                    8, 16*MB, 7, before, before, after, app=9, idle_window=phase)}
                self.assertEqual(lines["kerod idle wakeups"].endswith("OVER"), phase == "unfocused")
                self.assertFalse(lines["app idle wakeups"].endswith("OVER"))
                self.assertFalse(lines["app + kerod idle wakeups"].endswith("OVER"))

    def test_binary_memory_units_are_explicit(self):
        self.assertEqual(probe._mb(1 << 20), "1.0 MiB")

    def test_daemon_rss_and_footprint_disagree(self):
        for footprint, rss, verdict in ((60, 256, "OVER"), (300, 160, "ok")):
            for app in (None, 9):
                with self.subTest(footprint=footprint, rss=rss, app=app):
                    # The large baseline RSS must not be subtracted. App
                    # presence does not change the daemon's denominator.
                    before = sample(0, {7: counters(0, 0, 0, 20, 240),
                                        9: counters(0, 0, 0, 100, 100)})
                    after = sample(60, {7: counters(0, 0, 0, footprint, rss),
                                        9: counters(0, 0, 0, 100, 100)})
                    lines = {line.split("  ")[0]: line for line in probe.report(
                        8, 0, 7, before, before, after, app=app)}
                    self.assertTrue(lines["kerod resident (RSS) per pane"].endswith(verdict))
                    self.assertFalse(lines["kerod footprint per pane"].endswith(("ok", "OVER")))
                    self.assertIn("diagnostic", lines["kerod footprint per pane"])

    def test_daemon_rss_rejects_invalid_denominators(self):
        empty = {"pids": {}}
        for count in (0, -1, 1.5, True):
            with self.subTest(count=count), self.assertRaises(ValueError):
                probe.report(count, 0, 7, empty, empty, empty)

    def test_daemon_fixture_requires_distinct_terminal_ptys(self):
        fixture = [{"panes": [{"id": "a"}, {"id": "b"}]}]
        self.assertEqual(probe.validate_daemon_fixture(2, fixture), 2)
        # Split panes and separate sessions both count actual daemon terminals.
        self.assertEqual(probe.validate_daemon_fixture(
            2, [{"panes": [{"id": "a"}]}, {"panes": [{"id": "b"}]}]), 2)
        for invalid in ([{"panes": [{"id": "a"}]}],
                        [{"panes": [{"id": "a"}, {"id": "a"}]}],
                        [{"panes": [{"id": "a"}, {"id": "b", "view": "files"}]}]):
            with self.subTest(fixture=invalid), self.assertRaises(ValueError):
                probe.validate_daemon_fixture(2, invalid)

    def test_parse_frame(self):
        self.assertEqual(probe.parse_frame(PLAIN),
                         {"total": 0.6, "build": 0.1, "layout": 0.0, "paint": 0.2, "present": 0.2, "repaint": False})
        self.assertEqual(probe.parse_frame(PASSES)["build"], 4.5)
        self.assertEqual(probe.parse_frame(PASSES)["paint"], 2.0)
        self.assertEqual(probe.parse_frame(AGAIN)["build"], 0.0)
        self.assertEqual(probe.parse_frame(AGAIN)["paint"], 0.6)
        self.assertTrue(probe.parse_frame(AGAIN)["repaint"])
        self.assertFalse(probe.parse_frame(PASSES)["repaint"])
        self.assertIsNone(probe.parse_frame("2026/10/22 kero: attached to the daemon"))
        self.assertIsNone(probe.parse_frame(""))

    def test_percentile(self):
        self.assertIsNone(probe.percentile([], 50))
        self.assertEqual(probe.percentile([3.0], 99), 3.0)
        values = list(range(1, 101))
        self.assertEqual(probe.percentile(values, 50), 50)
        self.assertEqual(probe.percentile(values, 99), 99)
        self.assertEqual(probe.percentile(list(reversed(values)), 100), 100)
        self.assertEqual(probe.percentile([1, 2, 3, 4], 50), 2)

    def test_frames(self):
        out = probe.frames(["noise\n", PLAIN, PASSES, AGAIN])
        self.assertEqual(out["count"], 3)
        self.assertEqual(out["repaints"], 1)
        self.assertEqual(out["build"]["p99"], 4.5)
        self.assertEqual(out["build"]["p50"], 0.1)
        self.assertEqual(out["total"]["p99"], 9.5)
        none = probe.frames(["noise\n"])
        self.assertEqual(none["count"], 0)
        self.assertIsNone(none["paint"]["p99"])

    def test_frames_command_reads_a_range(self):
        with tempfile.TemporaryDirectory() as d:
            log = os.path.join(d, "app.log")
            with open(log, "w") as f:
                f.write("\n".join([PLAIN, PASSES, AGAIN]) + "\n")
            # Lines 2 and 3 alone.
            out = probe._frames_of(f"{log}:2:3")
            self.assertEqual(out["count"], 2)
            self.assertEqual(out["total"]["p50"], 1.0)

    def test_idle_rates(self):
        t0 = sample(100.0, {7: counters(1.0, 10, 100, 20, 30)})
        t1 = sample(160.0, {7: counters(1.6, 40, 1100, 21, 30)})
        r = probe.idle_rates(t0, t1, 7)
        self.assertAlmostEqual(r["cpu"], 1.0)
        self.assertAlmostEqual(r["idle_wakeups"], 30)
        self.assertAlmostEqual(r["interrupt_wakeups"], 1000)
        self.assertIsNone(probe.idle_rates(t0, t1, 8))
        self.assertIsNone(probe.idle_rates(t0, sample(100.0, t1["pids"]), 7))

    def test_report_with_app(self):
        base = sample(0.0, {7: counters(0.1, 0, 0, 12, 20)})
        t0 = sample(100.0, {7: counters(1.0, 0, 0, 40, 60), 9: counters(5.0, 0, 0, 280, 200)})
        t1 = sample(160.0, {7: counters(1.06, 0, 10, 40, 60), 9: counters(5.3, 3, 20, 290, 210)})
        idle = {"count": 105, "repaints": 100, "total": {}, "build": {}, "paint": {}}
        busy = {"count": 300, "total": {"p50": 0.5, "p99": 3.0}, "build": {"p50": 0.1, "p99": 5.0},
                "paint": {"p50": 0.2, "p99": 1.5}}
        text = "\n".join(probe.report(2, 16 * MB, 7, base, t0, t1, app=9, idle_frames=idle, busy_frames=busy, idle_window="unfocused"))
        self.assertIn("perf: 2 pane(s), 16 MiB fed to each, idle window 60 s", text)
        lines = {l.split("  ")[0]: l for l in text.splitlines()}
        self.assertIn("14.0 MiB", lines["kerod footprint per pane"])
        self.assertIn("diagnostic", lines["kerod footprint per pane"])
        self.assertFalse(lines["kerod footprint per pane"].endswith(("ok", "OVER")))
        self.assertTrue(lines["kerod resident (RSS) per pane"].endswith("OVER"))
        self.assertIn("0.60 %", lines["app + kerod idle CPU"])  # 0.1 % and 0.5 %
        self.assertTrue(lines["app + kerod idle CPU"].endswith("ok"))
        self.assertIn("30/min", lines["app + kerod idle wakeups"])  # 10 and 20 interrupt
        self.assertTrue(lines["app + kerod idle wakeups"].endswith("ok"))
        self.assertIn("(3/min package idle)", lines["app idle wakeups"])
        # Only the 5 frames that built the window are judged; the 100
        # that painted it again, as a cursor blinks, are shown apart.
        self.assertIn("5/min", lines["app idle frames built"])
        self.assertTrue(lines["app idle frames built"].endswith("ok"))
        self.assertIn("100/min", lines["app idle frames painted again"])
        self.assertTrue(lines["frame build p50 / p99 (streaming)"].endswith("OVER"))
        self.assertFalse(lines["app footprint (1 pane shown)"].endswith("OVER"))
        self.assertIn("not measured", lines["app retained-terminal RSS budget"])
        self.assertIn("not measured", lines["key -> paint p99"])

    def test_fixture_count_cannot_establish_retained_terminal_memory(self):
        base = sample(0, {7: counters(0, 0, 0, 12, 20)})
        before = sample(100, {7: counters(1, 0, 0, 80, 100), 9: counters(1, 0, 0, 260, 220)})
        after = sample(160, {7: counters(1, 0, 0, 80, 100), 9: counters(1, 0, 0, 260, 220)})
        text = "\n".join(probe.report(8, 16*MB, 7, base, before, after, app=9))
        lines = {line.split("  ")[0]: line for line in text.splitlines()}
        self.assertNotIn("app resident (RSS) per pane", lines)
        self.assertIn("220.0 MiB", lines["app resident (RSS)"])
        self.assertIn("260.0 MiB", lines["app footprint (1 pane shown)"])
        self.assertIn("not measured", lines["app retained-terminal RSS budget"])

    def test_report_hidden_window(self):
        base = sample(0.0, {7: counters(0.1, 0, 0, 12, 20)})
        t0 = sample(100.0, {7: counters(1.0, 0, 0, 40, 60), 9: counters(5.0, 0, 0, 280, 200)})
        t1 = sample(160.0, {7: counters(1.0, 0, 0, 40, 60), 9: counters(5.0, 0, 0, 280, 200)})
        none = probe.frames([])
        text = "\n".join(probe.report(1, 16 * MB, 7, base, t0, t1, app=9, idle_frames=none, busy_frames=none))
        lines = {l.split("  ")[0]: l for l in text.splitlines()}
        self.assertIn("not judged", lines["app idle frames built"])
        self.assertFalse(lines["app idle frames built"].endswith("ok"))
        self.assertIn("n/a (0 frames", lines["frames (streaming)"])

    def test_report_without_app(self):
        base = sample(0.0, {7: counters(0.1, 0, 0, 12, 20)})
        t0 = sample(100.0, {7: counters(1.0, 0, 0, 40, 60)})
        t1 = sample(130.0, {7: counters(1.6, 0, 500, 40, 60)})
        text = "\n".join(probe.report(1, 16 * MB, 7, base, t0, t1, app_reason="PERF_APP=0"))
        self.assertIn("app  ", text)
        self.assertIn("not measured (PERF_APP=0)", text)
        lines = {l.split("  ")[0]: l for l in text.splitlines()}
        self.assertTrue(lines["kerod idle wakeups, without the app"].endswith("OVER"))  # 1000/min
        self.assertTrue(lines["kerod idle CPU, without the app"].endswith("OVER"))  # 2 %
        self.assertNotIn("frame build", text)

    @unittest.skipUnless(sys.platform == "darwin", "proc_pid_rusage is macOS's")
    def test_sample_self(self):
        s = probe.sample([os.getpid()])
        own = s["pids"][str(os.getpid())]
        self.assertGreater(own["footprint"], 0)
        self.assertGreater(own["resident"], 0)
        self.assertGreater(own["cpu_ns"], 0)
        # CPU time in nanoseconds, not Mach ticks: this process has run for
        # less than an hour.
        self.assertLess(own["cpu_ns"], 3600e9)


if __name__ == "__main__":
    unittest.main()
