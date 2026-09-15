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

var ErrUnsupportedTeachLine = errors.New("line is not an implemented teach command")

// TeachCommand contains only display selectors.  The world reducer resolves
// the canonical target identity and spell bit from the committed snapshot;
// neither a player ID nor a spell index crosses the session boundary.
type TeachCommand struct {
	Alias      string
	Target     string
	Occurrence int
	Spell      string
}

type teachLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	Target     string `json:"target"`
	Occurrence int    `json:"occurrence"`
	Spell      string `json:"spell"`
}

func validTeachLine(line string) bool {
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

// ParseTeachLine admits the source command's 가르쳐 alias with a target,
// spell selector, and optional one-based target occurrence.  The optional
// occurrence follows the target, matching the command parser shape used by
// other canonical target commands.
func ParseTeachLine(line string) (TeachCommand, bool) {
	if !validTeachLine(line) {
		return TeachCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 3 || len(tokens) > 4 || tokens[0] != "가르쳐" {
		return TeachCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] || strings.IndexFunc(tokens[1], unicode.IsSpace) >= 0 || tokens[len(tokens)-1] == "" || strings.IndexFunc(tokens[len(tokens)-1], unicode.IsSpace) >= 0 {
		return TeachCommand{}, false
	}
	command := TeachCommand{Alias: tokens[0], Target: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		command.Spell = tokens[2]
		return command, true
	}
	if tokens[2] == "" {
		return TeachCommand{}, false
	}
	occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
	if err != nil || occurrence < 1 {
		return TeachCommand{}, false
	}
	command.Occurrence = int(occurrence)
	command.Spell = tokens[3]
	return command, true
}

func IsTeachLine(line string) bool {
	_, ok := ParseTeachLine(line)
	return ok
}

// ExecuteTeachLine connects magic1.c:teach to one durable command receipt.
// The reducer is deterministic and has no RNG/clock/allocator inputs; a
// command-ID retry is therefore served by ExecuteGame from the first receipt
// without granting the spell a second time or re-emitting its event.
func (o *Ownership) ExecuteTeachLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseTeachLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTeachLine
	}
	payload, err := json.Marshal(teachLineRequest{
		Kind: "teach", Line: line, Alias: command.Alias, Target: command.Target,
		Occurrence: command.Occurrence, Spell: command.Spell,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanTeach(actorID, command.Target, command.Spell, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyTeach(proposal)
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
