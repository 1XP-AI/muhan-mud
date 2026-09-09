package transport

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// shopPurchaseRequest is the durable command identity. Keep the field order
// and names stable: storage hashes the exact JSON bytes when it binds a
// command ID to an actor and a canonical stock identity.
type shopPurchaseRequest struct {
	ActorID string `json:"actor_id"`
	StockID string `json:"stock_id"`
}

// RunShopPurchase executes one caller-bound shop purchase through the durable
// world command receipt. The command ID and the exact actor/stock request must
// be reused after an uncertain result. Receipt lookup happens before the
// reducer, so a replay never calls the allocator or BuyShopItem again.
func (g *WorldConnector) RunShopPurchase(ctx context.Context, commandID, actorID, stockID string) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil shop purchase connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil shop purchase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing shop purchase command ID")
	}
	request, err := json.Marshal(shopPurchaseRequest{ActorID: actorID, StockID: stockID})
	if err != nil {
		return storage.WorldReceipt{}, err
	}

	g.commandMu.Lock()
	defer g.commandMu.Unlock()
	return engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.BuyShopItem(actorID, stockID, g.config.Allocate)
		if err != nil {
			return nil, nil, err
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return saved, response, nil
	})
}

var _ engine.CommandStore = (*storage.Postgres)(nil)
