package world

import (
	"errors"
	"reflect"
	"testing"
)

func changeClassRoomFlags(class byte, training bool) (flags [8]byte) {
	if training {
		flags[ChangeClassRoomFlag/8] |= 1 << (ChangeClassRoomFlag % 8)
	}
	if class > 0 && class <= ChangeClassMaxClass {
		bits := class - 1
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				at := ChangeClassRoomFlag + 3 - i
				flags[at/8] |= 1 << (at % 8)
			}
		}
	}
	return flags
}

func changeClassState(body LegacyMonster, roomFlags [8]byte) State {
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

func TestPlanApplyChangeClassUsesRoomBitsAndSourceLevelDown(t *testing.T) {
	body := LegacyMonster{
		Class: 4, Level: 5, Experience: 100000, Stats: [5]byte{10, 11, 12, 13, 14},
		HPMax: 200, HPCurrent: 77, MPMax: 200, MPCurrent: 66,
	}
	body.Timers[7] = LegacyTimer{Interval: 31, LastTime: 47, Misc: 59}
	s := changeClassState(body, changeClassRoomFlags(5, true))
	before := s.clone()

	p, err := s.PlanChangeClass("alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.Class != 5 || p.BeforeClass != 4 || p.Experience != 0 || p.ExperienceSpent != 100000 || p.TargetLevel != 1 || p.LevelsDown != 4 || p.Response != ChangeClassSuccessResponse || !p.Changed || p.NoOp {
		t.Fatalf("proposal=%+v", p)
	}
	if p.BeforeStats != body.Stats || p.BeforeTimers != body.Timers || !reflect.DeepEqual(s, before) {
		t.Fatal("planning changed or failed to capture source fields")
	}

	next, result, err := s.ApplyChangeClass(p)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["alice"].Body
	// LowerPlayerLevel is source down_level with the destination class's gains:
	// levels 5->1 subtract class-5 HP/MP gains and lower the level-four piety
	// cycle entry once.
	if actor.Class != 5 || actor.Level != 1 || actor.Experience != 0 || actor.HPMax != 192 || actor.HPCurrent != 192 || actor.MPMax != 194 || actor.MPCurrent != 194 || actor.Stats != [5]byte{10, 11, 12, 13, 13} {
		t.Fatalf("actor=%+v", actor)
	}
	if actor.Timers != body.Timers {
		t.Fatalf("timers changed: got=%+v want=%+v", actor.Timers, body.Timers)
	}
	if result.Action != "change_class" || !result.Confirmed || !result.Changed || result.LevelsDown != 4 || result.Class != 5 || result.TargetLevel != 1 || result.BeforeTimers != result.Timers || result.Broadcast || result.BroadcastPending {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("apply mutated input state")
	}
}

func TestChangeClassGatesFailClosed(t *testing.T) {
	baseBody := LegacyMonster{Class: 4, Level: 2, Experience: 100000}
	tests := []struct {
		name  string
		body  LegacyMonster
		flags [8]byte
		want  error
	}{
		{name: "blind", body: func() LegacyMonster {
			b := baseBody
			b.Flags[ChangeClassBlindFlag/8] |= 1 << (ChangeClassBlindFlag % 8)
			return b
		}(), flags: changeClassRoomFlags(5, true), want: ErrChangeClassBlind},
		{name: "not training room", body: baseBody, flags: changeClassRoomFlags(5, false), want: ErrChangeClassRoom},
		{name: "class above eight", body: func() LegacyMonster { b := baseBody; b.Class = 9; return b }(), flags: changeClassRoomFlags(5, true), want: ErrChangeClassUnsupportedClass},
		{name: "same class", body: func() LegacyMonster { b := baseBody; b.Class = 5; return b }(), flags: changeClassRoomFlags(5, true), want: ErrChangeClassSameClass},
		{name: "insufficient experience", body: func() LegacyMonster { b := baseBody; b.Experience = 99999; return b }(), flags: changeClassRoomFlags(5, true), want: ErrChangeClassExperience},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := changeClassState(tc.body, tc.flags)
			before := s.clone()
			if _, err := s.PlanChangeClass("alice"); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if !reflect.DeepEqual(s, before) {
				t.Fatal("gate mutated state")
			}
		})
	}
}

func TestChangeClassFamilyAndBareConfirmationBoundaries(t *testing.T) {
	familyBody := LegacyMonster{Class: 4, Level: 2, Experience: 100000}
	familyBody.Flags[ChangeClassFamilyFlag/8] |= 1 << (ChangeClassFamilyFlag % 8)
	family := changeClassState(familyBody, changeClassRoomFlags(5, true))
	if _, err := family.PlanChangeClass("alice"); !errors.Is(err, ErrChangeClassFamilyPending) {
		t.Fatalf("family err=%v", err)
	}

	s := changeClassState(LegacyMonster{Class: 4, Level: 2, Experience: 100000}, changeClassRoomFlags(5, true))
	before := s.clone()
	p, err := s.PlanChangeClass("alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if !p.PendingConfirmation || !p.NoOp || p.Changed || p.Confirmed || p.Response != ChangeClassPromptResponse || p.PendingReason == "" {
		t.Fatalf("prompt proposal=%+v", p)
	}
	next, result, err := s.ApplyChangeClass(p)
	if err != nil || !reflect.DeepEqual(next, s) || !reflect.DeepEqual(s, before) {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
	if !result.PendingConfirmation || !result.NoOp || result.Changed || result.Confirmed || result.Response != ChangeClassPromptResponse {
		t.Fatalf("prompt result=%+v", result)
	}
}

func TestChangeClassRejectsStaleVitalsStatsAndTimers(t *testing.T) {
	body := LegacyMonster{Class: 4, Level: 3, Experience: 100000, HPMax: 90, HPCurrent: 12, MPMax: 80, MPCurrent: 34, Stats: [5]byte{10, 11, 12, 13, 14}}
	body.Timers[0] = LegacyTimer{Interval: 4, LastTime: 8, Misc: 12}
	s := changeClassState(body, changeClassRoomFlags(5, true))
	p, err := s.PlanChangeClass("alice")
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	actor := changed.Players["alice"]
	actor.Body.Timers[0].LastTime++
	changed.Players["alice"] = actor
	if _, _, err := changed.ApplyChangeClass(p); !errors.Is(err, ErrChangeClassStaleProposal) {
		t.Fatalf("stale timer err=%v", err)
	}
	if _, _, err := s.ApplyChangeClass(ChangeClassProposal{}); !errors.Is(err, ErrChangeClassStaleProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
}
