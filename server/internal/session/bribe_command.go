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

var ErrUnsupportedBribeLine = errors.New("line is not an implemented bribe command")

// BribeCommand mirrors command9.c:bribe's suffix form.  The optional numeric
// token is the positive same-name occurrence used by the legacy parser:
//
//	<NPC> <amount>냥 뇌물
//	<NPC> <occurrence> <amount>냥 뇌물
type BribeCommand struct {
	NPCName       string
	NPCOccurrence int
	Amount        int32
}

type bribeLineRequest struct {
	Kind          string `json:"kind"`
	Line          string `json:"line"`
	NPCName       string `json:"npc_name"`
	NPCOccurrence int    `json:"npc_occurrence"`
	Amount        int32  `json:"amount"`
}

func ParseBribeLine(line string) (BribeCommand, bool) {
	if !utf8.ValidString(line) {
		return BribeCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return BribeCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || (len(tokens) != 3 && len(tokens) != 4) || tokens[len(tokens)-1] != "뇌물" {
		return BribeCommand{}, false
	}
	if tokens[0] == "" || strings.TrimSpace(tokens[0]) != tokens[0] {
		return BribeCommand{}, false
	}
	command := BribeCommand{NPCName: tokens[0], NPCOccurrence: 1}
	amountToken := tokens[1]
	if len(tokens) == 4 {
		occurrence, parseErr := strconv.ParseUint(tokens[1], 10, 31)
		if parseErr != nil || occurrence < 1 {
			return BribeCommand{}, false
		}
		command.NPCOccurrence = int(occurrence)
		amountToken = tokens[2]
	}
	amount, parseErr := world.ParseBribeAmount(amountToken)
	if parseErr != nil {
		return BribeCommand{}, false
	}
	command.Amount = amount
	return command, true
}

func IsBribeLine(line string) bool {
	_, ok := ParseBribeLine(line)
	return ok
}

// ExecuteBribeLine binds the pure bribe reducer to the durable receipt
// boundary.  A missing visible target becomes a committed deterministic
// no-op; replay returns the original response without selecting again.
func (o *Ownership) ExecuteBribeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseBribeLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedBribeLine
	}
	payload, err := json.Marshal(bribeLineRequest{
		Kind: "bribe", Line: line, NPCName: command.NPCName,
		NPCOccurrence: command.NPCOccurrence, Amount: command.Amount,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanBribe(actorID, command.NPCName, command.NPCOccurrence, command.Amount)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyBribe(proposal)
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
