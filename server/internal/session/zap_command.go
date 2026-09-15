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

var ErrUnsupportedZapLine = errors.New("line is not an implemented zap command")

// ZapCommand is the parser-facing form of magic1.c:zap. C parse() takes the
// last token as the verb, so prefix `zap 지팡이` is not this command.
type ZapCommand struct {
	ItemName         string
	Occurrence       int
	TargetName       string
	TargetOccurrence int
}

type zapLineRequest struct {
	Kind             string `json:"kind"`
	Line             string `json:"line"`
	ItemName         string `json:"item_name,omitempty"`
	Occurrence       int    `json:"occurrence,omitempty"`
	TargetName       string `json:"target_name,omitempty"`
	TargetOccurrence int    `json:"target_occurrence,omitempty"`
	Now              int32  `json:"now"`
}

type ZapOptions = world.ZapOptions

func validZapLine(line string) bool {
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

func looksLikeZapOccurrence(token string) bool {
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

func parseZapOccurrence(token string) (int, bool) {
	if strings.HasPrefix(token, "+") {
		return 0, false
	}
	n, err := strconv.ParseInt(token, 10, 31)
	if err != nil || n < 1 {
		return 0, false
	}
	return int(n), true
}

// ParseZapLine admits the original suffix form:
//
//	zap
//	<item> zap
//	<item> <occurrence> zap
//	<item> <target> zap
//	<item> <occurrence> <target> zap
//	<item> <target> <occurrence> zap
//	<item> <occurrence> <target> <occurrence> zap
func ParseZapLine(line string) (ZapCommand, bool) {
	if !validZapLine(line) {
		return ZapCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || tokens[len(tokens)-1] != "zap" || len(tokens) > 5 {
		return ZapCommand{}, false
	}
	command := ZapCommand{Occurrence: 1, TargetOccurrence: 1}
	rest := tokens[:len(tokens)-1]
	switch len(rest) {
	case 0:
		return command, true
	case 1:
		if rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] {
			return ZapCommand{}, false
		}
		command.ItemName = rest[0]
		return command, true
	case 2:
		if looksLikeZapOccurrence(rest[1]) {
			occ, ok := parseZapOccurrence(rest[1])
			if !ok || rest[0] == "" {
				return ZapCommand{}, false
			}
			command.ItemName, command.Occurrence = rest[0], occ
			return command, true
		}
		if rest[0] == "" || rest[1] == "" {
			return ZapCommand{}, false
		}
		command.ItemName, command.TargetName = rest[0], rest[1]
		return command, true
	case 3:
		if looksLikeZapOccurrence(rest[1]) && !looksLikeZapOccurrence(rest[2]) {
			occ, ok := parseZapOccurrence(rest[1])
			if !ok || rest[0] == "" || rest[2] == "" {
				return ZapCommand{}, false
			}
			command.ItemName, command.Occurrence, command.TargetName = rest[0], occ, rest[2]
			return command, true
		}
		if looksLikeZapOccurrence(rest[2]) && !looksLikeZapOccurrence(rest[1]) {
			occ, ok := parseZapOccurrence(rest[2])
			if !ok || rest[0] == "" || rest[1] == "" {
				return ZapCommand{}, false
			}
			command.ItemName, command.TargetName, command.TargetOccurrence = rest[0], rest[1], occ
			return command, true
		}
		return ZapCommand{}, false
	case 4:
		itemOcc, ok := parseZapOccurrence(rest[1])
		if !ok {
			return ZapCommand{}, false
		}
		targetOcc, ok := parseZapOccurrence(rest[3])
		if !ok || rest[0] == "" || rest[2] == "" {
			return ZapCommand{}, false
		}
		command.ItemName, command.Occurrence = rest[0], itemOcc
		command.TargetName, command.TargetOccurrence = rest[2], targetOcc
		return command, true
	default:
		return ZapCommand{}, false
	}
}

func IsZapLine(line string) bool {
	_, ok := ParseZapLine(line)
	return ok
}

func (o *Ownership) ExecuteZapLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteZapLineWithOptions(ctx, store, worldID, commandID, lease, line, ZapOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteZapLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options ZapOptions) (storage.WorldReceipt, error) {
	command, ok := ParseZapLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedZapLine
	}
	payload, err := json.Marshal(zapLineRequest{
		Kind: "zap", Line: line, ItemName: command.ItemName, Occurrence: command.Occurrence,
		TargetName: command.TargetName, TargetOccurrence: command.TargetOccurrence, Now: options.Now,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanZap(actorID, command.ItemName, command.Occurrence, command.TargetName, command.TargetOccurrence, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyZap(proposal)
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
