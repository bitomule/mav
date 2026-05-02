package mav

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Output struct {
	OK     bool              `json:"ok"`
	Cmd    string            `json:"cmd,omitempty"`
	Code   string            `json:"code,omitempty"`
	Fields map[string]string `json:"fields,omitempty"`
}

func OK(cmd string, fields map[string]string) Output {
	return Output{OK: true, Cmd: cmd, Fields: fields}
}

func Fail(code string, fields map[string]string) Output {
	return Output{OK: false, Code: code, Fields: fields}
}

func (o Output) Write(w io.Writer, jsonMode bool) error {
	if jsonMode {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(o)
	}
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
	_, err := fmt.Fprintln(w, strings.Join(parts, " "))
	return err
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\"") {
		b, _ := json.Marshal(value)
		return string(b)
	}
	return value
}
