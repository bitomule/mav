package mav

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompactOutputSortsAndQuotes(t *testing.T) {
	var b bytes.Buffer
	err := OK("capture", map[string]string{
		"file": "/tmp/mav/abc/screen.png",
		"next": "review screenshot",
	}).Write(&b, false)
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
	err := Fail("screen_not_found", map[string]string{"screen": "settings"}).Write(&b, false)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(b.String())
	want := "fail code=screen_not_found screen=settings"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
