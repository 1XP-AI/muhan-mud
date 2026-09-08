package world

import (
	"reflect"
	"testing"
)

func TestNPCLogoutClearsOnlyRemainingActiveNonnegativeEnemies(t *testing.T) {
	for _, last := range []bool{false, true} {
		for _, damage := range []int32{-1, 0, 17} {
			s := npcCanonicalFixture()
			if !last {
				s.Players["other"] = PlayerState{Body: LegacyMonster{Name: "Other", RoomID: 1}, Online: true}
				r := s.Rooms[1]
				r.PlayerIDs = append(r.PlayerIDs, "other")
				s.Rooms[1] = r
			}
			target := NPCEnemy{Target: EntityRef{Kind: "player", ID: "player"}, Damage: damage}
			kept := NPCEnemy{Target: EntityRef{Kind: "npc", ID: "npc-b"}, Damage: 23}
			n := s.NPCs["npc-a"]
			n.Enemies = []NPCEnemy{target, kept}
			s.NPCs["npc-a"] = n
			s.NPCs["remote"] = NPCState{Body: LegacyMonster{Name: "Remote", Type: 1, RoomID: 2}, Enemies: []NPCEnemy{target, kept}}
			r := s.Rooms[2]
			r.NPCIDs = []string{"remote"}
			s.Rooms[2] = r
			s.ActiveNPCIDs = []string{"npc-a", "remote"}
			next, _, err := s.LeavePlayer("player")
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"npc-a", "remote"} {
				want := []NPCEnemy{target, kept}
				if damage >= 0 && (id == "remote" || !last) {
					want = []NPCEnemy{kept}
				}
				if !reflect.DeepEqual(next.NPCs[id].Enemies, want) {
					t.Fatalf("last=%v damage=%d npc=%s: got %+v want %+v", last, damage, id, next.NPCs[id].Enemies, want)
				}
			}
			if len(s.NPCs["npc-a"].Enemies) != 2 || !s.Players["player"].Online {
				t.Fatal("source mutated")
			}
		}
	}
}

func TestNPCLogoutUnresolvedActiveEnemyRejectsWholeCandidate(t *testing.T) {
	s := npcCanonicalFixture()
	s.NPCs["remote"] = NPCState{Body: LegacyMonster{Name: "Remote", Type: 1, RoomID: 2}}
	r := s.Rooms[2]
	r.NPCIDs = []string{"remote"}
	s.Rooms[2] = r
	s.ActiveNPCIDs = []string{"remote"}
	next, departure, err := s.LeavePlayer("player")
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(departure, RoomDeparture{}) || !s.Players["player"].Online {
		t.Fatal("unresolved cleanup exposed partial logout")
	}
	// An unresolved NPC removed by last-occupant departure is not traversed by C.
	s.ActiveNPCIDs = []string{"npc-a"}
	n := s.NPCs["npc-a"]
	n.Enemies = nil
	s.NPCs["npc-a"] = n
	if _, _, err := s.LeavePlayer("player"); err != nil {
		t.Fatal(err)
	}
}

func TestNPCLogoutLateUnknownDoesNotPublishEarlierCleanup(t *testing.T) {
	s := npcCanonicalFixture()
	s.Players["other"] = PlayerState{Body: LegacyMonster{Name: "Other", RoomID: 1}, Online: true}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "other")
	s.Rooms[1] = r
	n := s.NPCs["npc-a"]
	n.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: 1}}
	s.NPCs["npc-a"] = n
	s.ActiveNPCIDs = []string{"npc-a", "npc-b"}
	next, _, err := s.LeavePlayer("player")
	if err == nil || !reflect.DeepEqual(next, State{}) || len(s.NPCs["npc-a"].Enemies) != 1 || !s.Players["player"].Online {
		t.Fatal("late unknown enemy list leaked earlier cleanup")
	}
}
