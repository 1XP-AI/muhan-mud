package world

import "fmt"

// RoomShopFlag and roomNoTeleportFlag are the zero-based values from
// src/mtype.h.  docs/rom_stor makes the room immediately after a shop the
// storage room and requires that room to be non-teleportable.
const (
	RoomShopFlag       = 0  // RSHOPP
	roomNoTeleportFlag = 12 // RNOTEL
	shopInventoryLimit = 200
	shopItemLimit      = 100000
)

// ShopPurchaseQuote is the source-backed part of a shop purchase.  Stock is
// identified by canonical item ID rather than display name, because names can
// repeat and the durable graph owns identity.  Price is the stock object's
// value, exactly as command7.c:buy uses it.
//
// The quote is an observation only.  BuyShopItem resolves it again against the
// same snapshot immediately before producing a candidate state; a durable
// command executor must persist this quote/result together with its request
// receipt for retry/replay semantics.
type ShopPurchaseQuote struct {
	ShopRoomID      int16
	StorageRoomID   int16
	StockID         string
	ItemName        string
	Price           int64
	RootWeight      int
	CarriedWeight   int
	CarriedRoots    int
	PurchasedWeight int
}

// ShopPurchaseResult is the deterministic receipt payload for the pure
// candidate.  PurchasedIDs are in preorder (root first, then each child in
// canonical Contents order), making the allocator contract and replay audit
// explicit without exposing storage IDs as owned by the buyer.
type ShopPurchaseResult struct {
	Action          string
	ShopRoomID      int16
	StorageRoomID   int16
	StockID         string
	ItemName        string
	Price           int64
	GoldBefore      int64
	GoldAfter       int64
	PurchasedRootID string
	PurchasedIDs    []string
}

// ShopPurchaseItemIDAllocator is called once per node in the copied item
// subtree.  It must return a fresh non-empty canonical item identity.  The
// transaction validates every returned ID before exposing a candidate state.
type ShopPurchaseItemIDAllocator func() (string, error)

// shopStorage resolves the source-defined room relationship.  C's buy() only
// operates from an RSHOPP room and loads rom_num+1; docs/rom_stor additionally
// requires the storage room's RNOTEL flag.  Requiring canonical Items here
// prevents silently mixing legacy nested values with ID-owned graph state.
func (s State) shopStorage(actorID string) (PlayerState, RoomState, RoomState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, RoomState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("online shop actor absent")
	}
	if actor.Items == nil {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("online player with migrated items required")
	}
	shop, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("shop room absent")
	}
	if !flag(shop.Resource.Flags[:], RoomShopFlag) {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("여기는 상점이 아닙니다")
	}
	// Adding one to an int16 room identity must not wrap to an unrelated room.
	if actor.Body.RoomID == 32767 {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("shop storage room identity overflow")
	}
	storageID := actor.Body.RoomID + 1
	storage, ok := s.Rooms[storageID]
	if !ok || storage.Items == nil || !flag(storage.Resource.Flags[:], roomNoTeleportFlag) {
		return PlayerState{}, RoomState{}, RoomState{}, fmt.Errorf("상점 저장고가 없습니다")
	}
	return actor, shop, storage, nil
}

// QuoteShopPurchase validates the source-backed room, stock identity, price,
// and buyer capacity without allocating an identity or mutating state.
func (s State) QuoteShopPurchase(actorID, stockID string) (ShopPurchaseQuote, error) {
	actor, shop, storage, err := s.shopStorage(actorID)
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	if stockID == "" {
		return ShopPurchaseQuote{}, fmt.Errorf("shop stock identity required")
	}
	stock, ok := storage.Items.Items[stockID]
	if !ok || !containsString(storage.Items.Inventory, stockID) {
		return ShopPurchaseQuote{}, fmt.Errorf("그런 물건은 팔지 않습니다")
	}
	if flag(stock.Object.Flags[:], objectInvisibleFlag) && !flag(actor.Body.Flags[:], playerDetectInvisibleFlag) {
		return ShopPurchaseQuote{}, fmt.Errorf("그런 물건은 보이지 않습니다")
	}
	if stock.Object.Value < 0 {
		// command7.c has no useful negative-price path: subtracting a negative
		// value would mint gold.  Canonical shop admission therefore rejects
		// malformed prices instead of turning source corruption into currency.
		return ShopPurchaseQuote{}, fmt.Errorf("invalid shop price")
	}
	rootWeight, err := storage.Items.objectWeight(stockID)
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	carriedWeight, err := actor.Items.Weight()
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	if rootWeight < 0 || carriedWeight < 0 {
		return ShopPurchaseQuote{}, fmt.Errorf("invalid item weight")
	}
	carriedRoots, err := shopCarriedRootCount(*actor.Items)
	if err != nil {
		return ShopPurchaseQuote{}, err
	}
	if carriedRoots >= shopInventoryLimit {
		return ShopPurchaseQuote{}, fmt.Errorf("당신은 더이상 가질 수 없습니다")
	}
	maxWeight := int64(maxPlayerWeight(actor.Body))
	if int64(rootWeight) > maxWeight-int64(carriedWeight) {
		return ShopPurchaseQuote{}, fmt.Errorf("당신은 더이상 가질 수 없습니다")
	}
	return ShopPurchaseQuote{
		ShopRoomID:      shop.Resource.ID,
		StorageRoomID:   storage.Resource.ID,
		StockID:         stockID,
		ItemName:        stock.Object.Name,
		Price:           int64(stock.Object.Value),
		RootWeight:      rootWeight,
		CarriedWeight:   carriedWeight,
		CarriedRoots:    carriedRoots,
		PurchasedWeight: rootWeight,
	}, nil
}

