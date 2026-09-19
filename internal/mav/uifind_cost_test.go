package mav

import (
	"os"
	"path/filepath"
	"testing"
)

// The cost block exists so that "jev is 8x faster" stops being a number in
// someone's notes and becomes something you re-derive by running the command.
// These tests are the control that says the instrument works: a route with no
// model and a route with one have to produce different numbers. If they came
// out the same, the instrument would be measuring nothing and would say so in
// exactly the same way as a find that cost nothing.
//
// Tokens are deliberately not here. find asks jev, not the large model, so the
// saving this command is part of is not in what find spends — it is in the
// tree the caller no longer pastes into its own context.

// fakeJev puts a `jevi` on PATH that answers without a network, reporting a
// latency of its own choosing. The reported latency is deliberately unrelated
// to how long the fake actually takes: that is what proves model_ms is read
// from jev's own measurement of the round trip rather than timed around the
// subprocess, which would charge jev for mav's fork and exec.
func fakeJev(t *testing.T, label string, reportedLatencyMS int) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nsleep 0.15\ncat >/dev/null\n" +
		`printf '%s\n' '{"ok":true,"provider":"fake","latency_ms":` +
		itoa(reportedLatencyMS) +
		`,"answers":{"answer":{"type":"choice","verdict":"yes","label":"` + label + `"}}}'` + "\n"
	path := filepath.Join(dir, "jevi")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	// The model route is only reachable with the refusals off and a key
	// present. The fake never looks at the key; ResolveJevKey insists on one.
	t.Setenv("CI", "")
	t.Setenv("MAV_FIND_DISABLE", "")
	t.Setenv("MAV_JEV_API_KEY", "test-key-not-a-real-one")
}

func TestTheLiteralRouteCostsNoModelTime(t *testing.T) {
	t.Setenv("CI", "1")
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "Wi-Fi")

	if got.ResolvedBy != ResolvedByLiteral {
		t.Fatalf("expected the literal route, got %q", got.ResolvedBy)
	}
	if got.Cost.ModelMS != 0 {
		t.Fatalf("no model was asked, so model_ms must be 0, got %d", got.Cost.ModelMS)
	}
	if got.Cost.LocalMS != got.Cost.TotalMS {
		t.Fatalf("with no model, every millisecond is ours: total=%d local=%d",
			got.Cost.TotalMS, got.Cost.LocalMS)
	}
}

func TestTheModelRouteReportsJevsOwnLatency(t *testing.T) {
	fakeJev(t, "1", 20)
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "the row that opens the camera settings")

	if got.ResolvedBy != ResolvedByModel {
		t.Fatalf("expected the model route, got %q reason=%q", got.ResolvedBy, got.Reason)
	}
	// 20 is what the fake reported; it sleeps 150ms. Timing the subprocess
	// instead would land near 150 and blame jev for our own fork.
	if got.Cost.ModelMS != 20 {
		t.Fatalf("model_ms must be jev's own figure (20), got %d", got.Cost.ModelMS)
	}
	if got.Cost.TotalMS < 100 {
		t.Fatalf("total_ms should cover the whole call (~150ms here), got %d", got.Cost.TotalMS)
	}
	// The split is the reason the block exists: a total that does not separate
	// the round trip from our own work cannot tell anyone what to optimise.
	if got.Cost.LocalMS != got.Cost.TotalMS-got.Cost.ModelMS {
		t.Fatalf("local_ms must be what is left after the round trip: total=%d model=%d local=%d",
			got.Cost.TotalMS, got.Cost.ModelMS, got.Cost.LocalMS)
	}
}

func TestAVetoedAnswerStillReportsWhatItCost(t *testing.T) {
	// The runs worth pricing most are the ones that end in nothing: a find
	// that pays for a round trip and returns no element still spent it.
	fakeJev(t, "4", 20) // 4 is "Borrar cuenta"
	c := CLI{}
	got := c.resolveFind(t.Context(), settingsScreen(), "the row that opens the camera settings")

	if got.Reason != ReasonDestructiveGuard {
		t.Fatalf("expected the destructive veto, got %q reason=%q", got.ResolvedBy, got.Reason)
	}
	if got.Cost.ModelMS != 20 {
		t.Fatalf("a vetoed run still paid for the round trip: model_ms=%d", got.Cost.ModelMS)
	}
}
