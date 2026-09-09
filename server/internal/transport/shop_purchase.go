package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
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
	g.commandMu.Lock()
	defer g.commandMu.Unlock()
	return g.runShopPurchaseLocked(ctx, commandID, actorID, stockID)
}

// RunShopPurchaseByName resolves the bounded terminal name/occurrence form
// against the authoritative shop snapshot and then reuses the canonical stock
// ID RunShopPurchase reducer/receipt contract. The client never supplies a
// stock ID. This method is also used by the live connector while it already
// owns commandMu through runShopPurchaseByNameLocked.
func (g *WorldConnector) RunShopPurchaseByName(ctx context.Context, commandID, actorID, line string) (storage.WorldReceipt, error) {
	if g == nil {
		return storage.WorldReceipt{}, errors.New("nil shop purchase connector")
	}
	if ctx == nil {
		return storage.WorldReceipt{}, errors.New("nil shop purchase context")
	}
	if commandID == "" {
		return storage.WorldReceipt{}, errors.New("missing shop purchase command ID")
	}
	g.commandMu.Lock()
	defer g.commandMu.Unlock()
	return g.runShopPurchaseByNameLocked(ctx, commandID, actorID, line)
}

func (g *WorldConnector) runShopPurchaseByNameLocked(ctx context.Context, commandID, actorID, line string) (storage.WorldReceipt, error) {
	command, ok := session.ParseShopPurchaseLine(line)
	if !ok {
		return storage.WorldReceipt{}, session.ErrUnsupportedShopPurchaseLine
	}
	snapshot, err := g.config.Store.LoadWorld(ctx, g.config.WorldID)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	state, err := world.DecodeState(snapshot.State)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	quote, err := state.QuoteShopPurchaseByName(actorID, command.Name, command.Occurrence)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	// The resolved ID is canonical server state. It is never parsed from the
	// terminal line or accepted as client authority.
	return g.runShopPurchaseLocked(ctx, commandID, actorID, quote.StockID)
}

// runShopPurchaseLocked is the shared durable implementation for the
// canonical-ID API and the name/occurrence terminal adapter. Callers must hold
// commandMu; the live connector does so across name resolution and receipt
// execution to prevent a local state change between the two steps.
func (g *WorldConnector) runShopPurchaseLocked(ctx context.Context, commandID, actorID, stockID string) (storage.WorldReceipt, error) {
	request, err := json.Marshal(shopPurchaseRequest{ActorID: actorID, StockID: stockID})
	if err != nil {
		return storage.WorldReceipt{}, err
	}

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

func renderShopPurchaseOutput(result world.ShopPurchaseResult) string {
	return fmt.Sprintf("당신은 %s을(를) 샀습니다.\r\n", result.ItemName)
}
