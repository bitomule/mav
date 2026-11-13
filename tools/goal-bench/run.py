#!/usr/bin/env python3
"""Controlled live comparison. Raw evidence stays in .scratch, never in git.

Requires a dedicated booted simulator with GoalFixture.app installed, a pinned
MAV 0.34.0 baseline, a candidate binary, MAV's configured Jev key, and TypeSafe's
TYPESAFE_API_KEY for the standalone AXe arm. Missing arms never satisfy the gate.
"""
import argparse
import hashlib
import json
import os
import pathlib
import shlex
import shutil
import signal
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE = "bcce9291c1ff41f30a8d9b2eb28a4be791930fb4"
BUNDLE = "com.example.GoalFixture"


def corpus():
    cases = []
    for kind, count in [("direct", 10), ("multi", 10), ("scroll", 5), ("form", 5)]:
        for i in range(1, count + 1):
            number = f"{i:02}"
            cases.append({"id": f"{kind}-{number}", "scenario": kind, "number": number, "positive": True, "acceptance": True,
                          "goal": f"Create a note saying 'Sample {number}' and save it" if kind == "form" else f"Open Details {number} inside Projects" if kind == "multi" else f"Swipe up through the list until Details {number} is visible, then open it" if kind == "scroll" else f"Open Details {number}",
                          "title": "Saved" if kind == "form" else f"Details {number}"})
    for kind in ["absent", "modal"]:
        for i in range(1, 4):
            cases.append({"id": f"{kind}-{i:02}", "scenario": kind, "number": "99", "positive": False,
                          "goal": "Open Details 99 without granting permissions or dismissing any modal", "title": "Details 99"})
    for kind in ("false-final", "focus-loss"):
        cases.append({"id": kind, "scenario": kind, "number": "99", "positive": False, "goal": "Create a note saying 'Sample 99' and save it", "title": "Saved"})
    cases.append({"id": "app-change", "scenario": "app-change", "number": "99", "positive": False, "goal": "Open Details 99 without leaving this app", "title": "Details 99"})
    cases.append({"id": "transition", "scenario": "transition", "number": "99", "positive": True, "acceptance": False, "goal": "Prepare navigation, then open Details 99", "title": "Details 99"})
    return cases


def run(argv, **kw):
    timeout = kw.pop("timeout", 120)
    # A timed-out agent must not keep sending input during the next trial.
    # Shims and their children inherit this dedicated process group.
    proc = subprocess.Popen(argv, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True, **kw)
    try:
        stdout, stderr = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.killpg(proc.pid, sig)
            except ProcessLookupError:
                pass
            try:
                proc.communicate(timeout=2)
                break
            except subprocess.TimeoutExpired:
                continue
        raise
    return subprocess.CompletedProcess(argv, proc.returncode, stdout, stderr)


def json_result(text, prefix):
    for line in text.splitlines():
        if line.startswith(prefix + " json="):
            # MAV's compact output quotes JSON as one shell-style field.
            value = line.split(" json=", 1)[1]
            try:
                decoded = json.loads(value)
                return json.loads(decoded) if isinstance(decoded, str) else decoded
            except json.JSONDecodeError:
                return json.loads(shlex.split(value)[0])
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return {}


def headings(tree):
    found = []
    if isinstance(tree, list):
        for node in tree:
            found.extend(headings(node))
    elif isinstance(tree, dict):
        role = str(tree.get("role_description", tree.get("role", tree.get("type", "")))).lower()
        if "heading" in role:
            found.append(tree.get("AXLabel", tree.get("label", "")))
        for key in ("children", "Children", "AXChildren"):
            if key in tree:
                found.extend(headings(tree[key]))
    return found


def tree_pids(tree):
    if isinstance(tree, list):
        return set().union(*(tree_pids(n) for n in tree)) if tree else set()
    if isinstance(tree, dict):
        found = {str(tree[k]) for k in ("pid", "AXPid", "AXPID") if k in tree and tree[k] is not None}
        for key in ("children", "Children", "AXChildren"):
            if key in tree:
                found.update(tree_pids(tree[key]))
        return found
    return set()


