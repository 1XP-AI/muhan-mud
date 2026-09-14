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
