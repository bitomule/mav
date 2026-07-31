package mav

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// dapIOTimeout bounds every blocking read from lldb-dap so a wedged adapter
// (or a never-terminating evaluated expression) cannot hang the worker's
// single-threaded accept loop, or the client's own shutdown path, forever.
const dapIOTimeout = 30 * time.Second

var errDAPTimeout = errors.New("dap_read_timeout")

type dapClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	seq    int
	mu     sync.Mutex
	paused bool
	thread int
	breaks map[int]dapBreakpoint
	dead   bool
}

type dapBreakpoint struct {
	ID   int
	File string
	Line int
}

type dapMessage struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Event      string          `json:"event,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    bool            `json:"success,omitempty"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

func newDAPClient() (*dapClient, error) {
	cmd := exec.Command("xcrun", "lldb-dap")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	client := &dapClient{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout), breaks: map[int]dapBreakpoint{}}
	if _, err := client.request("initialize", map[string]any{"clientID": "mav", "adapterID": "lldb-dap", "linesStartAt1": true, "columnsStartAt1": true}); err != nil {
		_ = client.close()
		return nil, err
	}
	return client, nil
}

func (d *dapClient) request(command string, arguments any) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dead {
		return nil, fmt.Errorf("dap_session_dead")
	}
	d.seq++
	seq := d.seq
	payload, _ := json.Marshal(map[string]any{"seq": seq, "type": "request", "command": command, "arguments": arguments})
	if _, err := fmt.Fprintf(d.stdin, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
		return nil, err
	}
	for {
		message, err := d.readMessage()
		if err != nil {
			if errors.Is(err, errDAPTimeout) {
				d.killLocked()
			}
			return nil, err
		}
		if message.Type == "event" && message.Event == "stopped" {
			var body struct {
				ThreadID int `json:"threadId"`
			}
			_ = json.Unmarshal(message.Body, &body)
			d.paused, d.thread = true, body.ThreadID
		}
		if message.Type == "event" && message.Event == "continued" {
			d.paused = false
		}
		if message.Type == "response" && message.RequestSeq == seq {
			if !message.Success {
				return message.Body, fmt.Errorf("dap_%s_failed: %s", command, message.Message)
			}
			return message.Body, nil
		}
	}
}

// attach may emit "initialized" before its response. DAP clients must then
// configure breakpoints and send configurationDone; waiting synchronously for
// the attach response can deadlock with adapters following that ordering.
func (d *dapClient) attach(arguments any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dead {
		return fmt.Errorf("dap_session_dead")
	}
	d.seq++
	seq := d.seq
	payload, _ := json.Marshal(map[string]any{"seq": seq, "type": "request", "command": "attach", "arguments": arguments})
	if _, err := fmt.Fprintf(d.stdin, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
		return err
	}
	for {
		message, err := d.readMessage()
		if err != nil {
			if errors.Is(err, errDAPTimeout) {
				d.killLocked()
			}
			return err
		}
		if message.Type == "event" && message.Event == "initialized" {
			return nil
		}
		if message.Type == "response" && message.RequestSeq == seq {
			if !message.Success {
				return fmt.Errorf("dap_attach_failed: %s", message.Message)
			}
			return nil
		}
	}
}

// readMessage blocks on the lldb-dap stdout pipe behind a goroutine so a
// wedged adapter cannot hang the caller (and, through it, the worker's
// single-threaded accept loop) forever. On timeout the caller kills the
// adapter process; the abandoned goroutine's result is dropped once it
// eventually unblocks (the closed pipe/killed process ensures it does).
func (d *dapClient) readMessage() (dapMessage, error) {
	type result struct {
		message dapMessage
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		message, err := d.readMessageBlocking()
		ch <- result{message, err}
	}()
	select {
	case r := <-ch:
		return r.message, r.err
	case <-time.After(dapIOTimeout):
		return dapMessage{}, errDAPTimeout
	}
}

func (d *dapClient) readMessageBlocking() (dapMessage, error) {
	length := 0
	for {
		line, err := d.reader.ReadString('\n')
		if err != nil {
			return dapMessage{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			length, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(line), "content-length:")))
		}
	}
	if length <= 0 {
		return dapMessage{}, fmt.Errorf("dap_invalid_frame")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(d.reader, body); err != nil {
		return dapMessage{}, err
	}
	var message dapMessage
	if err := json.Unmarshal(body, &message); err != nil {
		return dapMessage{}, err
	}
	return message, nil
}

// killLocked terminates the adapter process and marks the session dead so
// subsequent request()/attach() calls fail fast instead of writing to a
// stdin nobody is reading. Callers must hold d.mu.
func (d *dapClient) killLocked() {
	d.dead = true
	if d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
	}
}

