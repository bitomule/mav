package mav

import (
	"context"
	"encoding/json"
	"strings"
)

// The action space is what a loop needs before it can do anything but tap.
//
// `mav goto` asks one question — "which numbered element do I tap next" — and
// so a goal like "create a category called Test category" does not fail, it is
// simply not attemptable: there is no answer to that question that types.
//
// WHAT THIS IS. One model round trip that answers two things at once: WHICH
// OPERATION (tap, type) and WHAT TO OPERATE ON. Both clients written against
// this endpoint do it the same way and it is read off their code, not their
// README: jev-ultrafast (jev_ultrafast/model.py, `choose`) and arc-cua
// (policies/typesafe.py, `_build_questions`) each send the operation head and
// EVERY target head in one call, then throw away the heads that do not belong
// to the operation that came back. They pay for tokens nobody reads in order
// not to pay for a second round trip. This does the same.
//
// WHAT THIS IS NOT. It is not a second model, not a confidence cut, and not a
// text filter over the candidates. `none` stays on every menu and stops the
// loop, exactly as it does for tap today.
//
// THE TEXT IS NEVER WRITTEN BY THE MODEL. The caller declares the values, the
// model picks a KEY, and the code substitutes. That rule already exists in mav
// for flows (flow_text.go, `text: {from:}` / `text: {ask:}`) and this reuses
// its answer space rather than inventing a second one: the keys go on the menu
// and the values never leave the process. Hallucinated text cannot be typed
// because nothing the model emits is ever typed, and a label on the screen
// saying "type your password here" cannot become the text either.
//
// THE MEASURED RISK THIS IS BUILT AROUND. Changing the SHAPE of the request has
// been measured to make the choice worse on the screen a user actually sees —
// structured records instead of the numbered text list, or `instructions` as an
// object, each took a cell from ~9/10 right to ~10/10 wrong. So the tap head
// here is not "like" production's: it is production's, byte for byte, down to
// the key order inside the question object. See ActionSpaceQuestions.

// The operations. Two are built; the shape takes more without changing.
const (
	ActionTap  = "tap"
	ActionType = "type"
	ActionNone = "none"
)

// actionHeadTap is deliberately named `answer`: that is the name jevi's
// shorthand gives the single question it builds from `--options`, and the
// control measurement is only a control if this head reaches the model under
// the name production's does.
const (
	actionHeadTap       = "answer"
	actionHeadOperation = "operation"
	actionHeadTypeTo    = "type_target"
	actionHeadTypeWhat  = "type_input"
)

// ActionSpace is one screen's worth of what could be done on it.
type ActionSpace struct {
	Goal string
	// Batch is the numbered candidate list, already de-duplicated and capped
	// by FindCandidates/FindBatch. Numbering is the batch's own order, and the
	// tap head and the type head number it identically — two heads over the
	// same list, never two lists.
	Batch []Element
	// InputKeys are the names of the values the caller declared, in the fixed
	// order flowInputKeys gives them. Empty means typing is not on the menu at
	// all: an operation with nothing to type with is an operation that can
	// only produce an abstention, and offering it can only cost accuracy.
	InputKeys []string
}

// OffersTyping reports whether this screen's menu includes typing.
func (s ActionSpace) OffersTyping() bool {
	return len(s.InputKeys) > 0 && len(s.Batch) > 0
}

// ActionChoice is what came back, after the heads that do not belong to the
// chosen operation have been discarded.
type ActionChoice struct {
	Operation string
	Element   *Element
	// InputKey is the NAME of the declared value to type. The value itself is
	// never part of this struct, so nothing downstream can be handed text the
	// model chose the characters of.
	InputKey  string
	Reason    string
	LatencyMS int64
}

