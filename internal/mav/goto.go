package mav

import (
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
	Tab   string `json:"tab,omitempty"`
	Title string `json:"title,omitempty"`
	Modal string `json:"modal,omitempty"`
}

// IsZero reports a route that identifies nothing, which is a real state: a
// screen with no heading, no selected tab and no modal cannot be arrived at by
// route comparison, and goto says so rather than guessing.
func (r Route) IsZero() bool {
	return r.Tab == "" && r.Title == "" && r.Modal == ""
}

func (r Route) String() string {
	parts := make([]string, 0, 3)
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
	Titles []string
	Texts  []string
}

// IsZero reports that no criterion was declared. goto then cannot assert
// arrival at all and says `arrived=unverified`; it never says true.
func (a ArrivalCriterion) IsZero() bool {
	return len(a.Titles) == 0 && len(a.Texts) == 0
}

// Where the criterion came from. Reported on every run, because a criterion the
// machine deduced and one a person wrote are not the same evidence.
const (
	CriterionExplicit = "explicit"
	CriterionInferred = "inferred"
	CriterionNone     = "none"
)

// String writes the criterion back in the same syntax --arrived-when takes, so
// what goto deduced can be pasted straight back in to pin it.
func (a ArrivalCriterion) String() string {
	parts := make([]string, 0, len(a.Titles)+len(a.Texts))
	for _, t := range a.Titles {
		parts = append(parts, `title:"`+t+`"`)
	}
	for _, t := range a.Texts {
		parts = append(parts, `text:"`+t+`"`)
	}
	return strings.Join(parts, " ")
}

// ParseArrivalCriterion reads `title:"..."` and `text:"..."` terms. A bare word
// with no prefix is a title, because that is what people mean when they name a
// screen, and guessing the other way round would silently match a row label.
func ParseArrivalCriterion(spec string) ArrivalCriterion {
	var out ArrivalCriterion
	for _, term := range splitTerms(spec) {
		switch {
		case strings.HasPrefix(term, "title:"):
			if v := unquote(strings.TrimPrefix(term, "title:")); v != "" {
				out.Titles = append(out.Titles, v)
			}
		case strings.HasPrefix(term, "text:"):
			if v := unquote(strings.TrimPrefix(term, "text:")); v != "" {
				out.Texts = append(out.Texts, v)
			}
		default:
			if v := unquote(term); v != "" {
				out.Titles = append(out.Titles, v)
			}
		}
	}
	return out
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

// --- Deducing the criterion, once, before anything is at stake ---------------
//
// Arrival is still decided by CODE comparing ROUTES. What can be deduced is the
// CRITERION those routes are compared against, and only under one condition:
// the question is asked at step zero, from the starting screen, before a single
// tap. The model proposes what to look for while it has nothing invested in the
// answer, and it is never asked afterwards whether it got there. Handing it the
// before and after and asking "did you arrive" is a judge with errors
// correlated with the loop's own — same model, same tree — and is not done.
//
// An explicit --arrived-when always wins. A deduced criterion is only used when
// none was given, it goes through the same starting-screen guard, and if the
// model declines or names something useless goto behaves exactly as it does
// today: arrived=unverified. It can never be worse than saying nothing.
//
// jevi answers choices, not free text, so the question cannot be "what will the
// title be" in the open. The option list is built in code from names that are
// already in front of the model — the labels on the starting screen, plus any
// phrase the caller put in quotes in the goal — and the model picks one or
// declines. No threshold anywhere, no second model, no ranking by elimination.

// gotoTitleCandidateCap bounds the names offered. jev's latency does not move
// with the number of options (358 ms with 3, 387 with 20), so this is about the
// prompt staying readable, not about cost.
const gotoTitleCandidateCap = 40

// GotoTitleCandidates is the list of names the deduction chooses between:
// quoted phrases from the goal first, because a caller who quoted something
// usually quoted the destination's name, then every distinct label on the
// starting screen.
func GotoTitleCandidates(elements []Element, goal string) []string {
	out := make([]string, 0, gotoTitleCandidateCap)
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		key := normalizeForMatch(s)
		if s == "" || key == "" || seen[key] || len(out) >= gotoTitleCandidateCap {
			return
		}
		seen[key] = true
		out = append(out, s)
	}
	for _, phrase := range quotedPhrases(goal) {
		add(phrase)
	}
	for _, el := range elements {
		add(el.Label)
		add(el.Title)
	}
	return out
}

