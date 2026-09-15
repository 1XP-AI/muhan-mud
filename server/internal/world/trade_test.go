package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type tradeCatalog struct{ objects map[int16]LegacyObject }

func (c tradeCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{}, fmt.Errorf("monster lookup not expected")
}
func (c tradeCatalog) Object(id int16) (LegacyObject, error) {
	object, ok := c.objects[id]
	if !ok {
		return LegacyObject{}, fmt.Errorf("missing object %d", id)
	}
	return object, nil
}

func tradeFixture(t *testing.T, reward *LegacyObject) State {
	t.Helper()
	var npcFlags [8]byte
	npcFlags[npcTradeFlag/8] |= 1 << (npcTradeFlag % 8)
	wanted := LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13, ShotsCurrent: 1}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			200: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 200, Name: "교역방"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"merchant", "merchant-2"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Stats: [5]byte{10}},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{"offered": {Object: LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13, ShotsMax: 10, ShotsCurrent: 10}}}, Inventory: []string{"offered"}},
			},
		},
		NPCs: map[string]NPCState{
			"merchant": {
				Body:        LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: npcFlags},
				TradeOffers: []NPCTradeOffer{{Wanted: wanted, Reward: reward}},
			},
			"merchant-2": {
				Body:        LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: npcFlags},
				TradeOffers: []NPCTradeOffer{{Wanted: LegacyObject{Name: "다른물건", Keys: [3]string{"other"}, Type: 13}, Reward: reward}},
			},
		},
	}
}

func TestTradeNPCByNameConsumesWantedAndMaterializesRewardAtomically(t *testing.T) {
	reward := &LegacyObject{Name: "보상검", Keys: [3]string{"reward"}, Type: 13, Value: 20}
	s := tradeFixture(t, reward)
	before := s.clone()
	allocated := 0
	next, result, err := s.TradeNPCByName("actor", "사과", 1, "상인", 1, func() (string, error) {
		allocated++
		return "reward-1", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 1 || !result.Rewarded || result.RewardRootID != "reward-1" || result.OfferedItemID != "offered" {
		t.Fatalf("result=%+v allocated=%d", result, allocated)
	}
	if _, ok := next.Players["actor"].Items.Items["offered"]; ok || containsString(next.Players["actor"].Items.Inventory, "offered") {
		t.Fatal("offered item was not consumed")
	}
	got, ok := next.Players["actor"].Items.Items["reward-1"]
	if !ok || got.Object.Name != "보상검" || !containsString(next.Players["actor"].Items.Inventory, "reward-1") {
		t.Fatalf("reward=%+v inventory=%v", got, next.Players["actor"].Items.Inventory)
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("trade mutated source snapshot")
	}
}

func TestTradeNPCByNameNoRewardConsumesItemWithoutAllocating(t *testing.T) {
	s := tradeFixture(t, nil)
	allocated := 0
	next, result, err := s.TradeNPCByName("actor", "사과", 1, "상인", 1, func() (string, error) {
		allocated++
		return "unexpected", nil
	})
	if err != nil || result.Rewarded || allocated != 0 {
		t.Fatalf("result=%+v err=%v allocated=%d", result, err, allocated)
	}
	if len(next.Players["actor"].Items.Inventory) != 0 {
		t.Fatalf("inventory=%v", next.Players["actor"].Items.Inventory)
	}
}

func TestImportNPCTradeOffersUsesCarryPairsAndRejectsUnresolvedTemplates(t *testing.T) {
	s := tradeFixture(t, nil)
	npc := s.NPCs["merchant"]
	npc.TradeOffers = nil
	npc.Body.Carry = [10]int16{1, 2, 1, 0, 0, 3, 0, 4, 0, 0}
	s.NPCs["merchant"] = npc
	otherNPC := s.NPCs["merchant-2"]
	otherNPC.TradeOffers = nil
	s.NPCs["merchant-2"] = otherNPC
	catalog := tradeCatalog{objects: map[int16]LegacyObject{
		1: {Name: "사과", Keys: [3]string{"apple"}, Type: 13},
		2: {Name: "빵", Keys: [3]string{"bread"}, Type: 13},
		3: {Name: "검", Type: 13},
		4: {Name: "방패", Type: 5},
	}}
	next, err := s.ImportNPCTradeOffers(catalog)
	if err != nil {
		t.Fatal(err)
	}
	offers := next.NPCs["merchant"].TradeOffers
	if len(offers) != 2 || offers[0].Wanted.Name != "사과" || offers[0].Reward == nil || offers[0].Reward.Name != "검" || offers[1].Wanted.Name != "빵" || offers[1].Reward != nil {
		t.Fatalf("offers=%+v", offers)
	}
	broken := s.clone()
	brokenNPC := broken.NPCs["merchant"]
	brokenNPC.Body.Carry[0] = 99
	broken.NPCs["merchant"] = brokenNPC
	if _, err := broken.ImportNPCTradeOffers(catalog); err == nil {
		t.Fatal("unresolved trade template accepted")
	}
}

func TestTradeNPCByNameRejectsUnsafeOrAmbiguousBranchesWithoutMutation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*State)
		npc    string
		want   string
	}{
		{name: "unknown NPC", npc: "없음", want: "그것은 여기 없습니다"},
		{name: "wrong item", mutate: func(s *State) {
			p := s.Players["actor"]
			item := p.Items.Items["offered"]
			item.Object.Name = "다른것"
			item.Object.Keys[0] = "no"
			p.Items.Items["offered"] = item
			s.Players["actor"] = p
		}, want: "당신은 그런 물건을 갖고 있지 않습니다"},
		{name: "named item", mutate: func(s *State) {
			p := s.Players["actor"]
			item := p.Items.Items["offered"]
			item.Object.Flags[tradeNamedObjectFlag/8] |= 1 << (tradeNamedObjectFlag % 8)
			p.Items.Items["offered"] = item
			s.Players["actor"] = p
		}, want: "교역할 수 있는 물건이 아닙니다"},
		{name: "damaged item", mutate: func(s *State) {
			p := s.Players["actor"]
			item := p.Items.Items["offered"]
			item.Object.Type = 1
			item.Object.ShotsCurrent = 1
			item.Object.ShotsMax = 10
			p.Items.Items["offered"] = item
			s.Players["actor"] = p
		}, want: "난 그런거 필요없어요"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			before := s.clone()
			npcName := tc.npc
			if npcName == "" {
				npcName = "상인"
			}
			next, result, err := s.TradeNPCByName("actor", "사과", 1, npcName, 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Response, tc.want) {
				t.Fatalf("response=%q want=%q", result.Response, tc.want)
			}
			if !reflect.DeepEqual(next, before) {
				t.Fatal("rejected trade mutated state")
			}
		})
	}
}

