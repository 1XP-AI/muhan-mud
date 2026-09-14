package world

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func shopMarketplaceFlags(t *testing.T, pawn bool) (shop, storage [8]byte) {
	t.Helper()
	if pawn {
		shop[RoomPawnFlag/8] |= 1 << (RoomPawnFlag % 8)
	} else {
		shop[RoomShopFlag/8] |= 1 << (RoomShopFlag % 8)
	}
	storage[roomNoTeleportFlag/8] |= 1 << (roomNoTeleportFlag % 8)
	return shop, storage
}

func shopMarketplaceFixture(t *testing.T, pawn bool) State {
	t.Helper()
	shopFlags, storageFlags := shopMarketplaceFlags(t, pawn)
	temporary := [8]byte{}
	temporary[objectTemporaryFlag/8] |= 1 << (objectTemporaryFlag % 8)
	temporary[objectInventoryPermanentFlag/8] |= 1 << (objectInventoryPermanentFlag % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			200: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 200, Name: "전당포", Flags: shopFlags}},
				PlayerIDs: []string{"a"},
			},
			201: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 201, Name: "전당포 저장고", Flags: storageFlags}},
				Items: &ItemCollection{
					Items: map[string]Item{
						"stock": {Object: LegacyObject{Name: "기존 재고", Type: 13, Value: 80, Weight: 1}},
					},
					Inventory: []string{"stock"},
				},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items: &ItemCollection{
					Items: map[string]Item{
						"sword":        {Object: LegacyObject{Name: "검", Type: 13, Value: 100, Weight: 5, Flags: temporary}},
						"invisible":    {Object: LegacyObject{Name: "투명검", Type: 13, Value: 100, Weight: 1, Flags: [8]byte{1 << objectInvisibleFlag}}},
						"nested":       {Object: LegacyObject{Name: "상자", Type: 13, Value: 100, Weight: 1}, Contents: []string{"nested-child"}},
						"nested-child": {Object: LegacyObject{Name: "보석", Type: 13, Value: 1, Weight: 1}},
					},
					Inventory: []string{"sword", "invisible", "nested"},
				},
			},
		},
	}
}

