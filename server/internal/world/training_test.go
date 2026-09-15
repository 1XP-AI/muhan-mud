package world

import (
	"errors"
	"reflect"
	"testing"
)

func trainingRoomFlags(class byte, training bool) (flags [8]byte) {
	if training {
		flags[trainingRoomFlag/8] |= 1 << (trainingRoomFlag % 8)
	}
	if class > 0 && class <= 8 {
		bits := class - 1
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				roomFlag := trainingRoomFlag + 3 - i
				flags[roomFlag/8] |= 1 << (roomFlag % 8)
			}
		}
	}
	return flags
}

func trainingState(body LegacyMonster, roomFlags [8]byte) State {
	body.Type = 0
	body.RoomID = 1
	if body.Name == "" {
		body.Name = "Alice"
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Flags: roomFlags}},
			PlayerIDs: []string{"alice"},
		}},
		Players: map[string]PlayerState{"alice": {Body: body, Online: true}},
	}
}

func TestTrainingExperienceAndGoldFormulaMatchesCommand7(t *testing.T) {
	for _, test := range []struct {
		level byte
		exp   int64
		gold  int64
	}{
		{level: 1, exp: 128, gold: 6},
		// command7.c indexes needed_exp by level-1.  The level-100
		// threshold is therefore the source table's 100th entry, not the
		// later round 10,000,000 entry.
		{level: 100, exp: 7984959, gold: 399247},
		{level: 127, exp: 100000000, gold: 5000000},
		{level: 128, exp: 190000000, gold: 5000000},
		{level: 129, exp: 110000000, gold: 5000000},
	} {
		exp, err := TrainingExperienceNeeded(test.level)
		if err != nil || exp != test.exp {
			t.Fatalf("level%d experience=%d err=%v want=%d", test.level, exp, err, test.exp)
		}
		gold, err := TrainingGoldNeeded(test.level, exp)
		if err != nil || gold != test.gold {
			t.Fatalf("level%d gold=%d err=%v want=%d", test.level, gold, err, test.gold)
		}
	}
	if _, err := TrainingExperienceNeeded(0); !errors.Is(err, ErrTrainingUnsupportedLevel) {
		t.Fatalf("level zero err=%v", err)
	}
}

func TestPlanApplyTrainingClearsUpDmgAndChargesOneLevelAtomically(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 1, Experience: 128, Gold: 100, HPMax: 200, HPCurrent: 100, MPMax: 200, MPCurrent: 100, DicePlus: 10}
	body.Flags[trainingUpDmgFlag/8] |= 1 << (trainingUpDmgFlag % 8)
	s := trainingState(body, trainingRoomFlags(body.Class, true))
	before := s.clone()
	proposal, err := s.PlanTraining("alice")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Level != 2 || proposal.Class != 4 || proposal.LevelsGained != 1 || !proposal.UpDmgReleased || proposal.ExperienceNeeded != 128 || proposal.GoldNeeded != 6 || proposal.GoldSpent != 6 || proposal.Broadcast || !proposal.BroadcastPending {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("planning mutated state")
	}
	next, result, err := s.ApplyTraining(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	// up_level resets the class dice after the source PUPDMG release; the
	// release still supplies the pre-level HP/MP baseline and clears its flag.
	if actor.Level != 2 || actor.Class != 4 || actor.Experience != 128 || actor.Gold != 94 || actor.DicePlus != 0 || actor.HPMax != 100 || actor.MPMax != 101 || actor.HPCurrent != 100 || actor.MPCurrent != 100 || flag(actor.Flags[:], trainingUpDmgFlag) {
		t.Fatalf("actor=%+v", actor)
	}
	if result.Action != "train" || !result.Changed || result.Broadcast || !result.BroadcastPending || result.Level != 2 || result.GoldSpent != 6 || result.Response != "\n축하합니다! 당신의 레벨이 올랐습니다!" {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("apply mutated input state")
	}
}

func TestTrainingRoomClassBlindAndCaretakerGatesPreserveState(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 1, Experience: 128, Gold: 100}
	wrongClass := trainingState(body, [8]byte{trainingRoomFlag / 8: 1 << (trainingRoomFlag % 8)})
	if _, err := wrongClass.PlanTraining("alice"); !errors.Is(err, ErrTrainingClass) {
		t.Fatalf("wrong class err=%v", err)
	}
	noRoom := trainingState(body, trainingRoomFlags(body.Class, false))
	if _, err := noRoom.PlanTraining("alice"); !errors.Is(err, ErrTrainingRoom) {
		t.Fatalf("missing training room err=%v", err)
	}
	blind := body
	blind.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	blindState := trainingState(blind, [8]byte{})
	if _, err := blindState.PlanTraining("alice"); !errors.Is(err, ErrTrainingBlind) {
		t.Fatalf("blind err=%v", err)
	}
	caretaker := trainingState(LegacyMonster{Class: trainingCaretaker, Level: 127, Experience: 190000000, Gold: 5000000}, trainingRoomFlags(trainingCaretaker, true))
	if _, err := caretaker.PlanTraining("alice"); !errors.Is(err, ErrTrainingCaretaker) {
		t.Fatalf("caretaker err=%v", err)
	}
}

