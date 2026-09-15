package world

import (
	"reflect"
	"strings"
	"testing"
)

func socialGroupCommandFixture() State {
	const roomID int16 = 1
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			roomID: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: roomID}},
				PlayerIDs: []string{"leader", "actor", "actor-decoy", "visible-player", "hidden-player"},
				NPCIDs:    []string{"visible-npc", "hidden-npc"},
			},
		},
		Players: map[string]PlayerState{
			"leader": {
				Body:   LegacyMonster{Name: "Leader", Type: 0, RoomID: roomID, HPCurrent: 100, MPCurrent: 50},
				Online: true,
				FollowerIDs: []string{
					"actor", "visible-player", "hidden-player",
				},
				NPCFollowerIDs: []string{"visible-npc", "hidden-npc"},
				FollowerRefs: []EntityRef{
					{Kind: "npc", ID: "visible-npc"},
					{Kind: "player", ID: "hidden-player"},
					{Kind: "player", ID: "visible-player"},
					{Kind: "npc", ID: "hidden-npc"},
					{Kind: "player", ID: "actor"},
				},
			},
			"actor": {
				Body:        LegacyMonster{Name: "Actor", Type: 0, RoomID: roomID, HPCurrent: 80, MPCurrent: 40},
				Online:      true,
				FollowingID: "leader",
				FollowerIDs: []string{"actor-decoy"},
				FollowerRefs: []EntityRef{
					{Kind: "player", ID: "actor-decoy"},
				},
			},
			"actor-decoy": {
				Body:        LegacyMonster{Name: "Actor Decoy", Type: 0, RoomID: roomID, HPCurrent: 1, MPCurrent: 1},
				Online:      true,
				FollowingID: "actor",
			},
			"visible-player": {
				Body:        LegacyMonster{Name: "Visible Player", Type: 0, RoomID: roomID, HPCurrent: 70, MPCurrent: 30},
				Online:      true,
				FollowingID: "leader",
			},
			"hidden-player": {
				Body:        LegacyMonster{Name: "Hidden Player", Type: 0, RoomID: roomID, HPCurrent: 60, MPCurrent: 20},
				Online:      true,
				FollowingID: "leader",
			},
		},
		NPCs: map[string]NPCState{
			"visible-npc": {
				Body:              LegacyMonster{Name: "Visible NPC", Type: 1, RoomID: roomID, HPCurrent: 45, MPCurrent: 15},
				FollowingPlayerID: "leader",
			},
			"hidden-npc": {
				Body:              LegacyMonster{Name: "Hidden NPC", Type: 1, RoomID: roomID, HPCurrent: 35, MPCurrent: 10},
				FollowingPlayerID: "leader",
			},
		},
	}
}

func setSocialGroupPDMINV(body *LegacyMonster) {
	body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
}

func TestPlayerGroupUsesFollowingLeaderAndCanonicalMixedOrder(t *testing.T) {
	state := socialGroupCommandFixture()
	hiddenPlayer := state.Players["hidden-player"]
	setSocialGroupPDMINV(&hiddenPlayer.Body)
	state.Players["hidden-player"] = hiddenPlayer
	hiddenNPC := state.NPCs["hidden-npc"]
	setSocialGroupPDMINV(&hiddenNPC.Body)
	state.NPCs["hidden-npc"] = hiddenNPC

	before := state.clone()
	got, err := state.PlayerGroup("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Leader") {
		t.Fatalf("leader row missing: %q", got)
	}
	if strings.Contains(got, "Actor Decoy") {
		t.Fatalf("actor's follower list was used instead of following leader: %q", got)
	}
	if strings.Contains(got, "Hidden Player") || strings.Contains(got, "Hidden NPC") {
		t.Fatalf("PDMINV follower was rendered: %q", got)
	}
	visibleNPC := strings.Index(got, "Visible NPC")
	visiblePlayer := strings.Index(got, "Visible Player")
	actor := strings.Index(got, "Actor")
	if visibleNPC < 0 || visiblePlayer < 0 || actor < 0 || visibleNPC > visiblePlayer || visiblePlayer > actor {
		t.Fatalf("canonical mixed order was not preserved: %q", got)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("PlayerGroup mutated canonical state")
	}
}

func TestPlayerGroupReturnsSourceNoGroupWhenNoVisibleFollowerExists(t *testing.T) {
	state := socialGroupCommandFixture()
	leader := state.Players["leader"]
	for _, ref := range mixedFollowerRefs(leader) {
		switch ref.Kind {
		case "player":
			follower := state.Players[ref.ID]
			setSocialGroupPDMINV(&follower.Body)
			state.Players[ref.ID] = follower
		case "npc":
			npc := state.NPCs[ref.ID]
			setSocialGroupPDMINV(&npc.Body)
			state.NPCs[ref.ID] = npc
		}
	}

	got, err := state.PlayerGroup("actor")
	if err != nil {
		t.Fatal(err)
	}
	if got != "당신은 그룹에 속해 있지 않습니다.\r\n" {
		t.Fatalf("no-group response=%q", got)
	}
}
