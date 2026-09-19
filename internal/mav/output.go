package mav

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bitomule/mav/internal/mav/codes"
)

type Output struct {
	OK     bool              `json:"ok"`
	Cmd    string            `json:"cmd,omitempty"`
	Code   string            `json:"code,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

// CommandFailed requests a non-zero process exit after a structured failure
// line has already been written. It avoids duplicating the failure on stderr.
type CommandFailed struct{}

func (CommandFailed) Error() string { return "command failed" }

func OK(cmd string, fields map[string]string) Output {
	return Output{OK: true, Cmd: cmd, Fields: fields}
}

func Fail(code string, fields map[string]string) Output {
	return Output{OK: false, Code: code, Fields: fields}
}

func FailCode(code codes.Code, fields map[string]string) Output {
	merged := code.Fields()
	for key, value := range fields {
		merged[key] = value
	}
	return Output{OK: false, Code: code.ID, Fields: merged}
}

func (o Output) Write(w io.Writer) error {
	status := "ok"
	if !o.OK {
		status = "fail"
	}
	parts := []string{status}
	if o.Cmd != "" {
		parts = append(parts, "cmd="+quoteIfNeeded(o.Cmd))
	}
	if o.Code != "" {
		parts = append(parts, "code="+quoteIfNeeded(o.Code))
	}
	keys := make([]string, 0, len(o.Fields))
	for key := range o.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if o.Fields[key] == "" {
			continue
		}
		parts = append(parts, key+"="+quoteIfNeeded(o.Fields[key]))
	}
	if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
		return err
	}
	if !o.OK {
		// A failure has to reach main, which is who knows how to exit with
		// 1. Returning nil turned `mav ui tap ... && next-step` into a
		// chain that carried on after a failure, and forced every agent to
		// read stdout to know whether its own command had worked. The
		// `fail code=...` line is already written; this only brings the
		// exit code into agreement with it.
		return CommandFailed{}
	}
	return nil
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\"") {
		return jsonQuote(value)
	}
	return value
}

// jsonQuote is json.Marshal without its HTML escaping.
//
// json.Marshal escapes the three HTML-significant characters as <,
// > and &, which is right for a string about to be embedded in a
// web page and wrong for every line mav prints. The cost is not cosmetic:
// the remediation of ambiguous_booted_simulator, the most-read failure of
// the v0.19 line, told its reader to run `mav sim select <udid>`
// -- not a command -- in a field whose whole job is to be typed back.
// Quoting is otherwise unchanged and the result is still a valid JSON
// string.
func jsonQuote(value string) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		b, _ := json.Marshal(value)
		return string(b)
	}
	return strings.TrimRight(buf.String(), "\n")
}
