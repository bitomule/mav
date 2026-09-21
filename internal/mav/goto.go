package mav

import (
	"fmt"
	"sort"
	"strings"
)

// `mav goto` is `mav ui find` in a loop: look at the screen, decide what to tap
// to get closer, tap it, look again, until it arrives or gives up.
//
// A loop that presses buttons on its own is the most dangerous thing in this
// tool, so the design starts at when it stops and what it never touches rather
// than at how it advances. The reasoning behind each rule is in
// docs/design/goto.md; what follows is only what the rules ARE.
//
// The one that is easiest to get wrong, and was: arrival is decided by CODE,
// comparing ROUTES, and the criterion must be ABSENT from the starting route or
// the command is refused. Checking whether the criterion text appears anywhere
// in the tree declares victory at step zero — "Notificaciones" is already on the
// Settings list before you tap anything, tab bar labels are in every tree, and a
// Back button carries the previous screen's name.

// Route is the closest thing an iOS screen has to a URL: not an address, but the
// few elements that say WHERE you are rather than what is on offer. Read off
// roles and traits, never by searching free text.
type Route struct {
	// Screen is mav's own identity for the screen, read off the shallowest
	// element whose accessibility id names a view. It is the only part of a
	// route that survives a screen with no navigation-bar title, which on a
	// Spanish-locale SwiftUI app is most of them — measured on Boxy, where the
	// category grid and the box list both have a navigation bar and neither
	// has a heading in it.
	Screen string `json:"screen,omitempty"`
	Tab    string `json:"tab,omitempty"`
	Title  string `json:"title,omitempty"`
	Modal  string `json:"modal,omitempty"`
}

// IsZero reports a route that identifies nothing, which is a real state: a
// screen with no heading, no selected tab and no modal cannot be arrived at by
// route comparison, and goto says so rather than guessing.
func (r Route) IsZero() bool {
	return r.Screen == "" && r.Tab == "" && r.Title == "" && r.Modal == ""
}

func (r Route) String() string {
	parts := make([]string, 0, 4)
	if r.Screen != "" {
		parts = append(parts, "screen="+r.Screen)
	}
	if r.Tab != "" {
		parts = append(parts, "tab="+r.Tab)
	}
	if r.Title != "" {
		parts = append(parts, "title="+r.Title)
	}
	if r.Modal != "" {
		parts = append(parts, "modal="+r.Modal)
	}
	if len(parts) == 0 {
		return "(no route)"
	}
	return strings.Join(parts, " ")
}

// ExtractRoute reads the route off the tree.
//
// Title is the first `heading`, which on iOS is the navigation bar's title —
// verified on a real Settings screen, where `role=heading label=General` is the
// screen name and the second heading is its descriptive blurb. First, not any,
// for exactly that reason.
func ExtractRoute(elements []Element) Route {
	var route Route
	if id, _, ok := explicitScreenIdentity(elements); ok {
		route.Screen = id
	}
	for _, el := range elements {
		role := strings.ToLower(el.Role)
		switch {
		case route.Modal == "" && isModalRole(role):
			route.Modal = routeFirstNonEmpty(el.Label, el.Title, el.ID)
		case route.Title == "" && strings.Contains(role, "heading"):
			route.Title = strings.TrimSpace(el.Label)
		case route.Tab == "" && strings.Contains(role, "tab") && el.Selected == "true":
			route.Tab = routeFirstNonEmpty(el.Label, el.Title, el.ID)
		}
	}
	return route
}

func isModalRole(role string) bool {
	for _, hit := range []string{"alert", "sheet", "dialog", "popover"} {
		if strings.Contains(role, hit) {
			return true
		}
	}
	return false
}

func routeFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// ArrivalCriterion is what the caller declared with --arrived-when. Every term
// must hold, which is what makes a parameterised screen expressible:
// `title:"Order detail" text:"123"` is a different destination from the same
// title with a different order.
type ArrivalCriterion struct {
	Titles  []string
	Texts   []string
	Screens []string
}

// IsZero reports that no criterion was declared. goto then cannot assert
// arrival at all and says `arrived=unverified`; it never says true.
func (a ArrivalCriterion) IsZero() bool {
	return len(a.Titles) == 0 && len(a.Texts) == 0 && len(a.Screens) == 0
}

