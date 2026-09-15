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

var ErrUnsupportedTrackLine = errors.New("line is not an implemented track command")

type TrackCommand struct{}

type TrackOptions struct {
	Now  int32
	Roll func(int, int) int
}

type trackLineRequest struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
	Now  int32  `json:"now"`
}

func ParseTrackLine(line string) (TrackCommand, bool) {
	if !utf8.ValidString(line) {
		return TrackCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return TrackCommand{}, false
		}
	}
	if strings.TrimSpace(line) != "추적" {
		return TrackCommand{}, false
	}
	return TrackCommand{}, true
}

func IsTrackLine(line string) bool {
	_, ok := ParseTrackLine(line)
	return ok
}

func (o *Ownership) ExecuteTrackLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteTrackLineWithOptions(ctx, store, worldID, commandID, lease, line, TrackOptions{Now: now, Roll: roll})
}

func (o *Ownership) ExecuteTrackLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options TrackOptions) (storage.WorldReceipt, error) {
	if _, ok := ParseTrackLine(line); !ok {
		return storage.WorldReceipt{}, ErrUnsupportedTrackLine
	}
	payload, err := json.Marshal(trackLineRequest{Kind: "track", Line: line, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanTrack(actorID, options.Now, options.Roll)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyTrack(proposal)
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
