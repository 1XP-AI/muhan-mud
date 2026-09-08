package world

import (
	"errors"
	"testing"
)

type refreshCatalog struct{ failObject bool }

func (refreshCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{Name: "늑대"}, nil
}
func (c refreshCatalog) Object(int16) (LegacyObject, error) {
	if c.failObject {
		return LegacyObject{}, errors.New("missing")
	}
	return LegacyObject{Name: "검"}, nil
}

func TestRefreshRoomResourcesAndRepeat(t *testing.T) {
	room := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 5, Exits: []LegacyExit{{Flags: [4]byte{16}}}}, PermanentMonsters: [10]LegacyTimer{{Misc: 1}}, PermanentObjects: [10]LegacyTimer{{Misc: 2}}}
	r, err := RefreshRoomResources(room, refreshCatalog{}, 10, func(lo, hi int) int { return lo })
	if err != nil || len(r.Monsters) != 1 || r.Monsters[0].RoomID != 5 || len(r.Objects) != 1 || !flag(r.Objects[0].Flags[:], 0) || !flag(r.Exits[0].Flags[:], 2) {
		t.Fatalf("%+v %v", r, err)
	}
	again, err := RefreshRoomResources(r, refreshCatalog{}, 11, nil)
	if err != nil || len(again.Monsters) != 1 || len(again.Objects) != 1 {
		t.Fatalf("duplicate respawn: %+v %v", again, err)
	}
	if len(room.Monsters) != 0 || len(room.Objects) != 0 || flag(room.Exits[0].Flags[:], 2) {
		t.Fatal("mutated source")
	}
}

func TestRefreshFailureDoesNotPublishPartialRoom(t *testing.T) {
	room := LegacyRoom{PermanentMonsters: [10]LegacyTimer{{Misc: 1}}, PermanentObjects: [10]LegacyTimer{{Misc: 2}}}
	r, err := RefreshRoomResources(room, refreshCatalog{failObject: true}, 10, func(lo, hi int) int { return lo })
	if err == nil || len(r.Monsters) != 0 || len(room.Monsters) != 0 {
		t.Fatal("partial refresh escaped")
	}
}

func TestRefreshOwnsExistingNestedValues(t *testing.T) {
	room := LegacyRoom{Objects: []LegacyObject{{Name: "상자", Contents: []LegacyObject{{Name: "검"}}}}}
	r, err := RefreshRoomResources(room, nil, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Objects[0].Contents[0].Name = "변경"
	if room.Objects[0].Contents[0].Name != "검" {
		t.Fatal("nested alias")
	}
}
