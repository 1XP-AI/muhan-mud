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

var ErrUnsupportedPlayerLookupLine = errors.New("line is not an implemented player lookup command")

const (
	PlayerSearchUnavailableResponse = "현재 이용중이 아닙니다.\r\n"
	PlayerInfoUnavailableResponse   = "사용자정보는 현재 접속 중인 사용자만 조회할 수 있습니다.\r\n"
	PlayerInfoForbiddenResponse     = "당신은 그 사용자의 정보를 볼 수 없습니다.\r\n"
)

type PlayerLookupKind uint8

const (
	PlayerLookupSearch PlayerLookupKind = iota + 1
	PlayerLookupInfo
)

// PlayerLookupCommand keeps the client-supplied display name separate from
// the canonical player ID. The world reducer resolves the name again from the
// committed snapshot.
type PlayerLookupCommand struct {
	Kind   PlayerLookupKind
	Target string
}

type playerLookupLineRequest struct {
	Kind   string `json:"kind"`
	Line   string `json:"line"`
	Target string `json:"target"`
}

// ParsePlayerLookupLine accepts only the bounded Korean command names and one
// exact-name token. Prefixes, occurrences, offline-file selectors, and extra
// tokens remain outside this safe slice.
func ParsePlayerLookupLine(line string) (PlayerLookupCommand, bool) {
	if !utf8.ValidString(line) {
		return PlayerLookupCommand{}, false
	}
	for _, r := range line {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return PlayerLookupCommand{}, false
		}
	}
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil || len(tokens) != 2 || tokens[1] == "" {
		return PlayerLookupCommand{}, false
	}
	switch tokens[0] {
	case "사용자검색":
		return PlayerLookupCommand{Kind: PlayerLookupSearch, Target: tokens[1]}, true
	case "사용자정보":
		return PlayerLookupCommand{Kind: PlayerLookupInfo, Target: tokens[1]}, true
	default:
		return PlayerLookupCommand{}, false
	}
}

func IsPlayerLookupLine(line string) bool {
	_, ok := ParsePlayerLookupLine(line)
	return ok
}

// ExecutePlayerLookupLine routes both bounded read-only projections through
// ExecuteGame. Gate failures are recorded as unchanged-state receipts with a
// deterministic generic response; canonical projection failures still abort
// before commit rather than guessing from legacy metadata.
func (o *Ownership) ExecutePlayerLookupLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParsePlayerLookupLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedPlayerLookupLine
	}
	kind := "player-search"
	if command.Kind == PlayerLookupInfo {
		kind = "player-info"
	}
	request, err := json.Marshal(playerLookupLineRequest{Kind: kind, Line: line, Target: command.Target})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, request, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var text string
		switch command.Kind {
		case PlayerLookupSearch:
			text, err = state.PlayerSearch(actorID, command.Target)
			if errors.Is(err, world.ErrPlayerLookupUnavailable) {
				text, err = PlayerSearchUnavailableResponse, nil
			}
		case PlayerLookupInfo:
			text, err = state.PlayerInformation(actorID, command.Target)
			switch {
			case errors.Is(err, world.ErrPlayerInfoCanonicalOnly):
				text, err = PlayerInfoUnavailableResponse, nil
			case errors.Is(err, world.ErrPlayerInfoForbidden):
				text, err = PlayerInfoForbiddenResponse, nil
			}
		default:
			return nil, nil, ErrUnsupportedPlayerLookupLine
		}
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(text)
		return raw, response, err
	})
}

func (o *Ownership) ExecutePlayerSearchLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParsePlayerLookupLine(line)
	if !ok || command.Kind != PlayerLookupSearch {
		return storage.WorldReceipt{}, ErrUnsupportedPlayerLookupLine
	}
	return o.ExecutePlayerLookupLine(ctx, store, worldID, commandID, lease, line)
}

func (o *Ownership) ExecutePlayerInfoLookupLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	command, ok := ParsePlayerLookupLine(line)
	if !ok || command.Kind != PlayerLookupInfo {
		return storage.WorldReceipt{}, ErrUnsupportedPlayerLookupLine
	}
	return o.ExecutePlayerLookupLine(ctx, store, worldID, commandID, lease, line)
}
