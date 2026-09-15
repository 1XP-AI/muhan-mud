package game

import (
	"encoding/json"
	"os"
	"testing"
)

func TestWizardOriginalScenario(t *testing.T) {
	raw, err := os.ReadFile("../../../tests/scenarios/create_and_relogin.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Gender, Class, Stats, Weapon, Alignment, Race string }
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	w := NewCreationWizard()
	for _, input := range []string{fixture.Gender, fixture.Class, fixture.Stats, fixture.Weapon, fixture.Alignment, fixture.Race} {
		if err := w.Submit(input); err != nil {
			t.Fatal(err)
		}
	}
	draft, ok := w.Draft()
	if !ok || w.Stage() != CreationReady || draft.Stats != (Stats{12, 10, 13, 10, 10}) || draft.Class != 4 || draft.RaceID != 5 {
		t.Fatalf("wrong draft: %+v", draft)
	}
	if err := w.Submit("extra"); err == nil {
		t.Fatal("completed wizard accepted more input")
	}
}

func TestWizardInvalidInputDoesNotAdvance(t *testing.T) {
	w := NewCreationWizard()
	for _, step := range []struct {
		stage     CreationStage
		good, bad string
	}{
		{CreationGender, "여", "여분"}, {CreationClass, "4", "4garbage"},
		{CreationStats, "12 10 12 10 10", "99 10 12 10 10"}, {CreationWeapon, "1", "6"},
		{CreationAlignment, "악", "선물"}, {CreationRace, "7", "0"},
	} {
		if w.Stage() != step.stage || w.Prompt() == "" {
			t.Fatal("unexpected stage")
		}
		if _, ok := w.Draft(); ok {
			t.Fatal("partial draft escaped")
		}
		if err := w.Submit(step.bad); err == nil || w.Stage() != step.stage {
			t.Fatal("invalid input advanced")
		}
		if err := w.Submit(step.good); err != nil {
			t.Fatal(err)
		}
	}
	draft, _ := w.Draft()
	if draft.Male || !draft.Chaotic {
		t.Fatal("gender/alignment lost")
	}
	w.Cancel()
	if _, ok := w.Draft(); ok {
		t.Fatal("canceled draft retained")
	}
	if err := w.Submit("남"); err == nil {
		t.Fatal("canceled wizard resumed")
	}
}
