package world

import (
	"reflect"
	"testing"
)

func effectPlayer() LegacyMonster {
	return LegacyMonster{Class: 4, Level: 1, Stats: [5]byte{10, 25, 10, 10, 10}, Thaco: 20}
}
func enableEffect(p *LegacyMonster, bit uint) { p.Flags[bit/8] |= 1 << (bit % 8) }

func TestEffectsExpireStrictlyAfterDeadlineOnce(t *testing.T) {
	p := effectPlayer()
	enableEffect(&p, 19)
	p.Timers[16] = LegacyTimer{LastTime: 90, Interval: 10}
	boundary, err := ExpireEffects(p, [20]*LegacyObject{}, 100)
	if err != nil || !reflect.DeepEqual(boundary.Player, p) || len(boundary.Events) != 0 {
		t.Fatalf("%+v %v", boundary, err)
	}
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 101)
	if err != nil || got.Player.Stats[1] != 10 || got.Player.Armor != 100 || flag(got.Player.Flags[:], 19) || len(got.Events) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	again, err := ExpireEffects(got.Player, [20]*LegacyObject{}, 102)
	if err != nil || again.Player.Stats[1] != 10 || len(again.Events) != 0 {
		t.Fatal("effect removed twice")
	}
}

func TestEffectsPreserveLegacyRecalculationOrder(t *testing.T) {
	p := effectPlayer()
	enableEffect(&p, 52)
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 1)
	if err != nil || got.Player.Stats[0] != 7 || got.Player.Thaco != 20 {
		t.Fatalf("power must not recompute thaco: %+v %v", got, err)
	}
	enableEffect(&p, 0)
	got, err = ExpireEffects(p, [20]*LegacyObject{}, 1)
	if err != nil || got.Player.Thaco != 21 || len(got.Events) != 2 {
		t.Fatalf("bless after power: %+v %v", got, err)
	}
}

func TestEffectsDMExemptionHiddenBoundaryAndLightBroadcast(t *testing.T) {
	p := effectPlayer()
	p.Class = 12
	enableEffect(&p, 2)
	enableEffect(&p, 8)
	enableEffect(&p, 1)
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 300)
	if err != nil || !flag(got.Player.Flags[:], 2) || flag(got.Player.Flags[:], 8) || !flag(got.Player.Flags[:], 1) {
		t.Fatalf("%+v %v", got, err)
	}
	p = effectPlayer()
	enableEffect(&p, 17)
	enableEffect(&p, 1)
	got, err = ExpireEffects(p, [20]*LegacyObject{}, 301)
	if err != nil || flag(got.Player.Flags[:], 1) || len(got.Events) != 2 || !got.Events[1].Room {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEffectsInvalidStatReturnsNoPartialChanges(t *testing.T) {
	p := effectPlayer()
	p.Stats[1] = 1
	enableEffect(&p, 19)
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 1)
	if err == nil || !reflect.DeepEqual(got, EffectResult{}) || p.Stats[1] != 1 {
		t.Fatal("partial failed expiry")
	}
}

func TestEffectsAllFlagsAndOrderedMessages(t *testing.T) {
	p := effectPlayer()
	p.Stats = [5]byte{30, 30, 30, 30, 30}
	p.HPMax = 200
	p.MPMax = 200
	p.DicePlus = 10
	bits := []uint{19, 52, 59, 53, 54, 22, 2, 21, 20, 1, 8, 25, 0, 30, 36, 37, 38, 31, 32, 44, 43, 33, 17, 45}
	for _, bit := range bits {
		enableEffect(&p, bit)
	}
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 301)
	if err != nil {
		t.Fatal(err)
	}
	for _, bit := range bits {
		if flag(got.Player.Flags[:], bit) {
			t.Fatalf("effect %d remained", bit)
		}
	}
	if got.Player.Stats != ([5]byte{27, 15, 30, 27, 25}) || got.Player.HPMax != 100 || got.Player.MPMax != 100 || got.Player.DicePlus != 5 || len(got.Events) != 24 {
		t.Fatalf("%+v", got)
	}
	if got.Events[0].Text != "\n당신의 몸이 느려졌습니다." || !got.Events[22].Room || got.Events[23].Text != "\n당신의 행동이 정상적으로 되었습니다." {
		t.Fatalf("%+v", got.Events)
	}
}

func TestEffectsWidenedDeadlineDoesNotExpireEarly(t *testing.T) {
	p := effectPlayer()
	enableEffect(&p, 2)
	p.Timers[0] = LegacyTimer{LastTime: 2147483640, Interval: 100}
	got, err := ExpireEffects(p, [20]*LegacyObject{}, 2147483647)
	if err != nil || !flag(got.Player.Flags[:], 2) {
		t.Fatalf("%+v %v", got, err)
	}
}
