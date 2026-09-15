package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedGroupTalkLine is returned before a receipt exists when a
// line has no exact gtalk alias at either the C suffix boundary or the
// transport-friendly prefix boundary.
var ErrUnsupportedGroupTalkLine = errors.New("line is not an implemented group-talk command")

// GroupTalkCommand contains only client text and the source alias. Recipient
// identities are resolved from the authoritative world snapshot.
type GroupTalkCommand struct {
	Alias   string
	Message string
}

type groupTalkLineRequest struct {
	Kind    string `json:"kind"`
	Line    string `json:"line"`
	Alias   string `json:"alias"`
	Message string `json:"message"`
}

var groupTalkAliases = [...]string{"그룹말", "무리말", "="}

// ParseGroupTalkLine accepts command10.c's suffix form (`메시지 그룹말`) and
// the equivalent prefix form used by the Go terminal (`그룹말 메시지`). Both
// forms retain the exact alias boundary and preserve message spaces after
// trimming the command's outer whitespace. A bare alias is admitted so the
// world reducer can produce the durable empty-message response.
func ParseGroupTalkLine(line string) (GroupTalkCommand, bool) {
	if !utf8.ValidString(line) {
		return GroupTalkCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return GroupTalkCommand{}, false
		}
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return GroupTalkCommand{}, false
	}
	for _, alias := range groupTalkAliases {
		if trimmed == alias {
			return GroupTalkCommand{Alias: alias}, true
		}
		if message, ok := groupTalkPrefixMessage(trimmed, alias); ok {
			return GroupTalkCommand{Alias: alias, Message: message}, true
		}
	}
	for _, alias := range groupTalkAliases {
		if message, ok := groupTalkSuffixMessage(trimmed, alias); ok {
			return GroupTalkCommand{Alias: alias, Message: message}, true
		}
	}
	return GroupTalkCommand{}, false
}

// parseGroupTalkLine retains the package-local helper convention while the
// exported parser remains available to a later transport integration.
func parseGroupTalkLine(line string) (GroupTalkCommand, bool) {
	return ParseGroupTalkLine(line)
}

// IsGroupTalkLine reports whether a line has one of the exact group-talk
// aliases and a syntactically bounded message boundary.
func IsGroupTalkLine(line string) bool {
	_, ok := ParseGroupTalkLine(line)
	return ok
}

// ExecuteGroupTalkLine evaluates the read-only group projection through the
// authenticated ownership and durable ExecuteGame boundary. The world state
// bytes are returned unchanged; only GroupTalkResult is persisted, so receipt
// replay never rebuilds recipient selection or event order.
func (o *Ownership) ExecuteGroupTalkLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseGroupTalkLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedGroupTalkLine
	}
	request, err := json.Marshal(groupTalkLineRequest{
		Kind: "group-talk", Line: line, Alias: command.Alias, Message: command.Message,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		result, err := state.PlanGroupTalk(actorID, command.Message)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}

func groupTalkPrefixMessage(line, alias string) (string, bool) {
	if !strings.HasPrefix(line, alias) || len(line) == len(alias) {
		return "", false
	}
	boundary, size := utf8.DecodeRuneInString(line[len(alias):])
	if boundary == utf8.RuneError || !unicode.IsSpace(boundary) {
		return "", false
	}
	return strings.TrimSpace(line[len(alias)+size:]), true
}

func groupTalkSuffixMessage(line, alias string) (string, bool) {
	if !strings.HasSuffix(line, alias) || len(line) == len(alias) {
		return "", false
	}
	start := len(line) - len(alias)
	boundary, _ := utf8.DecodeLastRuneInString(line[:start])
	if boundary == utf8.RuneError || !unicode.IsSpace(boundary) {
		return "", false
	}
	return strings.TrimSpace(line[:start]), true
}
