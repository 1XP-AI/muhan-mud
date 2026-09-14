package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// ErrUnsupportedShopLine is returned before a receipt for aliases/argument
// forms outside the bounded C command contract. In particular, English list /
// sell aliases, prefix/key matching, and merchant-NPC purchase are not
// guessed from the source.
var ErrUnsupportedShopLine = errors.New("line is not an implemented shop marketplace command")

// ErrUnsupportedShopMarketplaceLine is a descriptive alias for callers that
// want to distinguish this slice from the existing purchase handler.
var ErrUnsupportedShopMarketplaceLine = ErrUnsupportedShopLine

type shopMarketplaceAction struct {
	kind       string
	name       string
	occurrence int
}

// parseShopMarketplaceLine admits only the exact global.c aliases: 품목 for
// list and 팔아 / 팔아 <exact-name> [occurrence] for sell. Bare 팔아 is C's
// cmnd->num < 2 ask-what print. The occurrence is explicit and one-based;
// implicit legacy prefix/key selection remains fail-closed.
func parseShopMarketplaceLine(line string) (shopMarketplaceAction, bool) {
	tokens, err := tokenizeLegacy(strings.TrimSpace(line))
	if err != nil {
		return shopMarketplaceAction{}, false
	}
	if len(tokens) == 1 && tokens[0] == "품목" {
		return shopMarketplaceAction{kind: "list", occurrence: 1}, true
	}
	if len(tokens) == 1 && tokens[0] == "팔아" {
		return shopMarketplaceAction{kind: "sell", occurrence: 1}, true
	}
	if len(tokens) < 2 || len(tokens) > 3 || tokens[0] != "팔아" || tokens[1] == "" {
		return shopMarketplaceAction{}, false
	}
	action := shopMarketplaceAction{kind: "sell", name: tokens[1], occurrence: 1}
	if len(tokens) == 3 {
		occurrence, err := strconv.Atoi(tokens[2])
		if err != nil || occurrence < 1 {
			return shopMarketplaceAction{}, false
		}
		action.occurrence = occurrence
	}
	return action, true
}

type shopMarketplaceRequest struct {
	Line       string `json:"line"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Occurrence int    `json:"occurrence"`
}

// ExecuteShopLine binds list and sell to the same actor-checked receipt
// boundary. list returns the original state bytes, including C's non-shop
// and missing-storage prints as successful responses. sell C-prints for
// non-RPAWNS and bare 팔아 keep the original state. A named leftover
// (inventory miss) commits F_CLR PHIDDN with no gold/item move, matching
// command7.c:244 before find_obj. A completed sale stores the
// player/storage/gold/PHIDDN candidate atomically. A replay never re-runs
// selection, hide-clear, or sale validation after a receipt exists.
// Unmigrated storage/nil Items fail closed before commit.
func (o *Ownership) ExecuteShopLine(ctx context.Context, store engine.CommandStore, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
	action, ok := parseShopMarketplaceLine(line)
	if !ok {
		return storage.WorldReceipt{}, ErrUnsupportedShopLine
	}
	payload, err := json.Marshal(shopMarketplaceRequest{Line: line, Kind: action.kind, Name: action.name, Occurrence: action.occurrence})
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	return o.ExecuteGame(ctx, store, worldID, commandID, lease, payload, func(raw json.RawMessage, actorID string) (json.RawMessage, json.RawMessage, error) {
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var state = raw
		var response any
		switch action.kind {
		case "list":
			result, listErr := s.ListShopItems(actorID)
			if listErr != nil {
				return nil, nil, listErr
			}
			response = result
		case "sell":
			next, result, sellErr := s.SellShopItemByName(actorID, action.name, action.occurrence)
			if sellErr != nil {
				return nil, nil, sellErr
			}
			response = result
			if result.Action == world.ShopSaleSoldAction || result.Action == world.ShopSaleNotHoldingAction {
				state, err = json.Marshal(next)
				if err != nil {
					return nil, nil, err
				}
			}
		default:
			return nil, nil, fmt.Errorf("unknown shop marketplace action")
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return nil, nil, err
		}
		return state, encoded, nil
	})
}
