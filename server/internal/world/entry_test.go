package world

import (
	"strings"
	"testing"
)

func TestRoomDeparturePreservesOrderAndDeactivatesLast(t *testing.T) {
	players := []RoomPlayerView{{ID: "a"}, {ID: "b"}}
	r, err := PlanRoomDeparture(players, "a")
	if err != nil || len(r.Players) != 1 || r.Players[0].ID != "b" || r.DeactivateMonsters || players[0].ID != "a" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = PlanRoomDeparture(r.Players, "b")
	if err != nil || len(r.Players) != 0 || !r.DeactivateMonsters {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := PlanRoomDeparture(players, "missing"); err == nil {
		t.Fatal("missing departure accepted")
	}
}

func TestRoomEntryRefreshAndOrdering(t *testing.T) {
	room := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}, PermanentMonsters: [10]LegacyTimer{{Misc: 1}}}
	before := []RoomPlayerView{{ID: "z", Name: "Zoe"}}
	entrant := RoomPlayerView{ID: "a", Name: "Alice"}
	got, err := PlanRoomEntry(room, before, entrant, SceneOptions{}, refreshCatalog{}, 1, func(lo, hi int) int { return lo })
	if err != nil || got.Room.BeenHere != 1 || len(got.Room.Monsters) != 1 || got.Players[0].ID != "a" || !got.AnnounceArrival || got.EnsureMonstersActive || !strings.Contains(got.Scene, "Zoe님") || strings.Contains(got.Scene, "Alice님") {
		t.Fatalf("%+v %v", got, err)
	}
	if room.BeenHere != 0 || len(room.Monsters) != 0 || before[0].ID != "z" {
		t.Fatal("mutated source")
	}
}

func TestRoomEntryActivatesWholeRoomOnlyForFirstOccupant(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		for _, spawn := range []bool{false, true} {
			room := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}, Monsters: []LegacyMonster{{Name: "existing", Type: 1}}}
			var players []RoomPlayerView
			if occupied {
				players = []RoomPlayerView{{ID: "old", Name: "Old"}}
			}
			if spawn {
				room.PermanentMonsters[0] = LegacyTimer{Misc: 1}
			}
			got, err := PlanRoomEntry(room, players, RoomPlayerView{ID: "new", Name: "New"}, SceneOptions{}, refreshCatalog{}, 1, func(lo, hi int) int { return lo })
			if err != nil {
				t.Fatal(err)
			}
			if got.EnsureMonstersActive != !occupied {
				t.Fatalf("occupied=%v spawn=%v: whole-room activation=%v", occupied, spawn, got.EnsureMonstersActive)
			}
		}
	}
}

func TestRoomEntryDuplicateAndFailedRefresh(t *testing.T) {
	p := RoomPlayerView{ID: "a", Name: "Alice"}
	if _, err := PlanRoomEntry(LegacyRoom{}, []RoomPlayerView{p}, p, SceneOptions{}, nil, 1, nil); err == nil {
		t.Fatal("duplicate entry")
	}
	room := LegacyRoom{BeenHere: 3, PermanentObjects: [10]LegacyTimer{{Misc: 1}}}
	got, err := PlanRoomEntry(room, nil, p, SceneOptions{}, refreshCatalog{failObject: true}, 1, nil)
	if err == nil || len(got.Players) != 0 || got.Room.BeenHere != 0 || got.Scene != "" || room.BeenHere != 3 {
		t.Fatal("partial entry escaped")
	}
}

func TestRoomEntryAnnouncementVisibility(t *testing.T) {
	for _, flags := range [][8]byte{{2}, {0, 4}, {4}} {
		got, err := PlanRoomEntry(LegacyRoom{}, nil, RoomPlayerView{ID: "a", Flags: flags}, SceneOptions{}, nil, 1, nil)
		if err != nil || got.AnnounceArrival != (flags[0] == 4) {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
