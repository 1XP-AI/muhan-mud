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

// ErrUnsupportedGiveLine is returned before a durable receipt exists when a
// line is outside the bounded legacy suffix form: <item|amount냥> <player> 줘.
var ErrUnsupportedGiveLine = errors.New("line is not an implemented give command")

// GiveCommand is the terminal-owned portion of command8.c:give. Canonical
// IDs and permissions are resolved by the world reducer from its snapshot.
// The suffix is intentionally required because C's command parser routes give
// from the trailing global.c token (global 47, alias 줘).
type GiveCommand struct {
	Alias            string
	Kind             world.GiveKind
	Source           string
	ItemName         string
	TargetName       string
	Amount           int64
	ItemOccurrence   int
	TargetOccurrence int
}

type giveLineRequest struct {
	Kind             string `json:"kind"`
	Line             string `json:"line"`
	Alias            string `json:"alias"`
	Source           string `json:"source"`
	ItemName         string `json:"item_name,omitempty"`
	TargetName       string `json:"target_name"`
	Amount           int64  `json:"amount,omitempty"`
	ItemOccurrence   int    `json:"item_occurrence"`
	TargetOccurrence int    `json:"target_occurrence"`
}

func validGiveLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' || r == '\x1b' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseGiveLine admits the exact three-token source order used by give():
// <item> <target> 줘 or <positive-decimal냥> <target> 줘. Occurrence and
// prefix selectors remain outside this bounded slice; duplicate display names
// therefore fail closed in the world resolver rather than being guessed.
func ParseGiveLine(line string) (GiveCommand, bool) {
	if !validGiveLine(line) {
		return GiveCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) != 3 || tokens[2] != "줘" {
		return GiveCommand{}, false
	}
	if tokens[0] == "" || tokens[1] == "" || strings.TrimSpace(tokens[0]) != tokens[0] || strings.TrimSpace(tokens[1]) != tokens[1] {
		return GiveCommand{}, false
	}
	command := GiveCommand{
		Alias: "줘", Source: tokens[0], TargetName: tokens[1],
		ItemOccurrence: 1, TargetOccurrence: 1,
	}
	if strings.HasSuffix(tokens[0], "냥") {
		amount, err := world.ParseGiveAmount(tokens[0])
		if err != nil {
			return GiveCommand{}, false
		}
		command.Kind, command.Amount = world.GiveMoney, amount
		return command, true
	}
	command.Kind, command.ItemName = world.GiveItem, tokens[0]
	return command, true
}

func IsGiveLine(line string) bool {
	_, ok := ParseGiveLine(line)
	return ok
}

// ExecuteGiveLine binds ParseGiveLine to the authenticated session and the
// durable receipt-first executor. Both item subtree and gold transfers return
// one typed result with target/observer projections separated in JSON.
func (o *Ownership) ExecuteGiveLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseGiveLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedGiveLine
	}
	payload, err := json.Marshal(giveLineRequest{
		Kind: string(command.Kind), Line: line, Alias: command.Alias,
		Source: command.Source, ItemName: command.ItemName,
		TargetName: command.TargetName, Amount: command.Amount,
		ItemOccurrence: command.ItemOccurrence, TargetOccurrence: command.TargetOccurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result world.GiveResult
		switch command.Kind {
		case world.GiveItem:
			next, result, err = state.GiveItem(actorID, command.ItemName, command.ItemOccurrence, command.TargetName, command.TargetOccurrence)
		case world.GiveMoney:
			next, result, err = state.GiveMoney(actorID, command.TargetName, command.Amount, command.TargetOccurrence)
		default:
			return nil, nil, ErrUnsupportedGiveLine
		}
		if err != nil {
			return nil, nil, err
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return nextRaw, response, err
	})
}
