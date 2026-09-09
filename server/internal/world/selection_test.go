package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func selectionTestFlags(bits ...int) [8]byte {
	var flags [8]byte
	for _, bit := range bits {
		flags[bit/8] |= 1 << (bit % 8)
	}
	return flags
}

func selectionWorldFixture() (State, MerchantOffers) {
	merchantFlags := selectionTestFlags(merchantPurchaseFlag)
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			200: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 200, Name: "상점방"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"merchant-1", "ordinary", "merchant-2"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true},
		},
		NPCs: map[string]NPCState{
			"merchant-1": {Body: LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: merchantFlags, Carry: [10]int16{99}}},
			"ordinary":   {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: 200}},
			"merchant-2": {Body: LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: merchantFlags}},
		},
	}
	offers := MerchantOffers{
		"merchant-1": {
			{Name: "검", Value: 7, Weight: 1},
			{Name: "보석", Value: 25, Weight: 1, Contents: []LegacyObject{{Name: "조각", Weight: 1}}},
		},
		"merchant-2": nil,
	}
	return state, offers
}

func TestPlanSelectionUsesCanonicalVisibleNPCAndImmutableOffers(t *testing.T) {
	state, offers := selectionWorldFixture()
	before := state.clone()
	p, err := state.PlanSelection("actor", "상인", 1, offers)
	if err != nil {
		t.Fatal(err)
	}
	if p.NPCID != "merchant-1" || !p.Merchant || !p.CatalogReady || len(p.Items) != 2 || p.Items[0].Number != 1 || p.Items[0].ItemName != "검" || p.Items[0].Price != 10 || p.Items[1].Price != 25 {
		t.Fatalf("proposal=%+v", p)
	}
	wantResponse := fmt.Sprintf("상인의 물건들:\n1) %-22s    %d냥\n2) %-22s    %d냥\n\n", "검", 10, "보석", 25)
	if p.Response != wantResponse {
		t.Fatalf("response=%q want=%q", p.Response, wantResponse)
	}
	// A caller-side mutation after planning cannot alter the immutable
	// template snapshot used by Apply or its receipt projection.
	offers["merchant-1"][0].Name = "변조"
	offers["merchant-1"][1].Contents[0].Name = "변조된 조각"
	next, result, err := state.ApplySelection(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, before) || result.Changed || result.Response != wantResponse || result.Items[0].ItemName != "검" || result.Items[1].ItemName != "보석" {
		t.Fatalf("next=%+v result=%+v", next, result)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("selection mutated source state")
	}
}

func TestPlanSelectionUsesExactCaseInsensitiveVisibleOccurrenceAndNoLegacyCarry(t *testing.T) {
	state, offers := selectionWorldFixture()
	p, err := state.PlanSelection("actor", "상인", 2, offers)
	if err != nil || p.NPCID != "merchant-2" || p.NPCName != "상인" || !p.NoOp || p.Response != "상인은 팔 물건이 없습니다.\n" {
		t.Fatalf("proposal=%+v err=%v", p, err)
	}
	// Prefix and key-like inputs are not canonical display-name selectors.
	p, err = state.PlanSelection("actor", "상", 1, offers)
	if err != nil || !p.NoOp || p.TargetFound || p.Response != "그런 사람은 없습니다.\n" {
		t.Fatalf("prefix proposal=%+v err=%v", p, err)
	}
	npc := state.NPCs["merchant-1"]
	npc.Body.Keys[0] = "shop"
	state.NPCs["merchant-1"] = npc
	p, err = state.PlanSelection("actor", "shop", 1, offers)
	if err != nil || p.TargetFound || p.Response != "그런 사람은 없습니다.\n" {
		t.Fatalf("key proposal=%+v err=%v", p, err)
	}
}

func TestPlanSelectionVisibilityAndMerchantFlagBranchesAreReadOnlyNoOps(t *testing.T) {
	state, offers := selectionWorldFixture()
	state.NPCs = map[string]NPCState{"ordinary": state.NPCs["ordinary"]}
	state.Rooms[200] = RoomState{Resource: state.Rooms[200].Resource, PlayerIDs: []string{"actor"}, NPCIDs: []string{"ordinary"}}
	p, err := state.PlanSelection("actor", "경비", 1, nil)
	if err != nil || !p.TargetFound || p.Merchant || !p.NoOp || p.Response != "경비는 아무것도 없습니다.\n" {
		t.Fatalf("ordinary proposal=%+v err=%v", p, err)
	}
	_, result, err := state.ApplySelection(p)
	if err != nil || result.Changed || result.Items != nil {
		t.Fatalf("ordinary result=%+v err=%v", result, err)
	}

	state, offers = selectionWorldFixture()
	npc := state.NPCs["merchant-1"]
	npc.Body.Flags = selectionTestFlags(merchantPurchaseFlag, merchantNPCInvisibleFlag)
	state.NPCs["merchant-1"] = npc
	npc = state.NPCs["merchant-2"]
	npc.Body.Flags = selectionTestFlags(merchantPurchaseFlag, merchantNPCInvisibleFlag)
	state.NPCs["merchant-2"] = npc
	p, err = state.PlanSelection("actor", "상인", 1, offers)
	if err != nil || p.TargetFound || !p.NoOp || p.Response != "그런 사람은 없습니다.\n" {
		t.Fatalf("invisible proposal=%+v err=%v", p, err)
	}
}

func TestPlanSelectionFailsClosedForMissingCanonicalNPCOrOfferCatalog(t *testing.T) {
	state, offers := selectionWorldFixture()
	if _, err := state.PlanSelection("actor", "상인", 1, nil); !errors.Is(err, ErrSelectionMerchantOffersUnresolved) {
		t.Fatalf("nil catalog err=%v", err)
	}
	missing := MerchantOffers{"merchant-2": nil}
	if _, err := state.PlanSelection("actor", "상인", 1, missing); !errors.Is(err, ErrSelectionMerchantOffersUnresolved) {
		t.Fatalf("missing entry err=%v", err)
	}
	state.NPCs = nil
	if _, err := state.PlanSelection("actor", "상인", 1, offers); !errors.Is(err, ErrSelectionNPCStateUnresolved) {
		t.Fatalf("missing NPC state err=%v", err)
	}
	state, offers = selectionWorldFixture()
	offers["merchant-1"][0].Name = "unsafe\nname"
	if _, err := state.PlanSelection("actor", "상인", 1, offers); !errors.Is(err, ErrSelectionMerchantOffersInvalid) {
		t.Fatalf("unsafe catalog err=%v", err)
	}
}

func TestApplySelectionRejectsStaleProposalAtomically(t *testing.T) {
	state, offers := selectionWorldFixture()
	p, err := state.PlanSelection("actor", "상인", 1, offers)
	if err != nil {
		t.Fatal(err)
	}
	npc := state.NPCs["merchant-1"]
	npc.Body.Name = "새상인"
	state.NPCs["merchant-1"] = npc
	next, result, err := state.ApplySelection(p)
	if !errors.Is(err, ErrSelectionStaleProposal) || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, SelectionResult{}) {
		t.Fatalf("stale next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestSelectionResponseFormattingIsStable(t *testing.T) {
	items := []SelectionItem{{Number: 1, ItemName: "검", Price: 10}}
	if got := selectionRender("상인", true, items); !strings.HasSuffix(got, "\n\n") || !strings.Contains(got, "1) 검") {
		t.Fatalf("render=%q", got)
	}
}
