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

rule_mav_tree_without_find() {
	case "$CMD" in
	*"mav ui tree"*) ;;
	*) return 1 ;;
	esac

	# Already doing it the cheap way. A command that reaches for find at all is
	# not the one this is aimed at, even when it also dumps a tree.
	case "$CMD" in
	*"mav ui find"*) return 1 ;;
	esac

	# No size gate. An earlier version of this rule stayed quiet under 40 nodes,
	# reasoning that reading a dozen elements is cheaper than asking anything.
	# That reasoning is about one call and the advice is not: an advisory that
	# appears on some screens and not others is one nobody learns, and the agent
	# has no way to tell which kind of screen it is about to get before it asks.
	# So it says it every time, like the rule above it.

	# The availability gate. Without a key, find answers resolved_by=none and
	# the advice is noise, so the advice is not given. Checked by asking
	# whether a key EXISTS, never by reading one and never over the network:
	# the environment variable, then the keychain entry queried without -w so
	# no secret is fetched, then the config file's presence on disk.
	if [ -z "${MAV_JEV_API_KEY:-}" ] &&
		! /usr/bin/security find-generic-password -s mav-jev >/dev/null 2>&1 &&
		[ ! -f "${XDG_CONFIG_HOME:-$HOME/.config}/bitomule/mav/config.json" ]; then
		return 1
	fi

	NUDGE="\`mav ui find \"<what you want to tap>\"\` resolves one element from a description in your own words, so you do not filter a whole screen by hand. \`mav ui tree\` no longer caps its output, so a dense screen is now a long one. find replaces \`mav ui tree | grep\`, not your judgement: it never returns an element it is unsure of, and resolved_by=none means fall back to the tree you were going to read anyway."
	return 0
}

rule_chained_taps_without_goto() {
	# `goto` is NOT like `find`, and the difference decides when this speaks.
	# find is worth suggesting on any tree, because it always buys context.
	# goto only wins from the SECOND step: measured, one tap through goto is
	# 5,012ms against 4,474ms for today's path, and two taps are 7,336ms
	# against 9,733ms. Suggesting it for a single tap would be advice that
	# makes things slower.
	#
	# So the signal has to be chained navigation, and this hook keeps no state
	# by design — no counter, nothing that can go stale. The one chain it CAN
	# see without state is a chain inside one command line: two or more taps
	# issued together. That is narrow, and narrow is the right side to err on
	# here: the alternative is a stateful "how many taps this session" that
	# would be wrong after a compaction and would nag on every single tap.
	case "$CMD" in
	*"mav ui tap"* | *"mav ui swipe"*) ;;
	*) return 1 ;;
	esac
	case "$CMD" in
	*"mav goto"*) return 1 ;;
	esac

	# Count the navigation commands on this line. One is not a chain.
	TAPS=$(printf '%s\n' "$CMD" | grep -o 'mav ui tap\|mav ui swipe' | wc -l | tr -d ' ')
	[ "${TAPS:-0}" -ge 2 ] || return 1

	# Same availability gate as the find rule: with no key there is nothing to
	# recommend, and it is answered by asking whether a key EXISTS, never by
	# reading one and never over the network.
	if [ -z "${MAV_JEV_API_KEY:-}" ] &&
		! /usr/bin/security find-generic-password -s mav-jev >/dev/null 2>&1 &&
		[ ! -f "${XDG_CONFIG_HOME:-$HOME/.config}/bitomule/mav/config.json" ]; then
		return 1
	fi

	NUDGE="You chained ${TAPS} navigation steps. \`mav goto \"<the screen you want>\" --arrived-when 'title:\"<its title>\"'\` does the whole walk in one call — measured at 7.3s against 9.7s for two steps done this way, and that gap widens with each extra step because goto reads the screen once per step where this path reads it twice. It refuses to tap anything destructive. With no --arrived-when it deduces a criterion at step zero and says so (criterion_source=inferred); when it cannot, it reports arrived=unverified rather than true. Measured on a two-hop route whose destination is named by something not visible at the start, it declined 10 times out of 10 — write --arrived-when when you need a verdict. For a SINGLE tap, keep doing what you are doing: goto is slower over one step."
	return 0
}

rules='rule_jevi_ask_oneoff rule_mav_tree_without_find rule_chained_taps_without_goto'

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
