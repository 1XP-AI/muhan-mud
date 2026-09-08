package world

import (
	"reflect"
	"testing"
)

func TestDeathTimersPlayerAndSelf(t *testing.T) {
	victim := LegacyTimer{LastTime: 90, Interval: 300, Misc: 7}
	attacker := LegacyTimer{LastTime: 80, Interval: 400, Misc: 9}
	for _, self := range []bool{false, true} {
		for _, days := range []int{7, 14} {
			calls := 0
			got, err := PlanDeathTimers(victim, attacker, true, self, false, 100, func(lo, hi int) int {
				calls++
				if lo != 7 || hi != 14 {
					t.Fatal("wrong range")
				}
				return days
			})
			if err != nil || calls != 1 {
				t.Fatalf("%+v %v", got, err)
			}
			wantVictim := LegacyTimer{Misc: 7}
			wantAttacker := LegacyTimer{LastTime: 100, Interval: int32(days * 86400), Misc: 9}
			if self {
				wantAttacker.Misc = 7
				wantVictim = wantAttacker
			}
			if got.Victim != wantVictim || got.Attacker != wantAttacker || got.RemoveEnemy {
				t.Fatalf("%+v", got)
			}
		}
	}
}

func TestDeathTimersSurvivalAndNPCDoNotRoll(t *testing.T) {
	for _, player := range []bool{false, true} {
		for _, self := range []bool{false, true} {
			if self && !player {
				continue
			}
			for _, survival := range []bool{false, true} {
				if player && !survival {
					continue
				}
				attacker := LegacyTimer{LastTime: 50, Interval: 60, Misc: 8}
				got, err := PlanDeathTimers(LegacyTimer{LastTime: 2, Interval: 3, Misc: 4}, attacker, player, self, survival, 100, func(int, int) int { t.Fatal("unneeded RNG"); return 7 })
				if self {
					attacker = LegacyTimer{Misc: 4}
				}
				if err != nil || got.Victim != (LegacyTimer{Misc: 4}) || got.Attacker != attacker || !got.RemoveEnemy {
					t.Fatalf("%+v %v", got, err)
				}
			}
		}
	}
}

func TestDeathTimersRejectInvalidInputs(t *testing.T) {
	for _, roll := range []func(int, int) int{nil, func(int, int) int { return 6 }, func(int, int) int { return 15 }} {
		got, err := PlanDeathTimers(LegacyTimer{}, LegacyTimer{}, true, false, false, 100, roll)
		if err == nil || !reflect.DeepEqual(got, DeathTimerPlan{}) {
			t.Fatal("partial/invalid result")
		}
	}
	if _, err := PlanDeathTimers(LegacyTimer{}, LegacyTimer{}, false, true, false, 100, nil); err == nil {
		t.Fatal("NPC cannot be self")
	}
}
