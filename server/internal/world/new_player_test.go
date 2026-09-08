package world

import (
	"reflect"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
)

func TestNewPlayerInitializesCreationWithoutAdmission(t *testing.T) {
	choices := game.CreationChoices{Male: true, Class: 4, Stats: game.Stats{12, 10, 12, 10, 10}, Weapon: 2, Chaotic: true, RaceChoice: 7}
	p, err := NewPlayer("alice", choices)
	if err != nil {
		t.Fatal(err)
	}
	b := p.Body
	if p.Online || b.Name != "Alice" || b.Level != 1 || b.Type != 0 || b.RoomID != 1 || b.Gold != 500 || b.Race != 5 || b.Class != 4 || b.Stats != [5]byte{12, 10, 13, 10, 10} || b.Proficiency != [5]int32{0, 1024, 0, 0, 0} {
		t.Fatalf("bad new player: %+v", p)
	}
	if b.HPMax != 56 || b.HPCurrent != 56 || b.MPMax != 50 || b.MPCurrent != 50 || b.DiceCount != 1 || b.DiceSides != 5 || b.DicePlus != 0 {
		t.Fatalf("bad initial level: %+v", b)
	}
	for _, bit := range []uint{12, 18, 28, 46} {
		if !flag(b.Flags[:], bit) {
			t.Fatalf("missing flag %d", bit)
		}
	}
	if p.Items == nil || p.Items.Validate() != nil || len(p.Items.Items) != 0 || len(b.Inventory) != 0 {
		t.Fatal("new player lacks empty canonical inventory")
	}
	other, err := NewPlayer("Bob", game.CreationChoices{Class: 4, Stats: choices.Stats, Weapon: 1, RaceChoice: 7})
	if err != nil || flag(other.Body.Flags[:], 12) || flag(other.Body.Flags[:], 28) {
		t.Fatalf("optional flags leaked: %+v %v", other, err)
	}
	p.Items.Items["probe"] = Item{}
	if len(other.Items.Items) != 0 {
		t.Fatal("new characters share inventory")
	}
}

func TestNewPlayerRejectsInvalidCreation(t *testing.T) {
	valid := game.CreationChoices{Class: 4, Stats: game.Stats{10, 10, 10, 10, 10}, Weapon: 1, RaceChoice: 7}
	bad := valid
	bad.Class = 12
	for _, tc := range []struct {
		name    string
		choices game.CreationChoices
	}{{"", valid}, {"Alice", bad}, {"Alice", game.CreationChoices{}}} {
		p, err := NewPlayer(tc.name, tc.choices)
		if err == nil || !reflect.DeepEqual(p, PlayerState{}) {
			t.Fatalf("partial player %+v %v", p, err)
		}
	}
}

func TestNewPlayerFromStoredDraft(t *testing.T) {
	choices := game.CreationChoices{Class: 4, Stats: game.Stats{12, 10, 12, 10, 10}, Weapon: 1, RaceChoice: 7}
	draft, err := game.BuildCreation(choices)
	if err != nil {
		t.Fatal(err)
	}
	want, err := NewPlayer("Alice", choices)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewPlayerFromDraft("Alice", draft)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v %v", got, err)
	}
	draft.Gold++
	got, err = NewPlayerFromDraft("Alice", draft)
	if err == nil || !reflect.DeepEqual(got, PlayerState{}) {
		t.Fatalf("corrupt draft accepted %+v %v", got, err)
	}
}
