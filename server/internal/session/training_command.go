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

// ErrUnsupportedTrainingLine is returned before a durable receipt exists for
// anything other than command7.c's exact bare global alias.
var ErrUnsupportedTrainingLine = errors.New("line is not an implemented training command")

// TrainingCommand carries the one parser-owned value. Actor identity and all
// mutable progression remain derived from the authenticated session and the
// committed world snapshot.
type TrainingCommand struct {
	Alias string
}

// TrainingOptions is intentionally empty. The source command has no clock or
// random input; keeping an options boundary lets the transport pass a stable
// API without allowing client-controlled progression values.
type TrainingOptions struct{}

// TrainOptions is a spelling alias for adapters that use the C function name.
type TrainOptions = TrainingOptions

type trainingLineRequest struct {
	Kind  string `json:"kind"`
	Line  string `json:"line"`
	Alias string `json:"alias"`
}

func validTrainingLine(line string) bool {
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

// ParseTrainingLine admits only the exact bare Korean alias from global.c
// (global token 47). Outer terminal whitespace is harmless, while arguments,
// alternate spellings and quoted forms stay outside this bounded slice.
func ParseTrainingLine(line string) (TrainingCommand, bool) {
	if !validTrainingLine(line) || strings.TrimSpace(line) != "수련" {
		return TrainingCommand{}, false
	}
	return TrainingCommand{Alias: "수련"}, true
}

func IsTrainingLine(line string) bool {
	_, ok := ParseTrainingLine(line)
	return ok
}

// Source-name aliases keep command adapters from inventing a second parser.
func ParseTrainLine(line string) (TrainingCommand, bool) { return ParseTrainingLine(line) }
func IsTrainLine(line string) bool                       { return IsTrainingLine(line) }

// ExecuteTrainingLine applies the source reducer through the authenticated
// session and durable receipt boundary. A replay returns the stored typed
// response without recomputing level or gold changes.
func (o *Ownership) ExecuteTrainingLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteTrainingLineWithOptions(ctx, store, worldID, commandID, lease, line, TrainingOptions{})
}

func (o *Ownership) ExecuteTrainingLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, _ TrainingOptions) (storage.WorldReceipt, error) {
	command, ok := ParseTrainingLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTrainingLine
	}
	payload, err := json.Marshal(trainingLineRequest{Kind: "training", Line: line, Alias: command.Alias})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanTraining(actorID)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyTraining(proposal)
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

// ExecuteTrainLine is the source-function spelling alias.
func (o *Ownership) ExecuteTrainLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	return o.ExecuteTrainingLine(ctx, store, worldID, commandID, lease, line)
}

func (o *Ownership) ExecuteTrainLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options TrainOptions) (storage.WorldReceipt, error) {
	return o.ExecuteTrainingLineWithOptions(ctx, store, worldID, commandID, lease, line, options)
}
