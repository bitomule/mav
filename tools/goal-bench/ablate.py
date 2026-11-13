#!/usr/bin/env python3
"""Prove critical goal tests fail when their guard is removed, then restore it.

Writes only within the worktree. No git reset/stash or external scratch files.
Never run concurrently with editing or reviewing these production files.
"""
import json
import os
import pathlib
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[2]
MUTATIONS = [
    ("initial_condition", "goal_engine.go", 'result.Succeeded, result.Outcome = "true", "already_satisfied"', 'result.Succeeded, result.Outcome = "unverified", "already_satisfied"', "TestGoalVerifiesAndAlreadySatisfiedDoesNotAct"),
    ("freshness", "goal_engine.go", "if fresh.fingerprint() != current.fingerprint() {", "if false {", "TestGoalDiscardsDecisionWhenGeometryFocusOrValueChanges"),
    ("reported_vs_verified", "goal_engine.go", "result.ReportedSuccess = true", 'result.ReportedSuccess = true; result.Succeeded = "true"', "TestGoalModelDoneCannotVerifyAndNoCriterionStaysUnverified"),
    ("partial_stop", "goal_engine.go", 'if receipt.Delivery != "completed" || receipt.Error != "" {', "if false {", "TestGoalNeverRepeatsPartialUnknownOrPostInputReadFailure"),
    ("flow_replay", "goal_cmd.go", 'if step.Receipt.Delivery != "not_sent" {', "if false {", "TestGoalFlowFailureNeverReplaysDeliveredInput"),
    ("navigation_progress", "goal_engine.go", 'return screenFingerprint(o.Elements)', 'return o.fingerprint()', "TestGoalNavigationKeepsUnchangedPolicyDespiteGeometryJitter"),
    ("modal_budget", "goal_engine.go", "if navigationSteps >= req.MaxSteps {", "if len(result.Steps) >= req.MaxSteps {", "TestGoalModalDoesNotConsumeNavigationBudget"),
    ("cancel_before_input", "goal_engine.go", 'if ctx.Err() != nil {\n\t\t\tresult.Outcome = "timeout"\n\t\t\treturn\n\t\t}\n\t\treceipt :=', 'if false {\n\t\t\tresult.Outcome = "timeout"\n\t\t\treturn\n\t\t}\n\t\treceipt :=', "TestGoalCancellationDuringPreflightPreventsInput"),
    ("hidden_evidence", "goal_condition.go", "s.MatchesRoute(ExtractRoute(visible), visible)", "s.MatchesRoute(ExtractRoute(o.Elements), o.Elements)", "TestGoalHiddenTextAndHeadingCannotSatisfyCondition"),
    ("route_progress", "goal_engine.go", 'return string(route) + "|" + screenFingerprint(visible)', '_ = route; return screenFingerprint(visible)', "TestGoalNavigationCountsUnreferencedWizardTitlesAsProgress"),
    ("known_rotation", "goal_cmd.go", 'if hid.Detected != 0 && hid.Rotation == 0 {', 'if false {', "TestGoalRunRefusesKnownRotationWithoutValidatedScreenSize"),
    ("unknown_cleanup", "drivers/axe/references.go", '(a.Cleanup != "released" && a.Cleanup != "notNeeded")', 'a.Cleanup == "uncertain"', "TestGoalUnknownReferenceCleanupNeverAllowsRetry"),
]


def main():
    out = ROOT / ".scratch/goal-ablations"
    out.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "TMPDIR": str(ROOT / ".scratch"), "GOCACHE": str(ROOT / ".build/go-cache"), "GOMODCACHE": str(ROOT / ".build/go-mod-cache")}
    results = []
    for name, file, before, after, test in MUTATIONS:
        path = ROOT / "internal/mav" / file
        original = path.read_text()
        if original.count(before) != 1:
            raise RuntimeError(f"{name}: expected exactly one mutation site")
        package = "./internal/mav/drivers/axe" if file.startswith("drivers/axe/") else "./internal/mav"
        command = ["go", "test", package, "-run", "^" + test + "$", "-count=1", "-v"]
        try:
            path.write_text(original.replace(before, after, 1))
            red = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, timeout=60)
        finally:
            path.write_text(original)
        (out / f"{name}-red.log").write_text(red.stdout + red.stderr)
        if red.returncode == 0 or "--- FAIL:" not in red.stdout:
            raise RuntimeError(f"{name}: mutation did not cause an executed test assertion to fail")
        green = subprocess.run(command, cwd=ROOT, env=env, text=True, capture_output=True, timeout=60)
        (out / f"{name}-green.log").write_text(green.stdout + green.stderr)
        if green.returncode:
            raise RuntimeError(f"{name}: restored guard did not pass")
        results.append({"guard": name, "test": test, "without_guard": "FAIL", "restored": "PASS"})
        print(json.dumps(results[-1]), flush=True)
        (out / "results.json").write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
