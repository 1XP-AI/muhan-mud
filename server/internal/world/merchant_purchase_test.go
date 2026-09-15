package world

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type merchantPurchaseCatalog struct {
	objects map[int16]LegacyObject
}

func (c merchantPurchaseCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{}, fmt.Errorf("monster lookup not expected")
}

func (c merchantPurchaseCatalog) Object(id int16) (LegacyObject, error) {
	object, ok := c.objects[id]
	if !ok {
		return LegacyObject{}, fmt.Errorf("missing object %d", id)
	}
	return object, nil
}

func merchantPurchaseFixture() (State, MerchantOffers) {
	var merchantFlags [8]byte
	merchantFlags[merchantPurchaseFlag/8] |= 1 << (merchantPurchaseFlag % 8)
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			200: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 200, Name: "상점방"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"merchant-1", "merchant-2"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{"old": {Object: LegacyObject{Name: "낡은 물건", Weight: 1}}}, Inventory: []string{"old"}},
			},
		},
		NPCs: map[string]NPCState{
			"merchant-1": {Body: LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Class: 4, Flags: merchantFlags}},
			"merchant-2": {Body: LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Class: 4, Flags: merchantFlags}},
		},
	}
	offers := MerchantOffers{
		"merchant-1": {
			{Name: "검", Value: 7, Weight: 2},
		},
		"merchant-2": {
			{Name: "검", Value: 7, Weight: 2},
			{Name: "검", Value: 25, Weight: 1, Contents: []LegacyObject{{Name: "보석", Weight: 1}}},
		},
	}
	return state, offers
}

func merchantPurchaseAllocator(ids ...string) MerchantPurchaseItemIDAllocator {
	index := 0
	return func() (string, error) {
		if index >= len(ids) {
			return "", fmt.Errorf("merchant allocator exhausted")
		}
		id := ids[index]
		index++
		return id, nil
	}
}

func TestImportNPCMerchantOffersResolvesCarryTemplatesAndFailsClosed(t *testing.T) {
	s, _ := merchantPurchaseFixture()
	npc := s.NPCs["merchant-1"]
	npc.Body.Carry = [10]int16{1, 2, 1}
	s.NPCs["merchant-1"] = npc
	catalog := merchantPurchaseCatalog{objects: map[int16]LegacyObject{
		1: {Name: "검", Value: 10},
		2: {Name: "보석", Value: 20, Contents: []LegacyObject{{Name: "조각", Weight: 1}}},
	}}
	offers, err := s.ImportNPCMerchantOffers(catalog)
	if err != nil || len(offers["merchant-1"]) != 2 || offers["merchant-1"][0].Name != "검" || offers["merchant-1"][1].Name != "보석" || offers["merchant-2"] == nil {
		t.Fatalf("offers=%+v err=%v", offers, err)
	}
	broken := s.clone()
	brokenNPC := broken.NPCs["merchant-1"]
	brokenNPC.Body.Carry[0] = 99
	broken.NPCs["merchant-1"] = brokenNPC
	if _, err := broken.ImportMerchantOffers(catalog); err == nil {
		t.Fatal("unresolved merchant Carry template accepted")
	}
}

