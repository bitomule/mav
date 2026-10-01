package idb

import (
	"context"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func withCompanionAt(t *testing.T, real string) {
	t.Helper()
	prev := resolveLink
	resolveLink = func(string) (string, error) { return real, nil }
	t.Cleanup(func() { resolveLink = prev })
}

// 1.1.8 is what Homebrew had installed when Xcode 27 shipped: every simulator
// tap failed with "SimulatorKit is required for HID interactions".
func TestProbeRefusesACompanionOlderThanTheFloor(t *testing.T) {
	withCompanionAt(t, "/opt/homebrew/Cellar/idb-companion/1.1.8/bin/idb_companion")
	exec := &fakeExec{tools: map[string]bool{"idb": true, "idb_companion": true}}
	report := New(exec).Probe(context.Background(), exec)
	if report.IsHealthy() {
		t.Fatalf("expected idb_companion 1.1.8 to be refused, got %s", report.State)
	}
	if !strings.Contains(report.Next, "mav setup --install idb") || !strings.Contains(report.Detail, "1.6.4") {
		t.Fatalf("expected the floor and the upgrade command, got detail=%q next=%q", report.Detail, report.Next)
	}
	if len(exec.commands) != 0 {
		t.Fatalf("the probe must not spawn anything, ran %v", exec.commands)
	}
}

func TestProbeAcceptsTheFloorCompanion(t *testing.T) {
	withCompanionAt(t, "/opt/homebrew/Cellar/idb-companion/1.6.4/bin/idb_companion")
	exec := &fakeExec{tools: map[string]bool{"idb": true, "idb_companion": true}}
	if report := New(exec).Probe(context.Background(), exec); report.State != drivers.HealthOK {
		t.Fatalf("expected OK, got %s (%s)", report.State, report.Detail)
	}
}

func TestProbeDoesNotRefuseACompanionWhoseVersionItCannotRead(t *testing.T) {
	withCompanionAt(t, "/Users/someone/tools/idb_companion")
	exec := &fakeExec{tools: map[string]bool{"idb": true, "idb_companion": true}}
	if report := New(exec).Probe(context.Background(), exec); report.State != drivers.HealthOK {
		t.Fatalf("expected OK for an unreadable version, got %s (%s)", report.State, report.Detail)
	}
}
