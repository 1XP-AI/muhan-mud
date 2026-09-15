package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func moonSetState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			7: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 7, Name: "숲"}},
				PlayerIDs: []string{"actor", "observer"},
			},
			1001: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1001, Name: "광장"}},
				PlayerIDs: []string{},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 7},
				Online: true,
				Items: &ItemCollection{
					Items: map[string]Item{
						"stone-1": {Object: LegacyObject{Name: "초인의 돌", Value: moonSetUnboundValue}},
						"stone-2": {Object: LegacyObject{Name: "초인의 돌", Value: moonSetUnboundValue}},
						"bound":   {Object: LegacyObject{Name: "초인의 돌", Value: 2000, Description: "옛장소의 광경이 어른거립니다.", Keys: [3]string{"", "옛장소"}}},
						"bag":     {Object: LegacyObject{Name: "가방"}, Contents: []string{"nested"}},
						"nested":  {Object: LegacyObject{Name: "초인의 돌", Value: moonSetUnboundValue}},
						"ready":   {Object: LegacyObject{Name: "초인의 돌", Value: moonSetUnboundValue}},
					},
					Inventory: []string{"stone-1", "stone-2", "bound", "bag"},
					Ready:     [20]string{"ready"},
				},
			},
			"observer": {
				Body:   LegacyMonster{Name: "Bob", Type: 0, RoomID: 7},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}, Inventory: []string{}},
			},
		},
	}
}

func moonSetSelectorState() State {
	s := moonSetState()
	actor := s.Players["actor"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"both":        {Object: LegacyObject{Name: "달빛", Keys: [3]string{"달", "", ""}, Value: moonSetUnboundValue}},
			"other":       {Object: LegacyObject{Name: "무관", Keys: [3]string{"없음", "", ""}, Value: moonSetUnboundValue}},
			"key-zero":    {Object: LegacyObject{Name: "무기", Keys: [3]string{"달빛열쇠", "", ""}, Value: moonSetUnboundValue}},
			"key-one":     {Object: LegacyObject{Name: "도구", Keys: [3]string{"", "달빛부적", ""}, Value: moonSetUnboundValue}},
			"key-two":     {Object: LegacyObject{Name: "물건", Keys: [3]string{"", "", "달빛돌"}, Value: moonSetUnboundValue}},
			"second-name": {Object: LegacyObject{Name: "달밤", Value: moonSetUnboundValue}},
		},
		Inventory: []string{"both", "other", "key-zero", "key-one", "key-two", "second-name"},
	}
	s.Players["actor"] = actor
	return s
}

