package mav

import "strings"

// `mav goto --input name="Test Category"` is how a caller declares the text a
// goto run is allowed to type, and declaring it is the ONLY way text ever gets
// typed. The model picks WHICH declared name belongs in a field; the code holds
// the characters and substitutes them.
//
// That rule is not new here — flows have had it since flow_text.go, as
// `text: {from:}` / `text: {ask:}` — and this reuses its answer space rather
// than growing a second one. What it buys is the same two things:
//
//   - text the model invented cannot be typed, because nothing the model says
//     is ever typed;
//   - text injected through the app's own labels cannot become an instruction
//     that ends up in a field, because the answer space is a fixed list of
//     names the CALLER wrote, and a label on screen cannot add to it.
//
// With no --input at all, goto is exactly what it was: a loop that taps. The
// typing operation is not even offered to the model, because an operation with
// nothing to type is one that can only produce an abstention and can only cost
// accuracy on the heads that matter.

// ParseGotoInputs reads every `--input name=value` off the command line.
//
// Repeated rather than one flag holding a list: a value with a comma in it is
// ordinary text ("Madrid, Spain"), and a list format would have to escape it.
//
// A later declaration of the same name wins, which is what a person retyping a
// command expects. An entry with no `=`, or with an empty name, is dropped —
// it declares nothing, and a half-parsed name on the menu would be a name the
// caller never wrote.
func ParseGotoInputs(args []string) map[string]string {
	inputs := map[string]string{}
	for i, arg := range args {
		var raw string
		switch {
		case arg == "--input" && i+1 < len(args):
			raw = args[i+1]
		case strings.HasPrefix(arg, "--input="):
			raw = strings.TrimPrefix(arg, "--input=")
		default:
			continue
		}
		name, value, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		inputs[name] = value
	}
	if len(inputs) == 0 {
		return nil
	}
	return inputs
}