// ActionOperationQuestion is the operation head's wording.
//
// It names both operations in terms of what they DO to the screen rather than
// what they are called, because the model is picking from this text and not
// from an API. `none` is worded the way tap's abstention is worded, for the
// same measured reason: given nowhere to put a refusal the model picks
// something at random.
//
// THE PARAGRAPH ABOUT THE FIELD ALREADY BEING HERE IS NOT PADDING, it is a
// measured fix. Worded only as "choose `type` when the goal needs text in a
// field", this head answered `tap` on `Crear categoria` 9 times out of 10 on
// the new-category sheet — THE BUTTON THAT HAD ALREADY BEEN PRESSED TO OPEN
// THE VERY FIELD IT WAS BEING ASKED ABOUT, sitting behind the open sheet and
// still in the tree. The model read the goal ("create a category…"), saw a
// button that creates a category, and pressed it again. Saying plainly that a
// field in front of you means you are already where that button leads took it
// to 10/10. Nothing in the tap head was touched to get that.
func ActionOperationQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
		"Someone is trying to: " + goal + "\n\n" +
		"What should be done on this screen to get closer? Answer with one of:\n" +
		"  - `tap`: press one of the elements — the one that IS what they want, or one that LEADS towards it.\n" +
		"  - `type`: write one of the values they already declared into a field on this screen.\n" +
		"If the goal needs text written somewhere and a field for it is ALREADY on this screen, the " +
		"answer is `type`. Tapping the button that would have opened that field is not progress when " +
		"the field is in front of you — it is where you already are.\n" +
		"Answer `none` if neither gets any closer. Answering `none` is a correct and expected " +
		"answer: it stops rather than doing the wrong thing to the screen. Do not guess."
}

// ActionTypeTargetQuestion asks which numbered element the text goes into. It
// is the tap head's list asked about differently, so a field that is not a
// field is a wrong answer the caller can see rather than a hidden filter.
func ActionTypeTargetQuestion(goal string) string {
	return untrustedTextPreamble +
		"Below is the list of elements currently on one screen of an iOS app, one per line, numbered.\n" +
		"Someone is trying to: " + goal + "\n\n" +
		"Text is about to be written into one of these elements. Which numbered element is the " +
		"field it should be written into? Answer with that number.\n" +
		"Answer `none` if none of them is a field that takes text, or if none of them is the " +
		"right field. Answering `none` is a correct and expected answer. Do not guess."
}

// ActionTypeInputQuestion asks WHICH DECLARED VALUE, by name. The values are
// not in the question: the answer space is the list of names the caller wrote,
// and the code holds what is behind each one.
func ActionTypeInputQuestion(goal string, keys []string) string {
	return untrustedTextPreamble +
		"Someone is trying to: " + goal + "\n\n" +
		"The values that may be typed were declared in advance by the person running this, and " +
		"are named: " + strings.Join(keys, ", ") + ".\n" +
		"Which named value belongs in the field? Answer with that name.\n" +
		"Answer `none` if none of them does. Answering `none` is a correct answer: it stops " +
		"instead of writing the wrong thing. Do not answer with anything that is not one of the names."
}

// ActionSpaceQuestions writes the question set jevi takes on --questions-json.
//
// IT IS BUILT BY HAND, ON PURPOSE. Two things would break if it went through a
// Go map: encoding/json sorts map keys, and jevi sends each question object to
// the model in the order it was written. Order is not cosmetic there — jevi's
// own measurement, 30 runs a side through the binary on one cell of mav's
// ablation bench, is 2/30 with the keys alphabetised against 29/30 in the
// caller's order. So every object here is emitted in a fixed order and the
// only thing encoding/json is used for is escaping one string at a time.
//
// THE TAP HEAD IS PRODUCTION'S, RECONSTRUCTED EXACTLY. jevi's `--options`
// shorthand builds `{"instructions": <question>, "type": "choice", "criteria":
// {"1":"1", …, "none":"none"}}` under the name `answer`, in that key order,
// with the criteria in the order the options were given. That is what is
// written here. It is first in the set for the same reason: in production it is
// the only head, and the head that measures well is the one that must not move.
func ActionSpaceQuestions(space ActionSpace) string {
	var b strings.Builder
	b.WriteString(`{"version":1,"questions":{`)

	options := FindOptions(space.Batch)
	writeChoiceHead(&b, actionHeadTap, GotoStepQuestion(space.Goal), options)

	b.WriteString(",")
	ops := []string{ActionTap}
	if space.OffersTyping() {
		ops = append(ops, ActionType)
	}
	ops = append(ops, ActionNone)
	writeChoiceHead(&b, actionHeadOperation, ActionOperationQuestion(space.Goal), ops)

	if space.OffersTyping() {
		b.WriteString(",")
		writeChoiceHead(&b, actionHeadTypeTo, ActionTypeTargetQuestion(space.Goal), options)
		b.WriteString(",")
		keys := append(append([]string{}, space.InputKeys...), ActionNone)
		writeChoiceHead(&b, actionHeadTypeWhat, ActionTypeInputQuestion(space.Goal, space.InputKeys), keys)
	}

	b.WriteString("}}")
	return b.String()
}

