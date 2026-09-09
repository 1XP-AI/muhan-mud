package world

import (
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
	if !strings.Contains(first.Response, "기존 재고") || !strings.Contains(first.Response, "80") {
		t.Fatalf("response=%q", first.Response)
	}

	room := s.Rooms[200]
	room.Resource.Flags[RoomShopFlag/8] = 0
	s.Rooms[200] = room
	if _, err := s.ListShopItems("a"); err == nil {
		t.Fatal("list accepted non-shop room")
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
	if result.Action != "sell-shop-item" || result.ItemID != "sword" || result.Payout != 50 || result.GoldBefore != 100 || result.GoldAfter != 150 || result.CarriedWeightAfter != 3 || !result.TemporaryBefore || !result.StoragePermanentAfter {
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
	if _, _, err := duplicate.SellShopItemByName("a", "검", 2); err == nil {
		t.Fatal("duplicate occurrence unexpectedly accepted")
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
