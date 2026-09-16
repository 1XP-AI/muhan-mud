package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func knowAlignmentFixture() State {
	var spells [16]byte
	spells[castKnowAlignmentSpell/8] |= 1 << uint(castKnowAlignmentSpell%8)
	var roomFlags [8]byte
	roomFlags[npcTalkRoomMagicExtend/8] |= 1 << uint(npcTalkRoomMagicExtend%8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장", Flags: roomFlags}},
				PlayerIDs: []string{"a", "b", "c", "d"},
			},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{
				Name: "Alice", Type: 0, Class: castClericClass, Level: 8, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18}, MPMax: 50, MPCurrent: 30, Spells: spells,
			}, Online: true},
			"b": {Body: LegacyMonster{Name: "Bob", Keys: [3]string{"Bobster"}, Type: 0, Class: castFighterClass, Level: 4, RoomID: 1}, Online: true},
			"c": {Body: LegacyMonster{Name: "Bobby", Type: 0, Class: castFighterClass, Level: 4, RoomID: 1}, Online: true},
			"d": {Body: LegacyMonster{Name: "Carol", Type: 0, Class: castFighterClass, Level: 4, RoomID: 1}, Online: true},
		},
	}
}

func TestPlanApplyKnowAlignmentTargetsCanonicalRoomOccurrenceWithoutRNG(t *testing.T) {
	s := knowAlignmentFixture()
	original := s
	calls := 0
	p, err := s.PlanCast("a", "선악", CastOptions{
		Now: 100, Target: "Bob", Occurrence: 2,
		Roll: func(int, int) int {
			calls++
			t.Fatal("know-alignment target unexpectedly consumed RNG")
			return 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || p.TargetID != "c" || p.TargetOccurrence != 2 || !p.Attempted || !p.Succeeded || !p.Broadcast || p.Cost != 6 || p.MPDelta != -6 {
		t.Fatalf("proposal=%+v calls=%d", p, calls)
	}
	if p.TimedFlag != castKnowAlignmentFlag || p.TimedInterval != 3200 || len(p.EffectRolls) != 0 {
		t.Fatalf("timed proposal=%+v", p)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated source state")
	}
	if next.Players["a"].Body.MPCurrent != 24 || next.Players["b"].Body.Flags != original.Players["b"].Body.Flags {
		t.Fatalf("caster/first target body=%+v/%+v", next.Players["a"].Body, next.Players["b"].Body)
	}
	target := next.Players["c"].Body
	if !flag(target.Flags[:], castKnowAlignmentFlag) || target.Timers[castKnowAlignmentTimer] != (LegacyTimer{LastTime: 100, Interval: 3200}) {
		t.Fatalf("target body=%+v", target)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("caster spell timer=%+v", next.Players["a"].Body.Timers[castSpellTimerIndex])
	}
	wantRoom := "\nAlice이 Bobby에게 선악감지 주문을 외웁니다.\r\n그는 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n"
	wantTarget := "\nAlice이 당신에게 선악감지 주문을 외웁니다.\r\n당신은 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n"
	if result.Response != "당신은 Bobby에게 선악감지 주문을 외웁니다.\r\n" || result.TargetID != "c" || result.TargetName != "Bobby" || result.TargetText != wantTarget || result.Event == nil || result.Event.Text != wantRoom || result.Event.ExcludeTargetID != "c" {
		t.Fatalf("result=%+v", result)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("reapplied know-alignment proposal err=%v", err)
	}
}

func TestPlanKnowAlignmentUsesLegacyKeysAndDoesNotDoubleCount(t *testing.T) {
	s := knowAlignmentFixture()
	delete(s.Players, "d")
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b", "c"}
	s.Rooms[1] = room
	p, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob", Occurrence: 1})
	if err != nil || p.TargetID != "b" {
		t.Fatalf("name match proposal=%+v err=%v", p, err)
	}
	p, err = s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bobst", Occurrence: 1})
	if err != nil || p.TargetID != "b" {
		t.Fatalf("key match proposal=%+v err=%v", p, err)
	}
	p, err = s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob", Occurrence: 2})
	if err != nil || p.TargetID != "c" {
		t.Fatalf("occurrence proposal=%+v err=%v", p, err)
	}
}

func TestPlanKnowAlignmentVisibilityAndMalformedRoomFailClosed(t *testing.T) {
	s := knowAlignmentFixture()
	delete(s.Players, "c")
	delete(s.Players, "d")
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = room
	target := s.Players["b"]
	setSettingFlag(&target.Body, castInvisibilityFlag, true)
	s.Players["b"] = target
	p, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil || p.Attempted || p.Response != knowAlignmentMissingResponse {
		t.Fatalf("invisible target was admitted: proposal=%+v err=%v", p, err)
	}
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, castDetectInvisibleFlag, true)
	s.Players["a"] = actor
	p, err = s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil || p.TargetID != "b" || !p.Attempted {
		t.Fatalf("detected invisible target rejected: proposal=%+v err=%v", p, err)
	}
	target = s.Players["b"]
	target.Body.Class = summonCaretakerClass
	setSettingFlag(&target.Body, summonDMInvisibleFlag, true)
	s.Players["b"] = target
	p, err = s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil || p.Attempted || p.Response != knowAlignmentMissingResponse {
		t.Fatalf("caretaker PDMINV target was admitted: proposal=%+v err=%v", p, err)
	}
	malformed := knowAlignmentFixture()
	room = malformed.Rooms[1]
	room.PlayerIDs = []string{"a", "missing"}
	malformed.Rooms[1] = room
	if _, err := malformed.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"}); err == nil {
		t.Fatal("malformed authoritative room identity was accepted")
	}
}

func TestPlanKnowAlignmentGatesStillClearHiddenWithoutCharging(t *testing.T) {
	for _, tc := range []struct {
		name       string
		prepare    func(*State)
		wantResult string
	}{
		{name: "mana", prepare: func(s *State) {
			actor := s.Players["a"]
			actor.Body.MPCurrent = 5
			setSettingFlag(&actor.Body, castHiddenFlag, true)
			s.Players["a"] = actor
		}, wantResult: "당신의 도력이 부족합니다.\r\n"},
		{name: "unlearned", prepare: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells[castKnowAlignmentSpell/8] &^= 1 << uint(castKnowAlignmentSpell%8)
			setSettingFlag(&actor.Body, castHiddenFlag, true)
			s.Players["a"] = actor
		}, wantResult: "당신은 아직 그 주술을 터득하지 못했습니다.\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := knowAlignmentFixture()
			tc.prepare(&s)
			p, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob", Roll: func(int, int) int { t.Fatal("gate consumed RNG"); return 1 }})
			if err != nil || p.Attempted || !p.HiddenCleared || !p.Changed || p.Response != tc.wantResult {
				t.Fatalf("proposal=%+v err=%v", p, err)
			}
			next, result, err := s.ApplyCast(p)
			nextActor := next.Players["a"]
			if err != nil || result.Response != tc.wantResult || nextActor.Body.MPCurrent != s.Players["a"].Body.MPCurrent || flag(nextActor.Body.Flags[:], castHiddenFlag) {
				t.Fatalf("next=%+v result=%+v err=%v", nextActor.Body, result, err)
			}
			nextTarget := next.Players["b"]
			if flag(nextTarget.Body.Flags[:], castKnowAlignmentFlag) {
				t.Fatal("gate mutated target")
			}
		})
	}
}

