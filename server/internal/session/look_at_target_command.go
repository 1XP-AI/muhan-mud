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

// ErrUnsupportedLookAtTargetLine is returned before a receipt exists when a
// line is outside the bounded explicit-target "보아" slice. Bare 보아 (the
// targetless action branch), legacy object fallback and full ANSI inspection
// remain separate follow-up work. A positive one-based occurrence is admitted
// only for the canonical NPC/player prefix form; the world reducer rejects
// unsupported object/exit occurrences without creating a receipt.
var ErrUnsupportedLookAtTargetLine = errors.New("line is not an implemented 보아 target command")

// LookAtTargetCommand is the parsed, bounded form of action.c's explicit
// target command. Target is a display name; the reducer resolves its
// authoritative player/NPC identity from the committed world snapshot.
type LookAtTargetCommand struct {
	Target     string
	Occurrence int
}

// ParseLookAtTargetLine accepts one 보아 target token, optionally followed by
// a positive one-based occurrence. The latter is the shape produced by the
// legacy parser for find_crt's prefix/occurrence lookup. Legacy token quoting
// is retained at this boundary, but a quoted multi-token target is not
// guessed to be a canonical character name.
func ParseLookAtTargetLine(line string) (LookAtTargetCommand, bool) {
	if !utf8.ValidString(line) {
		return LookAtTargetCommand{}, false
	}
	// A transport normally strips the submit delimiter, but this function is
	// also used directly by recovery/tests.  Do not trim a line break into a
	// valid receipt request or allow terminal/control injection in the target.
	for _, r := range line {
		switch r {
		case '\r', '\n', '\x00', '\x1b', '\u2028', '\u2029':
			return LookAtTargetCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) < 2 || len(tokens) > 3 || tokens[0] != "보아" || strings.TrimSpace(tokens[1]) == "" || strings.IndexFunc(tokens[1], unicode.IsSpace) >= 0 {
		return LookAtTargetCommand{}, false
	}
	if len(tokens) == 2 {
		return LookAtTargetCommand{Target: tokens[1]}, true
	}
	occurrence, ok := parseLookAtOccurrence(tokens[2])
	if !ok {
		return LookAtTargetCommand{}, false
	}
	return LookAtTargetCommand{Target: tokens[1], Occurrence: occurrence}, true
}

func parseLookAtOccurrence(token string) (int, bool) {
	if token == "" {
		return 0, false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(token, 10, 31)
	if err != nil || value < 1 {
		return 0, false
	}
	return int(value), true
}

// LookAtTargetLineText mirrors the naming used by other command boundaries;
// it returns a parsed display-name target, never a client-authoritative ID.
func LookAtTargetLineText(line string) (LookAtTargetCommand, bool) {
	return ParseLookAtTargetLine(line)
}

// ExecuteLookAtTargetLine connects the bounded action.c "보아 <target>"
// reducer to the durable receipt boundary. The response is stored in the
// receipt; a transport may derive world.RoomLookAtTargetEvent from the
// committed state only for a first (non-replayed) receipt. Canonical room
// roots and exact visible exits are resolved by the world proposal/apply
// reducer; no client identity is persisted or trusted here.
func (o *Ownership) ExecuteLookAtTargetLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParseLookAtTargetLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedLookAtTargetLine
	}
	payload, err := json.Marshal(struct {
		Line       string
		Target     string
		Occurrence int
	}{Line: line, Target: command.Target, Occurrence: command.Occurrence})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		proposal, err := s.PlanLookAtTargetProposalWithOccurrence(actorID, command.Target, command.Occurrence)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := s.ApplyLookAtTarget(proposal)
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result.Response)
		return state, response, err
	})
}
