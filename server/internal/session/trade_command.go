package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedTradeLine = errors.New("line is not an implemented NPC trade command")

// TradeCommand follows command10.c's suffix command syntax:
// 교환, <item> 교환, or <item> <monster> 교환. The optional two occurrences
// are an explicit Go identity extension used only when duplicate canonical
// names exist: <item> <monster> <item-occurrence> <monster-occurrence> 교환.
// Prefix/key guessing and client-owned IDs are intentionally not accepted.
type TradeCommand struct {
	ItemName       string
	ItemOccurrence int
	NPCName        string
	NPCOccurrence  int
}

func validTradeLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return false
		}
	}
	return true
}

func ParseTradeLine(line string) (TradeCommand, bool) {
	if !validTradeLine(line) {
		return TradeCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[len(tokens)-1] != "교환" {
		return TradeCommand{}, false
	}
	rest := tokens[:len(tokens)-1]
	command := TradeCommand{ItemOccurrence: 1, NPCOccurrence: 1}
	switch len(rest) {
	case 0:
		return command, true
	case 1:
		if rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] {
			return TradeCommand{}, false
		}
		command.ItemName = rest[0]
		return command, true
	case 2:
		if rest[0] == "" || rest[1] == "" || strings.TrimSpace(rest[0]) != rest[0] || strings.TrimSpace(rest[1]) != rest[1] {
			return TradeCommand{}, false
		}
		command.ItemName = rest[0]
		command.NPCName = rest[1]
		return command, true
	case 4:
		if rest[0] == "" || rest[1] == "" {
			return TradeCommand{}, false
		}
		itemOccurrence, err := strconv.Atoi(rest[2])
		if err != nil || itemOccurrence < 1 {
			return TradeCommand{}, false
		}
		npcOccurrence, err := strconv.Atoi(rest[3])
		if err != nil || npcOccurrence < 1 {
			return TradeCommand{}, false
		}
		command.ItemName = rest[0]
		command.NPCName = rest[1]
		command.ItemOccurrence = itemOccurrence
		command.NPCOccurrence = npcOccurrence
		return command, true
	default:
		return TradeCommand{}, false
	}
}

func IsTradeLine(line string) bool {
	_, ok := ParseTradeLine(line)
	return ok
}

type tradeRequest struct {
	Line           string `json:"line"`
	ItemName       string `json:"item_name"`
	ItemOccurrence int    `json:"item_occurrence"`
	NPCName        string `json:"npc_name"`
	NPCOccurrence  int    `json:"npc_occurrence"`
}

// ExecuteTradeLine keeps the actor and world receipt boundary identical to
// other commands. Reward IDs are deterministic descendants of commandID so an
// uncertain commit can be retried without an external allocator; replay never
// invokes this reducer a second time.
func (o *Ownership) ExecuteTradeLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseTradeLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTradeLine
	}
	request, err := json.Marshal(tradeRequest{Line: line, ItemName: command.ItemName, ItemOccurrence: command.ItemOccurrence, NPCName: command.NPCName, NPCOccurrence: command.NPCOccurrence})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		allocation := 0
		proposal, err := s.PlanTrade(actorID, command.ItemName, command.ItemOccurrence, command.NPCName, command.NPCOccurrence, func() (string, error) {
			allocation++
			return fmt.Sprintf("trade-%s-%d", commandID, allocation), nil
		})
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.NPCTradeResult
		if !proposal.Changed {
			result = proposal.Result
			response, err := json.Marshal(result)
			return raw, response, err
		}
		next, result, err = s.ApplyTrade(proposal)
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
