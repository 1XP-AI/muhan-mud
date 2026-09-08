package world

import (
	"reflect"
	"testing"
)

func TestNPCLoginAndRespawnPreserveAndSpawnIdentities(t *testing.T) {
	for _, death := range []bool{false, true} {
		s := playerDeathFixture()
		roomID := int16(1)
		if death {
			roomID = 1008
		} else {
			p := s.Players["a"]
			p.Online = false
			s.Players["a"] = p
			r := s.Rooms[1]
			r.PlayerIDs = nil
			s.Rooms[1] = r
		}
		r := s.Rooms[roomID]
		r.NPCIDs = []string{"old"}
		r.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
		s.Rooms[roomID] = r
		s.NPCs = map[string]NPCState{"old": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: roomID, HPCurrent: 77}}}
		calls := 0
		alloc := func() (string, error) { calls++; return "fresh", nil }
		roll := func(lo, hi int) int { return lo }
		var next State
		var err error
		if death {
			next, _, err = s.PlanPlayerDeath("a", "a", 100, SceneOptions{}, npcSpawnCatalog{}, roll, alloc)
		} else {
			next, _, err = s.EnterSavedPlayerWithIDs("a", SceneOptions{}, npcSpawnCatalog{}, 100, roll, alloc)
		}
		if err != nil || calls != 1 || !reflect.DeepEqual(next.Rooms[roomID].NPCIDs, []string{"old", "fresh"}) || len(next.Rooms[roomID].Resource.Monsters) != 0 {
			t.Fatalf("death%v entry NPC IDs %+v err%v calls%d", death, next.Rooms[roomID].NPCIDs, err, calls)
		}
		if next.NPCs["old"].Body.HPCurrent != 77 || next.NPCs["old"].Enemies != nil || next.NPCs["fresh"].Enemies == nil || len(s.NPCs) != 1 {
			t.Fatal("NPC identity/state alias")
		}
	}
}

func TestNPCAdmissionIDCollisionReturnsNoPartialCandidate(t *testing.T) {
	for _, death := range []bool{false, true} {
		s := playerDeathFixture()
		roomID := int16(1)
		if death {
			roomID = 1008
		} else {
			p := s.Players["a"]
			p.Online = false
			s.Players["a"] = p
			r := s.Rooms[1]
			r.PlayerIDs = nil
			s.Rooms[1] = r
		}
		r := s.Rooms[roomID]
		r.NPCIDs = []string{"old"}
		r.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
		s.Rooms[roomID] = r
		s.NPCs = map[string]NPCState{"old": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: roomID}}}
		alloc := func() (string, error) { return "old", nil }
		roll := func(lo, hi int) int { return lo }
		if death {
			next, result, err := s.PlanPlayerDeath("a", "a", 100, SceneOptions{}, npcSpawnCatalog{}, roll, alloc)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, PlayerDeathResult{}) {
				t.Fatal("partial death")
			}
		} else {
			next, result, err := s.EnterSavedPlayerWithIDs("a", SceneOptions{}, npcSpawnCatalog{}, 100, roll, alloc)
			if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, RoomEntry{}) {
				t.Fatal("partial login")
			}
		}
	}
}
