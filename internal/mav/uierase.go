package mav

import (
	"context"
	"fmt"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// maxEraseRounds bounds the erase-read-erase loop. Each round deletes at most
// as many characters as the longest field currently holds, so a field that is
// shrinking empties in one or two rounds; the cap only catches a field that
// keeps changing without emptying.
const maxEraseRounds = 8

// eraseSnapshot is what `mav ui erase` checks its own work against: the
// values of every editable field on screen, joined, plus the length of the
// longest one.
//
// It is the values and not a node count, because the tree of a field losing
// characters has exactly the same shape as one that lost none. And it is
// every editable field rather than "the focused one", because on a simulator
// AXe's tree carries no focus attribute at all — measured on Boxy's search
// field while it was demonstrably focused and accepting typed text, the node
// came back with role, subrole, value and frame and no AXFocused among them.
type eraseSnapshot struct {
	Values  string
	Longest int
	Fields  int
	// Driver is who read the tree, which is not always who erases. It goes
	// into the failures because "no editable field is on screen" reads very
	// differently depending on whether the reader was the one that can see
	// the app's fields at all.
	Driver string
}

func (c CLI) readEraseSnapshot(ctx context.Context, cfg Config) (eraseSnapshot, error) {
	described, err := c.describeUITreeUncached(ctx, cfg, preferDriverAuto, false)
	if err != nil {
		return eraseSnapshot{}, err
	}
	if described.Result.Err != nil {
		return eraseSnapshot{}, described.Result.Err
	}
	snap := eraseSnapshot{Driver: described.Driver}
	var values []string
	for _, el := range ExtractElements(described.Result.Stdout) {
		if !isEditableElement(el) {
			continue
		}
		snap.Fields++
		values = append(values, el.Value)
		if n := len([]rune(el.Value)); n > snap.Longest {
			snap.Longest = n
		}
	}
	snap.Values = strings.Join(values, "\x00")
	return snap, nil
}

// isEditableElement recognises a text input across the vocabularies the
// drivers speak. AXe answers with a role_description ("search text field"),
// macOS with an AX role ("AXTextField"), and both also carry a subrole, so
// the match is on role and subrole together with the spaces and the AX
// prefix removed.
func isEditableElement(el Element) bool {
	key := normaliseRoleKey(el.Role) + "|" + normaliseRoleKey(el.Subrole)
	for _, want := range []string{"textfield", "textview", "textarea", "searchfield", "combobox"} {
		if strings.Contains(key, want) {
			return true
		}
	}
	return false
}

func normaliseRoleKey(role string) string {
	key := strings.ToLower(role)
	key = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key)
	return strings.TrimPrefix(key, "ax")
}

// eraseProbeCharacter is typed and deleted again to tell two screens apart
// that look identical from the tree: a field that is already empty, and a
// field full of text that the driver cannot delete from. Both answer a round
// of deletions with a value that did not change — an empty search field shows
// its placeholder before and after, and so does a dead delete path.
//
// Guessing between them is what the old erase did, in the generous direction,
// and that is the whole defect. So it is measured instead: type one
// character, which must move the value, then delete that one character, which
// must move it back. Both halves moving is a working delete path over a field
// that had nothing in it.
const eraseProbeCharacter = "x"

type eraseProbeVerdict int

const (
	eraseProbeAlreadyEmpty eraseProbeVerdict = iota
	eraseProbeNoFocus                        // the field would not even take a character
	eraseProbeDeleteDead                     // it took the character and would not give it back
)

func (c CLI) probeEraseNoChange(ctx context.Context, cfg Config, target drivers.Target, spec drivers.TextSpec, before eraseSnapshot) (eraseProbeVerdict, eraseSnapshot, error) {
	if err := c.typeEraseProbe(ctx, cfg, target); err != nil {
		return eraseProbeNoFocus, before, err
	}
	typed, err := c.readEraseSnapshot(ctx, cfg)
	if err != nil {
		return eraseProbeNoFocus, before, err
	}
	if typed.Values == before.Values {
		return eraseProbeNoFocus, typed, nil
	}
	round := spec
	round.Deletions = 1
	if _, err := baguetteErase(ctx, c.router(), target, round); err != nil {
		return eraseProbeDeleteDead, typed, err
	}
	deleted, err := c.readEraseSnapshot(ctx, cfg)
	if err != nil {
		return eraseProbeDeleteDead, typed, err
	}
	if deleted.Values == before.Values {
		return eraseProbeAlreadyEmpty, deleted, nil
	}
	return eraseProbeDeleteDead, deleted, nil
}

// typeEraseProbe sends the single sentinel character. It routes CapType the
// same way `mav ui type` does, so the probe exercises the path a caller would
// actually be using.
func (c CLI) typeEraseProbe(ctx context.Context, cfg Config, target drivers.Target) error {
	prefer := "axe"
	if targetKind(cfg) == drivers.KindMac {
		prefer = ""
	}
	driver, _, err := c.router().Route(ctx, drivers.CapType, target, prefer)
	if err != nil {
		return err
	}
	td, ok := driver.(drivers.TypeDriver)
	if !ok {
		return fmt.Errorf("driver %q does not implement TypeDriver", driver.ID())
	}
	return td.Type(ctx, target, drivers.TextSpec{Text: eraseProbeCharacter})
}