func TestPlanApplyMoonSetBindsUnboundInventoryRoot(t *testing.T) {
	s := moonSetState()
	proposal, err := s.PlanMoonSet("actor", "초인의 돌", 1)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != MoonSetBind || !proposal.Changed || proposal.ItemID != "stone-1" || proposal.RoomID != 7 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyMoonSet(proposal)
	if err != nil {
		t.Fatal(err)
	}
	item := next.Players["actor"].Items.Items["stone-1"]
	if item.Object.Value != 7 || item.Object.Keys[1] != "숲" || item.Object.Description != "숲의 광경이 어른거립니다." {
		t.Fatalf("bound item=%+v", item.Object)
	}
	if result.Response != MoonSetBindResponse() || len(result.Events) != 2 || result.Events[0].Text != "\nAlice이 초인의 돌에 이곳의 장소를 기억시킵니다." {
		t.Fatalf("result=%+v", result)
	}
	if result.Events[1].Text != "\nAlice초인의 돌이 갑자기 밝은 빛을 내다 다시 투명해 집니다." {
		t.Fatalf("second event=%q", result.Events[1].Text)
	}
	if _, _, err := next.ApplyMoonSet(proposal); !errors.Is(err, ErrMoonSetStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	unbound := next.Players["actor"].Items.Items["stone-2"]
	if unbound.Object.Value != moonSetUnboundValue {
		t.Fatalf("other stone mutated: %+v", unbound.Object)
	}
}

func TestPlanMoonSetGatesUsageSquareBoundAndMissing(t *testing.T) {
	s := moonSetState()
	usage, err := s.PlanMoonSet("actor", "", 1)
	if err != nil || usage.Changed || usage.Action != MoonSetUsage || usage.Response != MoonSetUsageResponse {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}

	missing, err := s.PlanMoonSet("actor", "없는돌", 1)
	if err != nil || missing.Changed || missing.Action != MoonSetMissing || missing.Response != MoonSetMissingResponse {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
	nested, err := s.PlanMoonSet("actor", "초인의 돌", 4)
	if err != nil || nested.Changed || nested.Action != MoonSetMissing {
		t.Fatalf("nested/ready counted: %+v err=%v", nested, err)
	}

	bound, err := s.PlanMoonSet("actor", "초인의 돌", 3)
	if err != nil || bound.Changed || bound.Action != MoonSetBound || bound.ItemID != "bound" || bound.Response != MoonSetBoundResponse {
		t.Fatalf("bound=%+v err=%v", bound, err)
	}

	square := s.clone()
	actor := square.Players["actor"]
	actor.Body.RoomID = 1001
	square.Players["actor"] = actor
	squareRoom := square.Rooms[1001]
	squareRoom.PlayerIDs = []string{"actor"}
	square.Rooms[1001] = squareRoom
	forest := square.Rooms[7]
	forest.PlayerIDs = []string{"observer"}
	square.Rooms[7] = forest
	gated, err := square.PlanMoonSet("actor", "초인의 돌", 1)
	if err != nil || gated.Changed || gated.Action != MoonSetSquare || gated.Response != MoonSetSquareResponse {
		t.Fatalf("square=%+v err=%v", gated, err)
	}
}

func TestPlanMoonSetSelectsOccurrenceAndRejectsUnresolved(t *testing.T) {
	s := moonSetState()
	proposal, err := s.PlanMoonSet("actor", "초인의 돌", 2)
	if err != nil || proposal.ItemID != "stone-2" || !proposal.Changed {
		t.Fatalf("occurrence=%+v err=%v", proposal, err)
	}
	if _, err := s.PlanMoonSet("actor", "초인의 돌", 0); !errors.Is(err, ErrMoonSetInvalidOccurrence) {
		t.Fatalf("zero occurrence err=%v", err)
	}
	missingInventory := s.clone()
	actor := missingInventory.Players["actor"]
	actor.Items = nil
	missingInventory.Players["actor"] = actor
	if _, err := missingInventory.PlanMoonSet("actor", "초인의 돌", 1); !errors.Is(err, ErrMoonSetCanonicalInventoryNeeded) {
		t.Fatalf("unmigrated err=%v", err)
	}
	if _, err := s.PlanMoonSet("missing", "초인의 돌", 1); !errors.Is(err, ErrMoonSetActorAbsent) {
		t.Fatalf("absent err=%v", err)
	}
}

func TestPlanMoonSetMatchesNameAndKeyPrefixesInInventoryOrder(t *testing.T) {
	s := moonSetSelectorState()
	tests := []struct {
		name string
		want string
	}{
		{name: "달", want: "both"},
		{name: "달빛열", want: "key-zero"},
		{name: "달빛부", want: "key-one"},
		{name: "달빛돌", want: "key-two"},
	}
	for _, tt := range tests {
		proposal, err := s.PlanMoonSet("actor", tt.name, 1)
		if err != nil || proposal.Action != MoonSetBind || proposal.ItemID != tt.want {
			t.Fatalf("selector=%q proposal=%+v err=%v", tt.name, proposal, err)
		}
	}

	for _, tt := range []struct {
		occurrence int
		want       string
	}{{1, "both"}, {2, "key-zero"}, {3, "key-one"}, {4, "key-two"}, {5, "second-name"}} {
		proposal, err := s.PlanMoonSet("actor", "달", tt.occurrence)
		if err != nil || proposal.ItemID != tt.want || proposal.Occurrence != tt.occurrence {
			t.Fatalf("occurrence=%d proposal=%+v err=%v", tt.occurrence, proposal, err)
		}
	}
}

func TestPlanMoonSetVisibilitySkipsHiddenRootsUnlessPDINVI(t *testing.T) {
	hidden := moonSetSelectorState()
	actor := hidden.Players["actor"]
	item := actor.Items.Items["both"]
	item.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	actor.Items.Items["both"] = item
	hidden.Players["actor"] = actor

	ordinary, err := hidden.PlanMoonSet("actor", "달", 1)
	if err != nil || ordinary.ItemID != "key-zero" || ordinary.Action != MoonSetBind {
		t.Fatalf("ordinary hidden selection=%+v err=%v", ordinary, err)
	}

	detected := hidden.clone()
	actor = detected.Players["actor"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	detected.Players["actor"] = actor
	proposal, err := detected.PlanMoonSet("actor", "달", 1)
	if err != nil || proposal.ItemID != "both" || proposal.Action != MoonSetBind {
		t.Fatalf("PDINVI hidden selection=%+v err=%v", proposal, err)
	}
}

func TestPlanMoonSetMissingAndOutOfRangeRemainNoopReceipts(t *testing.T) {
	s := moonSetSelectorState()
	beforeBody := s.Players["actor"].Body
	beforeItems := s.Players["actor"].Items.clone()
	for _, tt := range []struct {
		name       string
		occurrence int
	}{{"없는키", 1}, {"달", 99}} {
		proposal, err := s.PlanMoonSet("actor", tt.name, tt.occurrence)
		if err != nil || proposal.Action != MoonSetMissing || proposal.Changed || proposal.ItemID != "" || len(proposal.Events) != 0 {
			t.Fatalf("name=%q occurrence=%d missing proposal=%+v err=%v", tt.name, tt.occurrence, proposal, err)
		}
	}
	if !reflect.DeepEqual(s.Players["actor"].Body, beforeBody) || !reflect.DeepEqual(*s.Players["actor"].Items, beforeItems) {
		t.Fatalf("missing lookup mutated actor state: before=%+v after=%+v", beforeItems, *s.Players["actor"].Items)
	}
}

func TestPlanMoonSetRejectsMalformedCanonicalIdentity(t *testing.T) {
	missingRoot := moonSetState()
	actor := missingRoot.Players["actor"]
	actor.Items.Inventory[0] = "missing"
	missingRoot.Players["actor"] = actor
	if _, err := missingRoot.PlanMoonSet("actor", "초인의", 1); err == nil {
		t.Fatal("unresolved inventory root was accepted")
	}

	emptyName := moonSetState()
	actor = emptyName.Players["actor"]
	item := actor.Items.Items["stone-1"]
	item.Object.Name = ""
	actor.Items.Items["stone-1"] = item
	emptyName.Players["actor"] = actor
	if _, err := emptyName.PlanMoonSet("actor", "초인의", 1); err == nil {
		t.Fatal("empty item identity was accepted")
	}
}

func TestMoonSetDescriptionRejectsOverflow(t *testing.T) {
	if _, err := moonSetDescription(""); !errors.Is(err, ErrMoonSetRoomNameInvalid) {
		t.Fatalf("empty err=%v", err)
	}
	longKey := strings.Repeat("a", MoonSetKeyMaxBytes+1)
	if _, err := moonSetDescription(longKey); !errors.Is(err, ErrMoonSetKeyTooLong) {
		t.Fatalf("key err=%v", err)
	}
	s := moonSetState()
	room := s.Rooms[7]
	room.Resource.Name = longKey
	s.Rooms[7] = room
	if _, err := s.PlanMoonSet("actor", "초인의 돌", 1); !errors.Is(err, ErrMoonSetKeyTooLong) {
		t.Fatalf("plan key err=%v", err)
	}
}

func TestApplyMoonSetRejectsUnchangedProposal(t *testing.T) {
	s := moonSetState()
	usage, err := s.PlanMoonSet("actor", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyMoonSet(usage)
	if !errors.Is(err, ErrMoonSetStaleProposal) || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, MoonSetResult{}) {
		t.Fatalf("unchanged apply next=%+v result=%+v err=%v", next, result, err)
	}
}