func (d *dapClient) waitPaused(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if d.paused {
			return nil
		}
		_, _ = d.request("threads", map[string]any{})
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("debug_wait_timeout")
}

func (d *dapClient) close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	dead := d.dead
	d.mu.Unlock()
	if !dead {
		// Bounded by dapIOTimeout via request()/readMessage - never hangs.
		_, _ = d.request("disconnect", map[string]any{"terminateDebuggee": false})
	}
	_ = d.stdin.Close()
	if d.cmd.Process != nil {
		_ = d.cmd.Process.Kill()
	}
	return nil
}

func (d *dapClient) addBreakpoint(file string, line int) (json.RawMessage, error) {
	lines := []int{line}
	for _, breakpoint := range d.breaks {
		if breakpoint.File == file && breakpoint.Line != line {
			lines = append(lines, breakpoint.Line)
		}
	}
	items := make([]map[string]int, 0, len(lines))
	for _, value := range lines {
		items = append(items, map[string]int{"line": value})
	}
	body, err := d.request("setBreakpoints", map[string]any{"source": map[string]any{"path": file}, "breakpoints": items})
	if err != nil {
		return body, err
	}
	var decoded struct {
		Breakpoints []struct {
			ID       int  `json:"id"`
			Line     int  `json:"line"`
			Verified bool `json:"verified"`
		} `json:"breakpoints"`
	}
	_ = json.Unmarshal(body, &decoded)
	verified := false
	for _, value := range decoded.Breakpoints {
		d.breaks[value.ID] = dapBreakpoint{ID: value.ID, File: file, Line: value.Line}
		verified = verified || value.Verified
	}
	if !verified && filepath.IsAbs(file) {
		short := filepath.Base(file)
		fallback, fallbackErr := d.request("setBreakpoints", map[string]any{
			"source":      map[string]any{"name": short, "path": short},
			"breakpoints": []map[string]int{{"line": line}},
		})
		if fallbackErr == nil {
			var fallbackDecoded struct {
				Breakpoints []struct {
					ID       int  `json:"id"`
					Line     int  `json:"line"`
					Verified bool `json:"verified"`
				} `json:"breakpoints"`
			}
			_ = json.Unmarshal(fallback, &fallbackDecoded)
			for _, value := range fallbackDecoded.Breakpoints {
				d.breaks[value.ID] = dapBreakpoint{ID: value.ID, File: short, Line: value.Line}
				verified = verified || value.Verified
			}
			if verified {
				return fallback, nil
			}
		}
	}
	if !verified {
		return body, fmt.Errorf("debug_symbols_missing")
	}
	return body, nil
}

func (d *dapClient) removeBreakpoint(id int) (json.RawMessage, error) {
	removed, ok := d.breaks[id]
	if !ok {
		return nil, fmt.Errorf("debug_breakpoint_not_found")
	}
	delete(d.breaks, id)
	var items []map[string]int
	for _, breakpoint := range d.breaks {
		if breakpoint.File == removed.File {
			items = append(items, map[string]int{"line": breakpoint.Line})
		}
	}
	return d.request("setBreakpoints", map[string]any{"source": map[string]any{"path": removed.File}, "breakpoints": items})
}

