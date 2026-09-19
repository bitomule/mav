package mav

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The hook in skills/mav/hooks/cheaper-way.sh runs on every Bash call of every
// session that has the mav skill installed, and it is the kind of thing that
// fails silently: a hook that hangs, crashes or says nothing looks exactly like
// a hook that decided to stay quiet. One of ours sat dead for two days without
// anyone noticing. So it gets tested here, from the Go suite that actually runs.
//
// The payload shapes below are not invented: they were read off a real
// PostToolUse payload captured from Claude Code (keys session_id, tool_name,
// tool_input.command, tool_response.stdout).

func hookPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "skills", "mav", "hooks", "cheaper-way.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("hook missing: %v", err)
	}
	return path
}

// runHook feeds one payload to the hook and returns its stdout. A non-zero exit
// is a failure in itself: the hook must never report an error to Claude Code,
// because the only thing that can come of it is an interrupted agent.
func runHook(t *testing.T, session, command, stdout string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"session_id":    session,
		"tool_name":     "Bash",
		"tool_input":    map[string]string{"command": command},
		"tool_response": map[string]any{"stdout": stdout},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", hookPath(t))
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook exited non-zero for %q: %v", command, err)
	}
	return string(out)
}

// nodeLines fakes n lines of `mav ui tree` output. Only the `node ` prefix
// matters to the hook's cost gate.
func nodeLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "node index=%d label=Item role=button enabled=true\n", i)
	}
	return b.String()
}

func nudgeFrom(t *testing.T, out string) string {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return ""
	}
	var parsed struct {
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			AdditionalContext  string `json:"additionalContext"`
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("hook printed something that is not JSON: %q", out)
	}
	if parsed.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Fatalf("hookEventName=%q, want PostToolUse", parsed.HookSpecificOutput.HookEventName)
	}
	// A permissionDecision here — "allow" included — would auto-approve the
	// call and walk past the user's own ask/deny rules. The hook must never
	// emit one, so the test asserts its absence rather than trusting review.
	if parsed.HookSpecificOutput.PermissionDecision != "" {
		t.Fatalf("hook emitted permissionDecision=%q; it must never decide permissions", parsed.HookSpecificOutput.PermissionDecision)
	}
	return parsed.HookSpecificOutput.AdditionalContext
}

func TestCheaperWayHookNudgesBareUITreeOnlyWhenItCost(t *testing.T) {
	// A tree big enough that --agent's ranked 40 would have been cheaper.
	got := nudgeFrom(t, runHook(t, "s-big", "mav ui tree", nodeLines(60)))
	if !strings.Contains(got, "--agent") {
		t.Fatalf("expected a nudge naming --agent, got %q", got)
	}
	if !strings.Contains(got, "60 elements") {
		t.Fatalf("expected the nudge to carry the measured size, got %q", got)
	}
	// It has to keep pointing at the ids, or an agent reads it as "use the
	// short one" and loses the selector path that the tree exists for.
	if !strings.Contains(got, "--id") {
		t.Fatalf("expected the nudge to say ids survive --agent, got %q", got)
	}
}

func TestCheaperWayHookStaysQuiet(t *testing.T) {
	cases := []struct {
		name    string
		command string
		stdout  string
	}{
		// Already doing it the cheap way.
		{"agent flag already used", "mav ui tree --agent", nodeLines(60)},
		// Small screen: --agent's cap of 40 would have saved nothing worth
		// a line, so saying it would be pure noise.
		{"tree too small to matter", "mav ui tree", nodeLines(10)},
		// Not our business.
		{"unrelated command", "ls -la", nodeLines(60)},
		{"another mav command", "mav ui tap --id foo", nodeLines(60)},
		// Already using a question set.
		{"jevi with question set", "jevi ask -f triage", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out := runHook(t, "s-"+tc.name, tc.command, tc.stdout); strings.TrimSpace(out) != "" {
				t.Fatalf("expected silence, got %q", out)
			}
		})
	}
}

func TestCheaperWayHookNudgesInlineJeviAsk(t *testing.T) {
	got := nudgeFrom(t, runHook(t, "s-jevi", `jevi ask "is this a bug?"`, ""))
	if !strings.Contains(got, "-f") {
		t.Fatalf("expected a nudge pointing at a question set, got %q", got)
	}
}

