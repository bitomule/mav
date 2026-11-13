package mav

import (
	"context"
	"strings"
	"testing"
)

func TestAXe2VerificationDoesNotReplayAnUncertainSelectorInput(t *testing.T) {
	cli, runner, out := failingAXeSelectorCLI(t)
	if err := cli.Run(context.Background(), []string{"ui", "tap", "--id", "continue", "--verify"}); err == nil {
		t.Fatal("verification turned a failed input into success")
	}
	if !strings.Contains(out.String(), "input delivery could not be confirmed") {
		t.Fatalf("original failure lost: %s", out.String())
	}
	if strings.Contains(out.String(), "ok cmd=ui.tap") {
		t.Fatalf("unverified tap reported success: %s", out.String())
	}
	assertNoCoordinateReplay(t, runner)
}