func TestListShopItemsIsDeterministicReadOnlyAndUsesCanonicalStorage(t *testing.T) {
	s := shopMarketplaceFixture(t, false)
	before := s.clone()
	first, err := s.ListShopItems("a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ListShopItems("a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(s, before) {
		t.Fatalf("list was not deterministic/read-only: first=%+v second=%+v", first, second)
	}
	if first.Action != "list-shop-items" || first.ShopRoomID != 200 || first.StorageRoomID != 201 || len(first.Items) != 1 || first.Items[0].ItemID != "stock" || first.Items[0].Price != 80 {
		t.Fatalf("listing=%+v", first)
	}
	want := ShopListHeaderResponse + "\r\n   " + fmt.Sprintf("%-30s", "기존 재고") + "   가격: 80"
	if first.Response != want {
		t.Fatalf("response=%q want=%q", first.Response, want)
	}
}

func TestListShopItemsPrintsCNotShopForNonRSHOPPIncludingPawnOnly(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	room := s.Rooms[200]
	room.Resource.Flags[RoomShopFlag/8] &^= 1 << (RoomShopFlag % 8)
	s.Rooms[200] = room
	before := s.clone()
	result, err := s.ListShopItems("a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != ShopListNotShopResponse || result.Action != ShopListNotShopAction || result.ShopRoomID != 200 || len(result.Items) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(result.Response, "전당포") {
		t.Fatalf("guessed pawn-shop message: %q", result.Response)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("not-shop list mutated snapshot")
	}

	plain := shopMarketplaceFixture(t, false)
	plainRoom := plain.Rooms[200]
	plainRoom.Resource.Flags = [8]byte{}
	plain.Rooms[200] = plainRoom
	plainResult, err := plain.ListShopItems("a")
	if err != nil || plainResult.Response != ShopListNotShopResponse {
		t.Fatalf("plain room result=%+v err=%v", plainResult, err)
	}
}

func TestListShopItemsPrintsCNoStockWhenStorageRoomMissing(t *testing.T) {
	s := shopMarketplaceFixture(t, false)
	delete(s.Rooms, 201)
	before := s.clone()
	result, err := s.ListShopItems("a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != ShopListNoStockResponse || result.Action != ShopListNoStockAction || result.ShopRoomID != 200 || result.StorageRoomID != 0 || len(result.Items) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("missing-storage list mutated snapshot")
	}
}

func TestListShopItemsEmptyStoragePrintsCHeaderWithoutGuessedNone(t *testing.T) {
	s := shopMarketplaceFixture(t, false)
	room := s.Rooms[201]
	room.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[201] = room
	result, err := s.ListShopItems("a")
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != ShopListHeaderResponse || strings.Contains(result.Response, "없음") || len(result.Items) != 0 {
		t.Fatalf("empty catalog=%+v", result)
	}
}

func TestListShopItemsFailClosesUnmigratedStorageAndInvalidStock(t *testing.T) {
	unmigrated := shopMarketplaceFixture(t, false)
	room := unmigrated.Rooms[201]
	room.Items = nil
	unmigrated.Rooms[201] = room
	if _, err := unmigrated.ListShopItems("a"); err == nil {
		t.Fatal("nil storage Items unexpectedly listed")
	}

	broken := shopMarketplaceFixture(t, false)
	brokenRoom := broken.Rooms[201]
	delete(brokenRoom.Items.Items, "stock")
	brokenRoom.Items.Inventory = []string{"missing"}
	broken.Rooms[201] = brokenRoom
	if _, err := broken.ListShopItems("a"); err == nil {
		t.Fatal("invalid stock root unexpectedly listed")
	}

	negative := shopMarketplaceFixture(t, false)
	negRoom := negative.Rooms[201]
	item := negRoom.Items.Items["stock"]
	item.Object.Value = -1
	negRoom.Items.Items["stock"] = item
	negative.Rooms[201] = negRoom
	if _, err := negative.ListShopItems("a"); err == nil {
		t.Fatal("negative stock value unexpectedly listed")
	}
}

func TestSellShopItemPrintsCNotPawnForNonRPAWNSIncludingShopOnly(t *testing.T) {
	s := shopMarketplaceFixture(t, false)
	hidden := s.Players["a"]
	hidden.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = hidden
	original := s.clone()
	next, result, err := s.SellShopItem("a", "sword")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopSaleNotPawnAction || result.Response != ShopSaleNotPawnResponse || result.ShopRoomID != 200 || result.Payout != 0 || result.ItemID != "" {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(result.Response, "상점") {
		t.Fatalf("guessed RSHOPP shop text: %q", result.Response)
	}
	if !reflect.DeepEqual(next, original) || !reflect.DeepEqual(s, original) {
		t.Fatal("not-pawn sale cloned or mutated")
	}
	nextPlayer := next.Players["a"]
	if !flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("not-pawn sale cleared PHIDDN")
	}

	plain := shopMarketplaceFixture(t, false)
	plainRoom := plain.Rooms[200]
	plainRoom.Resource.Flags = [8]byte{}
	plain.Rooms[200] = plainRoom
	_, plainResult, err := plain.SellShopItemByName("a", "검", 1)
	if err != nil || plainResult.Response != ShopSaleNotPawnResponse || plainResult.Action != ShopSaleNotPawnAction {
		t.Fatalf("plain room result=%+v err=%v", plainResult, err)
	}
}

func TestSellShopItemByNamePrintsCAskWhatWhenNameMissing(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	hidden := s.Players["a"]
	hidden.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = hidden
	original := s.clone()
	next, result, err := s.SellShopItemByName("a", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopSaleAskWhatAction || result.Response != ShopSaleAskWhatResponse || result.ShopRoomID != 200 || result.Payout != 0 || result.ItemID != "" {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(next, original) || !reflect.DeepEqual(s, original) {
		t.Fatal("ask-what sale cloned or mutated")
	}
	if next.Players["a"].Body.Gold != 100 || !containsString(next.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("ask-what moved gold/item: %+v", next.Players["a"])
	}
	askWhatPlayer := next.Players["a"]
	if !flag(askWhatPlayer.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("ask-what sale cleared PHIDDN")
	}
}

func TestSellShopItemByNamePrintsCNotPawnBeforeAskWhat(t *testing.T) {
	s := shopMarketplaceFixture(t, false)
	_, result, err := s.SellShopItemByName("a", "", 1)
	if err != nil || result.Response != ShopSaleNotPawnResponse || result.Action != ShopSaleNotPawnAction {
		t.Fatalf("bare sell outside pawn=%+v err=%v", result, err)
	}
	if strings.Contains(result.Response, "상점") {
		t.Fatalf("guessed RSHOPP shop text: %q", result.Response)
	}
}

func TestSellShopItemByNamePrintsCNotHoldingWhenNameMissingFromInventory(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	hidden := s.Players["a"]
	hidden.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = hidden
	original := s.clone()
	next, result, err := s.SellShopItemByName("a", "방패", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopSaleNotHoldingAction || result.Response != ShopSaleNotHoldingResponse || result.ShopRoomID != 200 || result.Payout != 0 {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.Gold != 100 || !containsString(next.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("not-holding moved gold/item: %+v", next.Players["a"])
	}
	nextPlayer := next.Players["a"]
	if flag(nextPlayer.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("not-holding leftover did not F_CLR PHIDDN")
	}
	originalPlayer := original.Players["a"]
	if !flag(originalPlayer.Body.Flags[:], playerHiddenStateFlag) || !reflect.DeepEqual(s, original) {
		t.Fatal("not-holding mutated source snapshot")
	}

	_, prefix, err := s.SellShopItemByName("a", "검술", 1)
	if err != nil || prefix.Response != ShopSaleNotHoldingResponse {
		t.Fatalf("prefix name=%+v err=%v", prefix, err)
	}
	_, occ, err := s.SellShopItemByName("a", "검", 2)
	if err != nil || occ.Response != ShopSaleNotHoldingResponse {
		t.Fatalf("missing occurrence=%+v err=%v", occ, err)
	}
}

func TestSellShopItemByNameFailClosesUnmigratedItems(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	actor := s.Players["a"]
	actor.Items = nil
	s.Players["a"] = actor
	original := s.clone()
	next, result, err := s.SellShopItemByName("a", "검", 1)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ShopSaleResult{}) || !reflect.DeepEqual(s, original) {
		t.Fatalf("unmigrated items sold: next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestSellShopItemMovesRootCreditsExactValueAndNormalizesTemporaryFlags(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	hidden := s.Players["a"]
	hidden.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = hidden
	before := s.clone()
	quote, err := s.QuoteShopSale("a", "sword")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Value != 100 || quote.Payout != 50 || quote.ItemWeight != 5 || quote.CarriedWeightBefore != 8 || quote.CarriedWeightAfter != 3 || !quote.TemporaryBefore || !quote.StoragePermanentAfter || !quote.VisibleToSeller {
		t.Fatalf("quote=%+v", quote)
	}
	next, result, err := s.SellShopItem("a", "sword")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ShopSaleSoldAction || result.ItemID != "sword" || result.Payout != 50 || result.GoldBefore != 100 || result.GoldAfter != 150 || result.CarriedWeightAfter != 3 || !result.TemporaryBefore || !result.StoragePermanentAfter {
		t.Fatalf("result=%+v", result)
	}
	player := next.Players["a"]
	if player.Body.Gold != 150 || flag(player.Body.Flags[:], playerHiddenStateFlag) || containsString(player.Items.Inventory, "sword") {
		t.Fatalf("player=%+v", player)
	}
	stored, ok := next.Rooms[201].Items.Items["sword"]
	if !ok || !containsString(next.Rooms[201].Items.Inventory, "sword") || !flag(stored.Object.Flags[:], objectRoomPermanentFlag) || flag(stored.Object.Flags[:], objectTemporaryFlag) || flag(stored.Object.Flags[:], objectInventoryPermanentFlag) {
		t.Fatalf("stored=%+v room=%+v", stored, next.Rooms[201])
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("sale mutated source snapshot")
	}

	named := before.clone()
	nextByName, namedResult, err := named.SellShopItemByName("a", "검", 1)
	if err != nil {
		t.Fatal(err)
	}
	if namedResult.Action != ShopSaleSoldAction || namedResult.ItemID != "sword" || namedResult.Payout != 50 || namedResult.GoldAfter != 150 {
		t.Fatalf("by-name result=%+v", namedResult)
	}
	namedNext := nextByName.Players["a"]
	if flag(namedNext.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("successful 팔아 did not F_CLR PHIDDN")
	}
	namedSource := named.Players["a"]
	if !flag(namedSource.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("successful 팔아 mutated source PHIDDN")
	}
}

func TestQuoteShopSaleRejectsUnsafeAndAmbiguousBranchesBeforeMutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		id     string
	}{
		{name: "wrong room flag", mutate: func(s *State) { room := s.Rooms[200]; room.Resource.Flags = [8]byte{}; s.Rooms[200] = room }, id: "sword"},
		{name: "missing no-teleport storage", mutate: func(s *State) { room := s.Rooms[201]; room.Resource.Flags = [8]byte{}; s.Rooms[201] = room }, id: "sword"},
		{name: "invisible", id: "invisible"},
		{name: "too cheap", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.Value = 38
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}, id: "sword"},
		{name: "poor weapon quality", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.Type = 1
			item.Object.ShotsMax = 8
			item.Object.ShotsCurrent = 1
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}, id: "sword"},
		{name: "event item", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.Flags[objectOneWevFlag/8] |= 1 << (objectOneWevFlag % 8)
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}, id: "sword"},
		{name: "nested item", id: "nested"},
		{name: "negative weight", mutate: func(s *State) {
			p := s.Players["a"]
			item := p.Items.Items["sword"]
			item.Object.Weight = -1
			p.Items.Items["sword"] = item
			s.Players["a"] = p
		}, id: "sword"},
		{name: "gold overflow", mutate: func(s *State) { p := s.Players["a"]; p.Body.Gold = int32(^uint32(0) >> 1); s.Players["a"] = p }, id: "sword"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := shopMarketplaceFixture(t, true)
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			before := s.clone()
			if _, err := s.QuoteShopSale("a", tc.id); err == nil {
				t.Fatal("unsafe sale unexpectedly quoted")
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("quote mutated snapshot")
			}
		})
	}

	duplicate := shopMarketplaceFixture(t, true)
	room := duplicate.Rooms[201]
	item := duplicate.Players["a"].Items.Items["sword"]
	room.Items.Items["sword"] = item
	room.Items.Inventory = append(room.Items.Inventory, "sword")
	duplicate.Rooms[201] = room
	if _, err := duplicate.QuoteShopSale("a", "sword"); err == nil {
		t.Fatal("duplicate ownership unexpectedly accepted")
	}

	good := shopMarketplaceFixture(t, true)
	goodActor := good.Players["a"]
	goodActor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	good.Players["a"] = goodActor
	if _, err := good.QuoteShopSaleByName("a", "투명검", 1); err != nil {
		t.Fatalf("detect invisible did not permit source visibility: %v", err)
	}
}

func TestSellShopItemRejectsMalformedStorageAndKeepsCanonicalIdentity(t *testing.T) {
	s := shopMarketplaceFixture(t, true)
	room := s.Rooms[201]
	delete(room.Items.Items, "stock")
	room.Items.Inventory = []string{"missing"}
	s.Rooms[201] = room
	if _, _, err := s.SellShopItem("a", "sword"); err == nil {
		t.Fatal("malformed storage unexpectedly accepted")
	}

	allocatorLike := shopMarketplaceFixture(t, true)
	next, _, err := allocatorLike.SellShopItem("a", "sword")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.Players["a"].Items.Items["sword"]; ok {
		t.Fatal("sold canonical ID remained player-owned")
	}
	if next.Rooms[201].Items.Items["sword"].Object.Name != "검" {
		t.Fatal("sold canonical ID/name changed")
	}
}
