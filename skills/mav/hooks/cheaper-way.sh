#!/bin/sh
# "There was a cheaper way and you did not take it" — said at the moment of the
# spend, which is the one place documentation cannot reach.
#
# WHY THIS IS NOT A DOCUMENTATION PROBLEM
#
# A model reads "prefer X" in a skill, repeats it back, and issues the expensive
# call anyway: comprehension is not compliance. Measured over 782 real agent
# tool calls in 51 sessions, a documented cheaper form was taken 6.9% of the
# time, and almost every session that took it had the flag written into its task
# text rather than having read the documentation.
#
# So this runs after the call, when the cost is a fact rather than a warning.
#
# WHAT IT WILL NOT DO, AND WHY EACH ONE IS A RULE
#
#   - It never blocks and never rewrites. PostToolUse cannot do either, and that
#     is the point of putting it here rather than on PreToolUse. A mechanism
#     that can rewrite a command will eventually rewrite it into a cheaper form
#     that quietly drops data the caller needed; a sentence cannot.
#   - It emits no `permissionDecision`, not even "allow": that auto-approves and
#     walks straight past the user's own ask/deny rules.
#   - It exits 0 on every path, touches no network, and runs no `mav`, no VCS
#     command and no build. A hook that hangs is worse than no hook, and the
#     default hook timeout is 600 seconds — ten minutes of a wedged session per
#     call. hooks.json pins this one to 2.
#   - It keeps no state: no counter, no per-session file, nothing that can go
#     stale or disagree with itself. See the note above `rules` for why the
#     rate limit it used to have was the wrong idea.
#
# ADDING A RULE
#
# Extend `rules()` below, one line per rule: an id, a matcher, a guard that says
# "already doing it the cheap way", and the sentence the agent reads. A rule
# earns its place when the cheap form already exists and is measurably unused —
# not when we merely wish people used it.

set -u

PAYLOAD="$(cat 2>/dev/null)" || exit 0
[ -n "$PAYLOAD" ] || exit 0
command -v jq >/dev/null 2>&1 || exit 0

field() { printf '%s' "$PAYLOAD" | jq -r "$1 // empty" 2>/dev/null; }

[ "$(field '.tool_name')" = "Bash" ] || exit 0

CMD="$(field '.tool_input.command')"
[ -n "$CMD" ] || exit 0

# --- rules -------------------------------------------------------------------
#
# It says it EVERY time, and it keeps no state at all — no counter, no per-session
# file, nothing to go stale or to disagree with itself. An earlier version said it
# twice and then every tenth call, out of a worry about being noisy. That was the
# wrong worry: if a cheap documented form exists and the expensive one is used,
# that is a mistake, and a mistake does not stop being one on the third
# repetition. Being stateless is the bonus — there is now nothing here that can
# be wrong about what happened earlier in the session.
#
# Each rule answers two questions and returns 1 as soon as one says no: did this
# command match without the cheap form, and did the expensive form actually cost
# anything on THIS call. The second question is not a rate limit — it is what
# stops the hook talking about a call where the cheap form would have saved
# nothing — and it is allowed to differ per rule, or to be absent.

rule_jevi_ask_oneoff() {
	case "$CMD" in
	*"jevi ask"*) ;;
	*) return 1 ;;
	esac
	case "$CMD" in
	*" -f "* | *--questions*) return 1 ;;
	esac

	# No cost gate: an inline question is already the expensive form of
	# itself. jevi's own help says the positional question is "for a one-off
	# from a terminal. Use -f for anything you run twice", and anything an
	# agent runs is something it will run again.
	NUDGE="A question set (\`jevi ask -f <set>\`) sends the same question without re-typing it and keeps the wording stable between runs, so the answers stay comparable — jevi's own help points you to it for anything you run twice, and an agent's questions are always run twice. The inline form is for a one-off at a terminal."
	return 0
}

rules='rule_jevi_ask_oneoff'

# --- emit --------------------------------------------------------------------

for rule in $rules; do
	NUDGE=''
	"$rule" || continue
	[ -n "$NUDGE" ] || continue
	jq -n --arg ctx "$NUDGE" \
		'{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $ctx}}' \
		2>/dev/null || :
	exit 0
done

exit 0