// Where the criterion came from. Reported on every run, because a criterion a
// person wrote and one nobody wrote are not the same evidence, and today the
// output says which without the caller having to remember what it passed.
const (
	CriterionExplicit = "explicit"
	CriterionNone     = "none"
)

// String writes the criterion back in the same syntax --arrived-when takes.
func (a ArrivalCriterion) String() string {
	parts := make([]string, 0, len(a.Titles)+len(a.Texts)+len(a.Screens))
	for _, t := range a.Screens {
		parts = append(parts, `screen:"`+t+`"`)
	}
	for _, t := range a.Titles {
		parts = append(parts, `title:"`+t+`"`)
	}
	for _, t := range a.Texts {
		parts = append(parts, `text:"`+t+`"`)
	}
	return strings.Join(parts, " ")
}

// arrivalPrefixes are the only prefixes --arrived-when understands, in the
// order the error message lists them.
var arrivalPrefixes = []string{"title:", "text:", "screen:"}

// ParseArrivalCriterion reads `title:"..."`, `text:"..."` and `screen:"..."`
// terms. A bare word with no prefix is a title, because that is what people
// mean when they name a screen, and guessing the other way round would silently
// match a row label.
//
// A term that LOOKS like a prefix and is not one is a syntax error, not a
// title: `id:boxes-view` used to become "find a title containing the literal
// text id:boxes-view", which never matches, so the caller saw arrived=false and
// went debugging their app instead of their command.
func ParseArrivalCriterion(spec string) (ArrivalCriterion, error) {
	var out ArrivalCriterion
	for _, term := range splitTerms(spec) {
		switch {
		case strings.HasPrefix(term, "title:"):
			if v := unquote(strings.TrimPrefix(term, "title:")); v != "" {
				out.Titles = append(out.Titles, v)
			}
		case strings.HasPrefix(term, "screen:"):
			if v := unquote(strings.TrimPrefix(term, "screen:")); v != "" {
				out.Screens = append(out.Screens, v)
			}
		case strings.HasPrefix(term, "text:"):
			if v := unquote(strings.TrimPrefix(term, "text:")); v != "" {
				out.Texts = append(out.Texts, v)
			}
		default:
			if word, ok := unknownArrivalPrefix(term); ok {
				return ArrivalCriterion{}, fmt.Errorf(
					"unknown --arrived-when prefix %q in term %q; the prefixes are %s, "+
						"and a term with no prefix is a title (quote it if the title itself "+
						"contains a colon)",
					word, term, strings.Join(arrivalPrefixes, " "))
			}
			if v := unquote(term); v != "" {
				out.Titles = append(out.Titles, v)
			}
		}
	}
	return out, nil
}

// unknownArrivalPrefix decides whether a term that matched no known prefix was
// still MEANT as one, and returns the word the caller used.
//
// Where the line sits, and why, because titles legitimately contain colons —
// `Moving Boxes: Office cables` is a real screen in the test bank. A term counts
// as a prefix only when all four hold:
//
//  1. it does not open with a quote: a quoted term is a literal title, which is
//     how you write a title that starts with a word and a colon;
//  2. it contains a colon;
//  3. everything before the first colon is one or more ASCII letters — no
//     spaces, no digits, no punctuation, which is what every real prefix is;
//  4. something follows the colon. A trailing colon is punctuation inside a
//     name, so the `Boxes:` of an unquoted `Moving Boxes: Office cables` stays
//     a title term.
func unknownArrivalPrefix(term string) (string, bool) {
	if strings.HasPrefix(term, `"`) {
		return "", false
	}
	colon := strings.Index(term, ":")
	if colon <= 0 || colon == len(term)-1 {
		return "", false
	}
	word := term[:colon]
	for _, r := range word {
		if r < 'a' || r > 'z' {
			if r < 'A' || r > 'Z' {
				return "", false
			}
		}
	}
	return word, true
}

