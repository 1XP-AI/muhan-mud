package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func familyTalkFixture() State {
	member := func(name string, room int16) PlayerState {
		body := LegacyMonster{Name: name, Type: 0, RoomID: room}
		body.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
		body.Daily[FamilyDailySlot].Max = 2
		return PlayerState{Body: body, Online: true}
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "room-member", "outsider"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}, PlayerIDs: []string{"remote"}},
		},
		Players: map[string]PlayerState{
			"actor":       member("Actor", 1),
			"room-member": member("RoomMember", 1),
			"remote":      member("Remote", 2),
			"outsider":    {Body: LegacyMonster{Name: "Outsider", Type: 0, RoomID: 1}, Online: true},
		},
	}
}

func familyTalkCatalog() FamilyCatalog {
	return FamilyCatalog{Families: map[int16]FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "두목"},
	}}
}

func TestPlanFamilyTalkUsesDeterministicFamilyRecipientsWithoutMutation(t *testing.T) {
	state := familyTalkFixture()
	before := state
	result, err := state.PlanFamilyTalk("actor", "안녕하세요", familyTalkCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "family-talk" || result.FamilyID != 2 || result.FamilyName != "청룡" || result.Message != "안녕하세요" || !result.Broadcast || result.Response != "예. 좋습니다.\r\n" {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Events) != 3 || result.Events[0].RecipientID != "actor" || result.Events[1].RecipientID != "room-member" || result.Events[2].RecipientID != "remote" {
		t.Fatalf("event order=%+v", result.Events)
	}
	for _, event := range result.Events {
		if event.FamilyID != 2 || event.ActorID != "actor" || event.Message != "안녕하세요" || !strings.Contains(event.Text, "Actor>>>") {
			t.Fatalf("event=%+v", event)
		}
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("family talk mutated state")
	}
}

func TestPlanFamilyTalkDoesNotApplyPDMINVAndRequiresCatalog(t *testing.T) {
	state := familyTalkFixture()
	remote := state.Players["remote"]
	remote.Body.Flags[10/8] |= 1 << (10 % 8) // PDMINV is not a family-talk gate in C.
	state.Players["remote"] = remote
	result, err := state.PlanFamilyTalk("actor", "hello", familyTalkCatalog())
	if err != nil || len(result.Events) != 3 {
		t.Fatalf("PDMINV unexpectedly filtered family member: result=%+v err=%v", result, err)
	}
	if _, err := state.PlanFamilyTalk("actor", "hello", FamilyCatalog{}); !errors.Is(err, ErrFamilyCatalogUnavailable) {
		t.Fatalf("missing catalog err=%v", err)
	}
}

func TestPlanFamilyTalkHandlesNoMemberSilentEmptyAndInvalidMessage(t *testing.T) {
	state := familyTalkFixture()
	actor := state.Players["actor"]
	actor.Body.Flags[FamilyMemberFlag/8] &^= 1 << (FamilyMemberFlag % 8)
	state.Players["actor"] = actor
	result, err := state.PlanFamilyTalk("actor", "hello", familyTalkCatalog())
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "속해있지") {
		t.Fatalf("nonmember result=%+v err=%v", result, err)
	}
	state = familyTalkFixture()
	actor = state.Players["actor"]
	actor.Body.Flags[familyTalkSilentFlag/8] |= 1 << (familyTalkSilentFlag % 8)
	state.Players["actor"] = actor
	result, err = state.PlanFamilyTalk("actor", "hello", familyTalkCatalog())
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "막혀") {
		t.Fatalf("silent result=%+v err=%v", result, err)
	}
	state = familyTalkFixture()
	result, err = state.PlanFamilyTalk("actor", "   ", familyTalkCatalog())
	if err != nil || result.Broadcast || !strings.Contains(result.Response, "무슨 말을") {
		t.Fatalf("empty result=%+v err=%v", result, err)
	}
	if _, err := state.PlanFamilyTalk("actor", "bad\nmessage", familyTalkCatalog()); !errors.Is(err, ErrFamilyTalkMessageInvalid) {
		t.Fatalf("invalid message err=%v", err)
	}
}
