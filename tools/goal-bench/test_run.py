import importlib.util
import json
import pathlib
import subprocess
import sys
import time
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("goal_bench", pathlib.Path(__file__).with_name("run.py"))
bench = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bench)
sys.path.insert(0, str(pathlib.Path(__file__).parent))
import summarize


class EvaluationGateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        (bench.ROOT / ".scratch").mkdir(exist_ok=True)

    def rows(self):
        return [{"case": c["id"], "arm": arm, "positive": c["positive"], "acceptance": c.get("acceptance", False),
                 "verified": c["positive"], "reported": c["positive"]}
                for c in bench.corpus() for arm in ("goto", "goal", "axe")]

    def test_gate_requires_complete_three_arm_corpus(self):
        rows = self.rows()
        self.assertEqual(sum(c.get("acceptance", False) for c in bench.corpus()), 30)
        self.assertTrue(bench.gate(rows)["numerical_gate_passed"])
        self.assertFalse(bench.gate(rows[:-1])["numerical_gate_passed"])
        self.assertFalse(bench.gate(rows + [rows[0]])["numerical_gate_passed"])

    def test_gate_rejects_false_success_and_fewer_than_27(self):
        rows = self.rows()
        negative = next(r for r in rows if r["arm"] == "goal" and not r["positive"])
        negative["reported"] = True
        self.assertFalse(bench.gate(rows)["numerical_gate_passed"])
        rows = self.rows()
        for row in [r for r in rows if r["arm"] == "goal" and r["acceptance"]][:4]:
            row["verified"], row["reported"] = False, False
        self.assertFalse(bench.gate(rows)["numerical_gate_passed"])

    def test_gate_rejects_regression_against_baseline(self):
        rows = self.rows()
        row = next(r for r in rows if r["arm"] == "goal" and r["acceptance"])
        row["verified"], row["reported"] = False, False
        self.assertEqual(bench.gate(rows)["positive_verified"]["goal"], 29)
        self.assertFalse(bench.gate(rows)["numerical_gate_passed"])

    def test_compact_json_and_independent_tree_evidence(self):
        doc = {"succeeded": "true", "steps": []}
        self.assertEqual(bench.json_result("goal json=" + json.dumps(json.dumps(doc)), "goal"), doc)
        tree = [{"role": "AXApplication", "pid": 101, "children": [{"role_description": "heading", "AXLabel": "Details"}, {"role": "button", "AXLabel": "Saved"}]}]
        self.assertEqual(bench.headings(tree), ["Details"])
        self.assertEqual(bench.tree_pids(tree), {"101"})

    def test_timeout_stops_agent_child_before_next_trial(self):
        scratch = bench.ROOT / ".scratch/timeout-isolation"
        scratch.mkdir(parents=True, exist_ok=True)
        marker = scratch / "late-input"
        marker.unlink(missing_ok=True)
        child = "import pathlib,time; time.sleep(0.5); pathlib.Path(" + repr(str(marker)) + ").write_text('input')"
        parent = "import subprocess,sys,time; subprocess.Popen([sys.executable,'-c'," + repr(child) + "]); time.sleep(10)"
        with self.assertRaises(subprocess.TimeoutExpired):
            bench.run([sys.executable, "-c", parent], timeout=0.2)
        time.sleep(0.6)
        self.assertFalse(marker.exists(), "timed-out child survived and affected the next trial")

    def test_native_trace_counts_abstention_staleness_and_wrong_taps(self):
        trace = {"events": [{"decision": {"operation": "BLOCKED"}}, {"decision": {"operation": "TAP"}, "discardedAsStale": True}]}
        self.assertEqual(bench.axe_trace_metrics(trace), {"abstentions": 1, "discarded": 1})
        result = {"history": [{"operation": "TAP", "target": "Help", "pageChanged": False}] * 2}
        self.assertEqual(bench.action_metrics("axe", result), (2, 1))

    def test_summary_uses_retained_trace_and_exports_only_metric_fields(self):
        with tempfile.TemporaryDirectory(dir=bench.ROOT / ".scratch") as directory:
            source = pathlib.Path(directory)
            row = {"case": "absent-01", "arm": "axe", "positive": False, "acceptance": False, "verified": False, "reported": False, "false_success": False, "error": False, "latency_ms": 10, "device_id": "private-value", "prompt": "private-text"}
            (source / "results.json").write_text(json.dumps({"metadata": {"axe_version": "v2.0.4", "device_id": "private-value"}, "rows": [row]}))
            (source / "absent-01-axe.stdout").write_text(json.dumps({"model": "jev-test", "history": [{"operation": "TAP", "target": "Help"}]}))
            (source / "absent-01-axe-trace.json").write_text(json.dumps({"events": [{"decision": {"operation": "BLOCKED"}, "discardedAsStale": True}]}))
            result = summarize.summarize(source)
            exported = json.dumps(result)
            self.assertNotIn("private-value", exported)
            self.assertNotIn("private-text", exported)
            self.assertEqual(result["rows"][0]["abstentions"], 1)
            self.assertEqual(result["rows"][0]["discarded"], 1)
            self.assertEqual(result["rows"][0]["wrong_distractor_actions"], 1)


if __name__ == "__main__":
    unittest.main()
