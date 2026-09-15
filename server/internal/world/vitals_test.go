package world

import (
	"reflect"
	"testing"
)

func vitalPlayer() LegacyMonster {
	return LegacyMonster{Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPCurrent: 10, HPMax: 100, MPCurrent: 5, MPMax: 100}
}
func TestVitalsNormalQuickHealingAndDeadline(t *testing.T) {
	p := vitalPlayer()
	room := LegacyRoom{}
	got, err := PlanVitals(p, &room, 0, nil)
	if err != nil || !reflect.DeepEqual(got.Player, p) {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = PlanVitals(p, &room, 1, nil)
	if err != nil || got.Player.HPCurrent != 15 || got.Player.MPCurrent != 10 || got.Player.Timers[8].Interval != 5 {
		t.Fatalf("%+v %v", got, err)
	}
	room.Flags[1] |= 32
	got, err = PlanVitals(p, &room, 1, nil)
	if err != nil || got.Player.HPCurrent != 100 || got.Player.MPCurrent != 100 || got.Player.Timers[8].Interval != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestVitalsPoisonDeathStopsBeforeFurtherDamage(t *testing.T) {
	p := vitalPlayer()
	p.HPCurrent = 1
	enableEffect(&p, 16)
	enableEffect(&p, 41)
	calls := 0
	got, err := PlanVitals(p, &LegacyRoom{}, 1, func(lo, hi int) int { calls++; return lo })
	if err != nil || !got.Death || got.Player.HPCurrent != 0 || calls != 2 || len(got.Messages) != 1 {
		t.Fatalf("%+v %v calls%d", got, err, calls)
	}
}
func TestVitalsDiseaseCooldownAndHarmRoomNewPoison(t *testing.T) {
	p := vitalPlayer()
	enableEffect(&p, 41)
	got, err := PlanVitals(p, &LegacyRoom{}, 1, func(lo, hi int) int { return lo })
	if err != nil || got.Player.HPCurrent != 9 || got.Player.Timers[3].Interval != 4 || got.Player.Timers[8].Interval != 30 {
		t.Fatalf("%+v %v", got, err)
	}
	p = vitalPlayer()
	room := LegacyRoom{}
	room.Flags[3] = 3 // harm + poison
	got, err = PlanVitals(p, &room, 1, func(lo, hi int) int { return lo })
	if err != nil || !flag(got.Player.Flags[:], 16) || got.Player.HPCurrent != 12 || got.Player.MPCurrent != 7 {
		t.Fatalf("original pre-infection ill snapshot: %+v %v", got, err)
	}
}
func TestVitalsEnvironmentalProtectionAndFailureAtomicity(t *testing.T) {
	p := vitalPlayer()
	room := LegacyRoom{}
	room.Flags[3] = 1
	room.Flags[2] = 32 // harm + fire
	got, err := PlanVitals(p, &room, 1, nil)
	if err != nil || got.Player.HPCurrent != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	enableEffect(&p, 30)
	got, err = PlanVitals(p, &room, 1, nil)
	if err != nil || got.Player.HPCurrent != 13 {
		t.Fatalf("%+v %v", got, err)
	}
	room.Flags[3] |= 8 // befuddle requires dice
	got, err = PlanVitals(p, &room, 1, nil)
	if err == nil || !reflect.DeepEqual(got, VitalResult{}) {
		t.Fatal("partial random failure")
	}
}

func TestVitalsHazardPriorityAndLegacyNegativeInterval(t *testing.T) {
	p := vitalPlayer()
	enableEffect(&p, 30)
	p.Stats[4] = 63
	room := LegacyRoom{}
	room.Flags[3] = 1
	room.Flags[2] = 96 // fire and water
	got, err := PlanVitals(p, &room, 1, nil)
	if err != nil || got.Player.HPCurrent != 2 || len(got.Messages) != 1 || got.Messages[0] != "\n물이 당신의 폐로 흘러듭니다." || got.Player.Timers[8].Interval != -16 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestVitalsRandomErrorDoesNotPublishNewPoison(t *testing.T) {
	p := vitalPlayer()
	room := LegacyRoom{}
	room.Flags[3] = 11
	got, err := PlanVitals(p, &room, 1, func(lo, hi int) int { return hi + 1 })
	if err == nil || !reflect.DeepEqual(got, VitalResult{}) || flag(p.Flags[:], 16) {
		t.Fatal("partial poisoning on error")
	}
	got, err = PlanVitals(p, nil, 1, nil)
	if err != nil || !reflect.DeepEqual(got.Player, p) {
		t.Fatal("roomless player changed")
	}
}

func TestVitalsMacroSecondDrawIsNotClampedAgain(t *testing.T) {
	p := vitalPlayer()
	p.Stats[2] = 63
	enableEffect(&p, 16)
	rolls := []int{20, 1}
	at := 0
	got, err := PlanVitals(p, &LegacyRoom{}, 1, func(int, int) int { v := rolls[at]; at++; return v })
	if err != nil || got.Player.HPCurrent != 16 || at != 2 {
		t.Fatalf("original MAX can produce negative damage: %+v %v", got, err)
	}
	p = vitalPlayer()
	room := LegacyRoom{}
	room.Flags[3] = 9
	rolls = []int{6, 6, 1, 1}
	at = 0
	got, err = PlanVitals(p, &room, 1, func(int, int) int { v := rolls[at]; at++; return v })
	if err != nil || got.Player.Timers[3].Interval != 2 || at != 4 {
		t.Fatalf("original dice reevaluation: %+v %v", got, err)
	}
}