func TestTrainingSupportsMultipleLevels(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 1, Experience: 5000, Gold: 10000, HPMax: 56, HPCurrent: 56, MPMax: 50, MPCurrent: 50}
	s := trainingState(body, trainingRoomFlags(body.Class, true))
	next, result, err := s.Train("alice")
	if err != nil {
		t.Fatal(err)
	}
	if result.LevelsGained < 2 || next.Players["alice"].Body.Level < 3 || result.GoldSpent <= 6 || result.NextExperienceNeeded <= result.ExperienceNeeded {
		t.Fatalf("next=%+v result=%+v", next.Players["alice"].Body, result)
	}
	if next.Players["alice"].Body.Gold != int32(10000-result.GoldSpent) {
		t.Fatalf("gold=%d spent=%d", next.Players["alice"].Body.Gold, result.GoldSpent)
	}
}

func TestTrainingAppliesSignedNegativeStatAtSourceCycleBoundary(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 3, Experience: 384, Gold: 100, HPMax: 62, HPCurrent: 12, MPMax: 51, MPCurrent: 13, Stats: [5]byte{10, 255, 12, 13, 14}}
	s := trainingState(body, trainingRoomFlags(body.Class, true))
	next, result, err := s.Train("alice")
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if result.LevelsGained != 1 || actor.Level != 4 || int(int8(actor.Stats[1])) != 0 || actor.HPMax != 65 || actor.MPMax != 51 || actor.HPCurrent != 65 || actor.MPCurrent != 51 {
		t.Fatalf("actor=%+v result=%+v", actor, result)
	}
}

func TestTrainingConvertsInvincibleAndCaretakerWithFamilyFailClosed(t *testing.T) {
	invBody := LegacyMonster{Class: 4, Level: 100, Experience: 7984959, Gold: 500000}
	inv := trainingState(invBody, trainingRoomFlags(invBody.Class, true))
	proposal, err := inv.PlanTraining("alice")
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := inv.ApplyTraining(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	if !result.Invincible || result.Caretaker || actor.Class != trainingInvincible || actor.Level != 2 || actor.Experience != 0 || actor.Gold != 100753 || result.GoldSpent != 399247 {
		t.Fatalf("invincible next=%+v result=%+v", actor, result)
	}

	familyBody := invBody
	familyBody.Flags[trainingFamilyFlag/8] |= 1 << (trainingFamilyFlag % 8)
	family := trainingState(familyBody, trainingRoomFlags(familyBody.Class, true))
	before := family.clone()
	if _, err := family.PlanTraining("alice"); !errors.Is(err, ErrTrainingFamilyPending) {
		t.Fatalf("family transition err=%v", err)
	}
	if !reflect.DeepEqual(family, before) {
		t.Fatal("family rejection mutated state")
	}

	caretakerBody := LegacyMonster{Class: trainingInvincible, Level: 127, Experience: 100000000, Gold: 5000000, HPMax: 200, HPCurrent: 10, MPMax: 200, MPCurrent: 10}
	caretakerState := trainingState(caretakerBody, trainingRoomFlags(caretakerBody.Class, true))
	next, result, err = caretakerState.Train("alice")
	if err != nil {
		t.Fatal(err)
	}
	actor = next.Players["alice"].Body
	// command7.c changes max pools for the caretaker conversion but does not
	// write current pools in that branch; preserve the source's 10/10 values.
	if !result.Caretaker || result.Invincible || actor.Class != trainingCaretaker || actor.Level != 127 || actor.Gold != 0 || actor.HPMax != 800 || actor.HPCurrent != 10 || actor.MPMax != 600 || actor.MPCurrent != 10 || actor.DiceCount != 4 || actor.DiceSides != 4 || actor.DicePlus != 4 || result.LevelsGained != 0 {
		t.Fatalf("caretaker next=%+v result=%+v", actor, result)
	}
}

func TestTrainingRejectsStaleProposal(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 1, Experience: 128, Gold: 100}
	s := trainingState(body, trainingRoomFlags(body.Class, true))
	proposal, err := s.PlanTraining("alice")
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	actor := changed.Players["alice"]
	actor.Body.Gold++
	changed.Players["alice"] = actor
	if _, _, err := changed.ApplyTraining(proposal); !errors.Is(err, ErrTrainingStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	if _, _, err := s.ApplyTraining(TrainingProposal{}); err == nil {
		t.Fatal("empty proposal accepted")
	}
}
