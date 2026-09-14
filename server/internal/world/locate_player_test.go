package world

import (
	"errors"
	"strings"
	"testing"
)

func locateSpellBits() [16]byte {
	var spells [16]byte
	spells[castLocateSpell/8] |= 1 << uint(castLocateSpell%8)
	return spells
}

func locateTestState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}},
				PlayerIDs: []string{"a"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "광장"}},
				PlayerIDs: []string{"b"},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body: LegacyMonster{
					Name: "Alice", Type: 0, Class: castMageClass, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 30,
					Spells: locateSpellBits(),
				},
				Online: true,
			},
			"b": {
				Body: LegacyMonster{
					Name: "Bob", Type: 0, Class: castFighterClass, Level: 8, RoomID: 2,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 80, HPCurrent: 80, MPMax: 20, MPCurrent: 20,
				},
				Online: true,
			},
		},
	}
}

func locateRolls(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	i := 0
	return func(low, high int) int {
		if i >= len(values) {
			t.Fatalf("unexpected extra roll at %d", i)
		}
		if low != 1 || high != 100 {
			t.Fatalf("unexpected locate bounds %d..%d", low, high)
		}
		value := values[i]
		i++
		return value
	}
}

func TestIsLocateCastSpellMatchesSourcePrefix(t *testing.T) {
	if !IsLocateCastSpell("천리안") || !IsLocateCastSpell("천리") || !IsLocateCastSpell("천") {
		t.Fatal("expected unique SLOCAT prefixes to match")
	}
	if IsLocateCastSpell("회복") || IsLocateCastSpell("천리안술") || IsLocateCastSpell("") {
		t.Fatal("non-locate tokens matched SLOCAT")
	}
}

func TestPlanApplyLocateGatesFollowSourceOrder(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*State)
		target   string
		want     string
		wantHide bool
	}{
		{name: "unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			s.Players["a"] = actor
		}, target: "Bob", want: "그런 주문을 터득하지 못했습니다"},
		{name: "mana", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.MPCurrent = 14
			s.Players["a"] = actor
		}, target: "Bob", want: "도력이 부족합니다"},
		{name: "prompt", target: "", want: "누구와 연결합니까"},
		{name: "missing", target: "Carol", want: "그런 사람은 존재하지 않습니다"},
		{name: "offline", mutate: func(s *State) {
			s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", Type: 0, RoomID: 1}, Online: false}
		}, target: "Carol", want: "그런 사람은 존재하지 않습니다"},
		{name: "dm-invis", mutate: func(s *State) {
			target := s.Players["b"]
			setSettingFlag(&target.Body, locateDMInvisibleFlag, true)
			s.Players["b"] = target
		}, target: "Bob", want: "그런 사람은 존재하지 않습니다"},
		{name: "invis-without-detect", mutate: func(s *State) {
			target := s.Players["b"]
			setSettingFlag(&target.Body, locateInvisibleFlag, true)
			s.Players["b"] = target
		}, target: "Bob", want: "그런 사람은 존재하지 않습니다"},
		{name: "dm-class", mutate: func(s *State) {
			target := s.Players["b"]
			target.Body.Class = locateDMClass
			s.Players["b"] = target
		}, target: "Bob", want: "정신력이 너무 높아"},
		{name: "married", mutate: func(s *State) {
			target := s.Players["b"]
			setSettingFlag(&target.Body, locateMarriedFlag, true)
			s.Players["b"] = target
		}, target: "Bob", want: "사생활은 엿볼 수가 없습니다"},
		{name: "hidden-unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			setSettingFlag(&actor.Body, castHiddenFlag, true)
			s.Players["a"] = actor
		}, target: "Bob", want: "그런 주문을 터득하지 못했습니다", wantHide: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := locateTestState()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			before := s
			p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: tc.target, Roll: locateRolls(t, 1)})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Attempted || result.Succeeded || result.Broadcast || !strings.Contains(result.Response, tc.want) {
				t.Fatalf("result=%+v", result)
			}
			if tc.wantHide {
				body := next.Players["a"].Body
				if !result.Changed || !result.HiddenCleared || flag(body.Flags[:], castHiddenFlag) {
					t.Fatalf("hidden not cleared: %+v", result)
				}
				return
			}
			if result.Changed || !reflectLocateState(next, before) {
				t.Fatalf("no-op mutated state result=%+v", result)
			}
		})
	}
}

