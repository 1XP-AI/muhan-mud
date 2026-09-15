package world

import (
	"reflect"
	"testing"
)

type orderedNPCSpawnCatalog struct{ refreshCatalog }

func (orderedNPCSpawnCatalog) Monster(id int16) (LegacyMonster, error) {
	name := "Zebra"
	if id == 2 {
		name = "Ant"
	}
	return LegacyMonster{Name: name, Type: 1}, nil
}

func TestNPCEntryRetainsSpawnOrderSeparateFromRoomOrder(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		room := RoomState{Resource: LegacyRoom{
			LegacyRoomHeader:  LegacyRoomHeader{ID: 1},
			PermanentMonsters: [10]LegacyTimer{{Misc: 1}, {Misc: 2}},
		}, NPCIDs: []string{"existing"}}
		npcs := map[string]NPCState{"existing": {Body: LegacyMonster{Name: "Middle", Type: 1, RoomID: 1}}}
		var players []RoomPlayerView
		if occupied {
			players = []RoomPlayerView{{ID: "old", Name: "Old"}}
		}
		allocated := []string{"zebra-id", "ant-id"}
		calls := 0
		next, entry, delta, err := planCanonicalNPCEntry(room, npcs, players, RoomPlayerView{ID: "new"}, SceneOptions{}, orderedNPCSpawnCatalog{}, 1, func(lo, hi int) int { return lo }, func() (string, error) {
			id := allocated[calls]
			calls++
			return id, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if calls != 2 || !reflect.DeepEqual(delta.SpawnOrder, allocated) || !reflect.DeepEqual(next.NPCIDs, []string{"ant-id", "existing", "zebra-id"}) || entry.EnsureMonstersActive != !occupied {
			t.Fatalf("occupied=%v delta=%+v room=%v activation=%v", occupied, delta, next.NPCIDs, entry.EnsureMonstersActive)
		}
		if len(delta.Spawned) != 2 || len(npcs) != 1 || !reflect.DeepEqual(room.NPCIDs, []string{"existing"}) {
			t.Fatal("spawn delta includes old NPC or mutates source")
		}
	}
}
