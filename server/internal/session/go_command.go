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

var ErrUnsupportedGoLine = errors.New("line is not an implemented go command")

// GoCommand is the parser-facing form of command6.c:go (cmdno 30).
// C parse() takes the last token as the verb (`<출구> [n] 가`), and the
// dispatched task also admits the prefix form (`가 <출구>`).
type GoCommand struct {
	Verb       string
	ExitName   string
	Occurrence int
}

type goLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Verb       string `json:"verb"`
	ExitName   string `json:"exit_name,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
	Now        int32  `json:"now"`
	Hour       int    `json:"hour"`
}

type GoOptions = world.GoOptions

func validGoLine(line string) bool {
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

func isGoVerb(token string) bool {
	return token == "가" || token == "들어가"
}

func looksLikeGoOccurrence(token string) bool {
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

func parseGoOccurrence(token string) (int, bool) {
	if strings.HasPrefix(token, "+") {
		return 0, false
	}
	n, err := strconv.ParseInt(token, 10, 31)
	if err != nil || n < 1 {
		return 0, false
	}
	return int(n), true
}

func parseGoRest(rest []string) (GoCommand, bool) {
	command := GoCommand{Occurrence: 1}
	if len(rest) >= 1 && looksLikeGoOccurrence(rest[len(rest)-1]) {
		occ, ok := parseGoOccurrence(rest[len(rest)-1])
		if !ok {
			return GoCommand{}, false
		}
		command.Occurrence = occ
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 0 {
		return command, true
	}
	if len(rest) != 1 || rest[0] == "" || strings.TrimSpace(rest[0]) != rest[0] {
		return GoCommand{}, false
	}
	command.ExitName = rest[0]
	return command, true
}

// ParseGoLine admits:
//
//	가 / 들어가
//	가 <exit> / 들어가 <exit>
//	가 <exit> <n> / 들어가 <exit> <n>
//	<exit> 가 / <exit> 들어가
//	<exit> <n> 가 / <exit> <n> 들어가
//
// Last-token verbs win when both ends look like 가/들어가, matching C parse().
func ParseGoLine(line string) (GoCommand, bool) {
	if !validGoLine(line) {
		return GoCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) == 0 || len(tokens) > 3 {
		return GoCommand{}, false
	}
	if isGoVerb(tokens[len(tokens)-1]) {
		command, ok := parseGoRest(tokens[:len(tokens)-1])
		if !ok {
			return GoCommand{}, false
		}
		command.Verb = tokens[len(tokens)-1]
		return command, true
	}
	if isGoVerb(tokens[0]) {
		command, ok := parseGoRest(tokens[1:])
		if !ok {
			return GoCommand{}, false
		}
		command.Verb = tokens[0]
		return command, true
	}
	return GoCommand{}, false
}

func IsGoLine(line string) bool {
	_, ok := ParseGoLine(line)
	return ok
}

func (o *Ownership) ExecuteGoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, hour int, catalog world.SpawnCatalog, roll func(int, int) int, allocate func() (string, error)) (storage.WorldReceipt, error) {
	return o.ExecuteGoLineWithOptions(ctx, store, worldID, commandID, lease, line, GoOptions{
		Now: now, Hour: hour, Roll: roll, Catalog: catalog, Allocate: allocate,
	})
}

func (o *Ownership) ExecuteGoLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options GoOptions) (storage.WorldReceipt, error) {
	command, ok := ParseGoLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedGoLine
	}
	payload, err := json.Marshal(goLineRequest{
		Kind: "go", Line: line, Verb: command.Verb, ExitName: command.ExitName,
		Occurrence: command.Occurrence, Now: options.Now, Hour: options.Hour,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	var committedNPCChaseIDs []string
	var committedArrivalTrapEvent *world.ArrivalTrapEvent
	receipt, err := o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanGo(actorID, command.ExitName, command.Occurrence, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyGo(proposal)
		if err != nil {
			return nil, nil, err
		}
		if proposal.NPCChase != nil {
			for _, move := range proposal.NPCChase.Moves {
				committedNPCChaseIDs = append(committedNPCChaseIDs, move.NPCID)
			}
		}
		if proposal.ArrivalTrapEvent != nil {
			event := *proposal.ArrivalTrapEvent
			committedArrivalTrapEvent = &event
		}
		nextRaw, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result.Response)
		return nextRaw, response, err
	})
	if err != nil {
		return receipt, err
	}
	receipt.NPCChaseIDs = nil
	receipt.ArrivalTrapEvent = nil
	if !receipt.Replayed && len(committedNPCChaseIDs) != 0 {
		receipt.NPCChaseIDs = append([]string(nil), committedNPCChaseIDs...)
	}
	if !receipt.Replayed && committedArrivalTrapEvent != nil {
		event := *committedArrivalTrapEvent
		receipt.ArrivalTrapEvent = &event
	}
	return receipt, nil
}