func TestApplyKnowAlignmentRejectsTargetAndReceiptTampering(t *testing.T) {
	s := knowAlignmentFixture()
	p, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	stale := s.clone()
	target := stale.Players["b"]
	target.Body.Alignment++
	stale.Players["b"] = target
	if _, _, err := stale.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale target accepted: %v", err)
	}
	tampered := p
	tampered.TimedInterval++
	if _, _, err := s.ApplyCast(tampered); !errors.Is(err, ErrCastInvalidProposal) {
		t.Fatalf("tampered interval accepted: %v", err)
	}
	tampered = p
	tampered.afterTarget.Body.Flags[0] ^= 1
	if _, _, err := s.ApplyCast(tampered); !errors.Is(err, ErrCastInvalidProposal) {
		t.Fatalf("tampered target projection accepted: %v", err)
	}
	room := s.Rooms[1]
	room.PlayerIDs[1], room.PlayerIDs[2] = room.PlayerIDs[2], room.PlayerIDs[1]
	s.Rooms[1] = room
	if _, _, err := s.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale room order accepted: %v", err)
	}
	if !strings.Contains(p.Response, "선악감지") {
		t.Fatalf("proposal response=%q", p.Response)
	}
}

func TestApplyKnowAlignmentTargetTamperingCannotFallThroughToSelfCast(t *testing.T) {
	s := knowAlignmentFixture()
	targeted, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	self, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100})
	if err != nil {
		t.Fatal(err)
	}

	// These are the fields an untrusted submitted proposal could rewrite to
	// make the generic reducer interpret a targeted receipt as a self-cast.
	tampered := targeted
	tampered.TargetName = ""
	tampered.TargetID = ""
	tampered.TargetOccurrence = 0
	tampered.Response = self.Response
	tampered.RoomText = self.RoomText
	tampered.TargetText = ""
	tampered.expectedTarget = PlayerState{}
	tampered.expectedTargetSet = false
	tampered.afterTarget = PlayerState{}
	tampered.afterBody = self.afterBody

	if _, _, err := s.ApplyCast(tampered); !errors.Is(err, ErrCastInvalidProposal) {
		t.Fatalf("targeted proposal fell through to self-cast: %v", err)
	}
}

func TestApplyKnowAlignmentRejectsIndividualTargetProjectionTampering(t *testing.T) {
	s := knowAlignmentFixture()
	proposal, err := s.PlanCast("a", "선악감지", CastOptions{Now: 100, Target: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*CastProposal)
	}{
		{name: "target name", mutate: func(p *CastProposal) { p.TargetName = "Alice" }},
		{name: "target id", mutate: func(p *CastProposal) { p.TargetID = p.ActorID }},
		{name: "response", mutate: func(p *CastProposal) { p.Response = "위조된 응답\r\n" }},
		{name: "room text", mutate: func(p *CastProposal) { p.RoomText = "위조된 방 출력\r\n" }},
		{name: "expected target snapshot", mutate: func(p *CastProposal) { p.expectedTarget.Body.Alignment++ }},
		{name: "after target snapshot", mutate: func(p *CastProposal) { p.afterTarget.Body.Alignment++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := proposal
			tt.mutate(&tampered)
			if _, _, err := s.ApplyCast(tampered); err == nil {
				t.Fatal("tampered targeted proposal was accepted")
			}
		})
	}
}
