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

var ErrUnsupportedReadScrollLine = errors.New("line is not an implemented read-scroll command")

// ReadScrollCommand keeps the terminal selector separate from the canonical
// item identity. The first bounded adapter admits one exact display-name
// token and the default occurrence only; prefixes/occurrence syntax remain
// explicit follow-up work.
type ReadScrollCommand struct {
	ItemName   string
	Occurrence int
}

type readScrollLineRequest struct {
	Kind       string `json:"kind"`
	Line       string `json:"line"`
	ItemName   string `json:"item_name"`
	Occurrence int    `json:"occurrence"`
	Now        int32  `json:"now"`
}

type ReadScrollOptions = world.ScrollOptions

func validReadScrollLine(line string) bool {
	if !utf8.ValidString(line) {
		return false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// ParseReadScrollLine admits `읽어 <item>` only. `읽어 게시판 <n>` is claimed
// by ParseBoardLine before this helper, preserving the board command route.
func ParseReadScrollLine(line string) (ReadScrollCommand, bool) {
	if !validReadScrollLine(line) {
		return ReadScrollCommand{}, false
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) != 2 || tokens[0] != "읽어" || tokens[1] == "" || strings.TrimSpace(tokens[1]) != tokens[1] {
		return ReadScrollCommand{}, false
	}
	return ReadScrollCommand{ItemName: tokens[1], Occurrence: 1}, true
}

func IsReadScrollLine(line string) bool {
	_, ok := ParseReadScrollLine(line)
	return ok
}

// ExecuteReadScrollLineWithOptions binds readscroll's proposal/apply reducer
// to the durable game receipt. Randomness is evaluated only during the first
// reducer call; engine replay returns the stored ScrollResult unchanged.
func (o *Ownership) ExecuteReadScrollLineWithOptions(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, options ReadScrollOptions) (storage.WorldReceipt, error) {
	command, ok := ParseReadScrollLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedReadScrollLine
	}
	payload, err := json.Marshal(readScrollLineRequest{Kind: "read-scroll", Line: line, ItemName: command.ItemName, Occurrence: command.Occurrence, Now: options.Now})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := state.PlanReadScroll(actorID, command.ItemName, command.Occurrence, options)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.ApplyReadScroll(proposal)
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

func (o *Ownership) ExecuteReadScrollLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string, now int32, roll func(int, int) int) (storage.WorldReceipt, error) {
	return o.ExecuteReadScrollLineWithOptions(ctx, store, worldID, commandID, lease, line, ReadScrollOptions{Now: now, Roll: roll})
}