def oracle(udid, title, expected_pid):
    # Independent of all three agents' reported outcomes and finalScreen.
    observations = []
    for _ in range(2):
        p = run(["axe", "--output", "json", "describe-ui", "--udid", udid])
        doc = json.loads(p.stdout)
        if p.returncode or doc.get("didError", True):
            return False
        tree = doc.get("data", {}).get("result")
        if isinstance(tree, dict) and "ax" in tree:
            tree = tree["ax"].get("roots")
        if str(expected_pid) not in tree_pids(tree):
            return False
        observations.append(headings(tree))
        time.sleep(0.25)
    return observations[0] == observations[1] and observations[0][:1] == [title]


def gate(rows):
    expected = {(c["id"], arm) for c in corpus() for arm in ("goto", "goal", "axe")}
    actual = {(r["case"], r["arm"]) for r in rows}
    positives = {arm: sum(r["verified"] for r in rows if r["arm"] == arm and r.get("acceptance", False)) for arm in ("goto", "goal", "axe")}
    false_goal = sum(r["reported"] and not r["verified"] for r in rows if r["arm"] == "goal")
    # The deterministic stale-coordinate regression is verified separately by
    # the named regression test and ablation evidence, not inferred from totals.
    return {"complete": actual == expected and len(rows) == len(expected), "positive_verified": positives,
            "goal_false_successes": false_goal,
            "numerical_gate_passed": actual == expected and len(rows) == len(expected) and positives["goal"] >= 27 and positives["goal"] >= positives["goto"] and false_goal == 0}


def instrument(out):
    """Transparent subprocess counters. No credentials or screen text logged."""
    shim = out / "shim"
    shim.mkdir(exist_ok=True)
    real = {name: shutil.which(name) for name in ("axe", "jevi")}
    log = out / "calls.jsonl"
    program = '''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys, time
real = REAL_BINARIES
name = pathlib.Path(sys.argv[0]).name
start = time.monotonic()
p = subprocess.run([real[name], *sys.argv[1:]], input=sys.stdin.buffer.read(), capture_output=True)
entry = {"tool":name,"read":name=="axe" and "describe-ui" in sys.argv,"model":name=="jevi" and "ask" in sys.argv,"ms":round((time.monotonic()-start)*1000)}
if entry["model"]:
    try:
        d=json.loads(p.stdout); entry["usage"]=d.get("usage"); entry["provider"]=d.get("provider"); entry["model_id"]=d.get("model")
        entry["label"]=d.get("answers",{}).get("answer",{}).get("label")
    except (ValueError,TypeError): pass
with open(os.environ["GOAL_BENCH_CALL_LOG"],"a") as f: f.write(json.dumps(entry)+"\\n")
sys.stdout.buffer.write(p.stdout);sys.stderr.buffer.write(p.stderr);sys.exit(p.returncode)
'''.replace("REAL_BINARIES", repr(real))
    for name in real:
        path = shim / name
        path.write_text(program)
        path.chmod(0o755)
    return log, {**os.environ, "PATH": str(shim) + os.pathsep + os.environ["PATH"], "GOAL_BENCH_CALL_LOG": str(log)}


def action_metrics(arm, result):
    if not result:
        return None, None
    steps = result.get("history", []) if arm == "axe" else result.get("steps", [])
    if steps is None:
        steps = []
    if not isinstance(steps, list):
        return None, None
    wrong, repeated, previous = 0, 0, None
    for step in steps:
        if arm == "goto":
            kind, target, changed = "tap", step.get("tapped", {}).get("label", ""), step.get("changed")
        elif arm == "goal":
            op = step.get("operation", {})
            kind, target, changed = op.get("kind"), op.get("target", {}).get("label", ""), step.get("changed")
        else:
            kind, target, changed = step.get("operation"), step.get("target", ""), step.get("pageChanged")
        # These controls are deliberately unrelated in every fixture scenario.
        # Other delivery failures are recorded separately from wrong targets.
        wrong += int(str(kind).lower() in ("tap", "openapp") and any(word in (target or "").lower() for word in ("help", "archive", "other item", "settings")))
        signature = (kind, target)
        repeated += int(signature == previous and changed is False)
        previous = signature
    return wrong, repeated


