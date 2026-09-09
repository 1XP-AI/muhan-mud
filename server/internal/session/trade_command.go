package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

var ErrUnsupportedTradeLine = errors.New("line is not an implemented NPC trade command")

// TradeCommand follows command10.c's suffix command syntax:
// <item> <monster> 교환. The optional two occurrences are an explicit Go
// identity extension used only when duplicate canonical names exist:
// <item> <monster> <item-occurrence> <monster-occurrence> 교환.
// Prefix/key guessing and client-owned IDs are intentionally not accepted.
type TradeCommand struct {
	ItemName       string
	ItemOccurrence int
	NPCName        string
	NPCOccurrence  int
}

func ParseTradeLine(line string) (TradeCommand, bool) {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || (len(tokens) != 3 && len(tokens) != 5) || tokens[len(tokens)-1] != "교환" {
		return TradeCommand{}, false
	}
	if tokens[0] == "" || tokens[1] == "" {
		return TradeCommand{}, false
	}
	command := TradeCommand{ItemName: tokens[0], ItemOccurrence: 1, NPCName: tokens[1], NPCOccurrence: 1}
	if len(tokens) == 5 {
		itemOccurrence, err := strconv.Atoi(tokens[2])
		if err != nil || itemOccurrence < 1 {
			return TradeCommand{}, false
		}
		npcOccurrence, err := strconv.Atoi(tokens[3])
		if err != nil || npcOccurrence < 1 {
			return TradeCommand{}, false
		}
		command.ItemOccurrence = itemOccurrence
		command.NPCOccurrence = npcOccurrence
	}
	return command, true
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
		next, result, err := s.TradeNPCByName(actorID, command.ItemName, command.ItemOccurrence, command.NPCName, command.NPCOccurrence, func() (string, error) {
			allocation++
			return fmt.Sprintf("trade-%s-%d", commandID, allocation), nil
		})
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
