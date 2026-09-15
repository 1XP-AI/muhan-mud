package session

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedItemRenameLine is returned before a receipt exists when the
// line is outside the exact suffix form admitted by this command lane.
var ErrUnsupportedItemRenameLine = errors.New("line is not an implemented item rename command")

// ItemRenameCommand is the parser-facing form of command8.c:chg_name.  The
// item name is a display selector, never a canonical item ID; the world
// reducer resolves the authoritative direct inventory root from its snapshot.
type ItemRenameCommand struct {
	Alias      string
	ItemName   string
	Name       string // descriptive alias for the selected item name
	Occurrence int
	NewName    string
}

// RenameCommand is a concise compatibility alias for command adapters.
type RenameCommand = ItemRenameCommand

type itemRenameLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	ItemName   string `json:"item_name"`
	Occurrence int    `json:"occurrence"`
	NewName    string `json:"new_name"`
}

// ParseItemRenameLine admits the source suffix form:
//
//	<item> [occurrence] <new name> 명명
//
// The first selector may be quoted through the legacy tokenizer.  The new
// name may contain ordinary ASCII spaces, matching chg_name's free-form copy
// after the final 명명 token.  Prefix/key matching and nested/equipped lookup
// are intentionally left to the fail-closed world boundary.
func ParseItemRenameLine(line string) (ItemRenameCommand, bool) {
	if !utf8.ValidString(line) {
		return ItemRenameCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return ItemRenameCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 3 || tokens[len(tokens)-1] != "명명" {
		return ItemRenameCommand{}, false
	}
	if tokens[0] == "" || strings.TrimSpace(tokens[0]) != tokens[0] {
		return ItemRenameCommand{}, false
	}
	command := ItemRenameCommand{Alias: "명명", ItemName: tokens[0], Name: tokens[0], Occurrence: 1}
	newNameStart := 1
	if len(tokens) >= 4 {
		candidate := tokens[1]
		if looksLikeItemRenameOccurrence(candidate) {
			if strings.HasPrefix(candidate, "+") {
				return ItemRenameCommand{}, false
			}
			occurrence, parseErr := strconv.ParseInt(candidate, 10, 31)
			if parseErr != nil || occurrence < 1 {
				return ItemRenameCommand{}, false
			}
			command.Occurrence = int(occurrence)
			newNameStart = 2
		}
	}
	if newNameStart >= len(tokens)-1 {
		return ItemRenameCommand{}, false
	}
	command.NewName = strings.Join(tokens[newNameStart:len(tokens)-1], " ")
	if err := world.ValidateItemRenameName(command.NewName); err != nil {
		return ItemRenameCommand{}, false
	}
	return command, true
}

func looksLikeItemRenameOccurrence(token string) bool {
	if token == "" {
		return false
	}
	for i, r := range token {
		if (r == '+' || r == '-') && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ParseRenameLine is the concise parser spelling used by command adapters.
func ParseRenameLine(line string) (ItemRenameCommand, bool) {
	return ParseItemRenameLine(line)
}

// IsItemRenameLine is a descriptive parser helper for a later dispatcher.
// The central command_parser.go remains intentionally untouched in this
// independently owned suffix lane.
func IsItemRenameLine(line string) bool {
	_, ok := ParseItemRenameLine(line)
	return ok
}

// IsRenameLine is the concise helper spelling used by command adapters.
func IsRenameLine(line string) bool { return IsItemRenameLine(line) }

// ExecuteItemRenameLine binds the pure item-rename proposal/apply boundary to
// ExecuteGame's durable receipt/replay protocol.  Replay returns the original
// typed response and never re-runs selection or flag mutation.
func (o *Ownership) ExecuteItemRenameLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseItemRenameLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedItemRenameLine
	}
	payload, err := json.Marshal(itemRenameLineRequest{
		Kind: "item_rename", Line: line, Alias: command.Alias,
		ItemName: command.ItemName, Occurrence: command.Occurrence, NewName: command.NewName,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanItemRename(actorID, command.ItemName, command.Occurrence, command.NewName)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyItemRename(proposal)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	})
}

// ExecuteRenameLine is the concise execution spelling used by command
// adapters.
func (o *Ownership) ExecuteRenameLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteItemRenameLine(ctx, store, worldID, commandID, lease, line)
}
