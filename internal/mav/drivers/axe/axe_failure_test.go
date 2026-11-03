package axe

import (
	"errors"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// A killed axe writes nothing to stderr; the error has to say how it ended
// instead of "command failed".
func TestFailureTextSaysHowASilentAxeEnded(t *testing.T) {
	killed := failureText(drivers.ExecResult{Code: -1, Err: errors.New("signal: killed")})
	if !strings.Contains(killed, "signal: killed") || !strings.Contains(killed, "reinstall axe") {
		t.Fatalf("killed: %q", killed)
	}
	exited := failureText(drivers.ExecResult{Code: 3, Err: errors.New("exit status 3")})
	if !strings.Contains(exited, "exit status 3") || strings.Contains(exited, "reinstall") {
		t.Fatalf("exited: %q", exited)
	}
	if got := failureText(drivers.ExecResult{Stderr: "No device\nmore", Code: 1, Err: errors.New("exit status 1")}); got != "No device" {
		t.Fatalf("stderr first line: %q", got)
	}
}
