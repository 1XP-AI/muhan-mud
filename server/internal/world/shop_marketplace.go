package world

import (
	"fmt"
	"strings"
)

// RoomPawnFlag is RPAWNS from src/mtype.h.  A pawn shop uses the same
// sequential storage-room convention documented by docs/rom_stor: the room
// immediately after the pawn shop must be a canonical, non-teleportable
// storage room.
const RoomPawnFlag = 2

const (
	shopSaleMissileType          = 4  // MISSILE is a proficiency, and the object types 0..4 use shots quality.
	shopSaleArmorType            = 5  // ARMOR
	shopSalePotionType           = 6  // POTION
	shopSaleScrollType           = 7  // SCROLL
	shopSaleWandType             = 8  // WAND
	shopSaleKeyType              = 11 // KEY
	shopSaleMaxPayout            = int64(100000)
	objectRoomPermanentFlag      = 0 // OPERMT
	objectTemporaryFlag          = 8 // OTEMPP
	objectInventoryPermanentFlag = 9 // OPERM2
)

// ShopListing is the deterministic, canonical projection used by the list
// receipt. IDs stay in the receipt for audit/replay but are not rendered to a
// player.
type ShopListing struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	Price    int64  `json:"price"`
	Weight   int    `json:"weight"`
}

// ShopListResult is read-only. Response is intentionally plain text rather
// than ANSI/obj_str output: the source formatting depends on an unported
// descriptor/title layer, while item order and exact value are deterministic.
type ShopListResult struct {
	Action        string        `json:"action"`
	ShopRoomID    int16         `json:"shop_room_id"`
	StorageRoomID int16         `json:"storage_room_id"`
	Items         []ShopListing `json:"items"`
	Response      string        `json:"response"`
}

// ShopSaleQuote is the validated observation immediately before a sale. It
// has no allocator, clock, RNG, or mutation. SellShopItem re-validates the
// same source boundaries before returning a candidate state.
type ShopSaleQuote struct {
	ShopRoomID            int16  `json:"shop_room_id"`
	StorageRoomID         int16  `json:"storage_room_id"`
	ItemID                string `json:"item_id"`
	ItemName              string `json:"item_name"`
	Value                 int64  `json:"value"`
	Payout                int64  `json:"payout"`
	ItemWeight            int    `json:"item_weight"`
	CarriedWeightBefore   int    `json:"carried_weight_before"`
	CarriedWeightAfter    int    `json:"carried_weight_after"`
	TemporaryBefore       bool   `json:"temporary_before"`
	StoragePermanentAfter bool   `json:"storage_permanent_after"`
	VisibleToSeller       bool   `json:"visible_to_seller"`
}

// ShopSaleResult is the durable response payload. The item retains its
// canonical ID while changing owner from the player's inventory to the pawn
// storage room. The storage transition sets OPERMT and clears player/temporary
// permanence bits so a transient player object cannot become a transient shop
// stock row.
type ShopSaleResult struct {
	Action                string `json:"action"`
	ShopRoomID            int16  `json:"shop_room_id"`
	StorageRoomID         int16  `json:"storage_room_id"`
	ItemID                string `json:"item_id"`
	ItemName              string `json:"item_name"`
	Value                 int64  `json:"value"`
	Payout                int64  `json:"payout"`
	GoldBefore            int64  `json:"gold_before"`
	GoldAfter             int64  `json:"gold_after"`
	ItemWeight            int    `json:"item_weight"`
	CarriedWeightBefore   int    `json:"carried_weight_before"`
	CarriedWeightAfter    int    `json:"carried_weight_after"`
	TemporaryBefore       bool   `json:"temporary_before"`
	StoragePermanentAfter bool   `json:"storage_permanent_after"`
	VisibleToSeller       bool   `json:"visible_to_seller"`
	Response              string `json:"response"`
}

// pawnShopStorage is deliberately separate from shopStorage in
// shop_transaction.go: purchase and sale have different room flags and must
// not silently widen one another's admission boundary.
func (s State) pawnShopStorage(actorID string) (PlayerState, RoomState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("online pawn-shop actor absent")
	}
	if actor.Items == nil {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("online player with migrated items required")
	}
	shop, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("pawn shop room absent")
	}
	if !flag(shop.Resource.Flags[:], RoomPawnFlag) {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("여기는 전당포가 아닙니다")
	}
	if actor.Body.RoomID == 32767 {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("pawn storage room identity overflow")
	}
	storageID := actor.Body.RoomID + 1
	storage, ok := s.Rooms[storageID]
	if !ok || storage.Items == nil || !flag(storage.Resource.Flags[:], roomNoTeleportFlag) {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("전당포 저장고가 없습니다")
	}
	return actor, shop, storage, nil
}

