package axe

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func agentOutput(didError bool, data string, err string) string {
	return fmt.Sprintf(`{"schema":"axe.output.agent-result","schemaVersion":"1","didError":%t,"data":%s%s}`, didError, data, err)
}
func agentCapture(id string, next string) string {
	return `{"cache":{"status":"refreshed","requiresRefresh":false},"capture":{"snapshotID":"` + id + `","applicationPID":101,"context":{"bootIdentity":"boot","screenID":0},"targets":["e1|tap|button|Save||save"],"scroll":[]` + next + `}}`
}

func TestGoalReferenceSnapshotPagesSameIdentityAndRejectsChanges(t *testing.T) {
	for _, changed := range []bool{false, true} {
		f := fake()
		n := 0
		f.handle = func(_ string, args []string) drivers.ExecResult {
			n++
			id := "snapshot-1"
			next := `,"nextRowOffset":64`
			if n == 2 {
				next = ""
				if changed {
					id = "snapshot-2"
				}
			}
			return drivers.ExecResult{Stdout: agentOutput(false, agentCapture(id, next), "")}
		}
		s, e := New(f).ReferenceSnapshot(t.Context(), drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"})
		if changed {
			if e == nil {
				t.Fatal("joined snapshots from different captures")
			}
			continue
		}
		if e != nil || s.ID != "snapshot-1" || len(s.Elements) != 2 || !strings.Contains(f.commands[1], "--snapshot-id snapshot-1 --row-offset 64") {
			t.Fatalf("%+v %v %v", s, e, f.commands)
		}
		if strings.Contains(strings.Join(f.commands, "\n"), "axe agent ") {
			t.Fatal("autonomous loop invoked")
		}
	}
}

func TestGoalReferenceActionPreservesCompletedPartialAndUnknownReceipts(t *testing.T) {
	for _, tc := range []struct {
		name, outcome, cleanup string
		events                 int
		didError               bool
		want                   string
	}{
		{"completed_after_refresh_failed", "enqueuedAndDrained", "released", 2, false, "completed"},
		{"partial", "failedNoReplay", "released", 1, true, "partial"},
		{"quarantined", "enqueuedAndDrained", "uncertain", 2, true, "unknown"},
		{"clean_refusal", "failedBeforeInput", "released", 0, true, "not_sent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fake()
			f.result.Stdout = agentOutput(tc.didError, fmt.Sprintf(`{"cache":{"requiresRefresh":true},"action":{"kind":"tap","outcome":%q,"cleanup":%q,"enqueuedEventCount":%d,"replayed":false}}`, tc.outcome, tc.cleanup, tc.events), "")
			r, e := New(f).ExecuteReference(t.Context(), drivers.Target{Kind: drivers.KindSim, UDID: "SIM-1"}, drivers.ReferenceSnapshot{ID: "snapshot-1", PID: 101}, drivers.ReferenceOperation{Kind: "tap", Ref: "e1"})
			if r.Delivery != tc.want || (tc.want == "completed" && e != nil) || len(f.commands) != 1 {
				t.Fatalf("%+v %v %v", r, e, f.commands)
			}
			if !strings.Contains(f.commands[0], "--snapshot-id snapshot-1 --ref e1 --expected-pid 101") || !strings.Contains(f.commands[0], "--agent --view full") {
				t.Fatal(f.commands)
			}
		})
	}
	for _, raw := range []string{"not JSON", `{"schema":"axe.output.agent-result","schemaVersion":"1","data":{}}`} {
		f := fake()
		f.result.Stdout = raw
		r, e := New(f).ExecuteReference(t.Context(), drivers.Target{Kind: drivers.KindSim}, drivers.ReferenceSnapshot{ID: "s", PID: 101}, drivers.ReferenceOperation{Kind: "tap", Ref: "e1"})
		if e == nil || r.Delivery != "unknown" {
			t.Fatalf("%+v %v", r, e)
		}
	}
}

func TestGoalReferenceTypeDoesNotCombineFocusAndTypingOrFallback(t *testing.T) {
	f := fake()
	f.result.Stdout = agentOutput(false, `{"cache":{"requiresRefresh":false},"action":{"kind":"typeText","outcome":"enqueuedAndDrained","cleanup":"released","enqueuedEventCount":2,"replayed":false}}`, "")
	d := New(f)
	s := drivers.ReferenceSnapshot{ID: "s", PID: 101}
	if _, e := d.ExecuteReference(t.Context(), drivers.Target{Kind: drivers.KindSim}, s, drivers.ReferenceOperation{Kind: "type", Ref: "e1", Text: "Sample"}); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(f.commands[0], "--focus") || len(f.commands) != 1 {
		t.Fatal(f.commands)
	}
	for _, target := range []drivers.Target{{Kind: drivers.KindDevice}, {Kind: drivers.KindMac}} {
		r, e := d.ExecuteReference(t.Context(), target, s, drivers.ReferenceOperation{Kind: "tap", Ref: "e1"})
		if e == nil || r.Delivery != "not_sent" {
			t.Fatalf("%+v %v", r, e)
		}
	}
	if len(f.commands) != 1 {
		t.Fatal("unsupported target sent input")
	}
}

func TestGoalUnknownReferenceCleanupNeverAllowsRetry(t *testing.T) {
	for _, outcome := range []string{"failedBeforeInput", "busy", "enqueuedAndDrained"} {
		f := fake()
		f.result.Stdout = agentOutput(true, fmt.Sprintf(`{"cache":{"requiresRefresh":true},"action":{"kind":"tap","outcome":%q,"cleanup":"futureCleanupState","enqueuedEventCount":0,"replayed":false}}`, outcome), "")
		r, e := New(f).ExecuteReference(t.Context(), drivers.Target{Kind: drivers.KindSim}, drivers.ReferenceSnapshot{ID: "s", PID: 101}, drivers.ReferenceOperation{Kind: "tap", Ref: "e1"})
		if e == nil || r.Delivery != "unknown" || r.Stale {
			t.Fatalf("unknown cleanup permitted retry: outcome=%s receipt=%+v err=%v", outcome, r, e)
		}
	}
}