// shopCarriedRootCount mirrors command7.c's buy() count: MAXWEAR ready slots
// plus direct inventory roots.  It intentionally does not count descendants
// inside a container, matching count_inv(ply_ptr, 0) rather than the bank/get
// paths' count_inv(..., -1).  The pre-add limit makes the resulting snapshot
// fit the legacy 200-object bound without inheriting count_inv's saturating
// comparison bug.
func shopCarriedRootCount(items ItemCollection) (int, error) {
	if err := items.Validate(); err != nil {
		return 0, err
	}
	count := len(items.Inventory)
	for _, id := range items.Ready {
		if id != "" {
			count++
		}
	}
	return count, nil
}

func shopSubtreeIDs(items ItemCollection, root string) ([]string, error) {
	if err := items.Validate(); err != nil {
		return nil, err
	}
	if _, ok := items.Items[root]; !ok {
		return nil, fmt.Errorf("shop stock item absent")
	}
	ids := make([]string, 0, 1)
	var visit func(string) error
	visit = func(id string) error {
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("shop stock subtree missing item")
		}
		ids = append(ids, id)
		for _, child := range item.Contents {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return ids, nil
}

// copyShopSubtree creates the player-owned copy of a shop stock root.  C's
// buy() copies the object and clears room/inventory/temporary permanence on
// the purchased object.  Canonical Go graphs cannot share nested pointers, so
// this applies the same ownership transition to every copied node while
// preserving the source Contents order.
func copyShopSubtree(source ItemCollection, root string, used map[string]struct{}, allocate ShopPurchaseItemIDAllocator) (ItemCollection, string, []string, error) {
	if allocate == nil {
		return ItemCollection{}, "", nil, fmt.Errorf("shop item ID allocator required")
	}
	ids, err := shopSubtreeIDs(source, root)
	if err != nil {
		return ItemCollection{}, "", nil, err
	}
	if len(ids) > shopItemLimit {
		return ItemCollection{}, "", nil, fmt.Errorf("shop item subtree too large")
	}
	copied := ItemCollection{Items: make(map[string]Item, len(ids))}
	newIDs := make(map[string]string, len(ids))
	for _, sourceID := range ids {
		id, err := allocate()
		if err != nil {
			return ItemCollection{}, "", nil, err
		}
		if id == "" {
			return ItemCollection{}, "", nil, fmt.Errorf("invalid allocated shop item ID")
		}
		if _, exists := used[id]; exists {
			return ItemCollection{}, "", nil, fmt.Errorf("duplicate allocated shop item ID")
		}
		used[id] = struct{}{}
		newIDs[sourceID] = id
		item := source.Items[sourceID]
		item.Object.Contents = nil
		item.Object.Flags[0] &^= 1 << 0 // OPERMT: room-permanent
		item.Object.Flags[1] &^= 1 << 0 // OTEMPP: temporary permanent (bit 8)
		item.Object.Flags[1] &^= 1 << 1 // OPERM2: inventory permanent (bit 9)
		item.Contents = nil
		copied.Items[id] = item
	}
	for _, sourceID := range ids {
		item := copied.Items[newIDs[sourceID]]
		for _, child := range source.Items[sourceID].Contents {
			item.Contents = append(item.Contents, newIDs[child])
		}
		copied.Items[newIDs[sourceID]] = item
	}
	newRoot := newIDs[root]
	copied.Inventory = []string{newRoot}
	if err := copied.Validate(); err != nil {
		return ItemCollection{}, "", nil, err
	}
	return copied, newRoot, newIDsInOrder(ids, newIDs), nil
}

func newIDsInOrder(sourceIDs []string, newIDs map[string]string) []string {
	out := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		out = append(out, newIDs[id])
	}
	return out
}

