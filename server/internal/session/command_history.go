package session

import (
	"strings"
	"unicode/utf8"
)

// CommandHistoryLimitBytes mirrors the legacy command buffer's useful input
// budget. History is connection-local input state, never world state, and is
// therefore intentionally not persisted in a receipt or PlayerState.
const CommandHistoryLimitBytes = 79

// ExpandHistoryLine applies the original command() `!` convention:
//
//   - `!` repeats the previous command;
//   - `!suffix` appends suffix to the previous command;
//   - every other line is submitted unchanged.
//
// The returned remembered value is the leading-space-trimmed expanded line.
// Empty expansions do not replace an existing history entry. The helper is
// pure so the connector can update history immediately before parsing while
// keeping the durable command reducer unaware of connection-local state.
func ExpandHistoryLine(previous, line string) (expanded, remembered string) {
	expanded = line
	if line == "!" {
		expanded = previous
	} else if strings.HasPrefix(line, "!") {
		expanded = previous + line[1:]
	}
	remembered = truncateHistory(strings.TrimLeft(expanded, " "))
	if remembered == "" {
		return expanded, previous
	}
	return expanded, remembered
}

func truncateHistory(value string) string {
	if len(value) <= CommandHistoryLimitBytes {
		return value
	}
	cut := CommandHistoryLimitBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}
