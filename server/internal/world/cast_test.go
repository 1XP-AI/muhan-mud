package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func castTestState(class byte, spell int) State {
	var spells [16]byte
	spells[spell/8] |= 1 << uint(spell%8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]PlayerState{"a": {
			Body: LegacyMonster{
				Name: "Alice", Type: 0, Level: 8, Class: class, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18},
				HPMax: 100, HPCurrent: 10, MPMax: 50, MPCurrent: 30,
				Spells: spells,
				Daily:  [10]LegacyDaily{{}, {}, {Max: 1, Current: 1}},
			},
			Online: true,
		}},
	}
}

func TestPlanApplyCastHealingSpellsUseSourceOrderAndTimer(t *testing.T) {
	tests := []struct {
		name       string
		class      byte
		spell      int
		spellName  string
		rolls      []int
		wantHP     int16
		wantMP     int16
		wantHPDiff int32
		wantRolls  int
	}{
		{name: "vigor", class: castClericClass, spell: castVigorSpell, spellName: "회복", rolls: []int{2, 6}, wantHP: 22, wantMP: 28, wantHPDiff: 12, wantRolls: 2},
		{name: "mend", class: castClericClass, spell: castMendSpell, spellName: "원기회복", rolls: []int{2, 6, 5}, wantHP: 29, wantMP: 26, wantHPDiff: 19, wantRolls: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := castTestState(tc.class, tc.spell)
			calls := 0
			p, err := s.PlanCast("a", tc.spellName, CastOptions{Now: 100, Roll: func(low, high int) int {
				if calls >= len(tc.rolls) {
					t.Fatalf("unexpected roll %d..%d", low, high)
				}
				value := tc.rolls[calls]
				calls++
				if value < low || value > high {
					t.Fatalf("roll %d outside %d..%d", value, low, high)
				}
				return value
			}})
			if err != nil {
				t.Fatal(err)
			}
			if calls != len(tc.rolls) || len(p.EffectRolls) != tc.wantRolls || !p.Attempted || !p.Succeeded || p.SpellFailed {
				t.Fatalf("proposal=%+v calls=%d", p, calls)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			body := next.Players["a"].Body
			wantMPDelta := int32(-tc.spell)
			if tc.spell == castVigorSpell {
				wantMPDelta = -2
			} else {
				wantMPDelta = -4
			}
			if body.HPCurrent != tc.wantHP || body.MPCurrent != tc.wantMP || result.HPDelta != tc.wantHPDiff || result.MPDelta != wantMPDelta {
				t.Fatalf("body=%+v result=%+v", body, result)
			}
			wantInterval := int32(3)
			if body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: wantInterval}) {
				t.Fatalf("spell timer=%+v", body.Timers[castSpellTimerIndex])
			}
			if result.Event == nil || result.SpellName != tc.spellName || !strings.Contains(result.Response, tc.spellName) {
				t.Fatalf("result=%+v", result)
			}
			if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
				t.Fatalf("second apply err=%v", err)
			}
		})
	}
}

func TestPlanApplyCastHealConsumesDailyAndFillsHP(t *testing.T) {
	s := castTestState(castClericClass, castHealSpell)
	actor := s.Players["a"]
	actor.Body.Daily[castDailyHealIndex] = LegacyDaily{Max: 1, Current: 1, LastTime: 0}
	s.Players["a"] = actor
	p, err := s.PlanCast("a", "완치", CastOptions{Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Attempted || !p.Succeeded || !p.DailyUsed || p.EffectRolls != nil || p.HPDelta != 90 || p.MPDelta != -20 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["a"].Body
	if body.HPCurrent != body.HPMax || body.MPCurrent != 10 || body.Daily[castDailyHealIndex].Current != 0 || result.Event == nil {
		t.Fatalf("body=%+v result=%+v", body, result)
	}
}

func TestCastFullHealAllowsCaretakerWhenDailyQuotaIsEmpty(t *testing.T) {
	s := castTestState(10, castHealSpell) // CARETAKER
	actor := s.Players["a"]
	actor.Body.Daily[castDailyHealIndex] = LegacyDaily{Max: 1, Current: 0, LastTime: 0}
	s.Players["a"] = actor
	p, err := s.PlanCast("a", "완치", CastOptions{Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Attempted || !p.Succeeded || p.DailyUsed || p.HPDelta != 90 || p.MPDelta != -20 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["a"].Body
	if body.HPCurrent != body.HPMax || body.MPCurrent != 10 || body.Daily[castDailyHealIndex].Current != 0 || !result.Succeeded {
		t.Fatalf("body=%+v result=%+v", body, result)
	}
}

func TestCastSpellFailureChargesManaClearsHiddenAndDoesNotRollEffect(t *testing.T) {
	// vigor's legacy CAST path rejects non-cleric/non-paladin classes before
	// spell_fail; mend is the direct-cast healing spell that actually reaches
	// the fighter/barbarian/assassin failure branch.
	s := castTestState(castFighterClass, castMendSpell)
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, castHiddenFlag, true)
	s.Players["a"] = actor
	calls := 0
	p, err := s.PlanCast("a", "원기회복", CastOptions{Now: 100, Roll: func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("spell-fail range=%d..%d", low, high)
		}
		return 100
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !p.Attempted || p.Succeeded || !p.SpellFailed || len(p.EffectRolls) != 0 || p.MPDelta != -4 {
		t.Fatalf("proposal=%+v calls=%d", p, calls)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["a"].Body
	if body.HPCurrent != 10 || body.MPCurrent != 26 || flag(body.Flags[:], castHiddenFlag) || result.Event != nil || !strings.Contains(result.Response, "실패") {
		t.Fatalf("body=%+v result=%+v", body, result)
	}
}

func TestCastGateResponsesAreNoOpReceipts(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*LegacyMonster)
		spell     string
		wantText  string
		wantState bool
	}{
		{name: "blind", mutate: func(body *LegacyMonster) { setSettingFlag(body, castBlindFlag, true) }, spell: "회복", wantText: "아무것도 보이지 않습니다", wantState: false},
		{name: "unknown", spell: "삭풍", wantText: "그런 주문은 존재하지 않습니다", wantState: false},
		{name: "class", mutate: func(body *LegacyMonster) { body.Class = castFighterClass }, spell: "완치", wantText: "불제자와 무사", wantState: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := castTestState(castClericClass, castVigorSpell)
			if tc.mutate != nil {
				actor := s.Players["a"]
				tc.mutate(&actor.Body)
				s.Players["a"] = actor
			}
			before := s
			p, err := s.PlanCast("a", tc.spell, CastOptions{Now: 100})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Changed != tc.wantState || !strings.Contains(result.Response, tc.wantText) || !reflect.DeepEqual(next, before) {
				t.Fatalf("next=%+v result=%+v", next, result)
			}
		})
	}
}

func TestCastClearsHiddenBeforeSpellGate(t *testing.T) {
	s := castTestState(castFighterClass, castVigorSpell)
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, castHiddenFlag, true)
	s.Players["a"] = actor
	p, err := s.PlanCast("a", "회복", CastOptions{Now: 100})
	if err != nil {
		t.Fatal(err)
	}
	if p.Attempted || p.Succeeded || p.Broadcast || !p.Changed || !p.HiddenCleared || !strings.Contains(p.Response, "불제자와 무사") {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["a"].Body
	if !result.Changed || !result.HiddenCleared || flag(body.Flags[:], castHiddenFlag) || body.MPCurrent != actor.Body.MPCurrent {
		t.Fatalf("next=%+v result=%+v", body, result)
	}
}
