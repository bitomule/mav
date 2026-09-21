package mav

import (
	"strings"
	"testing"
)

func mustCriterion(t *testing.T, spec string) ArrivalCriterion {
	t.Helper()
	c, err := ParseArrivalCriterion(spec)
	if err != nil {
		t.Fatalf("ParseArrivalCriterion(%q): %v", spec, err)
	}
	return c
}

// The defect: `--arrived-when 'id:boxes-view'` parsed, ran, and reported
// arrived=false, because an unknown prefix was quietly turned into "a title
// containing the literal text id:boxes-view" — a title no screen will ever
// have. The caller then debugged their app instead of their command.
func TestAnUnknownPrefixIsASyntaxErrorAndNotATitle(t *testing.T) {
	for _, spec := range []string{
		`id:boxes-view`,
		`route:settings`,
		`label:"Cámara"`,
		`title:"Cámara" id:boxes-view`,
	} {
		c, err := ParseArrivalCriterion(spec)
		if err == nil {
			t.Fatalf("%s parsed instead of failing, as criterion %s", spec, c.String())
		}
		if !c.IsZero() {
			t.Fatalf("%s failed but still returned a criterion: %s", spec, c.String())
		}
		// The message has to say what the prefixes ARE, or the caller is back
		// to guessing.
		for _, want := range []string{"title:", "text:", "screen:"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error does not mention %s: %v", spec, want, err)
			}
		}
	}
}

// The deliberate behaviour the fix must not break: a term with no prefix at all
// is a title, because that is what people mean when they name a screen.
func TestABareWordIsStillATitle(t *testing.T) {
	for spec, want := range map[string]string{
		"Cámara":            "Cámara",
		`"Idioma y región"`: "Idioma y región",
	} {
		c := mustCriterion(t, spec)
		if len(c.Titles) != 1 || c.Titles[0] != want {
			t.Errorf("%s parsed to %#v, want the single title %q", spec, c.Titles, want)
		}
	}
}

// Both sides of the line that separates "looks like a prefix" from "is a title
// with a colon in it". `Moving Boxes: Office cables` is a real screen name in
// the test bank, so the colon alone cannot be the signal.
func TestWhereAColonIsPunctuationAndWhereItIsAPrefix(t *testing.T) {
	titles := []struct {
		spec string
		why  string
	}{
		{`"Moving Boxes: Office cables"`, "quoted: the whole term is one title, colon and all"},
		{`Moving Boxes: Office cables`, "unquoted, so `Boxes:` is a term — but its colon is trailing"},
		{`"id:boxes-view"`, "quoting is how you ask for a title that starts with word-colon"},
		{`Boxes:`, "a trailing colon is punctuation in a name, not an empty prefix"},
		{`2:1000`, "digits before the colon are not a prefix word"},
		{`Category 2: 1000`, "the same shape the test bank's own titles have"},
	}
	for _, tc := range titles {
		c, err := ParseArrivalCriterion(tc.spec)
		if err != nil {
			t.Errorf("%s was rejected (%s): %v", tc.spec, tc.why, err)
			continue
		}
		if len(c.Titles) == 0 {
			t.Errorf("%s produced no title (%s)", tc.spec, tc.why)
		}
	}

	prefixes := []struct {
		spec string
		why  string
	}{
		{`id:boxes-view`, "a bare word, a colon, and a value is exactly a prefix's shape"},
		{`Id:boxes-view`, "case does not rescue an unknown prefix"},
		{`screenname:boxes`, "close to a real prefix is still not one"},
	}
	for _, tc := range prefixes {
		if _, err := ParseArrivalCriterion(tc.spec); err == nil {
			t.Errorf("%s was accepted as a title (%s)", tc.spec, tc.why)
		}
	}
}

// A known prefix with nothing after it keeps behaving as it always did: the
// term contributes nothing rather than becoming a title called "title:".
func TestAKnownPrefixWithNoValueIsStillNotATitle(t *testing.T) {
	for _, spec := range []string{"title:", "text:", "screen:"} {
		c := mustCriterion(t, spec)
		if !c.IsZero() {
			t.Errorf("%s produced %s, want an empty criterion", spec, c.String())
		}
	}
}
