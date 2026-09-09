package world

import (
	"fmt"
	"reflect"
	"testing"
)

func shopTransactionFixture(t *testing.T) State {
	t.Helper()
	var shopFlags, storageFlags, permanentFlags [8]byte
	shopFlags[RoomShopFlag/8] |= 1 << (RoomShopFlag % 8)
	storageFlags[roomNoTeleportFlag/8] |= 1 << (roomNoTeleportFlag % 8)
	permanentFlags[0] |= 1 << 0 // OPERMT
	permanentFlags[1] |= 1 << 0 // OTEMPP
	permanentFlags[1] |= 1 << 1 // OPERM2
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			100: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 100, Flags: shopFlags}},
				PlayerIDs: []string{"a"},
			},
			101: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 101, Flags: storageFlags}},
				Items: &ItemCollection{
					Items: map[string]Item{
						"stock-sword": {
							Object:   LegacyObject{Name: "검", Value: 40, Weight: 3, Flags: permanentFlags},
							Contents: []string{"stock-gem"},
						},
						"stock-gem": {
							Object: LegacyObject{Name: "보석", Weight: 2, Flags: permanentFlags},
						},
						"stock-sword-2": {
							Object: LegacyObject{Name: "검", Value: 60, Weight: 1, Flags: permanentFlags},
						},
					},
					Inventory: []string{"stock-sword", "stock-sword-2"},
				},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 100, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items: &ItemCollection{
					Items:     map[string]Item{"old": {Object: LegacyObject{Name: "낡은 물건", Weight: 1}}},
					Inventory: []string{"old"},
				},
			},
		},
	}
}

func sequenceShopAllocator(ids ...string) ShopPurchaseItemIDAllocator {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", fmt.Errorf("allocator exhausted")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func TestQuoteShopPurchaseUsesCanonicalRoomStorageAndExactStockPrice(t *testing.T) {
	s := shopTransactionFixture(t)
	quote, err := s.QuoteShopPurchase("a", "stock-sword-2")
	if err != nil {
		t.Fatal(err)
	}
	if quote.ShopRoomID != 100 || quote.StorageRoomID != 101 || quote.StockID != "stock-sword-2" || quote.ItemName != "검" || quote.Price != 60 {
		t.Fatalf("quote=%+v", quote)
	}
	if quote.RootWeight != 1 || quote.CarriedWeight != 1 || quote.CarriedRoots != 1 {
		t.Fatalf("quote capacity/weight=%+v", quote)
	}
	if _, err := s.QuoteShopPurchase("a", "missing"); err == nil {
		t.Fatal("missing shop stock unexpectedly quoted")
	}
}