def axe_trace_metrics(trace):
    events = trace.get("events", [])
    return {"abstentions": sum(e.get("decision", {}).get("operation") == "BLOCKED" for e in events),
            "discarded": sum(e.get("discardedAsStale") is True for e in events),
            "stale_discards": sum(e.get("discardedAsStale") is True and e.get("freshness") == "stale" for e in events),
            "blocked_confirmation_discards": sum(e.get("discardedAsStale") is True and e.get("freshness") == "unchanged" and e.get("decision", {}).get("operation") == "BLOCKED" for e in events)}


def execution_error(result):
    if not result or result.get("outcome") == "uncertain" or result.get("status") == "failed":
        return True
    steps = result.get("steps", [])
    return isinstance(steps, list) and any(s.get("receipt", {}).get("delivery") in ("partial", "unknown") or s.get("receipt", {}).get("error") for s in steps)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--udid", required=True)
    p.add_argument("--baseline", required=True)
    p.add_argument("--candidate", required=True)
    p.add_argument("--arms", nargs="+", choices=["goto", "goal", "axe"], default=["goto", "goal", "axe"])
    p.add_argument("--cases", nargs="+")
    p.add_argument("--output", default=".scratch/goal-evaluation")
    args = p.parse_args()
    out = (ROOT / args.output).resolve()
    if (ROOT / ".scratch").resolve() not in out.parents:
        raise SystemExit("raw evaluation evidence must stay under .scratch/")
    out.mkdir(parents=True, exist_ok=True)
    baseline = str(pathlib.Path(args.baseline).resolve())
    candidate = str(pathlib.Path(args.candidate).resolve())
    for name, source in (("baseline-mav", baseline), ("candidate-mav", candidate)):
        frozen = out / name
        if frozen != pathlib.Path(source):
            shutil.copyfile(source, frozen)
        frozen.chmod(0o755)
    baseline, candidate = str(out / "baseline-mav"), str(out / "candidate-mav")
    if "0.34.0" not in run([baseline, "--version"]).stdout:
        raise SystemExit("baseline must be MAV 0.34.0")
    if "axe" in args.arms and not os.environ.get("TYPESAFE_API_KEY"):
        raise SystemExit("TYPESAFE_API_KEY is required for the AXe arm; no synthetic substitutes")
    project = out / "project"
    (project / ".mav").mkdir(parents=True, exist_ok=True)
    (project / ".mav/config.yaml").write_text(f"target_kind: simulator\nsimulator_udid: {args.udid}\nbundle_id: {BUNDLE}\npreferred_ui_driver: axe\n")
    metadata = {"baseline_commit": BASE, "baseline_sha256": hashlib.sha256(pathlib.Path(baseline).read_bytes()).hexdigest(),
                "source_commit": run(["git", "-C", str(ROOT), "rev-parse", "HEAD"]).stdout.strip(),
                "source_dirty": bool(run(["git", "-C", str(ROOT), "status", "--porcelain"]).stdout.strip()),
                "candidate_sha256": hashlib.sha256(pathlib.Path(candidate).read_bytes()).hexdigest(),
                "fixture_source_sha256": hashlib.sha256(pathlib.Path(__file__).with_name("Fixture.swift").read_bytes()).hexdigest(),
                "corpus_sha256": hashlib.sha256(json.dumps(corpus(), sort_keys=True).encode()).hexdigest(),
                "axe_version": run(["axe", "--version"]).stdout.strip(), "jevi_version": run(["jevi", "--version"]).stdout.strip(),
                "mav_driver": "axe", "mav_provider": "same jevi binary, environment and configured MAV key for both arms",
                "cost_note": "null means unavailable from the provider; it never means free"}
    (out / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    call_log, instrumented_env = instrument(out)
    rows = []
    for case in corpus():
        if args.cases and case["id"] not in args.cases:
            continue
        # Rotate ordering to reduce a systematic warm-cache/order advantage.
        arms = list(args.arms)
        offset = len(rows) // max(1, len(arms)) % len(arms)
        arms = arms[offset:] + arms[:offset]
        for arm in arms:
            run(["xcrun", "simctl", "terminate", args.udid, BUNDLE])
            launched = run(["xcrun", "simctl", "launch", args.udid, BUNDLE, case["scenario"], case["number"]])
            if launched.returncode:
                raise RuntimeError("fixture launch failed")
            expected_pid = int(launched.stdout.rsplit(":", 1)[1].strip())
            time.sleep(1)
            if not oracle(args.udid, "Home", expected_pid):
                raise RuntimeError("fixture failed to reset to Home")
            trace = out / f"{case['id']}-{arm}-trace.json"
            if arm == "axe":
                argv = ["axe", "agent", case["goal"], "--udid", args.udid, "--max-steps", "12", "--trace", str(trace), "--json"]
            else:
                argv = [baseline if arm == "goto" else candidate, arm, case["goal"], "--arrived-when" if arm == "goto" else "--succeeded-when", f'title:"{case["title"]}"', "--max-steps", "12", "--timeout", "90s", "--prefer-driver", "axe"]
            start = time.monotonic()
            call_log.write_text("")
            try:
                # Use the wrapped executable for AXe too, but keep absolute
                # candidate/baseline paths to pin both MAV implementations.
                proc = run(argv, cwd=project, timeout=100, env=instrumented_env)
                (out / f"{case['id']}-{arm}.stdout").write_text(proc.stdout)
                (out / f"{case['id']}-{arm}.stderr").write_text(proc.stderr)
                result = json_result(proc.stdout, arm)
                error = proc.returncode != 0
            except subprocess.TimeoutExpired:
                result, error = {}, True
            elapsed = round((time.monotonic() - start) * 1000)
            verified = oracle(args.udid, case["title"], expected_pid)
            metrics = result.get("metrics", {})
            calls = [json.loads(line) for line in call_log.read_text().splitlines()]
            model_calls = [c for c in calls if c["model"]]
            abstentions = sum(c.get("label") == "none" for c in model_calls)
            costs = [(c.get("usage") or {}).get("cost") for c in model_calls]
            cost = sum(costs) if all(isinstance(c, (int, float)) for c in costs) else None
            wrong, repeated = action_metrics(arm, result)
            reported = result.get("arrived") == "true" if arm == "goto" else result.get("succeeded") == "true" if arm == "goal" else result.get("status") == "completed"
            trace_metrics = axe_trace_metrics(json.loads(trace.read_text())) if arm == "axe" and trace.is_file() else {}
            rows.append({"case": case["id"], "arm": arm, "positive": case["positive"], "acceptance": case.get("acceptance", False), "verified": verified, "reported": reported,
                         "model_reported_success": result.get("reported_success") if arm == "goal" else result.get("status") == "completed" if arm == "axe" else None,
                         "false_success": reported and not verified, "error": error, "execution_error": bool(execution_error(result)), "outcome": result.get("outcome", result.get("status", "process_error")),
                         "model_calls": len(model_calls) if arm != "axe" else result.get("modelCalls"),
                         "read_commands": sum(c["read"] for c in calls), "observations": metrics.get("reads", result.get("timing", {}).get("observations")),
                         "abstentions": abstentions if arm != "axe" else trace_metrics.get("abstentions"), "discarded": metrics.get("discarded_decisions") if arm != "axe" else trace_metrics.get("discarded"), "latency_ms": elapsed,
                         "wrong_distractor_actions": wrong, "repeated_without_progress": repeated,
                         "cost_usd": cost if arm != "axe" else result.get("usage", {}).get("estimatedCostUSD"),
                         "cost_kind": "provider_reported" if arm != "axe" else "AXe_estimate",
                         "providers": sorted({str(c.get("provider")) for c in model_calls}) if arm != "axe" else ["typesafe"],
                         "models": sorted({str(c.get("model_id")) for c in model_calls}) if arm != "axe" else [result.get("model")],
                         "steps": len(result.get("steps", [])) if isinstance(result.get("steps"), list) else result.get("steps")})
            (out / "results.json").write_text(json.dumps({"metadata": metadata, "rows": rows, "gate": gate(rows)}, indent=2) + "\n")
            print(json.dumps(rows[-1]), flush=True)
    if not gate(rows)["numerical_gate_passed"]:
        raise SystemExit("acceptance gate not satisfied; goto must remain available")


if __name__ == "__main__":
    main()
