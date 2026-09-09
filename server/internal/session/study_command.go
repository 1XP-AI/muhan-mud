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

var ErrUnsupportedStudyLine = errors.New("line is not an implemented study command")

// StudyCommand contains only the client-facing display selector. The world
// reducer resolves the canonical item identity from the authenticated actor's
// inventory at execution time.
type StudyCommand struct {
	Alias      string
	Name       string
	Occurrence int
}

type LearnCommand = StudyCommand

type studyLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	Alias      string `json:"alias"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

func validStudyLine(line string) bool {
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

// ParseStudyLine admits the source aliases 배워 and 연마 with one exact item
// name and an optional positive one-based occurrence. The target remains a
// display selector; item IDs never cross the session boundary.
func ParseStudyLine(line string) (StudyCommand, bool) {
	if !validStudyLine(line) {
		return StudyCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 || (tokens[0] != "배워" && tokens[0] != "연마") {
		return StudyCommand{}, false
	}
	if tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return StudyCommand{}, false
	}
	command := StudyCommand{Alias: tokens[0], Name: tokens[1], Occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.ParseUint(tokens[2], 10, 31)
		if err != nil || occurrence < 1 {
			return StudyCommand{}, false
		}
		command.Occurrence = int(occurrence)
	}
	return command, true
}

func ParseLearnLine(line string) (LearnCommand, bool) { return ParseStudyLine(line) }

func IsStudyLine(line string) bool {
	_, ok := ParseStudyLine(line)
	return ok
}

func IsLearnLine(line string) bool { return IsStudyLine(line) }

// ExecuteStudyLine binds the source study command to the durable receipt and
// replay boundary. There is no RNG or ID allocation in this reducer; a replay
// returns the first committed response without re-running item deletion or
// room transfer.
func (o *Ownership) ExecuteStudyLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseStudyLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedStudyLine
	}
	payload, err := json.Marshal(studyLineRequest{
		Kind: "study", Line: line, Alias: command.Alias, Name: command.Name, Occurrence: command.Occurrence,
	})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanStudy(actorID, command.Name, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyStudy(proposal)
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

// ExecuteLearnLine is the spelling alias used by callers that name the
// command after the player-facing verb.
func (o *Ownership) ExecuteLearnLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteStudyLine(ctx, store, worldID, commandID, lease, line)
}
