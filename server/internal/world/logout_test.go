package world

import (
	"reflect"
	"testing"
)

func TestLeavePlayerPreservesGameStateAndRemovesOccupancy(t *testing.T) {
	s := playerDeathFixture()
	next, departure, err := s.LeavePlayer("a")
	if err != nil || next.Players["a"].Online || len(next.Rooms[1].PlayerIDs) != 0 || !departure.DeactivateMonsters {
		t.Fatalf("%+v %v", departure, err)
	}
	want := s.Players["a"]
	want.Online = false
	if !reflect.DeepEqual(next.Players["a"], want) || !reflect.DeepEqual(next.Rooms[1].Items, s.Rooms[1].Items) || !s.Players["a"].Online {
		t.Fatal("game state changed")
	}
	next.Players["a"].Items.Items["changed"] = Item{}
	if len(s.Players["a"].Items.Items) != 1 {
		t.Fatal("aliased items")
	}
}

func TestLeavePlayerKeepsOtherOccupants(t *testing.T) {
	s := playerDeathFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1}, Online: true}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	next, departure, err := s.LeavePlayer("a")
	if err != nil || departure.DeactivateMonsters || !reflect.DeepEqual(next.Rooms[1].PlayerIDs, []string{"b"}) {
		t.Fatalf("%+v %v", departure, err)
	}
	if got, _, err := next.LeavePlayer("a"); err == nil || !reflect.DeepEqual(got, State{}) {
		t.Fatal("repeated logout accepted without receipt")
	}
}

func TestLeavePlayerDetachesFollowerEdgesBeforeCommit(t *testing.T) {
	s, err := followFixture().FollowPlayer("one", "leader")
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := s.LeavePlayer("leader")
	if err != nil || next.Players["leader"].FollowerIDs != nil || next.Players["one"].FollowingID != "" || !next.Players["one"].Online {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func resolveDMFollowLogoutDomain(s State) State {
	next := s.clone()
	for id, npc := range next.NPCs {
		if npc.Enemies == nil {
			npc.Enemies = []NPCEnemy{}
			next.NPCs[id] = npc
		}
	}
	if next.ActiveNPCIDs == nil {
		next.ActiveNPCIDs = []string{}
	}
	return next
}

func TestLeavePlayerClearsMDMFOLFlagsAndReciprocalEdges(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil || attached.NPCs["wolf-1"].FollowingPlayerID != "dm" || !PlayerFlagSet(attached.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatalf("attached=%+v err=%v", attached.NPCs["wolf-1"], err)
	}
	next, _, err := attached.LeavePlayer("dm")
	if err != nil {
		t.Fatal(err)
	}
	npc := next.NPCs["wolf-1"]
	dm := next.Players["dm"]
	if npc.FollowingPlayerID != "" || PlayerFlagSet(npc.Body, npcDMFollowFlag) || dm.NPCFollowerIDs != nil || dm.FollowerRefs != nil || dm.Online {
		t.Fatalf("logout left MDMFOL edge npc=%+v dm=%+v", npc, dm)
	}
	if attached.NPCs["wolf-1"].FollowingPlayerID != "dm" || !PlayerFlagSet(attached.NPCs["wolf-1"].Body, npcDMFollowFlag) || !attached.Players["dm"].Online {
		t.Fatal("LeavePlayer mutated the attached snapshot")
	}
	if _, _, err := next.LeavePlayer("dm"); err == nil {
		t.Fatal("repeated logout accepted")
	}
	if _, _, err := next.ApplyDMFollow(proposal); err == nil {
		t.Fatal("stale MDMFOL proposal recommitted after logout")
	}
}

func TestLeavePlayerRejectsUnmigratedMDMFOLEnemiesAndActiveOrder(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil {
		t.Fatal(err)
	}

	missingEnemies := attached.clone()
	npc := missingEnemies.NPCs["wolf-1"]
	npc.Enemies = nil
	missingEnemies.NPCs["wolf-1"] = npc
	got, departure, err := missingEnemies.LeavePlayer("dm")
	if err == nil || !reflect.DeepEqual(got, State{}) || !reflect.DeepEqual(departure, RoomDeparture{}) ||
		!missingEnemies.Players["dm"].Online || !PlayerFlagSet(missingEnemies.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatalf("unmigrated enemies next=%+v departure=%+v err=%v", got, departure, err)
	}

	missingActive := attached.clone()
	missingActive.ActiveNPCIDs = nil
	got, departure, err = missingActive.LeavePlayer("dm")
	if err == nil || !reflect.DeepEqual(got, State{}) || !reflect.DeepEqual(departure, RoomDeparture{}) ||
		!missingActive.Players["dm"].Online || missingActive.NPCs["wolf-1"].FollowingPlayerID != "dm" {
		t.Fatalf("unmigrated active next=%+v departure=%+v err=%v", got, departure, err)
	}
}
