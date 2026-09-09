package world

import (
	"reflect"
	"strings"
	"testing"
)

func groupTalkFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"leader", "one", "two"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]PlayerState{
			"leader": {
				Body:           LegacyMonster{Name: "Leader", Type: 0, RoomID: 1, Class: 4},
				Online:         true,
				FollowerIDs:    []string{"two", "one"},
				NPCFollowerIDs: []string{"wolf"},
				FollowerRefs: []EntityRef{
					{Kind: "player", ID: "two"},
					{Kind: "npc", ID: "wolf"},
					{Kind: "player", ID: "one"},
				},
			},
			"one": {
				Body:        LegacyMonster{Name: "One", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowingID: "leader",
			},
			"two": {
				Body:        LegacyMonster{Name: "Two", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowingID: "leader",
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {
				Body:              LegacyMonster{Name: "Wolf", Type: 1, RoomID: 1},
				FollowingPlayerID: "leader",
			},
		},
	}
}

func TestPlanGroupTalkUsesMixedFollowerRefsThenLeaderWithoutMutation(t *testing.T) {
	state := groupTalkFixture()
	before := state
	result, err := state.PlanGroupTalk("one", "hello group")
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "group-talk" || result.ActorID != "one" || result.LeaderID != "leader" || result.Message != "hello group" || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Events) != 4 {
		t.Fatalf("events=%+v", result.Events)
	}
	wantOrder := []EntityRef{{Kind: "player", ID: "two"}, {Kind: "npc", ID: "wolf"}, {Kind: "player", ID: "one"}, {Kind: "player", ID: "leader"}}
	for i, event := range result.Events {
		if event.RecipientKind != wantOrder[i].Kind || event.RecipientID != wantOrder[i].ID || event.ActorID != "one" || event.Message != "hello group" || !strings.Contains(event.Text, "One이 그룹원들에게") {
			t.Fatalf("event[%d]=%+v", i, event)
		}
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("group talk mutated the world")
	}
}

func TestPlanGroupTalkAppliesIgnoreAndDMVisibilityWithoutInventingRecipients(t *testing.T) {
	state := groupTalkFixture()
	two := state.Players["two"]
	two.Body.Flags[groupTalkIgnoreFlag/8] |= 1 << (groupTalkIgnoreFlag % 8)
	state.Players["two"] = two
	wolf := state.NPCs["wolf"]
	wolf.Body.Flags[groupTalkDMInvisibleFlag/8] |= 1 << (groupTalkDMInvisibleFlag % 8)
	state.NPCs["wolf"] = wolf
	result, err := state.PlanGroupTalk("one", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 2 || result.Events[0].RecipientID != "one" || result.Events[1].RecipientID != "leader" {
		t.Fatalf("events=%+v", result.Events)
	}
	if len(result.Notices) != 1 || !strings.Contains(result.Notices[0], "Two는") || !result.Broadcast {
		t.Fatalf("notices=%+v broadcast=%t", result.Notices, result.Broadcast)
	}

	// A caretaker bypasses PIGNOR, but PDMINV remains invisible.
	one := state.Players["one"]
	one.Body.Class = groupTalkCaretakerClass
	state.Players["one"] = one
	result, err = state.PlanGroupTalk("one", "hello")
	if err != nil || len(result.Events) != 3 || len(result.Notices) != 0 {
		t.Fatalf("caretaker result=%+v err=%v", result, err)
	}
}

func TestPlanGroupTalkHandlesEmptySilentAndNoVisibleGroups(t *testing.T) {
	state := groupTalkFixture()
	result, err := state.PlanGroupTalk("one", "   ")
	if err != nil || result.Broadcast || len(result.Events) != 0 || !strings.Contains(result.Response, "무슨말") {
		t.Fatalf("empty result=%+v err=%v", result, err)
	}
	one := state.Players["one"]
	one.Body.Flags[groupTalkSilentFlag/8] |= 1 << (groupTalkSilentFlag % 8)
	state.Players["one"] = one
	result, err = state.PlanGroupTalk("one", "hello")
	if err != nil || result.Broadcast || len(result.Events) != 0 || !strings.Contains(result.Response, "입이 막혀") {
		t.Fatalf("silent result=%+v err=%v", result, err)
	}

	state = groupTalkFixture()
	for id, player := range state.Players {
		if id == "leader" {
			continue
		}
		player.Body.Flags[groupTalkDMInvisibleFlag/8] |= 1 << (groupTalkDMInvisibleFlag % 8)
		state.Players[id] = player
	}
	npc := state.NPCs["wolf"]
	npc.Body.Flags[groupTalkDMInvisibleFlag/8] |= 1 << (groupTalkDMInvisibleFlag % 8)
	state.NPCs["wolf"] = npc
	result, err = state.PlanGroupTalk("one", "hello")
	if err != nil || result.Broadcast || len(result.Events) != 0 || !strings.Contains(result.Response, "그룹에") {
		t.Fatalf("invisible result=%+v err=%v", result, err)
	}
}

func TestPlanGroupTalkRejectsUnresolvedMixedFollowerOrderAndMalformedMessage(t *testing.T) {
	state := groupTalkFixture()
	leader := state.Players["leader"]
	leader.FollowerRefs = nil
	state.Players["leader"] = leader
	if _, err := state.PlanGroupTalk("one", "hello"); err == nil {
		t.Fatal("unresolved mixed follower order accepted")
	}
	state = groupTalkFixture()
	if _, err := state.PlanGroupTalk("one", "bad\nmessage"); err == nil {
		t.Fatal("control message accepted")
	}

	state = groupTalkFixture()
	leader = state.Players["leader"]
	leader.FollowerRefs = nil
	leader.NPCFollowerIDs = nil
	leader.FollowerIDs = nil
	state.Players["leader"] = leader
	for _, id := range []string{"one", "two"} {
		player := state.Players[id]
		player.FollowingID = ""
		state.Players[id] = player
	}
	npc := state.NPCs["wolf"]
	npc.FollowingPlayerID = ""
	state.NPCs["wolf"] = npc
	result, err := state.PlanGroupTalk("one", "")
	if err != nil || !strings.Contains(result.Response, "그룹에") {
		t.Fatalf("no-group result=%+v err=%v", result, err)
	}
}
