package world

import "testing"

func TestPlanArrivalTrapClearsPreparationWithoutTrap(t *testing.T) {
	calls := 0
	got, err := PlanArrivalTrap(LegacyRoom{}, ArrivalTrapInput{HP: 20, MP: 8, Prepared: true}, func(int, int) int {
		calls++
		return 1
	})
	if err != nil || !got.PreparationCleared || got.Triggered || got.HP != 20 || calls != 0 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, calls)
	}
}

func TestPlanArrivalTrapPreparedAvoidsBeforeNormalRoll(t *testing.T) {
	calls := 0
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapDart}}, ArrivalTrapInput{
		HP: 20, HPMax: 40, Prepared: true, Dexterity: 10,
	}, func(low, high int) int {
		calls++
		if low != 1 || high != 20 {
			t.Fatalf("unexpected first roll %d..%d", low, high)
		}
		return 1
	})
	if err != nil || !got.PreparationCleared || !got.Avoided || got.Triggered || got.HP != 20 || calls != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, calls)
	}
}

func TestPlanArrivalTrapDartAppliesPoisonAndDamageAfterArrival(t *testing.T) {
	rolls := []int{100, 7}
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapDart}}, ArrivalTrapInput{
		HP: 20, HPMax: 40, Dexterity: 10,
	}, func(low, high int) int {
		if low != 1 || high != 100 && high != 10 {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	})
	if err != nil || !got.Triggered || got.Avoided || !got.Poisoned || got.Damage != 7 || got.HP != 13 || got.Dead {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapPitRelocatesBeforeLethalResolution(t *testing.T) {
	rolls := []int{100, 15}
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapPit, TrapExit: 77}}, ArrivalTrapInput{
		HP: 10, HPMax: 30, Dexterity: 1,
	}, func(low, high int) int {
		if low != 1 || (high != 100 && high != 15) {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	})
	if err != nil || !got.Triggered || got.RelocateTo != 77 || got.Damage != 15 || got.HP != -5 || !got.Dead {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapMPDamageUsesIntelligenceAndOrder(t *testing.T) {
	rolls := []int{100, 3}
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapMPDam}}, ArrivalTrapInput{
		HP: 20, HPMax: 20, MP: 4, MPMax: 10, Intelligence: 1,
	}, func(low, high int) int {
		if low != 1 || (high != 100 && high != 6) {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	})
	if err != nil || got.MPDamage != 4 || got.MP != 0 || got.Damage != 3 || got.HP != 17 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapRejectsInvalidRandomSourceWithoutCandidate(t *testing.T) {
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapDart}}, ArrivalTrapInput{HP: 20, HPMax: 20, Dexterity: 1}, nil)
	if err == nil || got != (ArrivalTrapResult{}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapBlockUsesOneThirdHPMax(t *testing.T) {
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapBlock}}, ArrivalTrapInput{
		HP: 30, HPMax: 30, Dexterity: 1,
	}, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		return 100
	})
	if err != nil || !got.Triggered || got.Damage != 10 || got.HP != 20 || got.MPDamage != 0 || got.Dead || got.LoseItems || got.ClearSpells {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapRemovesSpells(t *testing.T) {
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapRMSpl}}, ArrivalTrapInput{
		HP: 20, HPMax: 20, MP: 8, MPMax: 10, Intelligence: 1,
	}, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		return 100
	})
	if err != nil || !got.Triggered || !got.ClearSpells || got.Damage != 0 || got.HP != 20 || got.MP != 8 || got.LoseItems {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestPlanArrivalTrapNakedMarksItemLoss(t *testing.T) {
	got, err := PlanArrivalTrap(LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Trap: TrapNaked}}, ArrivalTrapInput{
		HP: 20, HPMax: 20, Dexterity: 1,
	}, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("unexpected roll %d..%d", low, high)
		}
		return 100
	})
	if err != nil || !got.Triggered || !got.LoseItems || got.Damage != 0 || got.HP != 20 || got.ClearSpells || got.Dead {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestArrivalTrapEventRendersLeaderActorAndRoomTexts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		trap      byte
		effect    ArrivalTrapResult
		actorText string
		roomText  string
	}{
		{
			name:      "pit",
			trap:      TrapPit,
			effect:    ArrivalTrapResult{Trap: TrapPit, Triggered: true, Relocate: true, Damage: 7},
			actorText: "당신은 구덩이에 빠졌습니다!\n당신은 7점의 피해를 입었습니다.\n",
			roomText:  "\nAlice이 구덩이에 빠졌습니다.\r\n",
		},
		{
			name:      "dart",
			trap:      TrapDart,
			effect:    ArrivalTrapResult{Trap: TrapDart, Triggered: true, Poisoned: true, Damage: 4},
			actorText: "당신은 숨겨진 독화살에 맞았습니다!\n당신은 4점의 피해를 입었습니다.\n",
			roomText:  "\nAlice이 숨겨진 독화살에 맞았습니다.\r\n",
		},
		{
			name:      "block",
			trap:      TrapBlock,
			effect:    ArrivalTrapResult{Trap: TrapBlock, Triggered: true, Damage: 10},
			actorText: "당신은 커다란 돌에 맞았습니다!\n당신은 10점의 피해를 입었습니다.\n",
			roomText:  "\nAlice 위로 커다란 돌이 떨어졌습니다.\r\n",
		},
		{
			name:      "mpdam",
			trap:      TrapMPDam,
			effect:    ArrivalTrapResult{Trap: TrapMPDam, Triggered: true, MPDamage: 5, Damage: 3},
			actorText: "당신의 마음이 충격을 받았습니다!\n당신은 5점의 마력을 잃었습니다.\n당신은 3점의 피해를 입었습니다.\n",
			roomText:  "\nAlice이 강한 충격을 받았습니다.\r\n",
		},
		{
			name:      "rmspl",
			trap:      TrapRMSpl,
			effect:    ArrivalTrapResult{Trap: TrapRMSpl, Triggered: true, ClearSpells: true},
			actorText: "어두운 기운이 당신을 감쌉니다.\n당신의 주문이 사라집니다.\n",
			roomText:  "\n어두운 기운이 Alice을 감쌉니다.\r\n",
		},
		{
			name:      "naked",
			trap:      TrapNaked,
			effect:    ArrivalTrapResult{Trap: TrapNaked, Triggered: true, LoseItems: true},
			actorText: "붉은 액체가 당신위로 쏟아집니다.\n으악!!! 당신의 장비가 녹아버립니다.\n",
			roomText:  "\n붉은 액체가 Alice님위로 쏟아집니다.\r\n",
		},
		{
			name:      "alarm",
			trap:      TrapAlarm,
			effect:    ArrivalTrapResult{Trap: TrapAlarm, Triggered: true, Alarm: true},
			actorText: "경보장치가 울립니다!\n근처에 경비원들이 없길 바랍니다.\n",
			roomText:  "\nAlice이 경보장치를 건드렸습니다!\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := arrivalTrapEventFor("actor", "Alice", 2, tc.effect)
			if !ok || got.Trap != tc.trap || got.ActorID != "actor" || got.ActorName != "Alice" || got.RoomID != 2 {
				t.Fatalf("event=%+v ok=%t", got, ok)
			}
			if got.ActorText != tc.actorText || got.RoomText != tc.roomText {
				t.Fatalf("actor=%q room=%q", got.ActorText, got.RoomText)
			}
		})
	}
}

func TestArrivalTrapEventSkipsAvoidedAndSuppressedEffects(t *testing.T) {
	for _, effect := range []ArrivalTrapResult{
		{Trap: TrapDart, Avoided: true, Triggered: false},
		{Trap: TrapPit, Triggered: true, Suppressed: true},
		{Trap: TrapDart, Triggered: false},
	} {
		if got, ok := arrivalTrapEventFor("actor", "Alice", 2, effect); ok || got != (ArrivalTrapEvent{}) {
			t.Fatalf("effect=%+v produced event=%+v ok=%t", effect, got, ok)
		}
	}
}
