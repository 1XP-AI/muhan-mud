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

var ErrUnsupportedDMEnemyLine = errors.New("line is not an implemented DM enemy command")

// DMEnemyCommand is the exact bounded suffix form of dm6.c:list_enm:
// `<괴물> [<occurrence>] *enemy` or `<괴물> [<occurrence>] *적`.
type DMEnemyCommand struct {
	Verb       string
	Target     string
	Occurrence int
}

type dmEnemyLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Verb       string `json:"verb"`
	Target     string `json:"target"`
	Occurrence int    `json:"occurrence"`
}

func validDMEnemyLine(line string) bool {
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

func ParseDMEnemyLine(line string) (DMEnemyCommand, bool) {
	if !validDMEnemyLine(line) {
		return DMEnemyCommand{}, false
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.ContainsAny(trimmed, "'\"\\") {
		return DMEnemyCommand{}, false
	}
	tokens := strings.Split(trimmed, " ")
	compact := tokens[:0]
	for _, token := range tokens {
		if token != "" {
			compact = append(compact, token)
		}
	}
	if len(compact) < 2 || len(compact) > 3 {
		return DMEnemyCommand{}, false
	}
	verb := compact[len(compact)-1]
	if verb != "*enemy" && verb != "*적" {
		return DMEnemyCommand{}, false
	}
	target := compact[0]
	if target == "" || strings.TrimSpace(target) != target || strings.IndexFunc(target, unicode.IsSpace) >= 0 {
		return DMEnemyCommand{}, false
	}
	command := DMEnemyCommand{Verb: verb, Target: target, Occurrence: 1}
	if len(compact) == 3 {
		occurrenceToken := compact[1]
		if !looksLikeDMFollowOccurrence(occurrenceToken) || strings.HasPrefix(occurrenceToken, "+") {
			return DMEnemyCommand{}, false
		}
		occurrence, err := strconv.ParseInt(occurrenceToken, 10, 31)
		if err != nil || occurrence < 1 {
			return DMEnemyCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

func IsDMEnemyLine(line string) bool {
	_, ok := ParseDMEnemyLine(line)
	return ok
}

func (o *Ownership) ExecuteDMEnemyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseDMEnemyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedDMEnemyLine
	}
	payload, err := json.Marshal(dmEnemyLineRequest{
		Kind: "dm-enemy", Line: line, Verb: command.Verb, Target: command.Target, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", world.ErrDMEnemyStateInvalid, err)
		}
		proposal, err := state.PlanDMEnemy(actorID, command.Verb, command.Target, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		_, result, err := state.ApplyDMEnemy(proposal)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return raw, response, err
	})
}