func (w *runWorker) handleDebug(args map[string]string) ([]string, error) {
	action := args["action"]
	switch action {
	case "attach":
		if w.debug != nil {
			_ = w.debug.close()
		}
		client, err := newDAPClient()
		if err != nil {
			return nil, err
		}
		pid, err := strconv.Atoi(args["pid"])
		if err != nil || pid <= 0 {
			_ = client.close()
			return nil, fmt.Errorf("debug_pid_invalid")
		}
		if err := client.attach(map[string]any{"pid": pid, "stopOnEntry": true}); err != nil {
			_ = client.close()
			return nil, err
		}
		w.debug = client
		var breakpointErr error
		if file, line := args["file"], args["line"]; file != "" && line != "" {
			lineNumber, _ := strconv.Atoi(line)
			_, breakpointErr = client.addBreakpoint(file, lineNumber)
		}
		// Always send configurationDone even if the breakpoint failed, so the
		// stopOnEntry'd debuggee resumes instead of hanging forever.
		if _, err := client.request("configurationDone", map[string]any{}); err != nil {
			_ = client.close()
			w.debug = nil
			return nil, err
		}
		if breakpointErr != nil {
			return nil, breakpointErr
		}
		return []string{`{"attached":true}`}, nil
	case "detach":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		terminate := args["kill"] == "true"
		_, err := w.debug.request("disconnect", map[string]any{"terminateDebuggee": terminate})
		_ = w.debug.stdin.Close()
		if w.debug.cmd.Process != nil {
			_ = w.debug.cmd.Process.Kill()
		}
		w.debug = nil
		return []string{`{"detached":true}`}, err
	case "wait":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		timeout, _ := time.ParseDuration(args["timeout"])
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		return []string{`{"paused":true}`}, w.debug.waitPaused(timeout)
	case "request":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		if args["paused"] == "true" && !w.debug.paused {
			return nil, fmt.Errorf("debug_not_paused")
		}
		var arguments any = map[string]any{}
		if raw := args["arguments"]; raw != "" {
			if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
				return nil, fmt.Errorf("debug_arguments_invalid")
			}
		}
		if (args["command"] == "stepIn" || args["command"] == "next" || args["command"] == "stepOut" || args["command"] == "continue" || args["command"] == "pause") && w.debug.thread > 0 {
			if object, ok := arguments.(map[string]any); ok {
				object["threadId"] = w.debug.thread
			}
		}
		switch args["command"] {
		case "stepIn", "next", "stepOut", "continue":
			// Clear stopOnEntry/the previous stop before dispatch. If the
			// adapter sends a new stopped event while serving the request,
			// request() sets this back to true with the new thread id.
			w.debug.paused = false
		}
		body, err := w.debug.request(args["command"], arguments)
		if err != nil {
			return nil, err
		}
		return []string{string(body)}, nil
	case "state":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		threads, err := w.debug.request("threads", map[string]any{})
		if err != nil {
			return nil, err
		}
		threadID := w.debug.thread
		if threadID == 0 {
			var decoded struct {
				Threads []struct {
					ID int `json:"id"`
				} `json:"threads"`
			}
			_ = json.Unmarshal(threads, &decoded)
			if len(decoded.Threads) > 0 {
				threadID = decoded.Threads[0].ID
			}
		}
		stack, err := w.debug.request("stackTrace", map[string]any{"threadId": threadID, "levels": 20})
		if err != nil {
			return nil, err
		}
		result := map[string]json.RawMessage{"threads": threads, "stack": stack}
		var stackBody struct {
			StackFrames []struct {
				ID int `json:"id"`
			} `json:"stackFrames"`
		}
		_ = json.Unmarshal(stack, &stackBody)
		if len(stackBody.StackFrames) > 0 {
			scopes, scopeErr := w.debug.request("scopes", map[string]any{"frameId": stackBody.StackFrames[0].ID})
			if scopeErr == nil {
				result["scopes"] = scopes
				var scopeBody struct {
					Scopes []struct {
						VariablesReference int `json:"variablesReference"`
					} `json:"scopes"`
				}
				_ = json.Unmarshal(scopes, &scopeBody)
				if len(scopeBody.Scopes) > 0 {
					variables, variableErr := w.debug.request("variables", map[string]any{"variablesReference": scopeBody.Scopes[0].VariablesReference})
					if variableErr == nil {
						result["locals"] = variables
					}
				}
			}
		}
		encoded, _ := json.Marshal(result)
		return []string{string(encoded)}, nil
	case "break.add":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		line, err := strconv.Atoi(args["line"])
		if err != nil {
			return nil, fmt.Errorf("debug_breakpoint_invalid")
		}
		body, err := w.debug.addBreakpoint(args["file"], line)
		return []string{string(body)}, err
	case "break.remove":
		if w.debug == nil {
			return nil, fmt.Errorf("debug_session_missing")
		}
		id, err := strconv.Atoi(args["id"])
		if err != nil {
			return nil, fmt.Errorf("debug_breakpoint_invalid")
		}
		body, err := w.debug.removeBreakpoint(id)
		return []string{string(body)}, err
	default:
		return nil, fmt.Errorf("debug_action_unknown")
	}
}

