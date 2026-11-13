package macos

import (
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

func TestGoalCuaSeparatesSemanticTreeAndFreshTokens(t *testing.T) {
	f := cuaExec()
	d := NewCua(f)
	tree, s, err := d.ReferenceTree(t.Context(), macTarget())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tree.JSON), "s1:1") || s.Elements[1].Ref != "s1:1" || s.ID != "s1" {
		t.Fatalf("tree=%s snapshot=%+v", tree.JSON, s)
	}
	before := len(f.commands)
	r, err := d.ExecuteReference(t.Context(), macTarget(), s, drivers.ReferenceOperation{Kind: "tap", Ref: "s1:1"})
	if err != nil || r.Delivery != "completed" || len(f.commands) != before+1 {
		t.Fatalf("%+v %v commands=%v", r, err, f.commands)
	}
	last := f.commands[len(f.commands)-1]
	if !strings.Contains(last, `"element_token":"s1:1"`) || strings.Contains(last, `"x":`) {
		t.Fatal(last)
	}
}

func TestGoalCuaLostOrMalformedMutationReplyNeverCompletesOrRetries(t *testing.T) {
	for _, res := range []drivers.ExecResult{{Stdout: `{"ok":true}`, Code: 1, Err: errors.New("lost connection")}, {Stdout: "not JSON"}, {Stdout: `{"refusal":{"code":"stale_token"}}`}} {
		f := cuaExec()
		d := NewCua(f)
		_, s, err := d.ReferenceTree(t.Context(), macTarget())
		if err != nil {
			t.Fatal(err)
		}
		f.results["cua-driver call click"] = res
		before := len(f.commands)
		r, err := d.ExecuteReference(t.Context(), macTarget(), s, drivers.ReferenceOperation{Kind: "tap", Ref: "s1:1"})
		if err == nil || r.Delivery != "unknown" || len(f.commands) != before+1 {
			t.Fatalf("%+v %v commands=%v", r, err, f.commands)
		}
	}
}
