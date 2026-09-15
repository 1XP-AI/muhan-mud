package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errShopItemNotFound = errors.New("shop item not found")

// selectShopItemRoot is the canonical equivalent of object.c:find_obj for
// command7.c buy/sell. The selector is matched against one root's display name
// and key[0..2], each as a prefix; a root is counted once even when multiple
// fields match. Inventory order is authoritative, and visibility is evaluated
// before occurrence counting. The caller must re-check the returned identity
// through the quote/mutation reducer before changing state.
func selectShopItemRoot(c ItemCollection, selector string, occurrence int, visible func(LegacyObject) bool) (string, error) {
	if occurrence < 1 {
		return "", fmt.Errorf("invalid shop item occurrence")
	}
	selector = strings.TrimSpace(selector)
	if !validShopSelectorText(selector) {
		return "", fmt.Errorf("shop item selector required")
	}
	if err := validateShopSelectorCollection(c); err != nil {
		return "", err
	}
	found := 0
	for _, id := range c.Inventory {
		item := c.Items[id]
		if visible != nil && !visible(item.Object) {
			continue
		}
		if !shopObjectPrefixMatch(item.Object, selector) {
			continue
		}
		found++
		if found == occurrence {
			return id, nil
		}
	}
	return "", errShopItemNotFound
}

func shopObjectPrefixMatch(object LegacyObject, selector string) bool {
	if equalFoldPrefix(object.Name, selector) {
		return true
	}
	for _, key := range object.Keys {
		if key != "" && equalFoldPrefix(key, selector) {
			return true
		}
	}
	return false
}

func validateShopSelectorCollection(c ItemCollection) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid shop item collection: %w", err)
	}
	seen := make(map[string]bool, len(c.Items))
	var visit func(string) error
	visit = func(id string) error {
		if id == "" {
			return fmt.Errorf("invalid shop item root")
		}
		if seen[id] {
			return nil
		}
		item, ok := c.Items[id]
		if !ok {
			return fmt.Errorf("invalid shop item root")
		}
		if !validShopObjectText(item.Object.Name, false) {
			return fmt.Errorf("invalid shop item name")
		}
		for _, key := range item.Object.Keys {
			if !validShopObjectText(key, true) {
				return fmt.Errorf("invalid shop item key")
			}
		}
		seen[id] = true
		for _, child := range item.Contents {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, id := range c.Inventory {
		if err := visit(id); err != nil {
			return err
		}
	}
	for _, id := range c.Ready {
		if id == "" {
			continue
		}
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func validShopSelectorText(value string) bool {
	return validShopObjectText(value, false) && strings.TrimSpace(value) == value
}

func validShopObjectText(value string, allowEmpty bool) bool {
	if value == "" {
		return allowEmpty
	}
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// QuoteShopPurchaseByName resolves the client-facing stock-selector form
// to the canonical stock identity owned by the shop storage. Occurrence is
// one-based and must be positive. Matching follows object.c:find_obj for the
// display name and all three keys; merchant-NPC inventory remains outside this
// boundary, and BuyShopItem remains the sole mutating purchase contract.
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
	actor := s.Players[actorID]
	itemID, err := selectShopItemRoot(*storage.Items, name, occurrence, func(object LegacyObject) bool {
		return !flag(object.Flags[:], objectInvisibleFlag) || flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	})
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	return s.QuoteShopPurchase(actorID, itemID)
}

// BuyShopItemByName is the terminal 사/구입 reducer. C order is preserved:
// non-RSHOPP, then missing name, then load_rom(rom_num+1), then find_obj on
// storage first_obj. Those four prints are receipts. Gold/weight/success still
// go through BuyShopItem. Merchant-NPC purchase stays closed.
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
	actor := s.Players[actorID]
	itemID, err := selectShopItemRoot(*storage.Items, name, occurrence, func(object LegacyObject) bool {
		return !flag(object.Flags[:], objectInvisibleFlag) || flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	})
	if err != nil {
		if !errors.Is(err, errShopItemNotFound) {
			return State{}, ShopPurchaseResult{}, err
		}
	} else {
		return s.BuyShopItem(actorID, itemID, allocate)
	}
	return s.clone(), ShopPurchaseResult{
		Action:        ShopPurchaseNotSoldAction,
		ShopRoomID:    shop.Resource.ID,
		StorageRoomID: storage.Resource.ID,
		Response:      ShopPurchaseNotSoldResponse,
	}, nil
}