func reflectLocateState(got, want State) bool {
	return got.Players["a"].Body.MPCurrent == want.Players["a"].Body.MPCurrent &&
		got.Players["a"].Body.Timers[castSpellTimerIndex] == want.Players["a"].Body.Timers[castSpellTimerIndex] &&
		got.Players["b"].Body.MPCurrent == want.Players["b"].Body.MPCurrent &&
		got.Players["b"].Body.Flags == want.Players["b"].Body.Flags
}

func TestPlanApplyLocateLowercizeFindWhoAndDetectInvis(t *testing.T) {
	s := locateTestState()
	target := s.Players["b"]
	setSettingFlag(&target.Body, locateInvisibleFlag, true)
	s.Players["b"] = target
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, locateDetectInvisibleFlag, true)
	s.Players["a"] = actor

	p, err := s.PlanCast("a", "천리", CastOptions{Now: 100, Target: "bob", Hour: 12, Roll: locateRolls(t, 1, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.TargetID != "b" || result.TargetName != "Bob" || !result.LocateLinked || next.Players["a"].Body.MPCurrent != 15 {
		t.Fatalf("result=%+v mp=%d", result, next.Players["a"].Body.MPCurrent)
	}
	if !strings.Contains(result.Response, "마음을 Bob에게 집중") || !strings.Contains(result.Response, "광장") {
		t.Fatalf("response=%q", result.Response)
	}
}

func TestPlanApplyLocateSpellFailConsumesManaWithoutTimer(t *testing.T) {
	s := locateTestState()
	p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Roll: locateRolls(t, 100)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellFailed || result.Succeeded || result.Broadcast || result.Event != nil || next.Players["a"].Body.MPCurrent != 15 {
		t.Fatalf("result=%+v mp=%d", result, next.Players["a"].Body.MPCurrent)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex].LastTime != 0 || !strings.Contains(result.Response, "주문이 실패") {
		t.Fatalf("timer/response %+v %q", next.Players["a"].Body.Timers[castSpellTimerIndex], result.Response)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("replayed locate fail err=%v", err)
	}
}

func TestPlanApplyLocateVisionMissStillCasts(t *testing.T) {
	s := locateTestState()
	p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: locateRolls(t, 1, 100, 1)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.LocateLinked || !result.Broadcast || next.Players["a"].Body.MPCurrent != 15 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, locateUnlinkedResponse) || !strings.Contains(result.TargetText, "보려합니다") {
		t.Fatalf("response=%q target=%q", result.Response, result.TargetText)
	}
	if result.Event == nil || !strings.Contains(result.Event.Text, "천리안 주문을 외웠습니다") {
		t.Fatalf("event=%+v", result.Event)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("timer=%+v", next.Players["a"].Body.Timers[castSpellTimerIndex])
	}
}

func TestPlanApplyLocateSubDMTargetSkipsVisionRoll(t *testing.T) {
	s := locateTestState()
	target := s.Players["b"]
	target.Body.Class = locateSubDMClass
	s.Players["b"] = target
	calls := 0
	p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("bounds %d..%d", low, high)
		}
		return 1
	}})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.LocateLinked || !strings.Contains(result.Response, locateUnlinkedResponse) || next.Players["a"].Body.MPCurrent != 15 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestPlanApplyLocateDMCasterCanReadDM(t *testing.T) {
	s := locateTestState()
	actor := s.Players["a"]
	actor.Body.Class = locateDMClass
	s.Players["a"] = actor
	target := s.Players["b"]
	target.Body.Class = locateDMClass
	s.Players["b"] = target
	p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: locateRolls(t, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.LocateLinked || !result.Succeeded || !strings.Contains(result.Response, locateUnlinkedResponse) {
		t.Fatalf("result=%+v", result)
	}
}

func TestPlanApplyLocateUnmigratedDarkRoomFailsClosed(t *testing.T) {
	s := locateTestState()
	room := s.Rooms[2]
	room.Resource.Flags[1] = 1 << 0 // RF 8 / always dark
	s.Rooms[2] = room
	_, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Roll: locateRolls(t)})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyLocateLinkedSceneAndNotify(t *testing.T) {
	s := locateTestState()
	p, err := s.PlanCast("a", "천리안", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: locateRolls(t, 1, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.LocateLinked || result.MPDelta != -15 || result.TargetText == "" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, "광장") || !strings.Contains(result.TargetText, "주위를 보고 있습니다") {
		t.Fatalf("response=%q target=%q", result.Response, result.TargetText)
	}
	if next.Players["b"].Body.MPCurrent != 20 {
		t.Fatal("target state mutated")
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}
