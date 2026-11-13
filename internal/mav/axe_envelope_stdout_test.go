package mav

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Envelopes as AXe 2.0.4 prints them for a completed input, a batch, and a
// selector it rejected before sending anything.
const (
	axeCompletedEnvelope = `{"command":"key","data":{"result":{"outcome":"completed"}},"didError":false,"nextSteps":[],"schema":"axe.output.command-result","schemaVersion":"1"}`
	axeBatchEnvelope     = `{"command":"batch","data":{"result":{"outcome":"completed","steps":3}},"didError":false,"nextSteps":[],"schema":"axe.output.command-result","schemaVersion":"1"}`
	axeNotFoundEnvelope  = `{"command":"tap","data":{},"didError":true,"error":{"code":"elementNotFound","message":"No accessibility element matched --id 'BackButton'."},"nextSteps":[],"schema":"axe.output.command-result","schemaVersion":"1"}`
	axeAmbiguousEnvelope = `{"command":"tap","data":{},"didError":true,"error":{"code":"ambiguousElement","message":"Multiple (2) accessibility elements matched --id 'navbar_close'."},"nextSteps":[],"schema":"axe.output.command-result","schemaVersion":"1"}`
)

func stdoutLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func TestAXeEnvelopeStaysOffStdoutAfterTheResultLine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		response CommandResult
		prefix   string
	}{
		{"key", []string{"ui", "key", "--code", "40"}, CommandResult{Stdout: axeCompletedEnvelope}, "ok cmd=ui.key"},
		{"batch", []string{"ui", "batch", "--spec", `{"steps":[{"key":{"code":40}}]}`}, CommandResult{Stdout: axeBatchEnvelope}, "ok cmd=ui.batch"},
		{"tap failure", []string{"ui", "tap", "--id", "BackButton"}, CommandResult{Stdout: axeNotFoundEnvelope, Code: 1, Err: errors.New("exit status 1")}, "fail code=elementNotFound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, r, _, out := nativeCLI(t, "simulator")
			r.response = tc.response
			_ = c.Run(context.Background(), tc.args)
			lines := stdoutLines(out.String())
			if !strings.HasPrefix(lines[0], tc.prefix) {
				t.Fatalf("result line: %s", out)
			}
			if len(lines) != 1 {
				t.Fatalf("stdout must carry only mav's own line, got %d:\n%s", len(lines), out)
			}
		})
	}
}

func TestRawStillPrintsTheAXeEnvelope(t *testing.T) {
	c, r, _, out := nativeCLI(t, "simulator")
	r.response = CommandResult{Stdout: axeNotFoundEnvelope, Code: 1, Err: errors.New("exit status 1")}
	_ = c.Run(context.Background(), []string{"--raw", "ui", "tap", "--id", "BackButton"})
	if !strings.Contains(out.String(), `"schema":"axe.output.command-result"`) {
		t.Fatalf("--raw must keep the native envelope:\n%s", out)
	}
}

// A design-system nav bar wraps its button in a container with the same id
// and the same frame. They are one target on screen, and AXe refuses the id.
const coincidentNavBarTree = `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[
{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Group","AXFrame":"{{16, 62.333333333333343}, {32, 32}}","children":[
{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{16, 62.333333333333343}, {32, 32}}"}]}]}]`

const separateCloseButtonsTree = `[{"AXLabel":"App","type":"Application","AXFrame":"{{0, 0}, {402, 874}}","children":[
{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{16, 62}, {32, 32}}"},
{"AXUniqueId":"navbar_close","AXLabel":"Close","type":"Button","AXFrame":"{{354, 62}, {32, 32}}"}]}]`

func ambiguousTapRunner(r *nativeTestRunner, tree string) {
	r.respond = func(args []string) (CommandResult, bool) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, " tap ") && strings.Contains(joined, "--id navbar_close"):
			return CommandResult{Stdout: axeAmbiguousEnvelope, Code: 1, Err: errors.New("exit status 1")}, true
		case strings.Contains(joined, " describe-ui"):
			return CommandResult{Stdout: `{"schema":"axe.output.command-result","schemaVersion":"1","didError":false,"data":{"result":` + tree + `}}`}, true
		}
		return CommandResult{}, false
	}
}

func coordinateTaps(r *nativeTestRunner) []string {
	taps := []string{}
	for _, cmd := range r.commands {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, " tap ") && strings.Contains(joined, " -x ") {
			taps = append(taps, joined)
		}
	}
	return taps
}

func TestTapByIDTapsMatchesThatShareOneFrame(t *testing.T) {
	// The plain tap goes through the typed AXe path; a wait sends it through
	// the fast-path tap. Both must resolve the shared frame.
	for _, args := range [][]string{
		{"ui", "tap", "--id", "navbar_close"},
		{"ui", "tap", "--id", "navbar_close", "--wait-id", "navbar_close", "--wait-timeout", "1s"},
	} {
		t.Run(strings.Join(args[2:], " "), func(t *testing.T) {
			c, r, _, out := nativeCLI(t, "simulator")
			ambiguousTapRunner(r, coincidentNavBarTree)
			_ = c.Run(context.Background(), args)
			got := out.String()
			if !strings.HasPrefix(got, "ok cmd=ui.tap") {
				t.Fatalf("matches with one frame are one target and must be tapped:\n%s", got)
			}
			taps := coordinateTaps(r)
			if len(taps) != 1 || !strings.Contains(taps[0], "-x 32 -y 78") {
				t.Fatalf("want one tap at the shared frame's centre, got %v", taps)
			}
			if !strings.Contains(got, "selector_via=tree") || !strings.Contains(got, "matched_id=navbar_close") {
				t.Fatalf("the line must say the id was resolved through the tree:\n%s", got)
			}
		})
	}
}

func TestTapByIDStaysAmbiguousWhenMatchesAreApart(t *testing.T) {
	c, r, _, out := nativeCLI(t, "simulator")
	ambiguousTapRunner(r, separateCloseButtonsTree)
	_ = c.Run(context.Background(), []string{"ui", "tap", "--id", "navbar_close"})
	if !strings.HasPrefix(out.String(), "fail code=ambiguousElement") {
		t.Fatalf("two places on screen must stay ambiguous:\n%s", out)
	}
	if taps := coordinateTaps(r); len(taps) != 0 {
		t.Fatalf("no input may be sent for a real ambiguity: %v", taps)
	}
}
