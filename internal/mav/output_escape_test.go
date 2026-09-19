package mav

import (
	"bytes"
	"strings"
	"testing"
)

// Every mav line is read by an agent or pasted into a terminal, never
// embedded in a web page, so json.Marshal's HTML escaping is pure damage
// here. It turned the remediation of ambiguous_booted_simulator -- the
// most-read failure of the v0.19 line -- into an instruction to run
// `mav sim select <udid>`, which is not a command.
func TestQuotedFieldsKeepAnglesAndAmpersands(t *testing.T) {
	var out bytes.Buffer
	// Write returns the command's own failure, which is the point of a
	// fail line, not a problem with writing it.
	_ = Fail("probe", map[string]string{
		"next": "run `mav sim select <udid>` & try again",
	}).Write(&out)
	got := out.String()

	for _, escaped := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if strings.Contains(got, escaped) {
			t.Fatalf("a field an agent has to type back must not be HTML-escaped (%s):\n%s", escaped, got)
		}
	}
	if !strings.Contains(got, "<udid>") || !strings.Contains(got, "& try again") {
		t.Fatalf("the characters must survive verbatim:\n%s", got)
	}
}

// ...and the quoting itself is unchanged: a value with a space or a quote
// in it still comes back as one JSON string, not as loose words.
func TestQuotingStillEscapesWhatItMust(t *testing.T) {
	got := quoteIfNeeded(`he said "hi" there`)
	if got != `"he said \"hi\" there"` {
		t.Fatalf("got %s", got)
	}
	if quoteIfNeeded("plain") != "plain" {
		t.Fatalf("a bare token must stay bare, got %s", quoteIfNeeded("plain"))
	}
	if quoteIfNeeded("") != `""` {
		t.Fatalf("empty must stay explicit, got %s", quoteIfNeeded(""))
	}
}
