package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedInfoLine keeps command4.c's argument forms out of this first
// page reducer. The connection-local [엔터] -> info_2 continuation is rendered
// from a fresh canonical snapshot by transport and never creates a receipt.
var ErrUnsupportedInfoLine = errors.New("line is not an implemented info command")

// InfoContinuationPrompt and InfoContinuationCancelResponse are copied from
// command4.c's info()/info_2() boundary. The prompt is part of the durable
// first-page response; cancellation is handled by the connection-local
// continuation gate and does not mutate world state.
const (
	InfoContinuationPrompt         = "[엔터]를 누르세요. 그만보시려면 [.]을 치세요: "
	InfoContinuationCancelResponse = "중단되었습니다.\n"
)

// ExecuteInfoLine renders the deterministic first page of command4.c info.
// It is a no-state-change receipt: PlayerInfo validates and reads the
// canonical snapshot, while ExecuteGame persists the response for exact retry.
func (o *Ownership) ExecuteInfoLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	if strings.TrimSpace(line) != "정보" {
		return storage.WorldReceipt{}, ErrUnsupportedInfoLine
	}
	payload, err := json.Marshal(struct{ Line string }{Line: "정보"})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		text, err := state.PlayerInfo(actorID)
		if err != nil {
			return nil, nil, err
		}
		text += InfoContinuationPrompt
		response, err := json.Marshal(text)
		return raw, response, err
	})
}