func TestPlanTradePrintsCAskWhoWithoutLookingAtItems(t *testing.T) {
	s := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	player := s.Players["actor"]
	player.Items = nil
	player.Body.Inventory = []LegacyObject{{Name: "legacy"}}
	s.Players["actor"] = player
	before := s.clone()
	allocated := 0
	proposal, err := s.PlanTrade("actor", "", 1, "", 1, func() (string, error) {
		allocated++
		return "unexpected", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Changed || proposal.Result.Action != TradeAskWhoAction || proposal.Result.Response != TradeAskWhoResponse || allocated != 0 {
		t.Fatalf("proposal=%+v allocated=%d", proposal, allocated)
	}
	next, result, err := s.ApplyTrade(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != TradeAskWhoAction || result.Response != TradeAskWhoResponse || result.Changed {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(next, before) || !reflect.DeepEqual(s, before) {
		t.Fatal("ask-who mutated source or applied state")
	}
}

func TestPlanTradePrintsCUsageWhenNPCNameMissing(t *testing.T) {
	s := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	before := s.clone()
	proposal, err := s.PlanTrade("actor", "사과", 1, "", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Changed || proposal.Result.Action != TradeUsageAction || proposal.Result.Response != TradeUsageResponse {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyTrade(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != TradeUsageAction || result.Response != TradeUsageResponse || result.Changed {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(next, before) {
		t.Fatal("usage mutated state")
	}
}

func TestPlanApplyTradeAllocatesOnceAndReplaysWithoutRecommit(t *testing.T) {
	s := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13, Keys: [3]string{"reward"}})
	before := s.clone()
	allocated := 0
	proposal, err := s.PlanTrade("actor", "사과", 1, "상인", 1, func() (string, error) {
		allocated++
		return "reward-1", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 1 || !proposal.Changed || proposal.Result.Action != TradeNPCItemAction {
		t.Fatalf("proposal=%+v allocated=%d", proposal, allocated)
	}
	next, result, err := s.ApplyTrade(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 1 || !result.Rewarded || result.RewardRootID != "reward-1" || result.Changed != true {
		t.Fatalf("result=%+v allocated=%d", result, allocated)
	}
	if _, ok := next.Players["actor"].Items.Items["offered"]; ok {
		t.Fatal("offered item survived apply")
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("plan/apply mutated source snapshot")
	}
	if _, _, err := next.ApplyTrade(proposal); !errors.Is(err, ErrTradeStaleProposal) {
		t.Fatalf("stale apply err=%v", err)
	}
}

func TestPlanTradeFailClosedUnmigratedNPCAndItems(t *testing.T) {
	missingItems := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	actor := missingItems.Players["actor"]
	actor.Items = nil
	actor.Body.Inventory = []LegacyObject{{Name: "legacy"}}
	missingItems.Players["actor"] = actor
	if _, err := missingItems.PlanTrade("actor", "사과", 1, "상인", 1, nil); !errors.Is(err, ErrTradeItemsUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}

	unmigratedNPC := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	npc := unmigratedNPC.NPCs["merchant"]
	npc.TradeOffers = nil
	unmigratedNPC.NPCs["merchant"] = npc
	if _, err := unmigratedNPC.PlanTrade("actor", "사과", 1, "상인", 1, nil); !errors.Is(err, ErrTradeOffersUnmigrated) {
		t.Fatalf("nil offers err=%v", err)
	}
}

func TestSelectTradeInventoryRootMatchesEqualPrefixesOnceAndVisibility(t *testing.T) {
	var invisible [8]byte
	invisible[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	c := ItemCollection{
		Items: map[string]Item{
			"name":   {Object: LegacyObject{Name: "NeedleName"}},
			"key0":   {Object: LegacyObject{Name: "ZeroAlias", Keys: [3]string{"needle-zero", "", ""}}},
			"key1":   {Object: LegacyObject{Name: "OneAlias", Keys: [3]string{"", "needle-one", ""}}},
			"key2":   {Object: LegacyObject{Name: "TwoAlias", Keys: [3]string{"", "", "needle-two"}}},
			"mixed":  {Object: LegacyObject{Name: "NeedleMixed", Keys: [3]string{"needle-mixed", "", ""}}},
			"hidden": {Object: LegacyObject{Name: "NeedleHidden", Keys: [3]string{"needle-hidden", "", ""}, Flags: invisible}},
			"parent": {Object: LegacyObject{Name: "Box"}, Contents: []string{"child"}},
			"child":  {Object: LegacyObject{Name: "NeedleChild", Keys: [3]string{"needle-child", "", ""}}},
			"ready":  {Object: LegacyObject{Name: "ReadyNeedle", Keys: [3]string{"ready-needle", "", ""}}},
		},
		Inventory: []string{"name", "key0", "key1", "key2", "mixed", "hidden", "parent"},
		Ready:     [20]string{0: "ready"},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		occurrence int
		wantID     string
	}{
		{name: "NEEDLEN", occurrence: 1, wantID: "name"},
		{name: "NEEDLE-ZERO", occurrence: 1, wantID: "key0"},
		{name: "NEEDLE-ONE", occurrence: 1, wantID: "key1"},
		{name: "NEEDLE-TWO", occurrence: 1, wantID: "key2"},
		// mixed matches through both its display name and key[0], but is
		// counted once after the four preceding roots.
		{name: "NeEdLe", occurrence: 5, wantID: "mixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := selectTradeInventoryRoot(c, tc.name, tc.occurrence, false)
			if err != nil || id != tc.wantID {
				t.Fatalf("selector=%q occurrence=%d id=%q err=%v want=%q", tc.name, tc.occurrence, id, err, tc.wantID)
			}
		})
	}
	for _, tc := range []struct {
		name       string
		occurrence int
	}{
		{name: "NEEDLE", occurrence: 6},
		{name: "NEEDLE-CHILD", occurrence: 1},
		{name: "READY", occurrence: 1},
	} {
		if _, err := selectTradeInventoryRoot(c, tc.name, tc.occurrence, false); err == nil {
			t.Fatalf("ordinary selector=%q occurrence=%d unexpectedly matched hidden/nested/ready root", tc.name, tc.occurrence)
		}
	}
	id, err := selectTradeInventoryRoot(c, "NEEDLE", 6, true)
	if err != nil || id != "hidden" {
		t.Fatalf("PDINVI hidden selector id=%q err=%v", id, err)
	}
}

func TestSelectTradeNPCMatchesEqualPrefixesOrderAndVisibility(t *testing.T) {
	s := tradeFixture(t, nil)
	first := s.NPCs["merchant"]
	first.Body.Name = "Keeper"
	first.Body.Keys = [3]string{"vendor-first", "", ""}
	first.Body.Flags[npcInvisibleFlag/8] |= 1 << (npcInvisibleFlag % 8)
	s.NPCs["merchant"] = first
	second := s.NPCs["merchant-2"]
	second.Body.Name = "Keeper"
	second.Body.Keys = [3]string{"vendor-second", "", ""}
	second.TradeOffers[0].Wanted = first.TradeOffers[0].Wanted
	s.NPCs["merchant-2"] = second

	id, found, err := s.selectTradeNPC("actor", "VEND", 1)
	if err != nil || !found || id != "merchant-2" {
		t.Fatalf("ordinary hidden NPC id=%q found=%t err=%v", id, found, err)
	}
	actor := s.Players["actor"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["actor"] = actor
	id, found, err = s.selectTradeNPC("actor", "VEND", 1)
	if err != nil || !found || id != "merchant" {
		t.Fatalf("PDINVI NPC id=%q found=%t err=%v", id, found, err)
	}

	// find_crt skips caretaker-class PDMINV identities even for a detector.
	first = s.NPCs["merchant"]
	first.Body.Class = lookAtCaretakerClass
	first.Body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
	s.NPCs["merchant"] = first
	id, found, err = s.selectTradeNPC("actor", "VEND", 1)
	if err != nil || !found || id != "merchant-2" {
		t.Fatalf("caretaker PDMINV NPC id=%q found=%t err=%v", id, found, err)
	}

	broken := s.clone()
	room := broken.Rooms[200]
	room.NPCIDs = append(room.NPCIDs, "missing")
	broken.Rooms[200] = room
	if _, _, err := broken.selectTradeNPC("actor", "VEND", 1); !errors.Is(err, ErrTradeNPCUnresolved) {
		t.Fatalf("unresolved NPC err=%v", err)
	}
}

func TestTradeUsesPrefixSelectorsButExactCatalogIdentity(t *testing.T) {
	s := tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	npc := s.NPCs["merchant"]
	npc.Body.Name = "Keeper"
	npc.Body.Keys = [3]string{"vendor", "", ""}
	s.NPCs["merchant"] = npc
	item := s.Players["actor"].Items.Items["offered"]
	item.Object.Keys[0] = "APPLE-ALIAS"
	player := s.Players["actor"]
	player.Items.Items["offered"] = item
	s.Players["actor"] = player
	next, result, err := s.TradeNPCByName("actor", "APP", 1, "VEND", 1, func() (string, error) {
		t.Fatal("catalog mismatch allocated a reward")
		return "unexpected", nil
	})
	if err != nil || result.OfferedItemID != "offered" || result.Action != TradeRejectedAction || !strings.Contains(result.Response, "난 그런거 필요없어요") {
		t.Fatalf("prefix-selected exact mismatch next=%+v result=%+v err=%v", next, result, err)
	}
	if _, ok := next.Players["actor"].Items.Items["offered"]; !ok {
		t.Fatal("catalog mismatch consumed the offered item")
	}

	// A case-folded display/key prefix selects the canonical root, after
	// which the C name/key[0] identity remains exact and the exchange commits.
	s = tradeFixture(t, &LegacyObject{Name: "보상검", Type: 13})
	npc = s.NPCs["merchant"]
	npc.Body.Name = "Keeper"
	npc.Body.Keys = [3]string{"vendor", "", ""}
	s.NPCs["merchant"] = npc
	next, result, err = s.TradeNPCByName("actor", "APPLE", 1, "VEND", 1, func() (string, error) {
		return "reward-prefix", nil
	})
	if err != nil || result.Action != TradeNPCItemAction || result.OfferedItemID != "offered" || next.Players["actor"].Items.Items["reward-prefix"].Object.Name != "보상검" {
		t.Fatalf("prefix trade next=%+v result=%+v err=%v", next, result, err)
	}
}
