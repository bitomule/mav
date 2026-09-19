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
func runHook(t *testing.T, session, command, stdout string, extraEnv ...string) string {
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
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook exited non-zero for %q: %v", command, err)
	}
	return string(out)
}

// withKey and withoutKey pin the find rule's availability gate, which otherwise
// reads real machine state. The gate asks whether a key EXISTS in any of three
// places; the environment variable is the one a test can set, and pointing
// XDG_CONFIG_HOME at an empty directory removes the file. The keychain is the
// one place a test cannot control, so withoutKey skips on a machine that has an
// entry rather than asserting something that is not true there.
func withKey(t *testing.T) []string {
	t.Helper()
	return []string{"MAV_JEV_API_KEY=not-a-real-key", "XDG_CONFIG_HOME=" + t.TempDir()}
}

func withoutKey(t *testing.T) []string {
	t.Helper()
	if exec.Command("/usr/bin/security", "find-generic-password", "-s", "mav-jev").Run() == nil {
		t.Skip("this machine has a mav-jev keychain entry, so the no-key branch cannot be exercised here")
	}
	return []string{"MAV_JEV_API_KEY=", "XDG_CONFIG_HOME=" + t.TempDir()}
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

func TestCheaperWayHookStaysQuiet(t *testing.T) {
	cases := []struct {
		name    string
		command string
		stdout  string
	}{
		// The only tree case that stays silent is one already using find.
		// There is no size gate: an advisory that appears on some screens and
		// not others is one nobody learns, and the agent cannot tell which
		// kind of screen it is about to get before it asks.
		{"already using the cheap form", `mav ui tree && mav ui find "the save button"`, nodeLines(60)},
		// Not our business.
		{"unrelated command", "ls -la", nodeLines(60)},
		{"another mav command", "mav ui tap --id foo", nodeLines(60)},
		// Already using a question set.
		{"jevi with question set", "jevi ask -f triage", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// With a key present, so silence here proves the cost gate and
			// not the availability gate. Without this the tree cases would
			// pass on any machine that simply has no key, which is the
			// wrong reason and would hide a broken cost gate.
			out := runHook(t, "s-"+tc.name, tc.command, tc.stdout, withKey(t)...)
			if strings.TrimSpace(out) != "" {
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
// The find rule, and its two gates. The gates are the whole reason a rule about
// `mav ui tree` is safe to have at all: the previous one was removed for
// pointing at a form we do not recommend, and a rule that fires when the advice
// cannot be followed is the same mistake wearing a different hat.

func TestCheaperWayHookNudgesALargeTreeTowardsFind(t *testing.T) {
	got := nudgeFrom(t, runHook(t, "s-tree", "mav ui tree", nodeLines(60), withKey(t)...))
	if !strings.Contains(got, "mav ui find") {
		t.Fatalf("expected a nudge pointing at find, got %q", got)
	}
	// The sentence has to carry the limit as well as the recommendation:
	// "replaces the grep, not your judgement" is the line that keeps an agent
	// from treating find as an oracle.
	if !strings.Contains(got, "not your judgement") {
		t.Fatalf("the nudge must say what find does NOT replace, got %q", got)
	}
}

func TestTheFindRuleSpeaksOnEveryTreeHoweverSmall(t *testing.T) {
	// The rule David asked for: always, not above a threshold. A one-element
	// screen gets the same sentence as a 500-element one, because an advisory
	// that only sometimes appears is one nobody learns.
	for _, n := range []int{1, 12, 39, 40, 213, 500} {
		got := nudgeFrom(t, runHook(t, "s-every", "mav ui tree", nodeLines(n), withKey(t)...))
		if !strings.Contains(got, "mav ui find") {
			t.Fatalf("a %d-node tree got no nudge: %q", n, got)
		}
	}
}

func TestTheFindRuleSaysNothingWithoutAKey(t *testing.T) {
	// Without a key find answers resolved_by=none, so recommending it is
	// noise. This is the gate that makes the rule honest.
	env := withoutKey(t)
	if out := runHook(t, "s-nokey", "mav ui tree", nodeLines(120), env...); strings.TrimSpace(out) != "" {
		t.Fatalf("expected silence with no key available, got %q", out)
	}
}

// The control for the test above: it must be capable of failing. If the only
// reason the no-key case is silent were that the hook never speaks about trees,
// the assertion would prove nothing — so the same payload with a key must talk.
func TestTheNoKeyControlWouldSeeAFailure(t *testing.T) {
	if got := nudgeFrom(t, runHook(t, "s-ctl", "mav ui tree", nodeLines(120), withKey(t)...)); got == "" {
		t.Fatal("the same payload with a key must produce a nudge, or the no-key assertion is vacuous")
	}
}

func TestTheFindRuleNeverEmitsAPermissionDecision(t *testing.T) {
	// Not even to allow: a permissionDecision walks straight past the user's
	// own ask and deny rules, and this hook exists to advise, never to decide.
	out := runHook(t, "s-perm", "mav ui tree", nodeLines(120), withKey(t)...)
	if strings.Contains(out, "permissionDecision") {
		t.Fatalf("the hook must never emit a permission decision: %q", out)
	}
}

func TestCheaperWayHookSaysItEveryTime(t *testing.T) {
	// One shared TMPDIR across all five calls, so any state the script kept
	// would be visible to the later ones.
	tmp := t.TempDir()
	payload, err := json.Marshal(map[string]any{
		"session_id":    "s-repeat",
		"tool_name":     "Bash",
		"tool_input":    map[string]string{"command": `jevi ask "is this a bug?"`},
		"tool_response": map[string]any{"stdout": ""},
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
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask -f triage"}}`,
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask -f triage"},"tool_response":null}`,
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask -f triage"},"tool_response":{"stdout":null}}`,
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

// A matching command with no `tool_response` at all is not garbage — the
// surviving rule never reads one — so the hook has to speak, and speak valid
// JSON. This is the half of the payload handling the silence test above cannot
// cover, and it is where an absent or null field would blow up if the script
// stopped defaulting it.
func TestCheaperWayHookSpeaksWithoutAToolResponse(t *testing.T) {
	for _, payload := range []string{
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask \"q\""}}`,
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask \"q\""},"tool_response":null}`,
		`{"tool_name":"Bash","tool_input":{"command":"jevi ask \"q\""},"tool_response":{"stdout":null}}`,
	} {
		cmd := exec.Command("/bin/sh", hookPath(t))
		cmd.Stdin = strings.NewReader(payload)
		cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("hook exited non-zero on %q: %v", payload, err)
		}
		if got := nudgeFrom(t, string(out)); !strings.Contains(got, "-f") {
			t.Fatalf("expected the question-set nudge on %q, got %q", payload, got)
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
