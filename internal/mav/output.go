package mav

import (
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
		// Un fallo tiene que llegar a main, que es quien sabe salir con 1.
		// Devolver nil convertia `mav ui tap ... && siguiente-paso` en una
		// cadena que seguia adelante despues de un fallo, y obligaba a cada
		// agente a leer stdout para saber si su propio comando habia
		// funcionado. La linea `fail code=...` ya esta escrita; esto solo pone
		// el codigo de salida de acuerdo con ella.
		return CommandFailed{}
	}
	return nil
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
