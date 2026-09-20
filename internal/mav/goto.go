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

// Outcomes. Every run ends in exactly one of these, and every one of them is
// reported: a loop that stops without saying where it stopped is the failure
// this whole command is built around.
const (
	GotoArrived            = "arrived"
	GotoExhausted          = "exhausted"
	GotoTimeout            = "timeout"
	GotoStuck              = "stuck"
	GotoLooping            = "looping"
	GotoNoRoute            = "no_route"
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
	Refused      *Element   `json:"refused_element,omitempty"`
	Next         string     `json:"next,omitempty"`
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
// Measured: a tap by coordinates costs 277ms and a tap by text 1,480ms, and the
// difference is that read. It is the whole reason goto is faster than a loop of
// find.
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
	return "Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
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