func insertShopInventoryRoot(items *ItemCollection, root string) error {
	item, ok := items.Items[root]
	if !ok {
		return fmt.Errorf("purchased shop item absent")
	}
	at := len(items.Inventory)
	for i, id := range items.Inventory {
		other := items.Items[id].Object
		if other.Name > item.Object.Name || (other.Name == item.Object.Name && int8(other.Adjustment) > int8(item.Object.Adjustment)) {
			at = i
			break
		}
	}
	items.Inventory = append(items.Inventory, "")
	copy(items.Inventory[at+1:], items.Inventory[at:])
	items.Inventory[at] = root
	return nil
}

// BuyShopItem performs one source-backed shop purchase as a pure state
// transition.  The storage stock remains owned by the storage room; a deep
// copy with fresh canonical identities is inserted into the buyer's inventory,
// and the gold debit is applied only to the returned candidate.  Any failure
// returns zero state/result and leaves the receiver untouched.
func (s State) BuyShopItem(actorID, stockID string, allocate ShopPurchaseItemIDAllocator) (State, ShopPurchaseResult, error) {
	quote, err := s.QuoteShopPurchase(actorID, stockID)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	if allocate == nil {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("shop item ID allocator required")
	}
	actor := s.Players[actorID]
	goldBefore := int64(actor.Body.Gold)
	if goldBefore < 0 {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("invalid player gold")
	}
	if quote.Price > goldBefore {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("돈도 없으면서... 외상사절")
	}
	goldAfter := goldBefore - quote.Price
	if goldAfter < 0 || goldAfter > int64(1<<31-1) {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("player gold overflow")
	}
	_, _, storage, err := s.shopStorage(actorID)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	stockIDs, err := shopSubtreeIDs(*storage.Items, stockID)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	// Check the canonical collection bound before invoking an external ID
	// allocator.  A failed candidate must not consume identities merely to
	// discover that the destination map cannot hold the copied subtree.
	if len(actor.Items.Items)+len(stockIDs) > shopItemLimit {
		return State{}, ShopPurchaseResult{}, fmt.Errorf("shop item collection overflow")
	}
	used := make(map[string]struct{})
	for _, room := range s.Rooms {
		if room.Items != nil {
			for id := range room.Items.Items {
				used[id] = struct{}{}
			}
		}
	}
	for _, player := range s.Players {
		if player.Items != nil {
			for id := range player.Items.Items {
				used[id] = struct{}{}
			}
		}
	}
	for _, account := range s.BankAccounts {
		if account.Items != nil {
			for id := range account.Items.Items {
				used[id] = struct{}{}
			}
		}
	}
	for _, npc := range s.NPCs {
		if npc.Items != nil {
			for id := range npc.Items.Items {
				used[id] = struct{}{}
			}
		}
	}
	copied, newRoot, purchasedIDs, err := copyShopSubtree(*storage.Items, stockID, used, allocate)
	if err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	destination := actor.Items.clone()
	for id, item := range copied.Items {
		destination.Items[id] = item
	}
	if err := insertShopInventoryRoot(&destination, newRoot); err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	if err := destination.Validate(); err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	next := s.clone()
	nextPlayer := next.Players[actorID]
	// command7.c:buy clears PHIDDN immediately before the purchased object is
	// attached to the player.  A successful durable purchase must therefore
	// reveal the buyer as part of the same candidate, not in a later event.
	nextPlayer.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	nextPlayer.Body.Gold = int32(goldAfter)
	nextPlayer.Items = &destination
	next.Players[actorID] = nextPlayer
	if err := next.Validate(); err != nil {
		return State{}, ShopPurchaseResult{}, err
	}
	return next, ShopPurchaseResult{
		Action:          "buy-shop-item",
		ShopRoomID:      quote.ShopRoomID,
		StorageRoomID:   quote.StorageRoomID,
		StockID:         quote.StockID,
		ItemName:        quote.ItemName,
		Price:           quote.Price,
		GoldBefore:      goldBefore,
		GoldAfter:       goldAfter,
		PurchasedRootID: newRoot,
		PurchasedIDs:    purchasedIDs,
	}, nil
}
