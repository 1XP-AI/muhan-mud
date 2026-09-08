package world

import (
	"reflect"
	"testing"
)

func playerDeathFixture() State {
	s := stateFixture()
	s.War = &FamilyWar{}
	r := s.Rooms[1]
	r.Items = &ItemCollection{Items: map[string]Item{}}
	s.Rooms[1] = r
	s.Rooms[1008] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1008}}, Items: &ItemCollection{Items: map[string]Item{}}}
	p := s.Players["a"]
	p.Body.Class = 4
	p.Body.Level = 1
	p.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	p.Body.HPMax = 100
	p.Body.HPCurrent = 0
	p.Body.MPMax = 80
	p.Body.MPCurrent = 2
	p.Items = &ItemCollection{Items: map[string]Item{"weapon": {Object: LegacyObject{Name: "sword"}}}, Ready: [20]string{19: "weapon"}}
	s.Players["a"] = p
	return s
}

func TestPlayerDeathSelfCommitsOneCandidate(t *testing.T) {
	s := playerDeathFixture()
	next, result, err := s.PlanPlayerDeath("a", "a", 100, SceneOptions{}, nil, func(lo, hi int) int { return lo }, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := next.Players["a"]
	if p.Body.RoomID != 1008 || p.Body.HPCurrent != 100 || p.Body.MPCurrent != 8 || p.Body.Timers[11].Interval != 7*86400 || len(p.Items.Items) != 0 || len(next.Rooms[1].Items.Items) != 1 || len(next.Rooms[1].PlayerIDs) != 0 || next.Rooms[1008].PlayerIDs[0] != "a" || !result.BroadcastDeath {
		t.Fatalf("%+v %+v", p, result)
	}
	if s.Players["a"].Body.HPCurrent != 0 || len(s.Players["a"].Items.Items) != 1 || len(s.Rooms[1].Items.Items) != 0 {
		t.Fatal("mutated input")
	}
}

func TestPlayerDeathLateEntryFailureIsAtomic(t *testing.T) {
	s := playerDeathFixture()
	r := s.Rooms[1008]
	r.Resource.BeenHere = 2147483647
	s.Rooms[1008] = r
	next, result, err := s.PlanPlayerDeath("a", "a", 100, SceneOptions{}, nil, func(lo, hi int) int { return lo }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, PlayerDeathResult{}) {
		t.Fatal("partial death")
	}
}

func TestPlayerDeathWarLeaderAndSurvival(t *testing.T) {
	for _, survival := range []bool{false, true} {
		s := playerDeathFixture()
		s.War = &FamilyWar{Active: 2*16 + 3, CalledBy: 2, CalledAgainst: 3}
		v := s.Players["a"]
		v.Body.Daily[9].Max = 2
		v.Body.Flags[7] |= 2
		s.Players["a"] = v
		a := PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, PlayerEnemies: []string{"a"}}
		a.Body.Daily[9].Max = 3
		s.Players["b"] = a
		r := s.Rooms[1]
		r.PlayerIDs = append(r.PlayerIDs, "b")
		if survival {
			r.Resource.Flags[4] |= 16
		}
		s.Rooms[1] = r
		next, result, err := s.PlanPlayerDeath("a", "b", 100, SceneOptions{}, nil, func(lo, hi int) int {
			if survival {
				t.Fatal("survival RNG")
			}
			return lo
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !result.FamilyDefeated || *next.War != (FamilyWar{}) || len(next.Players["a"].Items.Items) != 1 || len(next.Rooms[1].Items.Items) != 0 || result.BroadcastDeath == survival {
			t.Fatalf("%+v", result)
		}
		if survival && len(next.Players["b"].PlayerEnemies) != 0 {
			t.Fatal("enemy not cleared")
		}
		if !survival && (len(next.Players["b"].PlayerEnemies) != 1 || next.Players["b"].Body.Timers[11].Interval != 7*86400) {
			t.Fatal("attacker state")
		}
		if len(s.Players["b"].PlayerEnemies) != 1 || s.War.Active == 0 {
			t.Fatal("input alias")
		}
	}
}
