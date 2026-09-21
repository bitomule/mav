package mav

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The text a flow types is declared by whoever wrote the flow. The model is
// only ever allowed to pick WHICH of the declared inputs to use - never to
// write the characters.
//
//	inputs:
//	  nombre: "Caja de herramientas"
//	  cantidad: "12"
//	steps:
//	  - type: { where: { find: "el campo del nombre" }, text: { from: nombre } }
//	  - type: { where: { find: "el campo de cantidad" }, text: { ask: "lo que toca escribir aquí" } }
//
// `from` costs nothing: it is a map lookup. `ask` puts the KEYS on the menu and
// the code substitutes the value behind the chosen one.
//
// That closes two holes at once. Hallucinated text can never be typed, because
// nothing the model says is ever typed. And text injected through the app's own
// labels cannot become an instruction that ends up in a field, because the
// answer space is a fixed list of names the flow author wrote.

// FlowTextSource is `text: { from: ... }` or `text: { ask: ... }`. Exactly one
// of the two.
type FlowTextSource struct {
	From string `yaml:"from,omitempty"`
	Ask  string `yaml:"ask,omitempty"`
}

// flowTextField is the `text:` value of a step, which stays what it always was
// - a literal string - and additionally accepts the mapping form above.
type flowTextField struct {
	Literal string
	Source  *FlowTextSource
}

func (f *flowTextField) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		f.Literal = node.Value
		return nil
	case yaml.MappingNode:
		var source FlowTextSource
		if err := decodeKnownNode(node, &source); err != nil {
			return err
		}
		from, ask := strings.TrimSpace(source.From), strings.TrimSpace(source.Ask)
		if (from == "") == (ask == "") {
			return fmt.Errorf("text_source_invalid")
		}
		f.Source = &FlowTextSource{From: from, Ask: ask}
		return nil
	default:
		return fmt.Errorf("text_invalid")
	}
}

// flowInputKeys is the answer space, in a fixed order so two runs on one flow
// put the same menu.
func flowInputKeys(inputs map[string]string) []string {
	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TextChoiceQuestion asks which declared input belongs in this field. The
// values are not sent: the question is which name, and the code holds the
// values.
func TextChoiceQuestion(ask string, keys []string) string {
	return untrustedTextPreamble +
		"A flow is about to type into a field on an iOS app. The values it may type are " +
		"declared by the person who wrote the flow, and are listed below by name.\n" +
		"What belongs in this field was described as: " + ask + "\n\n" +
		"Which named value is it? Answer with that name.\n" +
		"Answer `none` if none of them belongs there. Answering `none` is a correct " +
		"answer: it stops the flow instead of typing the wrong thing. Do not guess, and " +
		"do not answer with anything that is not one of the names."
}

func renderTextChoices(keys []string) string {
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("\n")
	}
	return b.String()
}

// resolveFlowStepText turns a step's declared text source into the characters
// to type.
func (c CLI) resolveFlowStepText(ctx context.Context, step FlowStep) (string, map[string]string, error) {
	source := step.Text
	fields := map[string]string{}
	if source == nil {
		return step.Params["text"], fields, nil
	}
	if source.From != "" {
		value, ok := step.Inputs[source.From]
		if !ok {
			fields["input"] = source.From
			return "", fields, fmt.Errorf("input_missing")
		}
		fields["input"] = source.From
		fields["text_from"] = "declared"
		return value, fields, nil
	}

	keys := flowInputKeys(step.Inputs)
	if len(keys) == 0 {
		return "", fields, fmt.Errorf("inputs_missing")
	}
	// One declared input is not a question. Asking anyway would spend a round
	// trip to be told the only answer there is.
	if len(keys) == 1 {
		fields["input"] = keys[0]
		fields["text_from"] = "only_input"
		return step.Inputs[keys[0]], fields, nil
	}

	if reason := findIsRefusedHere(); reason != "" {
		fields["find_reason"] = reason
		return "", fields, fmt.Errorf("text_unavailable")
	}
	key, keySource, ok := ResolveJevKey()
	if !ok {
		fields["find_reason"] = ReasonNoKey
		fields["next"] = MissingJevKeyNext()
		return "", fields, fmt.Errorf("text_unavailable")
	}
	fields["key_source"] = string(keySource)

	answer, err := askJevChoice(ctx, key,
		TextChoiceQuestion(source.Ask, keys), renderTextChoices(keys), append(append([]string{}, keys...), "none"))
	if err != nil {
		fields["find_reason"] = ReasonNoNetwork
		return "", fields, fmt.Errorf("text_unavailable")
	}
	fields["model_ms"] = fmt.Sprint(answer.LatencyMS)
	chosen := strings.TrimSpace(answer.Label)
	// The answer is read as a NAME and nothing else. Anything that is not one
	// of the declared keys - `none` included, and free text especially - is an
	// abstention, so there is no path by which what the model says reaches the
	// keyboard.
	value, ok := step.Inputs[chosen]
	if !ok {
		return "", fields, fmt.Errorf("text_abstained")
	}
	fields["input"] = chosen
	fields["text_from"] = "chosen"
	return value, fields, nil
}
