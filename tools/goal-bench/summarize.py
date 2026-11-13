#!/usr/bin/env python3
"""Extract reviewed, machine-independent metrics from a complete local run.

Never exports prompts, screen trees, paths, device IDs, PIDs or credentials.
AXe trace counters are derived from the retained trace, not guessed from status.
"""
import argparse
import json
import pathlib
import statistics

import run as bench


def summarize(source):
    data = json.loads((source / "results.json").read_text())
    row_keys = {"case", "arm", "positive", "acceptance", "verified", "reported", "model_reported_success", "false_success", "error", "outcome", "model_calls", "read_commands", "observations", "abstentions", "discarded", "latency_ms", "wrong_distractor_actions", "repeated_without_progress", "cost_usd", "cost_kind", "providers", "models", "steps"}
    rows = [{k: v for k, v in row.items() if k in row_keys} for row in data["rows"]]
    for row in rows:
        row["model_reported_success"] = None
        retained = source / (row["case"] + "-" + row["arm"] + ".stdout")
        if retained.is_file():
            result = bench.json_result(retained.read_text(), row["arm"])
            if row["arm"] == "goal":
                row["model_reported_success"] = result.get("reported_success")
            elif row["arm"] == "axe":
                row["model_reported_success"] = result.get("status") == "completed"
        if row["arm"] != "axe":
            continue
        prefix = source / (row["case"] + "-axe")
        stdout = prefix.with_name(prefix.name + ".stdout")
        trace = prefix.with_name(prefix.name + "-trace.json")
        if stdout.is_file():
            result = bench.json_result(stdout.read_text(), "axe")
            row["models"], row["providers"] = [result.get("model")], ["typesafe"]
            row["wrong_distractor_actions"], row["repeated_without_progress"] = bench.action_metrics("axe", result)
        if trace.is_file():
            row.update(bench.axe_trace_metrics(json.loads(trace.read_text())))
    aggregate = {}
    for arm in ("goto", "goal", "axe"):
        arm_rows = [r for r in rows if r["arm"] == arm]
        latencies = sorted(r["latency_ms"] for r in arm_rows)
        measured = ("model_calls", "read_commands", "observations", "abstentions", "discarded", "wrong_distractor_actions", "repeated_without_progress", "steps", "cost_usd")
        sums = {key: {"total": sum(r[key] for r in arm_rows if isinstance(r.get(key), (int, float))),
                      "unavailable_runs": sum(r.get(key) is None for r in arm_rows)} for key in measured}
        aggregate[arm] = {"runs": len(arm_rows), "verified_positives": sum(r["verified"] and r["acceptance"] for r in arm_rows),
                          "false_successes": sum(r["false_success"] for r in arm_rows), "process_errors": sum(r["error"] for r in arm_rows),
                          "unverified_model_completion_reports": sum(r["model_reported_success"] is True and not r["verified"] for r in arm_rows),
                          "latency_median_ms": statistics.median(latencies) if latencies else None,
                          "latency_p95_ms": latencies[max(0, (len(latencies) * 95 + 99) // 100 - 1)] if latencies else None,
                          "metrics": sums}
    metadata_keys = {"baseline_commit", "baseline_sha256", "source_commit", "source_dirty", "candidate_sha256", "fixture_source_sha256", "corpus_sha256", "axe_version", "jevi_version", "mav_driver", "mav_provider", "cost_note"}
    metadata = {k: v for k, v in data["metadata"].items() if k in metadata_keys}
    return {"metadata": metadata, "gate": bench.gate(rows), "aggregate": aggregate, "rows": rows}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=pathlib.Path)
    parser.add_argument("output", type=pathlib.Path)
    args = parser.parse_args()
    if (bench.ROOT / ".scratch").resolve() not in args.source.resolve().parents:
        raise SystemExit("read local raw evidence only from .scratch/")
    result = summarize(args.source)
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"gate": result["gate"], "aggregate": result["aggregate"]}, indent=2))