// splitTerms splits on spaces except inside double quotes, so a screen name with
// a space in it survives.
func splitTerms(spec string) []string {
	var terms []string
	var cur strings.Builder
	inQuote := false
	for _, r := range spec {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				terms = append(terms, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		terms = append(terms, cur.String())
	}
	return terms
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// MatchesRoute answers whether the criterion holds HERE. Titles are matched
// against the route's title only — never against every label on screen, which
// is the check that declared arrival at step zero. Texts are matched against
// the whole tree, because "order 123" is content rather than identity.
func (a ArrivalCriterion) MatchesRoute(route Route, elements []Element) bool {
	if a.IsZero() {
		return false
	}
	// A screen id is an identity, so it is compared whole. A title is a name
	// somebody wrote, so it is contained — `title:"Order detail"` has to hold
	// on `Order detail 123`.
	for _, want := range a.Screens {
		if normalizeForMatch(route.Screen) != normalizeForMatch(want) {
			return false
		}
	}
	for _, want := range a.Titles {
		if !strings.Contains(normalizeForMatch(route.Title), normalizeForMatch(want)) {
			return false
		}
	}
	for _, want := range a.Texts {
		if !treeContainsText(elements, want) {
			return false
		}
	}
	return true
}

func treeContainsText(elements []Element, want string) bool {
	norm := normalizeForMatch(want)
	if norm == "" {
		return false
	}
	for _, el := range elements {
		if strings.Contains(normalizeForMatch(findElementText(el)), norm) {
			return true
		}
	}
	return false
}

// Outcomes. Every run ends in exactly one of these, and every one of them is
// reported: a loop that stops without saying where it stopped is the failure
// this whole command is built around.
const (
	GotoArrived   = "arrived"
	GotoExhausted = "exhausted"
	GotoTimeout   = "timeout"
	GotoStuck     = "stuck"
	GotoLooping   = "looping"
	GotoNoRoute   = "no_route"
	// GotoDeadEnd is "I walked the route and there is nothing here that leads
	// any further", which used to be reported as no_route — the same label as
	// "there was no way to start". They are different facts: one is a command
	// that never moved, the other is a command that navigated and then ran out
	// of onward moves, which on a screen that IS the destination is not a
	// failure at all. Reporting both as no_route is what made goto look broken
	// while standing where it was asked to go.
	GotoDeadEnd            = "dead_end"
	GotoRefused            = "refused"
	GotoOutOfApp           = "out_of_app"
	GotoAmbiguousCriterion = "ambiguous_criterion"
	GotoCIRefused          = "ci_refused"
)

// Budgets. Hard, and not raisable from a flow: a cap you can lift by asking is
// not a cap. 12 steps because a screen in an iOS app sits 1-4 taps from the
// root; browser-use allows 500, which is reasonable where navigating is free
// and reversible, and it is neither here.
const (
	gotoMaxSteps        = 12
	gotoMaxUnchanged    = 2
	gotoMaxAbstentions  = 2
	gotoSettleThreshold = 2
)

// GotoStep is one iteration, kept whole so the output is evidence rather than a
// verdict: the caller has more context than this loop and should be able to
// judge the path for itself.
type GotoStep struct {
	Tapped      *Element `json:"tapped,omitempty"`
	ResolvedBy  string   `json:"resolved_by,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	RouteBefore Route    `json:"route_before"`
	RouteAfter  Route    `json:"route_after"`
	Changed     bool     `json:"changed"`
}

// GotoResult is the whole run.
type GotoResult struct {
	Arrived      string     `json:"arrived"` // "true" | "false" | "unverified"
	Outcome      string     `json:"outcome"`
	Goal         string     `json:"goal"`
	RouteInitial Route      `json:"route_initial"`
	RouteFinal   Route      `json:"route_final"`
	Steps        []GotoStep `json:"steps"`
	// CriterionSource says who wrote the criterion arrival was judged against:
	// "explicit" when the caller passed --arrived-when, "none" when there is
	// none and arrival cannot be asserted either way. Criterion prints it back
	// in the syntax the flag takes.
	CriterionSource string `json:"criterion_source"`
	Criterion       string `json:"criterion,omitempty"`
	// ObservedScreens is the menu the destination was named out of: every
	// distinct screen this run stood on. Reported whether or not
	// anything was picked, because a pick is only readable next to what it was
	// picked from — and an abstention says as much as a choice.
	ObservedScreens []string `json:"observed_screens,omitempty"`
	// observed is the running record the menu is built from, kept unexported
	// because it is the raw material of ObservedScreens rather than a second
	// copy of it in the output.
	observed []ObservedScreen
	Refused  *Element `json:"refused_element,omitempty"`
	// Dismissed records every permission alert this run answered, and with
	// which button. A loop that presses system dialogs has to be auditable
	// afterwards or it is doing it in silence.
	Dismissed []PermissionDismissal `json:"dismissed_permissions,omitempty"`
	Next      string                `json:"next,omitempty"`
}

// SeenRoutes tracks where the loop has already been, so going in circles is
// detected rather than run out on the step budget.
type SeenRoutes struct {
	fingerprints map[string]int
}

func NewSeenRoutes() *SeenRoutes {
	return &SeenRoutes{fingerprints: map[string]int{}}
}

// Visit records a screen and reports whether it has been here before.
func (s *SeenRoutes) Visit(fingerprint string) bool {
	s.fingerprints[fingerprint]++
	return s.fingerprints[fingerprint] > 1
}

// GotoRefusesDestructive is stricter than find's guard and deliberately has no
// escape hatch. find returns a destructive element when the caller's own words
// ask for one, because the caller reads the answer before anything is tapped.
// In goto nobody reads anything between the decision and the finger.
func GotoRefusesDestructive(el *Element) bool {
	return el != nil && IsDestructive(findElementText(*el))
}

// TapPoint is the centre of an element's frame, which is how goto taps: the
// point it already resolved, not a selector that would re-read the tree.
// The difference is that read. Measured 2026-09-21 on iPhone 17 Pro / iOS 26.3
// from a simpool slot, 10 taps each alternated in one batch, clean launch
// before every tap, all 20 landing: 786 ms by coordinate against 899 ms by
// label. An earlier note here claimed 277 ms against 1,480 ms; that gap was
// never re-measured after the tap path was pinned to physical touch, and it is
// ~113 ms per tap, not ~1,200.
func TapPoint(el Element) (x, y int, ok bool) {
	nums := frameNumbers(el.Frame)
	if len(nums) < 4 {
		return 0, 0, false
	}
	return int(nums[0] + nums[2]/2), int(nums[1] + nums[3]/2), true
}

func frameNumbers(frame string) []float64 {
	var out []float64
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		if v, ok := parseFloat(cur.String()); ok {
			out = append(out, v)
		}
		cur.Reset()
	}
	for _, r := range frame {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func parseFloat(s string) (float64, bool) {
	var v float64
	var frac float64 = 0
	neg := false
	seenDot := false
	digits := 0
	for _, r := range s {
		switch {
		case r == '-' && digits == 0 && !neg:
			neg = true
		case r == '.':
			if seenDot {
				return 0, false
			}
			seenDot = true
			frac = 1
		case r >= '0' && r <= '9':
			digits++
			if seenDot {
				frac /= 10
				v += float64(r-'0') * frac
			} else {
				v = v*10 + float64(r-'0')
			}
		default:
			return 0, false
		}
	}
	if digits == 0 {
		return 0, false
	}
	if neg {
		v = -v
	}
	return v, true
}

// GotoStepQuestion is the question the loop asks, and it is NOT find's.
//
// find asks "which element IS the thing described", and abstaining when nothing
// on screen is that thing is correct for find: the caller wanted an element and
// there is not one. goto needs a wider question, because a destination two taps
// away is reached through a row that is not itself the destination. Measured:
// with find's question, goto stopped with no_route at the Settings root when
// the goal was the language screen, which sits inside General.
//
// But the first attempt at the wider question over-corrected. Telling the model
// "they are not there yet, what gets them CLOSER" made it hesitate on the row
// that IS the destination — at Settings > General, find resolved the language
// row and goto did not, on the same screen in the same minute. So the question
// has to hold both cases open at once and say so plainly: the element that IS
// it and the element that LEADS to it are both right answers.
//
// The abstention wording is kept from find's, because that is the part measured
// to matter: on a screen with no way forward the model needs somewhere to put a
// refusal or it picks a row at random. What is NOT kept is find's "two or more
// could equally be it" — on an iOS list every row is rendered twice, so that
// clause made it abstain on everything until candidates were de-duplicated.
//
// THE SENTENCE THAT NAMES THE GOAL is the one that was wrong, and it took a
// table to find out, because the suspicion was on the wrong half.
//
// The report was that on Boxy's category grid — Moving Boxes top-left, Test
// Category 1 to its right, Test Category 2 below — the goal "la primera
// categoría" resolved to Test Category 1 instead of Moving Boxes, and that the
// LEADS clause was to blame: a row literally named "1" reads as the lead to
// "the first one". Both halves of that were measured and both are wrong.
//
// Measured 21 sep on origin/main, off the raw `axe describe-ui` JSON of two
// live Boxy screens, 40 runs a cell (hits / misses / abstentions counted
// separately, never folded into "resolved"):
//
//	                               first category   box contents   box contents   goal absent
//	                               on the grid      from the grid  from the box   (4 cells)
//	                               (IS)             (LEADS)        list (IS)      (none)
//	find's question                40/40            0/40, 40 abst  40/40          40/40 abst
//	goto's question, as it was     29-34/40         40/40          40/40          40/40 abst
//	find's naming line + LEADS     40/40            40/40          40/40          40/40 abst
//	this wording                   40/40            40/40          40/40          40/40 abst
//
// Read off that table:
//
//   - The LEADS clause is NOT the cause. Put find's naming sentence in front of
//     this exact clause and the broken cell goes to 40/40 with the multi-step
//     cell still at 40/40. The clause is doing its job and stays.
//   - The reported 3/40 does not reproduce. Three independent 40-run baselines
//     of the old wording gave 29, 31 and 34 hits — a real loss against find's
//     40/40, and a much smaller one than reported.
//   - What carries the loss is "Someone is trying to reach: X". Swap ONLY that
//     line into find's otherwise-unchanged question and the loss comes with it:
//     35/40, where find's own line is 40/40. Reading, unverified: "trying to
//     reach X" invites X to be read as the name of a destination, and there is
//     a row on that screen whose name contains a 1; "a user described where
//     they want to get to" marks X as the user's own words, and then "primera"
//     is read as a position.
//
// find's ambiguity clause was measured again here and still does not go in:
// with it the table is identical, cell for cell, so it buys nothing and it is
// the clause that once abstained on everything.
func GotoStepQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
		"A user described where they want to get to as: " + goal + "\n\n" +
		"Which numbered element should they tap next? Two kinds of answer are equally right:\n" +
		"  - the element that IS what they are looking for, if it is on this screen;\n" +
		"  - the element that LEADS towards it — a section that contains it, a row on the way.\n" +
		"Answer with that number.\n" +
		"Answer `none` only if no element on this screen is it and none leads any closer. " +
		"Answering `none` is a correct and expected answer: a caller that gets `none` stops " +
		"and looks for itself, whereas a caller that gets the wrong number taps the wrong " +
		"thing and walks further away. Do not guess."
}

// InterpretGotoAnswer reads the model's CHOICE, where find reads its VERDICT,
// and the difference is measured rather than preferred.
//
// The failure that forced this: `mav goto` would not arrive. Profiling the
// abstention showed the model picking the RIGHT element and jevi marking the
// answer `unsure` because its confidence sat at 0.37, under jevi's own default
// cut. mav read the verdict, so a correct answer was thrown away. That is not
// mav applying a numeric threshold — it is mav inheriting someone else's.
//
// Measured on one screen of ten rows, eight goals whose answer was on it and
// twelve whose answer was not:
//
//	                     picks the right row   declines when it should
//	reading the verdict        4/8                    10/12
//	reading the label          8/8                     9/12
//
// The verdict costs half the correct answers and buys almost nothing, because
// two of the three wrong picks carried `verdict: yes` anyway — it was not the
// guard it was believed to be.
//
// What still guards this is NOT a number, and that matters: `none` is an option
// the model can choose, and it chose it 9 times out of 12 when nothing fitted.
// The abstention lives in the choice, which is where the model can express it,
// rather than in a confidence score that no cut separates.
//
// And the three it did pick were all reasonable LEADS — "Batería" for ordering
// a battery, "Cámara" for opening the camera — which is exactly what goto's
// question asks for. In goto a wrong lead costs one step and is caught by the
// route not matching, by loop detection, or by the step budget; it can never be
// reported as arrival, because arrival is decided by code against the route.
//
// find keeps reading the verdict. Its caller taps what it returns with no loop
// underneath to catch a wrong lead, so the stricter reading stays where the
// consequence is stricter.
func InterpretGotoAnswer(label string, batch []Element) (*Element, string) {
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

// --- Dismissing a permission alert, and only when told exactly how ------------
//
// goto stops at any modal, because a loop that taps buttons on its own can
// grant a permission or accept something that cannot be taken back. That guard
// is unchanged. What this adds is one narrow door, and the caller holds the key.
//
// The door exists because some routes cannot avoid an alert: Boxy asks for
// speech recognition on the way in, and `simctl privacy` has no service for
// speech — measured, `revoke all` on the bundle does not suppress it — so
// "deny it beforehand and change nothing" is not available there.
//
// WHY THE CALLER HAS TO NAME THE BUTTON, rather than goto working it out.
// Two detectors were proposed and measured, and both fail on the case that
// matters — a permission alert with THREE options, where two of them grant:
//
//	role=sheet   "¿Permitir que la app Mapas use tu ubicación?"
//	role=button  "Permitir una vez"
//	role=button  "Permitir al usarse la app"
//	role=button  "No permitir"            <- the one that grants nothing, LAST
//
//   - `kTCCService*` in the tree: ZERO markers on that alert, in the app tree
//     and in the system tree. It identifies some alerts and silently misses
//     others, and the ones it misses are the multi-option ones.
//   - position: the non-granting option was last here and first elsewhere. A
//     rule that guesses wrong on a three-button alert GRANTS the permission,
//     and one sample is not enough to bet a permission on.
//
// Matching button text is language-dependent, and worse than it sounds: the
// same alert came back in Spanish from an app launched in English, because the
// app resolved its InfoPlist strings to es.lproj. So goto cannot read the label
// either — but the person running it knows what it says. Declaring it is an
// instruction, not a heuristic: an instruction cannot guess wrong.
//
// And it FAILS CLOSED. If the declared label is not on the modal, goto stops
// exactly as before rather than trying something else. With two of three
// buttons granting, the quiet failure is the one that grants.

// PermissionDismissal is what the caller authorised, and what was done with it.
type PermissionDismissal struct {
	Modal  string `json:"modal"`
	Action string `json:"action"`
}

// gotoMaxDismissals bounds how many alerts one run may dismiss. An app that
// asks again after every tap would otherwise let the loop spend its whole
// budget answering dialogs, which is a different failure from the one the
// budget is for.
const gotoMaxDismissals = 3

// FindDismissButton returns the button on this screen whose label is the one
// the caller declared, or nil. Matching folds case and accents, like every
// other label comparison here, and nothing else: no prefix, no substring, no
// nearest match. A near-miss on a permission alert is a granted permission.
//
// It refuses a declared label that names something destructive even though the
// caller asked for it, which is the one place goto overrules an explicit
// instruction. `find` would return it — its caller reads the answer before
// anything is tapped — and goto has nobody between the decision and the finger.
func FindDismissButton(elements []Element, declared string) *Element {
	want := normalizeForMatch(declared)
	if want == "" || IsDestructive(declared) {
		return nil
	}
	for i := range elements {
		if !strings.Contains(strings.ToLower(elements[i].Role), "button") {
			continue
		}
		if normalizeForMatch(elements[i].Label) != want {
			continue
		}
		if IsDestructive(findElementText(elements[i])) {
			return nil
		}
		return &elements[i]
	}
	return nil
}

// --- Naming the destination among the screens actually stood on --------------
//
// The criterion does not have to be a name guessed before setting off. It can
// be chosen from the screens the walk actually produced, once the walk is over.
//
// WHY THIS IS NOT THE BLIND JUDGE OF §2, and the two arguments that killed that
// one have to be refuted separately or this is the same thing wearing a hat:
//
//  1. §2 said the option list can only come from the goal (straw distractors)
//     or from the tree being looked at (a question strings.Contains answers for
//     free), and that there is no third source. There is one, and it is this:
//     THE ROUTES THE RUN VISITED. It is not derived from the goal, so the
//     distractors are not straw — every one of them is a screen this same model
//     chose to walk into believing it led to the goal, which is the hardest
//     distractor there is. And it is not free, because the starting screen and
//     every screen passed through are in the list on exactly the same footing
//     as the last one; no text search separates them.
//  2. §2 said the blindness is of prompt rather than of evidence. Here it is
//     structural. The menu is sorted alphabetically and carries no step
//     numbers, no order and no marker of where the run ended. The model cannot
//     tell which entry is the endpoint, so it cannot wave its own arrival
//     through even if it wanted to: it is naming a destination among screens,
//     not grading a journey. Code alone knows which of them was last, and code
//     alone turns the pick into a verdict.
//
// And what is asked is never "did you arrive" or "did that work". It is "which
// of these screens is the one someone was trying to reach", with `none` on the
// menu. Choosing between observed states is not self-assessment.
//
// It is also MONOTONE by construction: a pick that is not the final route is
// reported as unverified, exactly as today, never as arrived=false. The only
// transition this feature can cause is unverified -> true, so the single
// failure worth measuring is a true on the wrong screen.

// CriterionObserved is a criterion nobody wrote in advance: a title read off a
// screen this run stood on, chosen from the menu of all of them.
const CriterionObserved = "observed"

// gotoObservedMenuCap bounds the menu. A run has at most 12 steps, so this is
// never reached in practice; it is here so the prompt cannot grow without a
// bound if the step cap ever moves.
const gotoObservedMenuCap = 16

// ObservedScreen is one entry of the menu: a screen this run stood on, named
// by the best name it has, and remembered with the route it was read off so the
// pick can be turned back into a criterion in code.
type ObservedScreen struct {
	Name    string
	Shows   []string
	IsTitle bool
}

// Line is how a screen is put to the model: its name, then some of the text on
// it, IN THE ORDER THE TREE GIVES IT, which is roughly top to bottom.
//
// The text is not decoration, and leaving it out is what made the first version
// of this inert. Measured, 10 runs a lane, same goal: with names alone the
// model answered `none` 10/10 on a route whose destination was in the menu —
// correctly, because "the contents of the FIRST category, FIRST box" cannot be
// matched to a screen called "Moving Boxes: Office cables" by anyone who cannot
// see that Moving Boxes is the first category and Office cables the first box.
// That ordinal is on the screens, it was observed, and withholding it was
// asking the model to bridge information it had never been given — the same
// mistake as asking it to guess the destination's name before setting off.
func (o ObservedScreen) Line() string {
	if len(o.Shows) == 0 {
		return o.Name
	}
	return o.Name + " — " + strings.Join(o.Shows, ", ")
}

// gotoShowsCap bounds how much of a screen goes into the menu. Twelve entries
// covers a dense iOS screen's identifying text without the menu turning into a
// second tree dump.
const gotoShowsCap = 12

// ScreenShows reads the text a screen is showing, deduplicated and in tree
// order. The application node is skipped — it carries the app's name, which is
// on every screen and identifies none of them — and any text already contained
// in text kept earlier is skipped too, which is what collapses "Office cables"
// and "Código 8993" into the row they both came from.
//
// Accessibility IDENTIFIERS are deliberately left out, unlike everywhere else
// in this file where findElementText takes them. They are written for code, not
// for reading: including them put `_TtGC7SwiftUI32NavigationStackHosting` and
// `waveform.badge.mic` in front of the model, and — worse — Boxy's category
// ids, which are regenerated on every launch. A menu line that changes between
// two identical runs is not a description of a screen.
func ScreenShows(elements []Element) []string {
	out := make([]string, 0, gotoShowsCap)
	seen := map[string]bool{}
	for _, el := range elements {
		if strings.Contains(strings.ToLower(el.Role), "application") {
			continue
		}
		text := strings.TrimSpace(strings.Join(nonEmptyFields(el.Label, el.Title, el.Value), " "))
		key := normalizeForMatch(text)
		if key == "" || seen[key] {
			continue
		}
		contained := false
		for _, kept := range out {
			if strings.Contains(normalizeForMatch(kept), key) {
				contained = true
				break
			}
		}
		if contained {
			continue
		}
		seen[key] = true
		out = append(out, text)
		if len(out) >= gotoShowsCap {
			break
		}
	}
	return out
}

// ObservedScreens is the menu: every distinct screen this run actually stood
// on, ALPHABETICALLY, which is what destroys the chronology.
//
// A screen is named by its navigation-bar title when it has one, because that
// is the name a person would use, and by mav's own screen identity when it does
// not. The fallback is not a nicety: on Boxy neither the category grid nor the
// box list has a heading, so a title-only menu on that route has ONE entry —
// the destination — and a one-entry menu is a yes/no about the screen it
// stopped on, which is the self-assessment this whole design exists to avoid.
//
// A screen with neither is left out. There would be nothing to check a pick
// against, and offering an answer nothing can check is worse than offering
// fewer answers.
func ObservedScreens(recorded []ObservedScreen) []ObservedScreen {
	seen := map[string]bool{}
	out := make([]ObservedScreen, 0, gotoObservedMenuCap)
	for _, entry := range recorded {
		key := normalizeForMatch(entry.Name)
		if key == "" || seen[key] || len(out) >= gotoObservedMenuCap {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return normalizeForMatch(out[i].Name) < normalizeForMatch(out[j].Name)
	})
	return out
}

// RecordObservedScreen appends the screen goto is standing on to the record the
// menu is built from. Called on every read the loop takes, so the menu is
// exactly the set of screens it stood on and nothing else.
func RecordObservedScreen(recorded []ObservedScreen, route Route, elements []Element) []ObservedScreen {
	entry, ok := nameObservedRoute(route)
	if !ok {
		return recorded
	}
	key := normalizeForMatch(entry.Name)
	for _, existing := range recorded {
		if normalizeForMatch(existing.Name) == key {
			return recorded
		}
	}
	entry.Shows = ScreenShows(elements)
	return append(recorded, entry)
}

func nonEmptyFields(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func nameObservedRoute(r Route) (ObservedScreen, bool) {
	if title := strings.TrimSpace(r.Title); title != "" {
		return ObservedScreen{Name: title, IsTitle: true}, true
	}
	if screen := strings.TrimSpace(r.Screen); screen != "" {
		return ObservedScreen{Name: screen}, true
	}
	return ObservedScreen{}, false
}

// ObservedNames is the menu as the lines it is offered as, which is also what
// the output reports: a pick is only readable next to what it was picked from.
func ObservedNames(menu []ObservedScreen) []string {
	out := make([]string, 0, len(menu))
	for _, m := range menu {
		out = append(out, m.Line())
	}
	return out
}

// RenderObservedMenu writes the numbered menu the question is asked over.
func RenderObservedMenu(names []string) string {
	var b strings.Builder
	for i, name := range names {
		b.WriteString(itoa(i + 1))
		b.WriteString(") ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	return b.String()
}

// GotoObservedOptions is one option per name plus the abstention. `none` is not
// tidiable away: a name picked because there was nothing else to pick would be
// a criterion nobody wrote and nothing checked.
func GotoObservedOptions(names []string) []string {
	out := make([]string, 0, len(names)+1)
	for i := range names {
		out = append(out, itoa(i+1))
	}
	return append(out, "none")
}

// GotoObservedQuestion asks which screen the destination IS. It says nothing
// about a journey, a loop, an order or an outcome, because none of that is the
// model's business here and any of it would let it recognise the endpoint.
func GotoObservedQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is a numbered list of screens from one iOS app, one per line. Each is named by the " +
		"title it shows, or by its internal screen name when it shows no title, and is followed by " +
		"some of the text on it. They are in alphabetical order and the order means nothing.\n" +
		"Someone wanted to reach: " + goal + "\n\n" +
		"Which of these screens is the one they wanted to reach? Answer with that number.\n" +
		"Pick the screen that IS the one described, not a screen that merely leads to it or lists a way in.\n" +
		"Answer `none` if none of these screens is the one described. Naming a screen that is not the " +
		"one described is worse than answering `none`."
}

// InterpretGotoObservedAnswer reads the pick the way every other choice in this
// command is read: the label, and only the label. No verdict, no confidence.
func InterpretGotoObservedAnswer(label string, menu []ObservedScreen) (ObservedScreen, bool) {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" || label == "none" {
		return ObservedScreen{}, false
	}
	idx := 0
	for _, r := range label {
		if r < '0' || r > '9' {
			return ObservedScreen{}, false
		}
		idx = idx*10 + int(r-'0')
	}
	if idx < 1 || idx > len(menu) {
		return ObservedScreen{}, false
	}
	return menu[idx-1], true
}

// AcceptObservedCriterion turns a picked title into a criterion, or refuses it.
//
// It refuses the one pick that would be actively wrong: the screen the run
// started on. That is the same origin negation an explicit --arrived-when goes
// through, with the same difference in consequence as a deduced criterion — the
// explicit one stops the command because it is the caller's mistake, this one
// is simply dropped, leaving the run exactly as it behaves with no criterion.
func AcceptObservedCriterion(pick ObservedScreen, initial Route) (ArrivalCriterion, bool) {
	name := strings.TrimSpace(pick.Name)
	if name == "" {
		return ArrivalCriterion{}, false
	}
	var criterion ArrivalCriterion
	if pick.IsTitle {
		criterion = ArrivalCriterion{Titles: []string{name}}
	} else {
		criterion = ArrivalCriterion{Screens: []string{name}}
	}
	if criterion.MatchesRoute(initial, nil) {
		return ArrivalCriterion{}, false
	}
	return criterion, true
}