func (c CLI) debug(ctx context.Context, opts GlobalOptions, args []string) error {
	if len(args) == 0 {
		return Fail("debug_command_missing", nil).Write(c.Stdout)
	}
	cfg, err := LoadConfig(c.Root)
	if err != nil {
		return Fail("config_not_found", nil).Write(c.Stdout)
	}
	if isPhysicalDevice(cfg) {
		return Fail("debug_unsupported_on_device", nil).Write(c.Stdout)
	}
	run, err := c.resolveRun("")
	if err != nil {
		return Fail("run_not_found", map[string]string{"next": "mav open"}).Write(c.Stdout)
	}
	if !workerPing(run) {
		if _, err := startRunWorker(c.Root, run); err != nil {
			return Fail("debug_worker_unavailable", map[string]string{"stderr": err.Error()}).Write(c.Stdout)
		}
	}
	request := workerRequest{Command: "debug", Args: map[string]string{}}
	command := args[0]
	switch command {
	case "attach":
		process := flagValue(args[1:], "--process")
		if process == "" {
			process = cfg.ProcessName
		}
		bundle := flagValue(args[1:], "--bundle")
		if process == "" && bundle == "" {
			return Fail("debug_process_missing", map[string]string{"next": "set process_name in .mav/config.yaml"}).Write(c.Stdout)
		}
		var pidResult CommandResult
		pid := ""
		if bundle != "" {
			pidResult = c.Runner.Run(ctx, "xcrun", "simctl", "spawn", cfg.SimulatorUDID, "launchctl", "list")
			for _, line := range strings.Split(pidResult.Stdout, "\n") {
				if strings.Contains(line, bundle) {
					pid = strings.Fields(line)[0]
					break
				}
			}
		} else {
			pidResult = c.Runner.Run(ctx, "xcrun", "simctl", "spawn", cfg.SimulatorUDID, "pgrep", "-x", process)
			pid = strings.TrimSpace(pidResult.Stdout)
		}
		if pidResult.Err != nil || pid == "" {
			return Fail("debug_process_not_running", map[string]string{"process": process, "bundle": bundle}).Write(c.Stdout)
		}
		request.Args = map[string]string{"action": "attach", "pid": strings.Split(pid, "\n")[0]}
		if breakpoint := flagValue(args[1:], "--breakpoint"); breakpoint != "" {
			file, line, ok := strings.Cut(breakpoint, ":")
			if !ok {
				return Fail("debug_breakpoint_invalid", nil).Write(c.Stdout)
			}
			if !filepath.IsAbs(file) {
				file = filepath.Join(c.Root, file)
			}
			request.Args["file"], request.Args["line"] = file, line
		}
	case "wait":
		request.Args = map[string]string{"action": "wait", "timeout": flagValue(args[1:], "--timeout")}
	case "detach":
		request.Args = map[string]string{"action": "detach", "kill": strconv.FormatBool(hasFlag(args[1:], "--kill"))}
	case "eval":
		if len(args) < 2 {
			return Fail("debug_expression_missing", nil).Write(c.Stdout)
		}
		payload, _ := json.Marshal(map[string]any{"expression": strings.Join(args[1:], " "), "context": "repl"})
		request.Args = map[string]string{"action": "request", "command": "evaluate", "arguments": string(payload), "paused": "true"}
	case "step":
		if len(args) < 2 {
			return Fail("debug_step_missing", nil).Write(c.Stdout)
		}
		dapCommand := map[string]string{"in": "stepIn", "over": "next", "out": "stepOut", "continue": "continue"}[args[1]]
		if dapCommand == "" {
			return Fail("debug_step_invalid", nil).Write(c.Stdout)
		}
		payload, _ := json.Marshal(map[string]any{"threadId": 1})
		request.Args = map[string]string{"action": "request", "command": dapCommand, "arguments": string(payload), "paused": "true"}
	case "pause":
		threadID := 1
		if raw := flagValue(args[1:], "--thread"); raw != "" {
			if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
				threadID = parsed
			} else {
				return Fail("debug_thread_invalid", map[string]string{"thread": raw}).Write(c.Stdout)
			}
		}
		payload, _ := json.Marshal(map[string]any{"threadId": threadID})
		request.Args = map[string]string{"action": "request", "command": "pause", "arguments": string(payload)}
	case "state":
		request.Args = map[string]string{"action": "state"}
	case "break":
		if len(args) < 3 || (args[1] != "add" && args[1] != "remove") {
			return Fail("debug_break_usage", map[string]string{"usage": "mav debug break add File.swift:42 | mav debug break remove ID"}).Write(c.Stdout)
		}
		if args[1] == "remove" {
			request.Args = map[string]string{"action": "break.remove", "id": args[2]}
			break
		}
		file, line, ok := strings.Cut(args[2], ":")
		lineNumber, parseErr := strconv.Atoi(line)
		if !ok || parseErr != nil {
			return Fail("debug_breakpoint_invalid", nil).Write(c.Stdout)
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(c.Root, file)
		}
		request.Args = map[string]string{"action": "break.add", "file": file, "line": strconv.Itoa(lineNumber)}
	default:
		return Fail("debug_unknown_command", map[string]string{"command": command}).Write(c.Stdout)
	}
	response, err := sendWorkerRequest(run, request)
	if err != nil {
		return Fail("debug_failed", map[string]string{"stderr": firstLine(err.Error())}).Write(c.Stdout)
	}
	raw := "{}"
	if len(response.Results) > 0 {
		raw = response.Results[0]
	}
	if opts.Raw {
		_, err = fmt.Fprintln(c.Stdout, raw)
		return err
	}
	return c.OK("debug."+command, map[string]string{"result": raw}).Write(c.Stdout)
}