func quotedPhrases(goal string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range goal {
		if r != '"' {
			if inQuote {
				cur.WriteRune(r)
			}
			continue
		}
		if inQuote {
			out = append(out, cur.String())
			cur.Reset()
		}
		inQuote = !inQuote
	}
	return out
}

// RenderGotoTitleCandidates writes the numbered list the question is asked over.
func RenderGotoTitleCandidates(names []string) string {
	var b strings.Builder
	for i, name := range names {
		b.WriteString(itoa(i + 1))
		b.WriteString(") ")
		b.WriteString(name)
		b.WriteString("\n")
	}
	return b.String()
}

// GotoCriterionOptions is the option list: one per name, plus the abstention.
// `none` is not tidiable away here either — the service does not decline on its
// own, and a name it picked because it had to would be a criterion nobody wrote
// and nothing checked.
func GotoCriterionOptions(names []string) []string {
	out := make([]string, 0, len(names)+1)
	for i := range names {
		out = append(out, itoa(i+1))
	}
	return append(out, "none")
}

// GotoCriterionQuestion asks what the destination will be CALLED, not whether
// anyone got there.
//
// The clause about passing through is the whole risk of this feature in one
// sentence: a name that titles an intermediate screen would declare arrival
// halfway, which produces a false arrived=true — strictly worse than the
// unverified it replaces. So the question says it outright rather than leaving
// it to be inferred from the word "final".
func GotoCriterionQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is a list of names taken from the screen an iOS app is on RIGHT NOW, one per line, numbered.\n" +
		"Someone standing on this screen is about to navigate to: " + goal + "\n\n" +
		"When they get there they will be on a different screen, and that screen has a title in " +
		"its navigation bar. Which of these names will that final screen's title contain? " +
		"Answer with that number.\n" +
		"Do NOT pick a name that titles a screen they only pass THROUGH on the way — a section " +
		"they open, a list they scroll. It has to name where they STOP.\n" +
		"Answer `none` if the final screen's title will be something that is not in this list, " +
		"or if you cannot tell from here. Answering `none` is a correct and expected answer: a " +
		"caller that gets `none` reports that it could not confirm arrival, which is what it " +
		"does today anyway, whereas a wrong name makes it claim it arrived somewhere it did " +
		"not. Do not guess."
}

// InterpretGotoCriterionAnswer reads the pick the same way the step question's
// is read: the label, and only the label.
func InterpretGotoCriterionAnswer(label string, names []string) (string, bool) {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" || label == "none" {
		return "", false
	}
	idx := 0
	for _, r := range label {
		if r < '0' || r > '9' {
			return "", false
		}
		idx = idx*10 + int(r-'0')
	}
	if idx < 1 || idx > len(names) {
		return "", false
	}
	return names[idx-1], true
}

// AcceptInferredCriterion turns a deduced name into a criterion, or refuses it.
//
// It refuses the one deduction that would be actively harmful: a name that
// already holds on the screen we are standing on. That is the same guard an
// explicit --arrived-when goes through, with one difference in what happens
// next — an explicit criterion that matches the start is the caller's mistake
// and stops the run (ambiguous_criterion), while a deduction that lands on the
// current screen's own name is goto's mistake and is simply dropped, leaving
// the command exactly as it behaves with no criterion at all. A deduction must
// never make the command worse than not having deduced anything.
func AcceptInferredCriterion(name string, route Route, elements []Element) (ArrivalCriterion, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ArrivalCriterion{}, false
	}
	criterion := ArrivalCriterion{Titles: []string{name}}
	if criterion.MatchesRoute(route, elements) {
		return ArrivalCriterion{}, false
	}
	return criterion, true
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
	// "explicit" when the caller passed --arrived-when, "inferred" when goto
	// deduced it at step zero, "none" when there is none and arrival cannot be
	// asserted. A criterion the machine invented must never be presented as one
	// a person wrote, so the deduced one is printed too, in Criterion.
	CriterionSource string   `json:"criterion_source"`
	Criterion       string   `json:"criterion,omitempty"`
	Refused         *Element `json:"refused_element,omitempty"`
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
func GotoStepQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
		"Someone is trying to reach: " + goal + "\n\n" +
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
