package world

import (
	"reflect"
	"testing"
)

func equippedPlayer() PlayerState {
	return PlayerState{Body: LegacyMonster{Name: "Alice", Class: 4, Level: 1, RoomID: 2, Stats: [5]byte{10, 10}, HPCurrent: 30},
		Items: &ItemCollection{Items: map[string]Item{
			"sword": {Object: LegacyObject{Armor: 7, Adjustment: 2, Type: 0, Wear: 20}},
			"spare": {Object: LegacyObject{Armor: 127}},
		}, Inventory: []string{"spare"}, Ready: [20]string{19: "sword"}},
	}
}

func TestCanonicalEquipmentStatsUseOnlyReadyItems(t *testing.T) {
	p := equippedPlayer()
	stats, err := p.Items.CombatStats(p.Body)
	if err != nil || stats.Armor != 93 || stats.Thaco != 18 {
		t.Fatalf("%+v %v", stats, err)
	}
	p.Body.Flags[1] |= 1
	stats, err = p.Items.CombatStats(p.Body)
	if err != nil || stats.Armor != 83 {
		t.Fatalf("%+v %v", stats, err)
	}
	if p.Items.Ready[19] != "sword" || len(p.Items.Inventory) != 1 {
		t.Fatal("modified ownership")
	}
}

func TestSavedPlayerEntryRecalculatesPersistedEquipment(t *testing.T) {
	s := offlineFixture()
	s.Players["a"] = equippedPlayer()
	next, _, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	if err != nil || int8(next.Players["a"].Body.Armor) != 93 || int8(next.Players["a"].Body.Thaco) != 18 {
		t.Fatalf("%+v %v", next, err)
	}
	if !reflect.DeepEqual(next.Players["a"].Items, s.Players["a"].Items) || s.Players["a"].Body.Armor != 0 {
		t.Fatal("reimported equipment or mutated input")
	}
}

func TestEquipmentStatsFailureDoesNotPublishEntry(t *testing.T) {
	s := offlineFixture()
	p := equippedPlayer()
	p.Body.Stats[0] = 64
	s.Players["a"] = p
	next, entry, err := s.EnterSavedPlayer("a", SceneOptions{}, nil, 1, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(entry, RoomEntry{}) || s.Players["a"].Online || s.Rooms[2].Resource.BeenHere != 0 {
		t.Fatal("partial failed stat update")
	}
}
