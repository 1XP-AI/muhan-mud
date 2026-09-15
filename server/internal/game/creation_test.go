package game

import "testing"

func TestCreationStats(t *testing.T) {
	for _, tt := range []struct {
		input string
		ok    bool
	}{
		{"12 10 12 10 10", true}, {"3 3 3 3 3", true},
		{"18 18 12 3 3", true}, {"18 18 13 3 3", false},
		{"2 10 12 10 10", false}, {"19 3 3 3 3", false},
		{"12 10 12 10", false}, {"12 10 12 10 10 1", false},
		{"12junk 10 12 10 10", false}, {"999999999999999999999 3 3 3 3", false},
	} {
		_, err := ParseCreationStats(tt.input)
		if (err == nil) != tt.ok {
			t.Errorf("%q accepted=%v want=%v", tt.input, err == nil, tt.ok)
		}
	}
}

func TestRacialCreationAdjustments(t *testing.T) {
	base := Stats{12, 10, 12, 10, 10}
	for _, tt := range []struct {
		choice, id int
		want       Stats
	}{
		{1, 1, Stats{13, 10, 12, 10, 9}},
		{2, 2, Stats{11, 10, 11, 12, 10}},
		{3, 8, Stats{11, 10, 12, 10, 11}},
		{4, 3, Stats{12, 10, 11, 11, 10}},
		{5, 7, Stats{14, 10, 12, 9, 9}},
		{6, 4, Stats{11, 11, 12, 10, 10}},
		{7, 5, Stats{12, 10, 13, 10, 10}},
		{8, 6, Stats{13, 9, 13, 9, 10}},
	} {
		got, err := BuildCreation(CreationChoices{Male: true, Class: 4, Stats: base, Weapon: 1, RaceChoice: tt.choice})
		if err != nil {
			t.Fatal(err)
		}
		if got.RaceID != tt.id || got.Stats != tt.want || got.Gold != 500 || got.RoomID != 1 || got.Proficiency[0] != 1024 {
			t.Errorf("race %d: %+v", tt.choice, got)
		}
	}
	if base != (Stats{12, 10, 12, 10, 10}) {
		t.Fatal("input mutated")
	}
}

func TestCreationRejectsInvalidChoices(t *testing.T) {
	valid := CreationChoices{Class: 4, Stats: Stats{12, 10, 12, 10, 10}, Weapon: 1, RaceChoice: 7}
	for _, field := range []string{"class", "weapon", "race", "stats"} {
		bad := valid
		switch field {
		case "class":
			bad.Class = 9
		case "weapon":
			bad.Weapon = 0
		case "race":
			bad.RaceChoice = 0
		case "stats":
			bad.Stats[0] = 100
		}
		got, err := BuildCreation(bad)
		if err == nil || got != (Creation{}) {
			t.Fatalf("partial invalid character: %+v", got)
		}
	}
}
