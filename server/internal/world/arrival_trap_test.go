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
