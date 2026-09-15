package world

import (
	"reflect"
	"strings"
	"testing"
)

func descriptionFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Description: "기대어 "},
				Online: true,
			},
		},
	}
}

func TestDescriptionProposalApplyStoresCanonicalTrailingSpaceAndClears(t *testing.T) {
	state := descriptionFixture()
	before := state.Players["a"].Body.Description
	proposal, err := state.PlanDescription("a", "꼿꼿이 서")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Description != "꼿꼿이 서 " || proposal.Cleared || proposal.Response != "당신은 이제부터 꼿꼿이 서 서 있습니다.\r\n" {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := state.ApplyDescription(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if state.Players["a"].Body.Description != before {
		t.Fatalf("planning/apply mutated input state: %q", state.Players["a"].Body.Description)
	}
	if result.Action != "description" || result.ActorID != "a" || result.Description != "꼿꼿이 서 " || result.Cleared || next.Players["a"].Body.Description != "꼿꼿이 서 " {
		t.Fatalf("next=%+v result=%+v", next.Players["a"], result)
	}

	clear, err := next.PlanDescription("a", "")
	if err != nil {
		t.Fatal(err)
	}
	cleared, clearResult, err := next.ApplyDescription(clear)
	if err != nil {
		t.Fatal(err)
	}
	if !clear.Cleared || clear.Description != "" || clearResult.Response != "당신은 서 있습니다.\r\n" || !clearResult.Cleared || cleared.Players["a"].Body.Description != "" {
		t.Fatalf("clear=%+v result=%+v state=%q", clear, clearResult, cleared.Players["a"].Body.Description)
	}
}

func TestDescriptionRejectsInvalidInputAndStaleProposalAtomically(t *testing.T) {
	state := descriptionFixture()
	for _, text := range []string{"bad\ntext", "bad\x00text", string([]byte{0xff}), strings.Repeat("a", MaxDescriptionBytes)} {
		if _, err := state.PlanDescription("a", text); err == nil {
			t.Fatalf("invalid description accepted: %q", text)
		}
	}
	proposal, err := state.PlanDescription("a", "새 설명")
	if err != nil {
		t.Fatal(err)
	}
	changed := state.clone()
	actor := changed.Players["a"]
	actor.Body.Name = "Changed"
	changed.Players["a"] = actor
	if _, _, err := changed.ApplyDescription(proposal); err == nil {
		t.Fatal("stale description proposal applied")
	}
	if !reflect.DeepEqual(changed.Players["a"].Body, actor.Body) || state.Players["a"].Body.Description != "기대어 " {
		t.Fatal("stale description apply mutated a snapshot")
	}
}
