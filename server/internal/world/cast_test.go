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

func castCombatTestState(class byte, spell int) State {
	s := castTestState(class, spell)
	actor := s.Players["a"]
	actor.Items = &ItemCollection{Items: map[string]Item{}}
	stats, err := actor.Items.CombatStats(actor.Body)
	if err != nil {
		panic(err)
	}
	actor.Body.Armor, actor.Body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	s.Players["a"] = actor
	return s
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

func TestPlanApplyCastTimedSelfSpellsUseSourceFlagsAndIntervals(t *testing.T) {
	tests := []struct {
		name         string
		class        byte
		spell        int
		spellName    string
		flag         uint
		timer        int
		wantInterval int32
		wantCost     int16
		wantGlobal   int32
		wantRolls    int
	}{
		// INT 18 has bonus 2; a level-8 mage adds 120 seconds and RPMEXT adds 600.
		{name: "light mage", class: castMageClass, spell: castLightSpell, spellName: "발광", flag: castLightFlag, timer: castLightTimer, wantInterval: 1500, wantCost: 5, wantGlobal: 3, wantRolls: 1},
		{name: "invisibility mage", class: castMageClass, spell: castInvisibilitySpell, spellName: "은둔법", flag: castInvisibilityFlag, timer: castInvisibilityTimer, wantInterval: 3120, wantCost: 15, wantGlobal: 3, wantRolls: 1},
		{name: "detect invisible mage", class: castMageClass, spell: castDetectInvisibleSpell, spellName: "은둔감지술", flag: castDetectInvisibleFlag, timer: castDetectInvisibleTimer, wantInterval: 3120, wantCost: 10, wantGlobal: 3, wantRolls: 1},
		{name: "detect magic mage", class: castMageClass, spell: castDetectMagicSpell, spellName: "주문감지술", flag: castDetectMagicFlag, timer: castDetectMagicTimer, wantInterval: 3120, wantCost: 10, wantGlobal: 3, wantRolls: 1},
		{name: "levitate mage", class: castMageClass, spell: castLevitateSpell, spellName: "부양술", flag: castLevitateFlag, timer: castLevitateTimer, wantInterval: 4400, wantCost: 10, wantGlobal: 3, wantRolls: 1},
		{name: "resist fire mage", class: castMageClass, spell: castResistFireSpell, spellName: "방열진", flag: castResistFireFlag, timer: castResistFireTimer, wantInterval: 3200, wantCost: 12, wantGlobal: 3, wantRolls: 1},
		{name: "fly mage", class: castMageClass, spell: castFlySpell, spellName: "비상술", flag: castFlyFlag, timer: castFlyTimer, wantInterval: 3000, wantCost: 15, wantGlobal: 3, wantRolls: 1},
		{name: "resist magic mage", class: castMageClass, spell: castResistMagicSpell, spellName: "보마진", flag: castResistMagicFlag, timer: castResistMagicTimer, wantInterval: 3200, wantCost: 12, wantGlobal: 3, wantRolls: 1},
		{name: "resist cold mage", class: castMageClass, spell: castResistColdSpell, spellName: "방한진", flag: castResistColdFlag, timer: castResistColdTimer, wantInterval: 3200, wantCost: 12, wantGlobal: 3, wantRolls: 1},
		{name: "water breathing mage", class: castMageClass, spell: castWaterSpell, spellName: "수생술", flag: castWaterFlag, timer: castWaterTimer, wantInterval: 3200, wantCost: 12, wantGlobal: 3, wantRolls: 1},
		{name: "earth shield mage", class: castMageClass, spell: castEarthShieldSpell, spellName: "지방호", flag: castEarthShieldFlag, timer: castEarthShieldTimer, wantInterval: 3200, wantCost: 12, wantGlobal: 3, wantRolls: 1},
		// A cleric does not receive the mage term; know-alignment uses RPMEXT +800.
		{name: "know alignment cleric", class: castClericClass, spell: castKnowAlignmentSpell, spellName: "선악감지", flag: castKnowAlignmentFlag, timer: castKnowAlignmentTimer, wantInterval: 3200, wantCost: 6, wantGlobal: 3, wantRolls: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := castTestState(tc.class, tc.spell)
			room := s.Rooms[1]
			room.Resource.Flags[npcTalkRoomMagicExtend/8] |= 1 << (npcTalkRoomMagicExtend % 8)
			s.Rooms[1] = room
			calls := 0
			p, err := s.PlanCast("a", tc.spellName, CastOptions{Now: 100, Roll: func(low, high int) int {
				calls++
				if low != 1 || high != 100 {
					t.Fatalf("spell-fail range=%d..%d", low, high)
				}
				return 1
			}})
			if err != nil {
				t.Fatal(err)
			}
			if calls != tc.wantRolls || !p.Attempted || !p.Succeeded || p.SpellFailed || len(p.EffectRolls) != 0 || p.HPDelta != 0 || p.TimedFlag != tc.flag || p.TimedInterval != tc.wantInterval {
				t.Fatalf("proposal=%+v calls=%d", p, calls)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			body := next.Players["a"].Body
			if body.MPCurrent != 30-tc.wantCost || !flag(body.Flags[:], tc.flag) || body.Timers[tc.timer] != (LegacyTimer{LastTime: 100, Interval: tc.wantInterval}) || body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: tc.wantGlobal}) {
				t.Fatalf("body=%+v", body)
			}
			if result.TimedFlag != tc.flag || result.TimedInterval != tc.wantInterval || result.HPDelta != 0 || result.Event == nil || !strings.Contains(result.Response, tc.spellName) {
				t.Fatalf("result=%+v", result)
			}
			bad := p
			bad.TimedInterval++
			if _, _, err := s.ApplyCast(bad); !errors.Is(err, ErrCastInvalidProposal) {
				t.Fatalf("tampered timed receipt err=%v", err)
			}
		})
	}
}