func TestPurchaseMerchantByNameCopiesNestedOfferDebitsGoldAndKeepsNPCTemplate(t *testing.T) {
	s, offers := merchantPurchaseFixture()
	before := s.clone()
	next, result, err := s.PurchaseMerchantByName("actor", "상인", 2, "검", 2, offers, merchantPurchaseAllocator("purchased-root", "purchased-gem"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "merchant-purchase" || result.NPCID != "merchant-2" || result.Price != 25 || result.GoldBefore != 100 || result.GoldAfter != 75 || result.RewardRootID != "purchased-root" || !reflect.DeepEqual(result.PurchasedIDs, []string{"purchased-root", "purchased-gem"}) {
		t.Fatalf("result=%+v", result)
	}
	player := next.Players["actor"]
	if player.Body.Gold != 75 || !containsString(player.Items.Inventory, "purchased-root") {
		t.Fatalf("player=%+v", player)
	}
	if got := player.Items.Items["purchased-root"]; got.Object.Name != "검" || !reflect.DeepEqual(got.Contents, []string{"purchased-gem"}) {
		t.Fatalf("root=%+v", got)
	}
	if got := player.Items.Items["purchased-gem"]; got.Object.Name != "보석" {
		t.Fatalf("child=%+v", got)
	}
	if !reflect.DeepEqual(next.NPCs, before.NPCs) || !reflect.DeepEqual(s, before) {
		t.Fatal("merchant purchase mutated source or NPC template")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPurchaseMerchantByNameUsesExactPositiveOccurrencesAndSemanticNoOps(t *testing.T) {
	s, offers := merchantPurchaseFixture()
	called := false
	next, result, err := s.PurchaseMerchantByName("actor", "상인 일부", 1, "검", 1, offers, func() (string, error) {
		called = true
		return "unexpected", nil
	})
	if err != nil || result.Action != "merchant-purchase-rejected" || called || !reflect.DeepEqual(next, s) {
		t.Fatalf("prefix result=%+v err=%v called=%t", result, err, called)
	}
	next, result, err = s.PurchaseMerchantByName("actor", "없는 상인", 1, "검", 1, nil, nil)
	if err != nil || result.Response != "그것은 여기 없습니다.\r\n" || !reflect.DeepEqual(next, s) {
		t.Fatalf("unknown NPC result=%+v err=%v", result, err)
	}
	if _, err := s.PlanMerchantPurchase("actor", "상인", 0, "검", 1, offers, nil); err == nil {
		t.Fatal("zero NPC occurrence accepted")
	}
	if _, err := s.PlanMerchantPurchase("actor", "상인", 1, "검", 0, offers, nil); err == nil {
		t.Fatal("zero item occurrence accepted")
	}
}

func TestPurchaseMerchantByNameRejectsUnresolvedCapacityWeightAndGoldBeforeAllocation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State, *MerchantOffers)
		want   string
	}{
		{name: "unresolved", mutate: nil, want: "canonical merchant offers required"},
		{name: "gold", mutate: func(s *State, _ *MerchantOffers) {
			p := s.Players["actor"]
			p.Body.Gold = 9
			s.Players["actor"] = p
		}, want: "냥입니다"},
		{name: "weight", mutate: func(_ *State, offers *MerchantOffers) {
			stock := (*offers)["merchant-1"][0]
			stock.Weight = 121
			(*offers)["merchant-1"][0] = stock
		}, want: "가질 수 없습니다"},
		{name: "capacity", mutate: func(s *State, _ *MerchantOffers) {
			p := s.Players["actor"]
			for i := 0; i < 151; i++ {
				id := fmt.Sprintf("carried-%03d", i)
				p.Items.Items[id] = Item{Object: LegacyObject{Name: id}}
				p.Items.Inventory = append(p.Items.Inventory, id)
			}
			s.Players["actor"] = p
		}, want: "가질 수 없습니다"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, offers := merchantPurchaseFixture()
			if tc.name == "unresolved" {
				offers = nil
			}
			if tc.mutate != nil {
				tc.mutate(&s, &offers)
			}
			before := s.clone()
			called := false
			next, result, err := s.PurchaseMerchantByName("actor", "상인", 1, "검", 1, offers, func() (string, error) {
				called = true
				return "should-not-allocate", nil
			})
			if tc.name == "unresolved" {
				if err == nil || called || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, MerchantPurchaseResult{}) || !reflect.DeepEqual(s, before) {
					t.Fatalf("unresolved result=%+v next=%+v err=%v called=%t", result, next, err, called)
				}
				return
			}
			if err != nil || result.Action != "merchant-purchase-rejected" || !strings.Contains(result.Response, tc.want) || called || !reflect.DeepEqual(next, before) {
				t.Fatalf("rejected result=%+v next=%+v err=%v called=%t", result, next, err, called)
			}
		})
	}
}

func TestApplyMerchantPurchaseRejectsStaleCandidateAtomically(t *testing.T) {
	s, offers := merchantPurchaseFixture()
	proposal, err := s.PlanMerchantPurchase("actor", "상인", 1, "검", 1, offers, merchantPurchaseAllocator("purchase"))
	if err != nil {
		t.Fatal(err)
	}
	changed := s.Players["actor"]
	changed.Body.Gold--
	s.Players["actor"] = changed
	if next, _, err := s.ApplyMerchantPurchase(proposal); err == nil || !reflect.DeepEqual(next, State{}) {
		t.Fatalf("stale candidate applied: next=%+v err=%v", next, err)
	}
}
