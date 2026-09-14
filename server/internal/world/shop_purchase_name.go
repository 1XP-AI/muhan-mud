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

// BuyShopItemByName is the terminal 사/구입 reducer. C order is preserved:
// non-RSHOPP, then missing name, then load_rom(rom_num+1), then find_obj on
// storage first_obj. Those four prints are receipts. Gold/weight/success still
// go through BuyShopItem. Prefix/key and merchant-NPC purchase stay closed.
func (s State) BuyShopItemByName(actorID, name string, occurrence int, allocate ShopPurchaseItemIDAllocator) (State, ShopPurchaseResult, error) {
	if occurrence < 1 {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("invalid shop stock occurrence")
	}
	_, shop, printed, handled, err := s.shopPurchaseShop(actorID)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	if handled {
		return s.clone(), printed, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return s.clone(), ShopPurchaseResult{
			Action:     ShopPurchaseAskWhatAction,
			ShopRoomID: shop.Resource.ID,
			Response:   ShopPurchaseAskWhatResponse,
		}, nil
	}
	storage, missing, handled, err := s.shopPurchaseStorage(s.Players[actorID], shop)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	if handled {
		return s.clone(), missing, nil
	}
	found := 0
	for _, id := range storage.Items.Inventory {
		item, ok := storage.Items.Items[id]
		if !ok || id == "" || item.Object.Name == "" {
			return State{}, ShopPurchaseResult{}, fmt.Errorf("invalid shop stock root")
		}
		if !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		found++
		if found == occurrence {
			return s.BuyShopItem(actorID, id, allocate)
		}
	}
	return s.clone(), ShopPurchaseResult{
		Action:        ShopPurchaseNotSoldAction,
		ShopRoomID:    shop.Resource.ID,
		StorageRoomID: storage.Resource.ID,
		Response:      ShopPurchaseNotSoldResponse,
	}, nil
}
