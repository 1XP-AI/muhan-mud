package world

import (
	"fmt"
	"strings"
)

// QuoteShopPurchaseByName resolves the client-facing, exact stock-name form
// to the canonical stock identity owned by the shop storage. Occurrence is
// one-based and must be positive. This helper intentionally has no prefix/key
// matching and does not inspect merchant NPC inventory; BuyShopItem remains
// the sole mutating purchase contract.
func (s State) QuoteShopPurchaseByName(actorID, name string, occurrence int) (ShopPurchaseQuote, error) {
	if occurrence < 1 {
		return ShopPurchaseQuote{}, fmt.Errorf("invalid shop stock occurrence")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ShopPurchaseQuote{}, fmt.Errorf("shop stock name required")
	}
	_, _, storage, err := s.shopStorage(actorID)
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	found := 0
	for _, id := range storage.Items.Inventory {
		item, ok := storage.Items.Items[id]
		if !ok || id == "" || item.Object.Name == "" {
			return ShopPurchaseQuote{}, fmt.Errorf("invalid shop stock root")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return s.QuoteShopPurchase(actorID, id)
		}
	}
	return ShopPurchaseQuote{}, fmt.Errorf("그런 물건은 팔지 않습니다")
}
