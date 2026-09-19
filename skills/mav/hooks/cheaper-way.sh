#!/bin/sh
# "There was a cheaper way and you did not take it" — said at the moment of the
# spend, which is the one place documentation cannot reach.
#
# WHY THIS IS NOT A DOCUMENTATION PROBLEM
#
# Measured over 782 real `mav ui tree` calls in 51 agent sessions: `mav ui tree
# --agent` already exists, already costs nothing extra and already saves the
# tokens, and it was used 54 times — 6.9%, by 7 sessions of 51. Of those 7, six
# used it on their first or second call because the flag was written into their
# task text. None of them got there from the skill, which at the time never
# mentioned the flag in 940 lines. A model reads "prefer X", repeats it back,
# and issues the expensive call anyway: comprehension is not compliance.
#
# So this runs after the call, when the cost is a fact rather than a warning.
#
# WHAT IT WILL NOT DO, AND WHY EACH ONE IS A RULE
#
#   - It never blocks and never rewrites. PostToolUse cannot do either, and that
#     is the point of putting it here rather than on PreToolUse: the full tree is
#     what makes `--id` selectors work, so a mechanism that can truncate or
#     redirect it risks the 99% to optimise the 1%. Silently rewriting
#     `mav ui tree` to `--agent` would also cap the screen at 40 elements, which
#     is the truncation bug we have already paid for once.
#   - It emits no `permissionDecision`, not even "allow": that auto-approves and
#     walks straight past the user's own ask/deny rules.
#   - It exits 0 on every path, touches no network, and runs no `mav`, no `git`
#     and no build. A hook that hangs is worse than no hook, and the default
#     hook timeout is 600 seconds — ten minutes of a wedged session per call.
#     hooks.json pins this one to 2.
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

# `occurrences <id>` returns how many times this rule has matched in this
# session, this one included. The counter lives in a temp directory and its
# failure is silent on purpose: a lost counter costs one extra reminder, and
# there is no failure here worth interrupting an agent's work for.
SESSION="$(field '.session_id')"
[ -n "$SESSION" ] || SESSION="unknown"
STATE="${TMPDIR:-/tmp}/mav-cheaper-way/$SESSION"

occurrences() {
	_dir="$STATE"
	_file="$_dir/$1"
	mkdir -p "$_dir" 2>/dev/null || { printf '1\n'; return; }
	_n=0
	[ -f "$_file" ] && _n="$(cat "$_file" 2>/dev/null)"
	case "$_n" in
	'' | *[!0-9]*) _n=0 ;;
	esac
	_n=$((_n + 1))
	printf '%s\n' "$_n" >"$_file" 2>/dev/null || :
	printf '%s\n' "$_n"
}

# Saying it once is too easy to miss — a long session compacts, and the line
# goes with it. Saying it 782 times is noise the agent learns to skip. So: the
# first two occurrences, then every tenth.
worth_saying() {
	case "$1" in
	1 | 2) return 0 ;;
	esac
	[ $(($1 % 10)) -eq 0 ]
}

# --- rules -------------------------------------------------------------------

# Every rule answers three questions in this order, and returns 1 as soon as one
# of them says no: did this command match, was the cheap form already used, and
# did the expensive form actually cost anything on THIS call. That last question
# is what keeps the hook quiet on the calls where the cheap form would not have
# helped, and it is different for each rule.

rule_mav_ui_tree() {
	case "$CMD" in
	*"mav ui tree"*) ;;
	*) return 1 ;;
	esac
	case "$CMD" in
	*--agent*) return 1 ;;
	esac

	# The cost gate: `--agent` ranks and caps at 40, so below that it saves
	# nothing worth a line. Count what the full tree actually returned.
	nodes="$(field '.tool_response.stdout' | grep -c '^node ' 2>/dev/null)"
	case "$nodes" in
	'' | *[!0-9]*) return 1 ;;
	esac
	[ "$nodes" -gt 40 ] || return 1

	NUDGE="That tree returned $nodes elements. \`mav ui tree --agent\` returns the same screen ranked with focused and actionable elements first, each line marked actionable=true|false, and it keeps the ids \`mav ui tap --id\` needs. Use it to read a screen and decide what to touch; keep the bare tree for when you need every element or the frames."
	return 0
}

rule_jevi_ask_oneoff() {
	case "$CMD" in
	*"jevi ask"*) ;;
	*) return 1 ;;
	esac
	case "$CMD" in
	*" -f "* | *--questions*) return 1 ;;
	esac

	# The cost gate here is not output size but repetition, because that is
	# what jevi's own help says: the positional question is "for a one-off
	# from a terminal. Use -f for anything you run twice". So the first
	# one-off is correct and says nothing; the second is the tell.
	[ "$(occurrences jevi-ask-oneoff-seen)" -ge 2 ] || return 1

	NUDGE="That is the second one-off \`jevi ask\` in this session. A question set (\`jevi ask -f <set>\`) sends the same question without re-typing it, keeps the wording stable between runs so the answers stay comparable, and is what jevi's own help points you to for anything you run twice."
	return 0
}

rules='rule_mav_ui_tree rule_jevi_ask_oneoff'

# --- emit --------------------------------------------------------------------

for rule in $rules; do
	NUDGE=''
	"$rule" || continue
	[ -n "$NUDGE" ] || continue
	worth_saying "$(occurrences "$rule")" || continue
	jq -n --arg ctx "$NUDGE" \
		'{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: $ctx}}' \
		2>/dev/null || :
	exit 0
done

exit 0
