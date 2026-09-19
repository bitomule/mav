package mav

import (
	"strings"
	"unicode"
)

// `mav ui find` answers one question: which element on this screen is the one
// the caller described in their own words? It returns an element or it returns
// nothing, and the second is a real answer rather than a failure.
//
// Three rules from the measurements this was built on, none of them negotiable:
//
//  1. No numeric threshold anywhere. Correct picks score from 0.76 and wrong
//     ones reach 0.88, so no cut separates them. What separates them is the
//     model declining to answer, so that is what is read.
//  2. find never returns an element it is not sure of. It abstains and the
//     caller falls back to the tree, which is what it was going to read anyway.
//  3. A veto in code that can only ever remove a yes, never add one. Asked for
//     something that was not on the screen at all, the model still chose an
//     element 13 times out of 26 — so the code has to be able to overrule it,
//     and must be unable to overrule it in the other direction.

// FindResult is the whole answer. resolved_by is the first field because it is
// the one that decides what the caller does next.
type FindResult struct {
	ResolvedBy string   `json:"resolved_by"`
	Element    *Element `json:"element"`
	Goal       string   `json:"goal"`
	Verdict    string   `json:"verdict,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Candidates int      `json:"candidates"`
	// Omitted counts candidates that did not fit in the batch sent to the
	// model. It exists because the failure this command was built to close is
	// a tree that drops half a screen without saying so; find must not repeat
	// it in its own output.
	Omitted int    `json:"omitted,omitempty"`
	Next    string `json:"next,omitempty"`
}

// How a find resolved.
const (
	// ResolvedByLiteral: the goal is the element's own text. No model was
	// consulted, so this works with no key and no network.
	ResolvedByLiteral = "literal"
	// ResolvedByModel: the model chose it and every veto let it through.
	ResolvedByModel = "model"
	// ResolvedByNone: find is not answering with an element. Reason says why,
	// and "I could not ask" is a different answer from "I looked and I am not
	// sure", which is why the reasons below are distinct.
	ResolvedByNone = "none"
)

// Reasons for a resolved_by=none. The first three are "could not ask"; the
// rest are "asked, and the answer does not pass".
const (
	ReasonNoKey            = "no_key"
	ReasonNoNetwork        = "no_network"
	ReasonCIRefused        = "ci_refused"
	ReasonNoCandidates     = "no_candidates"
	ReasonAbstained        = "abstained"
	ReasonNotACandidate    = "veto_not_a_candidate"
	ReasonDestructiveGuard = "veto_destructive"
)

// findCandidateCap bounds the batch handed to the model. When it bites, the
// count of what was left out is reported; it is never applied in silence.
const findCandidateCap = 120

// FindCandidates picks the elements that could plausibly be the answer:
// actionable, and carrying some text to be described by. Order is the tree's
// own, which is deterministic, so two runs on one screen send the same batch.
func FindCandidates(elements []Element) []Element {
	out := make([]Element, 0, len(elements))
	for _, el := range elements {
		if !isActionable(el) {
			continue
		}
		if findElementText(el) == "" {
			continue
		}
		out = append(out, el)
	}
	return out
}

// elementText is everything about an element a person could have meant when
// they described it.
func findElementText(el Element) string {
	parts := make([]string, 0, 4)
	for _, s := range []string{el.Label, el.Title, el.Value, el.ID} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// FindLiteral resolves a goal that is simply an element's own text, with no
// model involved. Measured at 14.4% of real searches, and it is the part that
// keeps working with no key and no network.
//
// Only an unambiguous match counts: two elements reading "Guardar" is exactly
// the case where a person has to be asked which one, so it falls through to the
// model rather than picking the first.
func FindLiteral(candidates []Element, goal string) (*Element, bool) {
	want := normalizeForMatch(goal)
	if want == "" {
		return nil, false
	}
	var hit *Element
	found := 0
	for i := range candidates {
		if literalFieldsMatch(candidates[i], want) {
			found++
			if found > 1 {
				return nil, false
			}
			hit = &candidates[i]
		}
	}
	if found == 1 {
		return hit, true
	}
	return nil, false
}

func literalFieldsMatch(el Element, want string) bool {
	for _, field := range []string{el.Label, el.Title, el.Value, el.ID} {
		if normalizeForMatch(field) == want {
			return true
		}
	}
	return false
}

// normalizeForMatch folds case, collapses whitespace and strips the combining
// marks off accented letters, so "cámara" typed as "Camara" still matches the
// label the app actually renders.
func normalizeForMatch(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsSpace(r) {
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
			continue
		}
		lastSpace = false
		b.WriteRune(foldAccent(r))
	}
	return strings.TrimSpace(b.String())
}

// foldAccent maps the accented Latin letters that appear in the languages mav
// drives to their base letter. A table rather than a normalisation library
// because the set is small, closed, and adding a dependency for it is not worth
// the import.
func foldAccent(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'ö', 'õ':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	}
	return r
}

// destructiveTerms is the lexicon the guard reads. It is deliberately broad and
// multilingual: a false positive costs one abstention and a fall back to the
// tree, a false negative costs the user's data.
var destructiveTerms = []string{
	// Spanish
	"borrar", "borra", "eliminar", "elimina", "suprimir",
	"cerrar sesion", "cerrar la sesion", "restablecer", "restaurar",
	"desactivar", "dar de baja", "cancelar suscripcion", "vaciar",
	"anular", "revocar", "desinstalar", "formatear",
	// English
	"delete", "erase", "remove", "wipe", "destroy",
	"sign out", "log out", "logout", "signout",
	"reset", "revoke", "deactivate", "unsubscribe", "uninstall",
	"clear all", "empty trash", "discard", "factory",
}

// IsDestructive reports whether a piece of text names an action that cannot be
// taken back.
func IsDestructive(text string) bool {
	norm := normalizeForMatch(text)
	if norm == "" {
		return false
	}
	for _, term := range destructiveTerms {
		if strings.Contains(norm, term) {
			return true
		}
	}
	return false
}

// VetoChoice is the rule the model cannot get around, and the direction matters:
// it takes a yes away and it has no way to produce one. Both arms exist because
// both were measured.
//
//   - not a candidate: asked for something absent from the screen, the model
//     picked anyway in 13 of 26 runs. An answer naming something that was not in
//     the batch is discarded rather than looked up.
//   - destructive: on 29 screens carrying Delete and Sign out, no wrong pick got
//     past this guard. An element that destroys something is only ever returned
//     when the caller's own words asked for that.
//
// Returns the reason it vetoed, or "" for no veto.
func VetoChoice(chosen *Element, goal string, candidates []Element) string {
	if chosen == nil {
		return ReasonNotACandidate
	}
	inBatch := false
	for i := range candidates {
		if sameElement(candidates[i], *chosen) {
			inBatch = true
			break
		}
	}
	if !inBatch {
		return ReasonNotACandidate
	}
	if IsDestructive(findElementText(*chosen)) && !IsDestructive(goal) {
		return ReasonDestructiveGuard
	}
	return ""
}

// sameElement compares on the identifying fields rather than the whole struct,
// because frame coordinates drift by fractions of a point between two reads of
// one unchanged screen.
func sameElement(a, b Element) bool {
	return a.ID == b.ID && a.Label == b.Label && a.Role == b.Role &&
		a.Value == b.Value && a.Title == b.Title
}

// FindBatch is the candidate list actually sent, with the count of what did not
// fit. Splitting it out keeps the cap visible to the tests and to the output.
func FindBatch(candidates []Element) (batch []Element, omitted int) {
	if len(candidates) <= findCandidateCap {
		return candidates, 0
	}
	return candidates[:findCandidateCap], len(candidates) - findCandidateCap
}

// RenderFindCandidates writes the batch as the numbered list the model chooses
// from. One line per element, identity fields only: the model is picking, not
// tapping, so coordinates are noise it could hallucinate arithmetic over.
func RenderFindCandidates(batch []Element) string {
	var b strings.Builder
	for i, el := range batch {
		b.WriteString(itoa(i + 1))
		b.WriteString(") ")
		fields := make([]string, 0, 5)
		if el.Label != "" {
			fields = append(fields, "label="+el.Label)
		}
		if el.Title != "" && el.Title != el.Label {
			fields = append(fields, "title="+el.Title)
		}
		if el.Value != "" {
			fields = append(fields, "value="+el.Value)
		}
		if el.ID != "" {
			fields = append(fields, "id="+el.ID)
		}
		if el.Role != "" {
			fields = append(fields, "role="+el.Role)
		}
		if el.Enabled == "false" {
			fields = append(fields, "enabled=false")
		}
		b.WriteString(strings.Join(fields, " "))
		b.WriteString("\n")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

// FindQuestion is the wording sent with the candidate list. It says what the
// screen is, what the caller wants, and — the part the measurements insisted on
// — that declining is a correct answer rather than a failure to be avoided.
func FindQuestion(goal string) string {
	return "Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
		"A user described what they want to tap as: " + goal + "\n\n" +
		"Which numbered element is it? Answer with that number.\n" +
		"Answer `none` if no element on this screen is the one described, or if two or more " +
		"could equally be it. Answering `none` is a correct and expected answer: a caller that " +
		"gets `none` reads the screen itself, whereas a caller that gets the wrong number taps " +
		"the wrong thing. Do not guess."
}

// FindOptions is the option list for the question: every candidate index, plus
// the abstention. `none` is an option and not only an inferred silence, so the
// model has somewhere to put a refusal.
func FindOptions(batch []Element) []string {
	out := make([]string, 0, len(batch)+1)
	for i := range batch {
		out = append(out, itoa(i+1))
	}
	return append(out, "none")
}

// InterpretFindAnswer turns the model's answer into a choice, applying rule 2:
// anything that is not a plain yes on a real index is an abstention. Nothing
// numeric is read — confidence is deliberately not a parameter of this function,
// because correct picks score from 0.76 and wrong ones reach 0.88.
func InterpretFindAnswer(verdict, label string, batch []Element) (*Element, string) {
	if verdict != "yes" {
		return nil, ReasonAbstained
	}
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" || label == "none" {
		return nil, ReasonAbstained
	}
	idx := 0
	for _, r := range label {
		if r < '0' || r > '9' {
			return nil, ReasonNotACandidate
		}
		idx = idx*10 + int(r-'0')
	}
	if idx < 1 || idx > len(batch) {
		return nil, ReasonNotACandidate
	}
	el := batch[idx-1]
	return &el, ""
}