func TestBuyShopItemDeepCopiesNestedStockAndDebitsAtomically(t *testing.T) {
	s := shopTransactionFixture(t)
	original := s.clone()
	next, result, err := s.BuyShopItem("a", "stock-sword", sequenceShopAllocator("bought-sword", "bought-gem"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "buy-shop-item" || result.StockID != "stock-sword" || result.ItemName != "검" || result.Price != 40 || result.GoldBefore != 100 || result.GoldAfter != 60 {
		t.Fatalf("result=%+v", result)
	}
	if result.PurchasedRootID != "bought-sword" || !reflect.DeepEqual(result.PurchasedIDs, []string{"bought-sword", "bought-gem"}) {
		t.Fatalf("purchased IDs=%+v", result)
	}
	player := next.Players["a"]
	if player.Body.Gold != 60 || !containsString(player.Items.Inventory, "bought-sword") || len(player.Items.Items) != 3 {
		t.Fatalf("player=%+v", player)
	}
	if !reflect.DeepEqual(player.Items.Items["bought-sword"].Contents, []string{"bought-gem"}) || player.Items.Items["bought-gem"].Object.Name != "보석" {
		t.Fatalf("nested purchased graph=%+v", player.Items.Items)
	}
	for _, id := range result.PurchasedIDs {
		item := player.Items.Items[id]
		if flag(item.Object.Flags[:], 0) || flag(item.Object.Flags[:], 8) || flag(item.Object.Flags[:], 9) {
			t.Fatalf("purchased item retained permanence flags: id=%s flags=%v", id, item.Object.Flags)
		}
	}
	if !reflect.DeepEqual(next.Rooms[101].Items, original.Rooms[101].Items) || next.Rooms[101].Items.Items["stock-sword"].Contents[0] != "stock-gem" {
		t.Fatal("shop stock was consumed or aliased")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("buy mutated source snapshot")
	}
	player.Items.Items["bought-gem"] = Item{Object: LegacyObject{Name: "changed"}}
	if next.Rooms[101].Items.Items["stock-gem"].Object.Name != "보석" {
		t.Fatal("purchased nested graph aliases storage")
	}
}

func TestBuyShopItemIsDeterministicForReplayCandidate(t *testing.T) {
	s := shopTransactionFixture(t)
	first, firstResult, err := s.BuyShopItem("a", "stock-sword", sequenceShopAllocator("copy-root", "copy-child"))
	if err != nil {
		t.Fatal(err)
	}
	second, secondResult, err := s.BuyShopItem("a", "stock-sword", sequenceShopAllocator("copy-root", "copy-child"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("same snapshot/request did not produce deterministic candidate: first=%+v second=%+v", firstResult, secondResult)
	}
}

func TestBuyShopItemRejectsFundsWeightAndCapacityWithoutPartialMutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
	}{
		{
			name: "insufficient funds",
			mutate: func(s *State) {
				p := s.Players["a"]
				p.Body.Gold = 39
				s.Players["a"] = p
			},
		},
		{
			name: "weight",
			mutate: func(s *State) {
				room := s.Rooms[101]
				item := room.Items.Items["stock-sword"]
				item.Object.Weight = 120
				room.Items.Items["stock-sword"] = item
				s.Rooms[101] = room
			},
		},
		{
			name: "direct-root capacity",
			mutate: func(s *State) {
				p := s.Players["a"]
				for i := 0; i < 199; i++ {
					id := fmt.Sprintf("carried-%03d", i)
					p.Items.Items[id] = Item{Object: LegacyObject{Name: id}}
					p.Items.Inventory = append(p.Items.Inventory, id)
				}
				s.Players["a"] = p
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := shopTransactionFixture(t)
			tc.mutate(&s)
			original := s.clone()
			called := false
			next, result, err := s.BuyShopItem("a", "stock-sword", func() (string, error) {
				called = true
				return "should-not-allocate", nil
			})
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ShopPurchaseResult{}) || !reflect.DeepEqual(s, original) {
				t.Fatalf("accepted/partially applied %s: next=%+v result=%+v err=%v", tc.name, next, result, err)
			}
			if called {
				t.Fatalf("allocator called before %s validation", tc.name)
			}
		})
	}
}

func TestBuyShopItemRejectsDuplicateIDsAllocatorFailureAndMalformedPrice(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		alloc  ShopPurchaseItemIDAllocator
	}{
		{name: "existing stock ID", alloc: sequenceShopAllocator("stock-sword")},
		{name: "existing player ID", alloc: sequenceShopAllocator("old")},
		{name: "duplicate newly allocated ID", alloc: sequenceShopAllocator("copy", "copy")},
		{name: "allocator failure", alloc: func() (string, error) { return "copy", fmt.Errorf("allocator unavailable") }},
		{
			name: "negative price",
			mutate: func(s *State) {
				room := s.Rooms[101]
				item := room.Items.Items["stock-sword"]
				item.Object.Value = -1
				room.Items.Items["stock-sword"] = item
				s.Rooms[101] = room
			},
			alloc: sequenceShopAllocator("unused", "unused-child"),
		},
		{
			name: "storage room identity overflow",
			mutate: func(s *State) {
				shop := s.Rooms[100]
				player := s.Players["a"]
				delete(s.Rooms, 100)
				delete(s.Rooms, 101)
				shop.Resource.ID = 32767
				player.Body.RoomID = 32767
				shop.PlayerIDs = []string{"a"}
				s.Rooms[32767] = shop
				s.Players["a"] = player
			},
			alloc: sequenceShopAllocator("unused"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := shopTransactionFixture(t)
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			original := s.clone()
			next, result, err := s.BuyShopItem("a", "stock-sword", tc.alloc)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, ShopPurchaseResult{}) || !reflect.DeepEqual(s, original) {
				t.Fatalf("accepted/partially applied %s: next=%+v result=%+v err=%v", tc.name, next, result, err)
			}
		})
	}
}