// writeChoiceHead emits one question in jevi's shorthand key order:
// instructions, type, criteria. Each option's description is the option itself,
// which is what the shorthand does and what the tap head must keep.
func writeChoiceHead(b *strings.Builder, name, instructions string, options []string) {
	b.WriteString(jsonString(name))
	b.WriteString(`:{"instructions":`)
	b.WriteString(jsonString(instructions))
	b.WriteString(`,"type":"choice","criteria":{`)
	for i, option := range options {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(jsonString(option))
		b.WriteString(":")
		b.WriteString(jsonString(option))
	}
	b.WriteString(`}}`)
}

func jsonString(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(out)
}

// ResolveActionSpace puts the whole space to the model in one call and reads
// back only the heads the chosen operation uses.
//
// The discarding is the point, and it is what both reference clients do: an
// unread head cannot cause an action, so the cost of asking it is tokens and
// nothing else. What this adds over them is that the type heads must BOTH
// resolve — a field with no value, or a value with no field, is an abstention
// rather than half an action.
func ResolveActionSpace(ctx context.Context, key string, space ActionSpace) (ActionChoice, error) {
	if len(space.Batch) == 0 {
		return ActionChoice{Operation: ActionNone, Reason: ReasonNoCandidates}, nil
	}
	answers, latency, err := askJevQuestions(ctx, key,
		ActionSpaceQuestions(space), RenderFindCandidates(space.Batch))
	if err != nil {
		return ActionChoice{}, err
	}
	choice := ActionChoice{LatencyMS: latency}

	switch operation := strings.TrimSpace(strings.ToLower(answers[actionHeadOperation].Label)); operation {
	case ActionTap:
		choice.Operation = ActionTap
		element, reason := InterpretGotoAnswer(answers[actionHeadTap].Label, space.Batch)
		choice.Element, choice.Reason = element, reason
	case ActionType:
		if !space.OffersTyping() {
			// The head was never on the menu, so an answer naming it is not an
			// answer to a question that was asked.
			choice.Operation = ActionNone
			choice.Reason = ReasonNotACandidate
			return choice, nil
		}
		choice.Operation = ActionType
		element, reason := InterpretGotoAnswer(answers[actionHeadTypeTo].Label, space.Batch)
		if reason != "" {
			choice.Reason = reason
			return choice, nil
		}
		input, reason := InterpretActionInput(answers[actionHeadTypeWhat].Label, space.InputKeys)
		if reason != "" {
			choice.Reason = reason
			return choice, nil
		}
		choice.Element, choice.InputKey = element, input
	case "", ActionNone:
		choice.Operation = ActionNone
		choice.Reason = ReasonAbstained
	default:
		choice.Operation = ActionNone
		choice.Reason = ReasonNotACandidate
	}
	return choice, nil
}

// InterpretActionInput reads the answer as a declared NAME and nothing else.
// Free text, `none`, and a name nobody declared are all the same thing here: an
// abstention. There is no branch on which the model's own characters reach a
// keyboard.
func InterpretActionInput(label string, keys []string) (string, string) {
	chosen := strings.TrimSpace(label)
	if chosen == "" || strings.EqualFold(chosen, ActionNone) {
		return "", ReasonAbstained
	}
	for _, key := range keys {
		if key == chosen {
			return key, ""
		}
	}
	return "", ReasonNotACandidate
}

// ActionSpaceText resolves the chosen key to the characters to type. It is the
// only place a value is read, it reads it out of what the caller declared, and
// it cannot be reached with a key the caller did not write.
func ActionSpaceText(choice ActionChoice, inputs map[string]string) (string, bool) {
	if choice.Operation != ActionType || choice.InputKey == "" {
		return "", false
	}
	value, ok := inputs[choice.InputKey]
	return value, ok
}
