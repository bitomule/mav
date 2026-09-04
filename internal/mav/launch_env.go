package mav

import (
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
	tokens, ok := shellTokens(command)
	if !ok {
		return nil, command, false
	}
	consumed := 0
	for _, token := range tokens {
		name, value, isAssignment := splitAssignment(token.value)
		if !isAssignment {
			break
		}
		assignments = append(assignments, envAssignment{Name: name, Value: value})
		consumed = token.end
	}
	if len(assignments) == 0 {
		return nil, command, true
	}
	return assignments, strings.TrimSpace(command[consumed:]), true
}

// recipeEnvPrefix is splitEnvPrefix for callers that only route on the
// command: a line that cannot be tokenized has no usable prefix, and it is
// the shell's problem.
func recipeEnvPrefix(command string) ([]envAssignment, string) {
	assignments, rest, ok := splitEnvPrefix(command)
	if !ok {
		return nil, strings.TrimSpace(command)
	}
	return assignments, strings.TrimSpace(rest)
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
		out[assignment.Name] = os.Expand(assignment.Value, lookup)
	}
	return out
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
	value string
	end   int // byte offset just past the token in the original string
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
			started = true
		case ch == '\\' && i+1 < len(command):
			i++
			current.WriteByte(command[i])
			started = true
		case ch == ' ' || ch == '\t' || ch == '\n':
			if started {
				tokens = append(tokens, shellToken{value: current.String(), end: i})
				current.Reset()
				started = false
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
		tokens = append(tokens, shellToken{value: current.String(), end: len(command)})
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