// ListShopItems reads the source-defined RSHOPP/RNOTEL storage in canonical
// inventory order. C's list handler does not apply the normal room-object
// visibility filter, so this bounded projection lists every valid storage
// root. It rejects malformed/negative stock instead of exposing guessed
// prices or falling back to legacy Resource.Objects.
func (s State) ListShopItems(actorID string) (ShopListResult, error) {
	_, shop, storage, err := s.shopStorage(actorID)
	if err != nil {
		return ShopListResult{}, err
	}
	if storage.Items == nil {
		return ShopListResult{}, fmt.Errorf("canonical shop storage required")
	}
	items := make([]ShopListing, 0, len(storage.Items.Inventory))
	for _, id := range storage.Items.Inventory {
		item, ok := storage.Items.Items[id]
		if !ok || id == "" || item.Object.Name == "" {
			return ShopListResult{}, fmt.Errorf("invalid shop stock root")
		}
		if item.Object.Value < 0 {
			return ShopListResult{}, fmt.Errorf("invalid shop stock value")
		}
		weight, err := storage.Items.objectWeight(id)
		if err != nil || weight < 0 {
			if err != nil {
				return ShopListResult{}, err
			}
			return ShopListResult{}, fmt.Errorf("invalid shop stock weight")
		}
		items = append(items, ShopListing{ItemID: id, ItemName: item.Object.Name, Price: int64(item.Object.Value), Weight: weight})
	}
	return ShopListResult{
		Action:        "list-shop-items",
		ShopRoomID:    shop.Resource.ID,
		StorageRoomID: storage.Resource.ID,
		Items:         items,
		Response:      renderShopListing(items),
	}, nil
}

func renderShopListing(items []ShopListing) string {
	if len(items) == 0 {
		return "상품들:\r\n  없음.\r\n"
	}
	var out strings.Builder
	out.WriteString("상품들:")
	for _, item := range items {
		fmt.Fprintf(&out, "\r\n   %-30s   가격: %d", item.ItemName, item.Price)
	}
	out.WriteString("\r\n")
	return out.String()
}

