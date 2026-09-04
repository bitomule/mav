package mav

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// envAssignment is one `NAME=value` pair written in front of a recipe
// command, kept in the order it was written so the trail and any later
// expansion read the way the author wrote it.
type envAssignment struct {
	Name  string
	Value string

	// Literal says the value was written inside single quotes, so it must
	// not be expanded. A shell delivers 'literal $NOT_A_VAR' unchanged, and
	// silently disagreeing with the syntax mav is imitating is the same
	// class of quiet wrongness this whole path exists to remove.
	Literal bool
}

// splitEnvPrefix separates the leading `NAME=value` assignments of a recipe
// command from the command itself, the way a shell would.
//
// It exists because a recipe line like
//
//	FOO=bar xcrun simctl launch "$MAV_UDID" "$MAV_BUNDLE_ID"
//
// is routed to a driver instead of a shell, and a driver that reads only the
// bundle id would drop the assignment without saying so. Whoever wrote it
// would see the app start and believe FOO arrived.
//
// ok is false when the line cannot be tokenized (an unbalanced quote): the
// caller then leaves the command alone and lets the shell path deal with it,
// which is where a malformed line belongs anyway.
func splitEnvPrefix(command string) (assignments []envAssignment, rest string, ok bool) {
	assignments, consumed, ok := scanEnvPrefix(command)
	if !ok {
		return nil, command, false
	}
	if len(assignments) == 0 {
		return nil, command, true
	}
	return assignments, strings.TrimSpace(command[consumed:]), true
}

// scanEnvPrefix is the shared tokenizing walk behind splitEnvPrefix and
// rawEnvPrefix: it reads the leading `NAME=value` tokens and reports how many
// bytes of the original command they occupy, so a caller can either take the
// parsed assignments (splitEnvPrefix) or the exact source bytes (rawEnvPrefix).
func scanEnvPrefix(command string) (assignments []envAssignment, consumed int, ok bool) {
	tokens, ok := shellTokens(command)
	if !ok {
		return nil, 0, false
	}
	for _, token := range tokens {
		name, value, isAssignment := splitAssignment(token.value)
		if !isAssignment {
			break
		}
		assignments = append(assignments, envAssignment{Name: name, Value: value, Literal: token.singleQuoted})
		consumed = token.end
	}
	return assignments, consumed, true
}

// rawEnvPrefix is splitEnvPrefix for callers that must reproduce the prefix
// verbatim rather than normalize it: it returns the exact source bytes the
// author wrote for the assignments (raw), not shellQuote's re-encoding of
// them. Needed wherever a rewritten command goes on to run in a shell, since
// joinEnvPrefix's single-quoting would freeze a value like $MAV_APP_PATH into
// a literal instead of letting it expand.
func rawEnvPrefix(command string) (raw string, rest string, ok bool) {
	assignments, consumed, ok := scanEnvPrefix(command)
	if !ok {
		return "", command, false
	}
	if len(assignments) == 0 {
		return "", command, true
	}
	return command[:consumed], strings.TrimSpace(command[consumed:]), true
}

// recipeEnvPrefixRaw is rawEnvPrefix for callers that only route on the
// command, mirroring recipeEnvPrefix's fallback: an unparseable line has no
// usable prefix, and it is the shell's problem.
func recipeEnvPrefixRaw(command string) (raw string, rest string) {
	raw, rest, ok := rawEnvPrefix(command)
	if !ok {
		return "", strings.TrimSpace(command)
	}
	return raw, rest
}

// joinRawEnvPrefix is joinEnvPrefix for verbatim prefixes: it re-attaches the
// author's own bytes instead of re-quoting them.
func joinRawEnvPrefix(raw string, command string) string {
	if raw == "" {
		return command
	}
	return raw + " " + command
}

// recipeEnvPrefix is splitEnvPrefix for callers that only route on the
// command. ok is false when the line cannot be tokenized (an unbalanced
// quote): callers that route a recipe to the driver vs. the shell must treat
// that as "not a recognized driver form" and send it to the shell, where the
// author sees the real syntax error instead of a silently prefix-less launch.
func recipeEnvPrefix(command string) (assignments []envAssignment, rest string, ok bool) {
	assignments, rest, ok = splitEnvPrefix(command)
	if !ok {
		return nil, strings.TrimSpace(command), false
	}
	return assignments, strings.TrimSpace(rest), true
}