// The hook says it every time: there is no counter, and the same call repeated
// gets the same answer. This is the assertion that keeps someone from
// reintroducing a rate limit as a "noise" fix — the noise is the expensive call,
// not the sentence about it.
func TestCheaperWayHookSaysItEveryTime(t *testing.T) {
	// One shared TMPDIR across all five calls, so any state the script kept
	// would be visible to the later ones.
	tmp := t.TempDir()
	payload, err := json.Marshal(map[string]any{
		"session_id":    "s-repeat",
		"tool_name":     "Bash",
		"tool_input":    map[string]string{"command": "mav ui tree"},
		"tool_response": map[string]any{"stdout": nodeLines(60)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 1; i <= 5; i++ {
		cmd := exec.Command("/bin/sh", hookPath(t))
		cmd.Stdin = strings.NewReader(string(payload))
		cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("call %d exited non-zero: %v", i, err)
		}
		got := nudgeFrom(t, string(out))
		if got == "" {
			t.Fatalf("call %d stayed quiet; the hook must say it every time", i)
		}
		if i == 1 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("call %d said something different: %q vs %q", i, got, first)
		}
	}
}

func TestCheaperWayHookSurvivesGarbage(t *testing.T) {
	// Every one of these has to exit 0 and print nothing. A hook that fails
	// loudly on a payload it did not expect turns a bad input into a stopped
	// agent, which is worse than the tokens it was trying to save.
	for _, payload := range []string{
		"",
		"not json at all",
		"{}",
		`{"tool_name":"Bash"}`,
		`{"tool_name":"Bash","tool_input":{}}`,
		`{"tool_name":"Read","tool_input":{"file_path":"/tmp/x"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"mav ui tree"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"mav ui tree"},"tool_response":null}`,
		`{"tool_name":"Bash","tool_input":{"command":"mav ui tree"},"tool_response":{"stdout":null}}`,
	} {
		cmd := exec.Command("/bin/sh", hookPath(t))
		cmd.Stdin = strings.NewReader(payload)
		cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook exited non-zero on %q: %v", payload, err)
		}
		if strings.TrimSpace(string(out)) != "" {
			t.Fatalf("hook spoke on %q: %q", payload, out)
		}
	}
}

// The hook is only delivered by `mav install-skills`, which copies the whole
// skill directory. If the manifest stops pointing at hooks.json, or hooks.json
// stops pointing at the script, the hook is silently not there at all — the
// exact failure this file exists to catch.
func TestSkillPluginManifestWiresUpTheHook(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "skills", "mav"))
	if err != nil {
		t.Fatal(err)
	}

	var manifest struct {
		Name  string `json:"name"`
		Hooks string `json:"hooks"`
	}
	raw, err := os.ReadFile(filepath.Join(root, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "mav" {
		t.Fatalf("plugin name=%q, want mav", manifest.Name)
	}
	if manifest.Hooks != "./hooks/hooks.json" {
		t.Fatalf("manifest hooks=%q, want ./hooks/hooks.json", manifest.Hooks)
	}

	var hooks struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	raw, err = os.ReadFile(filepath.Join(root, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &hooks); err != nil {
		t.Fatal(err)
	}

	if len(hooks.Hooks) != 1 {
		t.Fatalf("expected exactly one hook event, got %v", hooks.Hooks)
	}
	entries, ok := hooks.Hooks["PostToolUse"]
	if !ok {
		t.Fatalf("expected PostToolUse only; PreToolUse could block or rewrite a call, which this must never do. got %v", hooks.Hooks)
	}
	for _, entry := range entries {
		if entry.Matcher != "Bash" {
			t.Fatalf("matcher=%q, want Bash", entry.Matcher)
		}
		for _, h := range entry.Hooks {
			if !strings.Contains(h.Command, "cheaper-way.sh") {
				t.Fatalf("command=%q does not run cheaper-way.sh", h.Command)
			}
			if !strings.Contains(h.Command, "${CLAUDE_PLUGIN_ROOT}") {
				t.Fatalf("command=%q must locate itself with ${CLAUDE_PLUGIN_ROOT}", h.Command)
			}
			// The default hook timeout is 600 seconds. On a hook that runs
			// after every Bash call, that is ten minutes of a wedged
			// session, and it is the failure mode that cost us a day.
			if h.Timeout <= 0 || h.Timeout > 5 {
				t.Fatalf("timeout=%d, want a small explicit one (<=5s)", h.Timeout)
			}
		}
	}
}
