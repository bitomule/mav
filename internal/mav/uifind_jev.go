package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The bridge to jev. mav is Go and jevi is a Rust binary, so this shells out
// rather than linking, and it hands jevi the key for that one child process
// only — nothing is written anywhere and no other process inherits it.
//
// --soft is passed on purpose: jevi's own exit codes carry answers (1 for a no,
// 3 for an unsure), and `find` must not inherit that. Here the exit code says
// only whether jevi ran; the answer is read out of the JSON.

// jevAnswer is the shape jevi --json returns for a choice question.
type jevAnswer struct {
	OK      bool `json:"ok"`
	Answers map[string]struct {
		Verdict string `json:"verdict"`
		Label   string `json:"label"`
		Type    string `json:"type"`
	} `json:"answers"`
	// LatencyMS is jev's own measurement of the round trip it made. Read
	// rather than re-measured here: timing the subprocess would fold jev's
	// start-up into the model's time and attribute to the network something
	// that is ours.
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error"`
}

// jevChoice is what the caller needs out of a jev round: a verdict, a label,
// what the round trip took, and — separately — whether asking worked at all.
type jevChoice struct {
	Verdict   string
	Label     string
	LatencyMS int64
}

// errJevUnavailable means the question was never put. Distinct from an answer
// of "none", because "I could not ask" and "I looked and I am not sure" are
// different facts for whoever called.
var errJevUnavailable = errors.New("jev unavailable")

// errJevTooOld is kept apart from errJevUnavailable because the remedy differs
// and the caller prints it. "jev could not be reached" sends someone looking at
// their network; "your jevi predates the check mav relies on" sends them to one
// command. Collapsing the two would cost exactly the investigation this error
// exists to skip.
var errJevTooOld = errors.New("jevi too old")

// askJevChoice puts one choice question to jev over the given text.
func askJevChoice(ctx context.Context, key, question, text string, options []string) (jevChoice, error) {
	// The floor is checked here and nowhere else: this is the one path that
	// spawns jevi, and it costs one `jevi --version` per PROCESS rather than per
	// call. jevversion.go says why that distinction is load-bearing.
	if err := checkJevVersion(ctx); err != nil {
		return jevChoice{}, fmt.Errorf("%w: %s", errJevTooOld, err)
	}
	cmd := exec.CommandContext(ctx, "jevi", "ask",
		"--options", strings.Join(options, ","),
		"--json", "--soft", question)
	cmd.Stdin = strings.NewReader(text)
	// Inherit the environment, then override the provider key for this child
	// alone. mav's key is mav's; jevi's own stored key is never relied on.
	cmd.Env = append(os.Environ(), jevProviderEnvVar+"="+key)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The exit code is deliberately not read. jevi answers with it — 1 for a
	// no, 3 for an unsure — and --soft only softens the codes for "could not
	// ask" (4, 5), not those two. Treating a non-zero exit as a failure to
	// reach the model reported every abstention as `no_network`, which is the
	// one distinction this command is supposed to keep straight. The answer is
	// in the JSON; whether jevi ran at all is whether the JSON parses.
	_ = cmd.Run()

	var doc jevAnswer
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &doc); err != nil {
		return jevChoice{}, errJevUnavailable
	}
	if !doc.OK {
		return jevChoice{}, errJevUnavailable
	}
	answer, ok := doc.Answers["answer"]
	if !ok {
		for _, a := range doc.Answers {
			answer = a
			ok = true
			break
		}
	}
	if !ok {
		return jevChoice{}, errJevUnavailable
	}
	return jevChoice{
		Verdict:   answer.Verdict,
		Label:     answer.Label,
		LatencyMS: doc.LatencyMS,
	}, nil
}

// askJevQuestions puts a whole question SET — several heads in one round trip —
// and returns every answer by head name.
//
// Same transport as askJevChoice on purpose: the set goes in argv and the text
// to judge goes down stdin, which is where askJevChoice puts it, so nothing
// about how the screen reaches the model changes between the one-head call and
// the many-head one. The only difference is the question document, which is
// what is being measured.
func askJevQuestions(ctx context.Context, key, questionsJSON, text string) (map[string]jevChoice, int64, error) {
	cmd := exec.CommandContext(ctx, "jevi", "ask",
		"--questions-json", questionsJSON, "--json", "--soft")
	cmd.Stdin = strings.NewReader(text)
	cmd.Env = append(os.Environ(), jevProviderEnvVar+"="+key)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The exit code is not read, for the reason spelled out on askJevChoice:
	// jevi answers with it, and an answer is not a failure.
	_ = cmd.Run()

	var doc jevAnswer
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &doc); err != nil {
		return nil, 0, errJevUnavailable
	}
	if !doc.OK {
		return nil, 0, errJevUnavailable
	}
	out := make(map[string]jevChoice, len(doc.Answers))
	for name, answer := range doc.Answers {
		out[name] = jevChoice{Verdict: answer.Verdict, Label: answer.Label, LatencyMS: doc.LatencyMS}
	}
	return out, doc.LatencyMS, nil
}

// findIsRefusedHere reports the reason find must not consult a model in this
// environment, or "". CI is the one that matters: a judgement that costs money
// and varies between runs has no place in a pipeline, and refusing out loud is
// the difference between that being a guarantee and being a convention.
func findIsRefusedHere() string {
	if os.Getenv("CI") != "" {
		return ReasonCIRefused
	}
	if os.Getenv("MAV_FIND_DISABLE") != "" {
		return ReasonCIRefused
	}
	return ""
}