// joinEnvPrefix puts an assignment prefix back in front of a command mav
// rewrote underneath it.
func joinEnvPrefix(assignments []envAssignment, command string) string {
	if len(assignments) == 0 {
		return command
	}
	parts := make([]string, 0, len(assignments)+1)
	for _, assignment := range assignments {
		parts = append(parts, assignment.Name+"="+shellQuote(assignment.Value))
	}
	parts = append(parts, command)
	return strings.Join(parts, " ")
}

// expandEnvAssignments resolves `$VAR` and `${VAR}` in the values against the
// same variables the shell path exports, falling back to mav's own
// environment. Without it a recipe that says FOO=$MAV_RUN_DIR on the driver
// path would deliver the literal string, which is the same silent lie in a
// smaller costume.
func expandEnvAssignments(assignments []envAssignment, env map[string]string) map[string]string {
	if len(assignments) == 0 {
		return nil
	}
	out := make(map[string]string, len(assignments))
	lookup := func(key string) string {
		if value, ok := env[key]; ok {
			return value
		}
		if value, ok := out[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	for _, assignment := range assignments {
		if assignment.Literal {
			out[assignment.Name] = assignment.Value
			continue
		}
		out[assignment.Name] = os.Expand(assignment.Value, lookup)
	}
	return out
}

// rejectCommandSubstitution refuses a value mav cannot honour. On the driver
// path there is no shell, so `$(date)` or a backtick would be delivered to the
// app as its own literal text -- the app would start, the value would be
// wrong, and the trail (which carries names only) would show nothing. Failing
// is the honest answer: the recipe asked for something this route cannot do.
func rejectCommandSubstitution(assignments []envAssignment) error {
	var offenders []string
	for _, assignment := range assignments {
		if assignment.Literal {
			continue
		}
		if strings.Contains(assignment.Value, "$(") || strings.Contains(assignment.Value, "`") {
			offenders = append(offenders, assignment.Name)
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)
	return fmt.Errorf("launch_env_command_substitution: %s uses command substitution, which mav cannot run for a launch it hands to a driver; next: compute the value in the recipe's build or app_path step, or single-quote it to pass the text as written", strings.Join(offenders, ","))
}

// envNames lists the variable names sorted, for the commands trail.
func envNames(env map[string]string) []string {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type shellToken struct {
	value        string
	end          int  // byte offset just past the token in the original string
	singleQuoted bool // any part of it was written inside single quotes
}

// shellTokens splits on unquoted whitespace and removes one level of quoting,
// which is as much shell as reading an assignment prefix needs. Anything
// beyond that (expansions, operators) is left inside the token: the prefix
// loop stops at the first token that is not an assignment, so a complicated
// command body never reaches this logic.
func shellTokens(command string) ([]shellToken, bool) {
	var tokens []shellToken
	var current strings.Builder
	started := false
	single := false
	quote := byte(0)
	for i := 0; i < len(command); i++ {
		ch := command[i]
		switch {
		case quote != 0:
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' && ch == '\\' && i+1 < len(command) {
				i++
				current.WriteByte(command[i])
				continue
			}
			current.WriteByte(ch)
		case ch == '\'' || ch == '"':
			quote = ch
			single = single || ch == '\''
			started = true
		case ch == '\\' && i+1 < len(command):
			i++
			current.WriteByte(command[i])
			started = true
		case ch == ' ' || ch == '\t' || ch == '\n':
			if started {
				tokens = append(tokens, shellToken{value: current.String(), end: i, singleQuoted: single})
				current.Reset()
				started = false
				single = false
			}
		default:
			current.WriteByte(ch)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if started {
		tokens = append(tokens, shellToken{value: current.String(), end: len(command), singleQuoted: single})
	}
	return tokens, true
}

// splitAssignment recognizes NAME=value with the same name rule as the shell.
func splitAssignment(token string) (name string, value string, ok bool) {
	index := strings.IndexByte(token, '=')
	if index <= 0 {
		return "", "", false
	}
	name = token[:index]
	for i := 0; i < len(name); i++ {
		ch := name[i]
		isLetter := ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		isDigit := ch >= '0' && ch <= '9'
		if isLetter || (i > 0 && isDigit) {
			continue
		}
		return "", "", false
	}
	return name, token[index+1:], true
}