// QuoteShopSale validates the canonical direct inventory root and exact C
// pawn-shop payout without changing the snapshot. C's find_obj visibility
// gate is the only visibility admitted here: OINVIS requires PDINVI, while
// OHIDDN/prefix/key matching remain outside this bounded slice.
func (s State) QuoteShopSale(actorID, itemID string) (ShopSaleQuote, error) {
	actor, shop, storage, err := s.pawnShopStorage(actorID)
	if err != nil {
		return ShopSaleQuote{}, err
	}
	if itemID == "" {
		return ShopSaleQuote{}, fmt.Errorf("sale item identity required")
	}
	if !containsString(actor.Items.Inventory, itemID) {
		return ShopSaleQuote{}, fmt.Errorf("당신은 그런 물건을 갖고 있지 않습니다")
	}
	item, ok := actor.Items.Items[itemID]
	if !ok || item.Object.Name == "" {
		return ShopSaleQuote{}, fmt.Errorf("sale item absent")
	}
	visible := !flag(item.Object.Flags[:], objectInvisibleFlag) || flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	if !visible {
		return ShopSaleQuote{}, fmt.Errorf("당신은 그런 물건을 갖고 있지 않습니다")
	}
	if len(item.Contents) != 0 {
		// command7.c explicitly refuses objects with first_obj; accepting a
		// nested canonical subtree would silently change the source contract.
		return ShopSaleQuote{}, fmt.Errorf("전당포주인이 안에 물건이 있는 물건은 사지 않습니다")
	}
	if item.Object.Value < 0 {
		return ShopSaleQuote{}, fmt.Errorf("invalid sale value")
	}
	value := int64(item.Object.Value)
	payout := value / 2
	if payout > shopSaleMaxPayout {
		payout = shopSaleMaxPayout
	}
	if payout < 20 {
		return ShopSaleQuote{}, fmt.Errorf("전당포주인이 쓰레기를 사지 않습니다")
	}
	if shopSalePoorQuality(item.Object) {
		return ShopSaleQuote{}, fmt.Errorf("전당포주인이 품질이 낮은 물건을 사지 않습니다")
	}
	if item.Object.Type == shopSalePotionType || item.Object.Type == shopSaleScrollType {
		return ShopSaleQuote{}, fmt.Errorf("전당포주인이 두루마기나 독약을 사지 않습니다")
	}
	if flag(item.Object.Flags[:], objectOneWevFlag) {
		return ShopSaleQuote{}, fmt.Errorf("전당포주인이 이벤트 물건을 사지 않습니다")
	}
	itemWeight, err := actor.Items.objectWeight(itemID)
	if err != nil {
		return ShopSaleQuote{}, err
	}
	if itemWeight < 0 {
		return ShopSaleQuote{}, fmt.Errorf("invalid sale item weight")
	}
	carriedBefore, err := actor.Items.Weight()
	if err != nil {
		return ShopSaleQuote{}, err
	}
	if carriedBefore < 0 {
		return ShopSaleQuote{}, fmt.Errorf("invalid carried weight")
	}
	if int64(actor.Body.Gold) < 0 || int64(actor.Body.Gold) > int64(^uint32(0)>>1) {
		return ShopSaleQuote{}, fmt.Errorf("invalid player gold")
	}
	if int64(actor.Body.Gold) > int64(^uint32(0)>>1)-payout {
		return ShopSaleQuote{}, fmt.Errorf("player gold overflow")
	}
	// The destination itself is validated by State.Validate and again by the
	// transfer candidate. Keep the check here so an invalid storage cannot
	// reach any money calculation or item mutation.
	if _, ok := storage.Items.Items[itemID]; ok {
		return ShopSaleQuote{}, fmt.Errorf("item already owned by pawn storage")
	}
	plan, err := TransferItemRoots(*actor.Items, *storage.Items, []string{itemID})
	if err != nil {
		return ShopSaleQuote{}, err
	}
	carriedAfter, err := plan.Source.Weight()
	if err != nil || carriedAfter < 0 {
		if err != nil {
			return ShopSaleQuote{}, err
		}
		return ShopSaleQuote{}, fmt.Errorf("invalid carried weight after sale")
	}
	return ShopSaleQuote{
		ShopRoomID:            shop.Resource.ID,
		StorageRoomID:         storage.Resource.ID,
		ItemID:                itemID,
		ItemName:              item.Object.Name,
		Value:                 value,
		Payout:                payout,
		ItemWeight:            itemWeight,
		CarriedWeightBefore:   carriedBefore,
		CarriedWeightAfter:    carriedAfter,
		TemporaryBefore:       flag(item.Object.Flags[:], objectTemporaryFlag),
		StoragePermanentAfter: true,
		VisibleToSeller:       true,
	}, nil
}

// QuoteShopSaleByName keeps the client-facing command boundary deterministic
// without exposing canonical IDs. It accepts exact case-insensitive direct
// inventory names and an explicit one-based occurrence; legacy prefix/key
// matching is intentionally fail-closed until it has a source fixture.
func (s State) QuoteShopSaleByName(actorID, name string, occurrence int) (ShopSaleQuote, error) {
	actor, _, _, err := s.pawnShopStorage(actorID)
	if err != nil {
		return ShopSaleQuote{}, err
	}
	detect := flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	itemID, err := selectInventoryRoot(*actor.Items, name, occurrence, func(object LegacyObject) bool {
		return detect || !flag(object.Flags[:], objectInvisibleFlag)
	})
	if err != nil {
		return ShopSaleQuote{}, err
	}
	return s.QuoteShopSale(actorID, itemID)
}

func shopSalePoorQuality(object LegacyObject) bool {
	if (object.Type <= shopSaleMissileType || object.Type == shopSaleArmorType) && object.ShotsCurrent <= object.ShotsMax/8 {
		return true
	}
	return (object.Type == shopSaleWandType || object.Type == shopSaleKeyType) && object.ShotsCurrent < 1
}

