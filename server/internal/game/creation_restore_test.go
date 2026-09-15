package game

import "testing"

func TestCreationChoicesRoundTrip(t *testing.T) {
	for class := 1; class <= 8; class++ {
		for race := 1; race <= 8; race++ {
			for weapon := 1; weapon <= 5; weapon++ {
				for flags := 0; flags < 4; flags++ {
					want := CreationChoices{Class: class, RaceChoice: race, Weapon: weapon, Male: flags&1 != 0, Chaotic: flags&2 != 0, Stats: Stats{3, 18, 10, 10, 10}}
					draft, err := BuildCreation(want)
					if err != nil {
						t.Fatal(err)
					}
					got, err := draft.Choices()
					if err != nil || got != want {
						t.Fatalf("%+v -> %+v: %v", want, got, err)
					}
				}
			}
		}
	}
}

func TestCreationChoicesRejectsCorruptDraft(t *testing.T) {
	valid, err := BuildCreation(CreationChoices{Class: 4, RaceChoice: 7, Weapon: 1, Stats: Stats{12, 10, 12, 10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Creation){
		func(d *Creation) { d.Gold++ }, func(d *Creation) { d.RoomID = 2 }, func(d *Creation) { d.Class = 12 }, func(d *Creation) { d.RaceID = 0 },
		func(d *Creation) { d.Proficiency[0] = 0 }, func(d *Creation) { d.Proficiency[1] = 1024 }, func(d *Creation) { d.Proficiency[0] = -1 },
		func(d *Creation) { d.Stats[0] = int(^uint(0) >> 1) }, func(d *Creation) { d.Stats[0] = 18 }, func(d *Creation) { d.Stats[2] = 3 },
	} {
		d := valid
		change(&d)
		got, err := d.Choices()
		if err == nil || got != (CreationChoices{}) {
			t.Fatalf("accepted %+v: %+v %v", d, got, err)
		}
	}
}