func TestPlanApplyCastCombatBuffsRefreshCanonicalStats(t *testing.T) {
	tests := []struct {
		name         string
		spell        int
		spellName    string
		flag         uint
		timer        int
		wantArmor    byte
		wantThaco    byte
		wantInterval int32
	}{
		// Cleric INT 18 contributes +1200, the class level-band contributes
		// +120, and the room has no RPMEXT extension.
		{name: "bless", spell: castBlessSpell, spellName: "성현진", flag: castBlessFlag, timer: castBlessTimer, wantArmor: 100, wantThaco: 20, wantInterval: 2520},
		{name: "protection", spell: castProtectionSpell, spellName: "수호진", flag: castProtectionFlag, timer: castProtectionTimer, wantArmor: 90, wantThaco: 20, wantInterval: 2520},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := castCombatTestState(castClericClass, tc.spell)
			calls := 0
			p, err := s.PlanCast("a", tc.spellName, CastOptions{Now: 100, Roll: func(low, high int) int {
				calls++
				if low != 1 || high != 100 {
					t.Fatalf("spell-fail range=%d..%d", low, high)
				}
				return 1
			}})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !p.Attempted || !p.Succeeded || p.SpellFailed || p.Cost != 10 || p.TimedFlag != tc.flag || p.TimedInterval != tc.wantInterval {
				t.Fatalf("proposal=%+v calls=%d", p, calls)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			body := next.Players["a"].Body
			if !flag(body.Flags[:], tc.flag) || body.Timers[tc.timer] != (LegacyTimer{LastTime: 100, Interval: tc.wantInterval}) || body.MPCurrent != 20 || body.Armor != tc.wantArmor || body.Thaco != tc.wantThaco {
				t.Fatalf("body=%+v", body)
			}
			if result.Event == nil || !result.Succeeded || result.TimedInterval != tc.wantInterval || !strings.Contains(result.Response, tc.spellName) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestCastCombatBuffRequiresCanonicalEquipmentBeforeRandom(t *testing.T) {
	s := castTestState(castClericClass, castBlessSpell)
	calls := 0
	_, err := s.PlanCast("a", "성현진", CastOptions{Now: 100, Roll: func(int, int) int {
		calls++
		return 1
	}})
	if !errors.Is(err, ErrCastSpellUnavailable) || calls != 0 {
		t.Fatalf("err=%v random calls=%d", err, calls)
	}
}

func TestCastInvisibilityCombatGateClearsHiddenWithoutRolling(t *testing.T) {
	s := castTestState(castMageClass, castInvisibilitySpell)
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true}
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.PlayerEnemies = []string{"b"}
	setSettingFlag(&actor.Body, castHiddenFlag, true)
	s.Players["a"] = actor
	calls := 0
	p, err := s.PlanCast("a", "은둔법", CastOptions{Now: 100, Roll: func(int, int) int {
		calls++
		return 1
	}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || p.Attempted || p.Succeeded || !p.Changed || !p.HiddenCleared || p.TimedFlag != 0 || !strings.Contains(p.Response, "싸우고") {
		t.Fatalf("proposal=%+v calls=%d", p, calls)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["a"].Body
	if flag(body.Flags[:], castHiddenFlag) || body.MPCurrent != actor.Body.MPCurrent || result.Attempted || result.TimedFlag != 0 || result.TimedInterval != 0 {
		t.Fatalf("body=%+v result=%+v", body, result)
	}
}

func TestPlanApplyCastCleansingSelfSpells(t *testing.T) {
	tests := []struct {
		name       string
		class      byte
		spell      int
		spellName  string
		flag       uint
		initial    bool
		want       bool
		wantCost   int16
		wantGlobal int32
	}{
		{name: "cure poison", class: castMageClass, spell: castCurePoisonSpell, spellName: "해독", flag: castCurePoisonFlag, initial: true, want: false, wantCost: 6, wantGlobal: 3},
		{name: "remove disease", class: castClericClass, spell: castDiseaseSpell, spellName: "치료", flag: castDiseaseFlag, initial: true, want: false, wantCost: 12, wantGlobal: 3},
		// rm_blind is admitted for the canonical class gate; magic1.c's PBLIND
		// gate is tested separately, so a visible caster has no flag to clear.
		{name: "remove blindness", class: castPaladinClass, spell: castRemoveBlindSpell, spellName: "개안술", flag: castRemoveBlindFlag, initial: false, want: false, wantCost: 12, wantGlobal: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := castTestState(tc.class, tc.spell)
			actor := s.Players["a"]
			setSettingFlag(&actor.Body, tc.flag, tc.initial)
			s.Players["a"] = actor
			calls := 0
			p, err := s.PlanCast("a", tc.spellName, CastOptions{Now: 100, Roll: func(low, high int) int {
				calls++
				if low != 1 || high != 100 {
					t.Fatalf("spell-fail range=%d..%d", low, high)
				}
				return 1
			}})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !p.Attempted || !p.Succeeded || p.SpellFailed || p.TimedFlag != 0 || p.TimedInterval != 0 || len(p.EffectRolls) != 0 || p.HPDelta != 0 || p.MPDelta != -int32(tc.wantCost) {
				t.Fatalf("proposal=%+v calls=%d", p, calls)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			body := next.Players["a"].Body
			if flag(body.Flags[:], tc.flag) != tc.want || body.MPCurrent != 30-tc.wantCost || body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: tc.wantGlobal}) {
				t.Fatalf("body=%+v", body)
			}
			if result.Event == nil || !strings.Contains(result.Response, tc.spellName) || result.TimedFlag != 0 || result.TimedInterval != 0 {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