// SellShopItem moves one direct player inventory root to pawn storage and
// credits the exact quote payout in the same candidate. No random double-pay
// branch is admitted: command7.c's time/RNG branch is not durable and is
// explicitly left for a future source fixture.
func (s State) SellShopItem(actorID, itemID string) (State, ShopSaleResult, error) {
	quote, err := s.QuoteShopSale(actorID, itemID)
	if err != nil {
		return State{}, ShopSaleResult{}, err
	}
	actor := s.Players[actorID]
	storage := s.Rooms[quote.StorageRoomID]
	if storage.Items == nil {
		return State{}, ShopSaleResult{}, fmt.Errorf("canonical pawn storage required")
	}
	plan, err := TransferItemRoots(*actor.Items, *storage.Items, []string{itemID})
	if err != nil {
		return State{}, ShopSaleResult{}, err
	}
	// A room-owned object must not retain a player/temporary permanence bit.
	// Mark it room-permanent so the canonical storage survives room refresh and
	// clear both inventory-permanent forms exactly like the C ownership switch.
	sold, ok := plan.Destination.Items[itemID]
	if !ok {
		return State{}, ShopSaleResult{}, fmt.Errorf("sold item absent from pawn storage")
	}
	sold.Object.Flags[objectRoomPermanentFlag/8] |= 1 << (objectRoomPermanentFlag % 8)
	sold.Object.Flags[objectTemporaryFlag/8] &^= 1 << (objectTemporaryFlag % 8)
	sold.Object.Flags[objectInventoryPermanentFlag/8] &^= 1 << (objectInventoryPermanentFlag % 8)
	plan.Destination.Items[itemID] = sold
	if err := plan.Source.Validate(); err != nil {
		return State{}, ShopSaleResult{}, err
	}
	if err := plan.Destination.Validate(); err != nil {
		return State{}, ShopSaleResult{}, err
	}
	carriedAfter, err := plan.Source.Weight()
	if err != nil || carriedAfter < 0 {
		if err != nil {
			return State{}, ShopSaleResult{}, err
		}
		return State{}, ShopSaleResult{}, fmt.Errorf("invalid carried weight after sale")
	}
	goldBefore := int64(actor.Body.Gold)
	goldAfter := goldBefore + quote.Payout
	if goldAfter < goldBefore || goldAfter > int64(^uint32(0)>>1) {
		return State{}, ShopSaleResult{}, fmt.Errorf("player gold overflow")
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	nextPlayer.Items = &plan.Source
	nextPlayer.Body.Gold = int32(goldAfter)
	nextPlayer.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = nextPlayer
	nextStorage := next.Rooms[quote.StorageRoomID]
	nextStorage.Items = &plan.Destination
	next.Rooms[quote.StorageRoomID] = nextStorage
	if err := next.Validate(); err != nil {
		return State{}, ShopSaleResult{}, err
	}
	return next, ShopSaleResult{
		Action:                "sell-shop-item",
		ShopRoomID:            quote.ShopRoomID,
		StorageRoomID:         quote.StorageRoomID,
		ItemID:                quote.ItemID,
		ItemName:              quote.ItemName,
		Value:                 quote.Value,
		Payout:                quote.Payout,
		GoldBefore:            goldBefore,
		GoldAfter:             goldAfter,
		ItemWeight:            quote.ItemWeight,
		CarriedWeightBefore:   quote.CarriedWeightBefore,
		CarriedWeightAfter:    carriedAfter,
		TemporaryBefore:       quote.TemporaryBefore,
		StoragePermanentAfter: flag(sold.Object.Flags[:], objectRoomPermanentFlag) && !flag(sold.Object.Flags[:], objectTemporaryFlag) && !flag(sold.Object.Flags[:], objectInventoryPermanentFlag),
		VisibleToSeller:       quote.VisibleToSeller,
		Response:              fmt.Sprintf("당신은 %s을(를) 팔고 %d냥을 받았습니다.\r\n", quote.ItemName, quote.Payout),
	}, nil
}

// SellShopItemByName is the parser-facing form of SellShopItem.
func (s State) SellShopItemByName(actorID, name string, occurrence int) (State, ShopSaleResult, error) {
	quote, err := s.QuoteShopSaleByName(actorID, name, occurrence)
	if err != nil {
		return State{}, ShopSaleResult{}, err
	}
	return s.SellShopItem(actorID, quote.ItemID)
}
