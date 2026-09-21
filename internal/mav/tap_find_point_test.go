package mav

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A step that resolved by `find` taps the coordinate it resolved, not the
// label. Every other selector still taps by handle, because only `find` has
// already paid for the read a label tap would pay for again -- and, when a
// model answered, has just asked the screen what sits under that exact point.
//
// Measured on iPhone 17 Pro / iOS 26.3 from a simpool slot, 10 taps each
// alternated in one batch, clean launch before every tap, all 20 navigated:
// 786 ms by coordinate against 899 ms by label.

// {{0, 100}, {200, 50}} centres on 100,125.
const tapFindTree = `[{"AXUniqueId":"openBox","AXLabel":"Abrir caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}]`

const tapFindHit = `{"AXUniqueId":"openBox","AXLabel":"Abrir caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}`

// Another element under the same point: the screen moved under the step.
const tapFindMovedHit = `{"AXUniqueId":"somethingElse","AXLabel":"Nada que ver","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}`

const tapFindMovedTree = `[{"AXUniqueId":"otherBox","AXLabel":"Otra caja","type":"Button","enabled":true,"AXFrame":"{{0, 100}, {200, 50}}"}]`

func tapFindFlow(t *testing.T, step string) (CLI, *sequenceRecordingRunner, *bytes.Buffer, string) {
	t.Helper()
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.Tools = map[string]bool{"axe": true, "idb": true}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - "+step+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &sequenceRecordingRunner{
		tools: cfg.Tools,
		seq:   map[string][]string{},
		calls: map[string]int{},
		out:   map[string]string{},
	}
	out := &bytes.Buffer{}
	return CLI{Runner: runner, Root: root, Stdout: out, Stderr: &bytes.Buffer{}}, runner, out, flowPath
}

func TestTapByFindTapsTheResolvedCoordinate(t *testing.T) {
	fakeJev(t, "1", 10)
	c, runner, out, flowPath := tapFindFlow(t, `tap: { where: { find: "la caja" } }`)
	runner.out["axe describe-ui"] = tapFindTree
	runner.out["axe describe-ui --point 100,125"] = tapFindHit

	if err := c.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	trail := strings.Join(runner.commands, "\n")
	if !strings.Contains(trail, "axe tap --tap-style physical -x 100 -y 125") {
		t.Fatalf("a find must tap the point it resolved: %v", runner.commands)
	}
	// The whole saving: the label never goes back to axe to be resolved a
	// second time against a screen this step has already read.
	if strings.Contains(trail, "--label") {
		t.Fatalf("a find must not hand the label back to axe: %v", runner.commands)
	}
	// One whole-screen read -- the one the decision was made from -- plus the
	// one-point guard. Nothing else.
	if got := countCalls(runner.commands, "axe describe-ui"); got != 2 {
		t.Fatalf("expected the decision read and the point guard, got %d: %v", got, runner.commands)
	}
}

func TestTapBySelectorStillTapsByHandle(t *testing.T) {
	// The change is ring-fenced to `find`. A compound selector is untouched:
	// nothing asked the screen on its behalf, so its coordinate carries no
	// evidence a label tap does not.
	c, runner, out, flowPath := tapFindFlow(t, `tap: { where: { text: "Abrir caja", role: "Button" } }`)
	runner.out["axe describe-ui"] = tapFindTree

	if err := c.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("err=%v out=%s", err, out.String())
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "--label Abrir caja") {
		t.Fatalf("a compound selector must still tap by handle: %v", runner.commands)
	}
}

func TestTapByFindRefusesToTapAScreenThatKeptMoving(t *testing.T) {
	// The ablation the coordinate tap has to survive. The element the model
	// chose is gone from the re-read and the point disagrees again, so the
	// step fails instead of pressing a coordinate nothing stands on any more.
	fakeJev(t, "1", 10)
	c, runner, out, flowPath := tapFindFlow(t, `tap: { where: { find: "la caja" } }`)
	runner.seq["axe describe-ui"] = []string{tapFindTree, tapFindMovedTree}
	runner.out["axe describe-ui --point 100,125"] = tapFindMovedHit

	err := c.Run(context.Background(), []string{"run", flowPath})
	if err == nil && !strings.Contains(out.String(), "element_moved") {
		t.Fatalf("a moving screen must fail the step: err=%v out=%s", err, out.String())
	}
	if strings.Contains(strings.Join(runner.commands, "\n"), "axe tap") {
		t.Fatalf("nothing may be tapped once the guard said no twice: %v", runner.commands)
	}
}
