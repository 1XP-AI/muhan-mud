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
