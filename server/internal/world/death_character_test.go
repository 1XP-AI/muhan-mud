package world

import (
	"reflect"
	"testing"
)

func TestDeathCharacterRecoveryAndEquipment(t *testing.T) {
	for _, playerKiller := range []bool{false, true} {
		body := LegacyMonster{Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, MPMax: 80, HPCurrent: -1, MPCurrent: 2}
		body.Flags[2] = 1
		body.Flags[5] = 2
		items := ItemCollection{Items: map[string]Item{"coat": {Object: LegacyObject{Armor: 20}}}}
		items.Ready[0] = "coat"
		before := items.clone()
		got, err := PlanDeathCharacter(body, items, DeathCharacterRules{AttackerPlayer: playerKiller, WarAllowsLoss: true})
		if err != nil {
			t.Fatal(err)
		}
		wantMP := int16(80)
		if playerKiller {
			wantMP = 8
		}
		if got.Body.HPCurrent != 100 || got.Body.MPCurrent != wantMP || flag(got.Body.Flags[:], 16) || flag(got.Body.Flags[:], 41) || got.Body.Armor != 100 {
			t.Fatalf("body %+v", got.Body)
		}
		if got.Kept.Ready[0] != "" {
			t.Fatal("still equipped")
		}
		if playerKiller && len(got.Dropped.Items) != 1 {
			t.Fatal("missing floor payload")
		}
		if !playerKiller && len(got.Kept.Inventory) != 1 {
			t.Fatal("missing inventory")
		}
		if !reflect.DeepEqual(items, before) || body.HPCurrent != -1 {
			t.Fatal("input mutated")
		}
	}
}

func TestDeathCharacterSelfAndFailure(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, MPMax: 80, MPCurrent: 40}
	items := ItemCollection{Items: map[string]Item{}}
	got, err := PlanDeathCharacter(body, items, DeathCharacterRules{AttackerPlayer: true, Self: true, Survival: true})
	if err != nil || got.Body.MPCurrent != 40 {
		t.Fatalf("%+v %v", got, err)
	}
	// Fail late, during combat-stat recomputation, after candidate recovery.
	body.Stats[1] = 255
	got, err = PlanDeathCharacter(body, items, DeathCharacterRules{AttackerPlayer: true})
	if err == nil || !reflect.DeepEqual(got, DeathCharacterPlan{}) {
		t.Fatalf("partial result %+v %v", got, err)
	}
}
