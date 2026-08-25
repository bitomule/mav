package mav

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestCompactOutputSortsAndQuotes(t *testing.T) {
	var b bytes.Buffer
	err := OK("capture", map[string]string{
		"file": "/tmp/mav/abc/screen.png",
		"next": "review screenshot",
	}).Write(&b)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(b.String())
	want := `ok cmd=capture file=/tmp/mav/abc/screen.png next="review screenshot"`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFailOutput(t *testing.T) {
	var b bytes.Buffer
	// A failure returns CommandFailed BESIDES writing the line: it is what
	// makes the process exit code match what the output says. Writing and
	// returning nil left `mav ... && next` chaining after a failure.
	var failed CommandFailed
	if err := Fail("screen_not_found", map[string]string{"screen": "settings"}).Write(&b); !errors.As(err, &failed) {
		t.Fatalf("a failure must propagate: %v", err)
	}
	got := strings.TrimSpace(b.String())
	want := "fail code=screen_not_found screen=settings"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// allowFail accepts the error that now accompanies a `fail` line, and
// keeps treating any other as a real error. The tests using it already
// assert the specific code on stdout, which is the useful check; what they
// cannot do is demand success.
func allowFail(t *testing.T, err error) {
	t.Helper()
	var failed CommandFailed
	if err != nil && !errors.As(err, &failed) {
		t.Fatal(err)
	}
}
