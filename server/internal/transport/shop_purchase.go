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

// shopPurchaseRequest is the durable command identity for the canonical-ID
// API. Keep the field order and names stable: storage hashes the exact JSON
// bytes when it binds a command ID to an actor and a canonical stock identity.
type shopPurchaseRequest struct {
	ActorID string `json:"actor_id"`
	StockID string `json:"stock_id"`
}

// shopPurchaseNameRequest is the terminal 사/구입 identity. Aliases that parse
// to the same exact name and occurrence replay as one command; the client
// never supplies a stock ID. Bare 사/구입 keep an empty name so C's
// "무엇을 사시려구요?" receipt can bind without inventing stock.
type shopPurchaseNameRequest struct {
	ActorID    string `json:"actor_id"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
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
	request, err := json.Marshal(shopPurchaseNameRequest{ActorID: actorID, Name: command.Name, Occurrence: command.Occurrence})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	receipt, err := engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.BuyShopItemByName(actorID, command.Name, command.Occurrence, g.config.Allocate)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if result.Action != "buy-shop-item" {
			return raw, response, nil
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		return saved, response, nil
	})
	return g.finishShopPurchase(ctx, receipt, err)
}

// runShopPurchaseLocked is the durable implementation for the canonical-ID
// API. Callers must hold commandMu. C return(0) prints commit as receipts
// without cloning stock; gold/weight failures still abort before commit.
func (g *WorldConnector) runShopPurchaseLocked(ctx context.Context, commandID, actorID, stockID string) (storage.WorldReceipt, error) {
	request, err := json.Marshal(shopPurchaseRequest{ActorID: actorID, StockID: stockID})
	if err != nil {
		return storage.WorldReceipt{}, err
	}

	receipt, err := engine.Execute(ctx, g.config.Store, g.config.WorldID, commandID, request, func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.BuyShopItem(actorID, stockID, g.config.Allocate)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		if result.Action != "buy-shop-item" {
			return raw, response, nil
		}
		saved, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		return saved, response, nil
	})
	return g.finishShopPurchase(ctx, receipt, err)
}

func (g *WorldConnector) finishShopPurchase(ctx context.Context, receipt storage.WorldReceipt, err error) (storage.WorldReceipt, error) {
	if err != nil {
		return receipt, err
	}
	if receipt.Replayed {
		return receipt, nil
	}
	var result world.ShopPurchaseResult
	if json.Unmarshal(receipt.Response, &result) != nil || result.Event == nil {
		return receipt, nil
	}
	g.mu.Lock()
	audience := false
	for connection := range g.connections {
		if connection != nil && connection.events != nil && connection.lease.ActorID != result.Event.ExcludeActorID {
			audience = true
			break
		}
	}
	g.mu.Unlock()
	if !audience {
		return receipt, nil
	}
	if after, ok := g.snapshot(ctx); ok {
		g.publishShopPurchase(after, result)
	}
	return receipt, nil
}

func (g *WorldConnector) publishShopPurchase(after world.State, result world.ShopPurchaseResult) {
	if result.Action != "buy-shop-item" || result.Event == nil {
		return
	}
	publishWorldRoomEvent(g, after, result.Event.RoomID, result.Event.ActorID, result.Event.ExcludeActorID, result.Event.Text)
}

var _ engine.CommandStore = (*storage.Postgres)(nil)

func renderShopPurchaseOutput(result world.ShopPurchaseResult) string {
	if result.Response != "" {
		return result.Response
	}
	return fmt.Sprintf("당신은 %s을(를) 샀습니다.\r\n", result.ItemName)
}
