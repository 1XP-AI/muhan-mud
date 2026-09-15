package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func recallSpellBits() [16]byte {
	var spells [16]byte
	spells[castRecallSpell/8] |= 1 << uint(castRecallSpell%8)
	return spells
}

func recallTestState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}},
				PlayerIDs: []string{"a"},
			},
			recallSquareRoom: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: recallSquareRoom, Name: "광장"}},
				PlayerIDs: []string{},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body: LegacyMonster{
					Name: "Alice", Type: 0, Class: castClericClass, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 80, MPCurrent: 40,
					Spells: recallSpellBits(),
				},
				Online: true,
			},
		},
	}
}

func TestIsRecallCastSpellMatchesSourcePrefix(t *testing.T) {
	if !IsRecallCastSpell("귀환") || !IsRecallCastSpell("귀") {
		t.Fatal("expected unique SRECAL prefixes to match")
	}
	if IsRecallCastSpell("회복") || IsRecallCastSpell("소환") || IsRecallCastSpell("천리안") || IsRecallCastSpell("") {
		t.Fatal("non-recall tokens matched SRECAL")
	}
	if IsTargetedCastSpell("귀환") || IsTargetedCastSpell("귀") {
		t.Fatal("SRECAL targeting is admitted by the recall-specific parser boundary")
	}
}

func TestPlanApplyRecallGatesFollowSourceOrder(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*State)
		want     string
		wantHide bool
	}{
		{name: "mana", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.MPCurrent = 29
			s.Players["a"] = actor
		}, want: "당신이 도력이 부족합니다"},
		{name: "class-mage", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Class = castMageClass
			s.Players["a"] = actor
		}, want: "불제자만이 이 주술을 사용할 수 있습니다"},
		{name: "class-paladin", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Class = castPaladinClass
			s.Players["a"] = actor
		}, want: "불제자만이 이 주술을 사용할 수 있습니다"},
		{name: "unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			s.Players["a"] = actor
		}, want: "그런 주문을 터득하지 못했습니다"},
		{name: "hidden-unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			setSettingFlag(&actor.Body, castHiddenFlag, true)
			s.Players["a"] = actor
		}, want: "그런 주문을 터득하지 못했습니다", wantHide: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := recallTestState()
			tc.mutate(&s)
			p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Roll: func(int, int) int {
				t.Fatal("recall gate invoked RNG")
				return 0
			}})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Succeeded || result.Broadcast || result.Attempted || !strings.Contains(result.Response, tc.want) {
				t.Fatalf("result=%+v", result)
			}
			if next.Players["a"].Body.RoomID != 1 {
				t.Fatalf("gate moved actor: %+v", next.Players["a"].Body)
			}
			if tc.wantHide {
				body := next.Players["a"].Body
				if !result.Changed || !result.HiddenCleared || flag(body.Flags[:], castHiddenFlag) {
					t.Fatalf("hidden not cleared: %+v", result)
				}
				return
			}
			if result.MPDelta != 0 || next.Players["a"].Body.MPCurrent != s.Players["a"].Body.MPCurrent {
				t.Fatalf("gate consumed mana %+v", result)
			}
		})
	}
}

func TestPlanApplyRecallMovesCasterToSquareWithoutRng(t *testing.T) {
	s := recallTestState()
	p, err := s.PlanCast("a", "귀", CastOptions{Now: 100, Hour: 12, Roll: func(int, int) int {
		t.Fatal("recall success invoked RNG")
		return 0
	}})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "귀환" || result.SpellIndex != castRecallSpell || result.Cost != 30 || result.MPDelta != -30 {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.RoomID != recallSquareRoom || !containsString(next.Rooms[recallSquareRoom].PlayerIDs, "a") || containsString(next.Rooms[1].PlayerIDs, "a") {
		t.Fatalf("occupancy dest=%v source=%v room=%d", next.Rooms[recallSquareRoom].PlayerIDs, next.Rooms[1].PlayerIDs, next.Players["a"].Body.RoomID)
	}
	if next.Rooms[recallSquareRoom].Resource.BeenHere != 1 || next.Players["a"].Body.MPCurrent != 10 {
		t.Fatalf("beenhere=%d mp=%d", next.Rooms[recallSquareRoom].Resource.BeenHere, next.Players["a"].Body.MPCurrent)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("timer=%+v", next.Players["a"].Body.Timers[castSpellTimerIndex])
	}
	if !strings.HasPrefix(result.Response, recallCasterText) || !strings.Contains(result.Response, "광장") {
		t.Fatalf("response=%q", result.Response)
	}
	if result.Event == nil || result.Event.RoomID != 1 || result.Event.ExcludeActorID != "a" || !strings.Contains(result.Event.Text, "그녀 자신에게 귀환") {
		t.Fatalf("event=%+v", result.Event)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestPlanApplyRecallInvincibleClassAndMaleBroadcast(t *testing.T) {
	s := recallTestState()
	actor := s.Players["a"]
	actor.Body.Class = castInvincible
	setSettingFlag(&actor.Body, recallMaleFlag, true)
	s.Players["a"] = actor
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.MPDelta != -30 || next.Players["a"].Body.RoomID != recallSquareRoom {
		t.Fatalf("result=%+v room=%d", result, next.Players["a"].Body.RoomID)
	}
	if result.Event == nil || !strings.Contains(result.Event.Text, "그 자신에게 귀환") {
		t.Fatalf("event=%+v", result.Event)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex].Interval != 5 {
		t.Fatalf("invincible timer=%+v", next.Players["a"].Body.Timers[castSpellTimerIndex])
	}
}

func TestPlanApplyRecallMissingSquareFailsClosed(t *testing.T) {
	s := recallTestState()
	delete(s.Rooms, recallSquareRoom)
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyRecallTargetedFormMovesTarget(t *testing.T) {
	s := recallTestState()
	actor := s.Players["a"]
	actor.Body.RoomID = 2
	s.Players["a"] = actor
	s.Players["b"] = PlayerState{Body: LegacyMonster{
		Name: "Bob", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2,
		Stats: [5]byte{12, 12, 12, 18, 18}, HPMax: 90, HPCurrent: 73,
		MPMax: 40, MPCurrent: 21, Spells: [16]byte{3},
	}, Online: true}
	s.Players["c"] = PlayerState{Body: LegacyMonster{
		Name: "Carol", Type: 0, Class: castFighterClass, Level: 4, RoomID: 1,
		HPMax: 80, HPCurrent: 80,
	}, Online: true}
	s.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"c"}}
	s.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "마을"}}, PlayerIDs: []string{"a", "b"}}
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bob"})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.TargetID != "b" || result.TargetName != "Bob" || result.MPDelta != -30 {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.RoomID != 2 || next.Players["a"].Body.MPCurrent != 10 {
		t.Fatalf("caster moved or wrong MP: %+v", next.Players["a"].Body)
	}
	if next.Players["b"].Body.RoomID != 1 || next.Players["b"].Body.HPCurrent != 73 || next.Players["b"].Body.MPCurrent != 21 {
		t.Fatalf("target body=%+v", next.Players["b"].Body)
	}
	if len(next.Rooms[2].PlayerIDs) != 1 || next.Rooms[2].PlayerIDs[0] != "a" || len(next.Rooms[1].PlayerIDs) != 2 || next.Rooms[1].PlayerIDs[0] != "b" || next.Rooms[1].PlayerIDs[1] != "c" || next.Rooms[1].Resource.BeenHere != 1 {
		t.Fatalf("occupancy source=%v dest=%v been=%d", next.Rooms[2].PlayerIDs, next.Rooms[1].PlayerIDs, next.Rooms[1].Resource.BeenHere)
	}
	if result.Response != "귀환 주문을 Bob에게 외웠습니다.\r\n" || result.TargetText != "Alice이 당신에게 귀환 주문을 외웠습니다.\r\n" || result.Event == nil || result.Event.RoomID != 2 || result.Event.ExcludeActorID != "a" || result.Event.ExcludeTargetID != "b" || result.Event.Text != "Alice이 Bob에게 귀환 주문을 외웠습니다." {
		t.Fatalf("messages result=%+v", result)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func recallDuplicateTargetState() State {
	state := recallTestState()
	actor := state.Players["a"]
	actor.Body.RoomID = 2
	state.Players["a"] = actor
	state.Players["b"] = PlayerState{Body: LegacyMonster{
		Name: "Bob", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2,
		HPMax: 90, HPCurrent: 70,
	}, Online: true}
	state.Players["c"] = PlayerState{Body: LegacyMonster{
		Name: "Bob", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2,
		HPMax: 90, HPCurrent: 60,
	}, Online: true}
	state.Players["d"] = PlayerState{Body: LegacyMonster{
		Name: "Bobby", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2,
		HPMax: 90, HPCurrent: 50,
	}, Online: true}
	state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}}}
	state.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "마을"}}, PlayerIDs: []string{"a", "c", "b", "d"}}
	return state
}

func TestPlanApplyRecallTargetOccurrenceUsesAuthoritativeRoomOrder(t *testing.T) {
	first := recallDuplicateTargetState()
	p, err := first.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetID != "c" || p.TargetOccurrence != 1 || p.TargetName != "Bo" {
		t.Fatalf("first occurrence proposal=%+v", p)
	}
	next, result, err := first.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.TargetID != "c" || next.Players["c"].Body.RoomID != recallTargetRoom || next.Players["b"].Body.RoomID != 2 {
		t.Fatalf("first occurrence result=%+v c=%+v b=%+v", result, next.Players["c"].Body, next.Players["b"].Body)
	}

	second := recallDuplicateTargetState()
	p, err = second.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 2})
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetID != "b" || p.TargetOccurrence != 2 {
		t.Fatalf("second occurrence proposal=%+v", p)
	}
	next, result, err = second.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.TargetID != "b" || next.Players["b"].Body.RoomID != recallTargetRoom || next.Players["c"].Body.RoomID != 2 {
		t.Fatalf("second occurrence result=%+v b=%+v c=%+v", result, next.Players["b"].Body, next.Players["c"].Body)
	}
}

func TestPlanApplyRecallTargetOccurrenceCountsVisibleMatchesOnly(t *testing.T) {
	state := recallDuplicateTargetState()
	hidden := state.Players["c"]
	setSettingFlag(&hidden.Body, recallInvisibleFlag, true)
	state.Players["c"] = hidden
	p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetID != "b" {
		t.Fatalf("hidden first match counted: proposal=%+v", p)
	}

	detector := state.Players["a"]
	setSettingFlag(&detector.Body, recallDetectInvisibleFlag, true)
	state.Players["a"] = detector
	p, err = state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 1})
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetID != "c" {
		t.Fatalf("detect-invisible did not select first match: proposal=%+v", p)
	}
}

func TestPlanApplyRecallTargetOccurrenceOutOfRangeIsDeterministicNoOp(t *testing.T) {
	state := recallDuplicateTargetState()
	p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 4})
	if err != nil {
		t.Fatal(err)
	}
	if p.TargetOccurrence != 4 || p.Attempted || p.Response != recallTargetMissingText {
		t.Fatalf("out-of-range proposal=%+v", p)
	}
	next, result, err := state.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded || result.Attempted || result.Broadcast || result.Changed || result.TargetID != "" || result.MPDelta != 0 || result.Response != recallTargetMissingText || !reflect.DeepEqual(next.Players, state.Players) || !recallIDsEqual(next.Rooms[2].PlayerIDs, state.Rooms[2].PlayerIDs) || !recallIDsEqual(next.Rooms[1].PlayerIDs, state.Rooms[1].PlayerIDs) || next.Rooms[1].Resource.BeenHere != state.Rooms[1].Resource.BeenHere || next.Rooms[2].Resource.BeenHere != state.Rooms[2].Resource.BeenHere {
		t.Fatalf("out-of-range mutated state/result=%+v next=%+v", result, next)
	}
}

func TestApplyRecallTargetOccurrenceRejectsStaleRoomOrder(t *testing.T) {
	state := recallDuplicateTargetState()
	p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 2})
	if err != nil {
		t.Fatal(err)
	}
	room := state.Rooms[2]
	room.PlayerIDs = []string{"a", "b", "c", "d"}
	state.Rooms[2] = room
	if _, _, err := state.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("room-order stale proposal err=%v", err)
	}
}

func TestApplyRecallTargetOccurrenceNoOpRejectsStaleRoomOrder(t *testing.T) {
	state := recallDuplicateTargetState()
	p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 4})
	if err != nil {
		t.Fatal(err)
	}
	room := state.Rooms[2]
	room.PlayerIDs = []string{"a", "b", "c", "d"}
	state.Rooms[2] = room
	if _, _, err := state.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("out-of-range room-order stale proposal err=%v", err)
	}
}

func TestApplyRecallTargetOccurrenceRejectsStaleIdentityAndBody(t *testing.T) {
	t.Run("target-body", func(t *testing.T) {
		state := recallDuplicateTargetState()
		p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 2})
		if err != nil {
			t.Fatal(err)
		}
		target := state.Players["b"]
		target.Body.HPCurrent--
		state.Players["b"] = target
		if _, _, err := state.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
			t.Fatalf("target body stale proposal err=%v", err)
		}
	})

	t.Run("occurrence-binding", func(t *testing.T) {
		state := recallDuplicateTargetState()
		p, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo", Occurrence: 2})
		if err != nil {
			t.Fatal(err)
		}
		p.TargetOccurrence = 1
		if _, _, err := state.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
			t.Fatalf("occurrence stale proposal err=%v", err)
		}
	})
}

func TestPlanRecallRejectsNegativeTargetOccurrence(t *testing.T) {
	state := recallDuplicateTargetState()
	if _, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bob", Occurrence: -1}); err == nil {
		t.Fatal("negative target occurrence admitted")
	}
}

func TestPlanApplyRecallTargetedResolutionUsesRoomOrderAndFailsClosed(t *testing.T) {
	newState := func() State {
		state := recallTestState()
		actor := state.Players["a"]
		actor.Body.RoomID = 2
		state.Players["a"] = actor
		state.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2}, Online: true}
		state.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carl", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2}, Online: true}
		state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}}}
		state.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "마을"}}, PlayerIDs: []string{"a", "c", "b"}}
		return state
	}
	for _, tc := range []struct {
		name   string
		query  string
		mutate func(*State)
	}{
		{name: "missing", query: "Bob", mutate: func(s *State) {
			delete(s.Players, "b")
			room := s.Rooms[2]
			room.PlayerIDs = []string{"a", "c"}
			s.Rooms[2] = room
		}},
		{name: "wrong-room", query: "Bob", mutate: func(s *State) {
			target := s.Players["b"]
			target.Body.RoomID = 1
			s.Players["b"] = target
			room := s.Rooms[2]
			room.PlayerIDs = []string{"a", "c"}
			s.Rooms[2] = room
			room = s.Rooms[1]
			room.PlayerIDs = []string{"b"}
			s.Rooms[1] = room
		}},
		{name: "offline", query: "Bob", mutate: func(s *State) {
			target := s.Players["b"]
			target.Online = false
			s.Players["b"] = target
			room := s.Rooms[2]
			room.PlayerIDs = []string{"a", "c"}
			s.Rooms[2] = room
		}},
		{name: "invalid", query: "Bob\n", mutate: func(*State) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newState()
			tc.mutate(&state)
			beforeRoom := state.Rooms[2].PlayerIDs
			proposal, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := state.ApplyCast(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Succeeded || result.Attempted || result.Broadcast || result.Changed || result.TargetID != "" || result.MPDelta != 0 || result.Response != recallTargetMissingText {
				t.Fatalf("unexpected target result=%+v", result)
			}
			if !recallIDsEqual(next.Rooms[2].PlayerIDs, beforeRoom) || next.Players["a"].Body.MPCurrent != state.Players["a"].Body.MPCurrent {
				t.Fatalf("target no-op mutated state: room=%v mp=%d", next.Rooms[2].PlayerIDs, next.Players["a"].Body.MPCurrent)
			}
		})
	}

	state := newState()
	target := state.Players["c"]
	target.Body.Name = "Bobby"
	state.Players["c"] = target
	proposal, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bo"})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "c" || proposal.TargetName != "Bo" || proposal.Response != recallTargetCasterText("Bobby") {
		t.Fatalf("room-order target=%+v", proposal)
	}
}

func mustRecallProposal(t *testing.T, s State, target string) CastProposal {
	t.Helper()
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func recallTargetGateState() State {
	state := recallTestState()
	actor := state.Players["a"]
	actor.Body.RoomID = 2
	state.Players["a"] = actor
	state.Players["b"] = PlayerState{Body: LegacyMonster{
		Name: "Bob", Type: 0, Class: castFighterClass, Level: 4, RoomID: 2,
		HPMax: 90, HPCurrent: 90, MPMax: 40, MPCurrent: 20,
	}, Online: true}
	state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}}}
	state.Rooms[2] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "마을"}}, PlayerIDs: []string{"a", "b"}}
	return state
}

func TestPlanApplyRecallTargetedGatesFollowSourceOrder(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   string
	}{
		{name: "mana", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.MPCurrent = 29
			s.Players["a"] = actor
		}, want: recallManaResponse},
		{name: "class", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Class = castMageClass
			s.Players["a"] = actor
		}, want: recallClassResponse},
		{name: "unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			s.Players["a"] = actor
		}, want: recallUnlearnedResponse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := recallTargetGateState()
			tc.mutate(&state)
			proposal, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bob"})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := state.ApplyCast(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if result.Succeeded || result.Attempted || result.Broadcast || result.MPDelta != 0 || result.Response != tc.want {
				t.Fatalf("target gate result=%+v", result)
			}
			if next.Players["a"].Body.RoomID != 2 || next.Players["a"].Body.MPCurrent != state.Players["a"].Body.MPCurrent || next.Players["b"].Body.RoomID != 2 {
				t.Fatalf("target gate mutated state actor=%+v target=%+v", next.Players["a"].Body, next.Players["b"].Body)
			}
		})
	}
}

func TestPlanRecallTargetedDestinationLoadFailureHasNoPartialMutation(t *testing.T) {
	state := recallTargetGateState()
	delete(state.Rooms, recallTargetRoom)
	beforeActor := state.Players["a"]
	beforeTarget := state.Players["b"]
	beforeSource := append([]string(nil), state.Rooms[2].PlayerIDs...)
	if _, err := state.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bob"}); !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("destination failure err=%v", err)
	}
	if !reflect.DeepEqual(state.Players["a"], beforeActor) || !reflect.DeepEqual(state.Players["b"], beforeTarget) || !recallIDsEqual(state.Rooms[2].PlayerIDs, beforeSource) {
		t.Fatalf("destination failure mutated actor=%+v target=%+v source=%v", state.Players["a"], state.Players["b"], state.Rooms[2].PlayerIDs)
	}
}

func TestApplyRecallTargetedDestinationLoadFailureHasNoPartialMutation(t *testing.T) {
	state := recallTargetGateState()
	proposal := mustRecallProposal(t, state, "Bob")
	delete(state.Rooms, recallTargetRoom)
	beforeActor := state.Players["a"]
	beforeTarget := state.Players["b"]
	beforeSource := append([]string(nil), state.Rooms[2].PlayerIDs...)
	if _, _, err := state.ApplyCast(proposal); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("destination apply failure err=%v", err)
	}
	if !reflect.DeepEqual(state.Players["a"], beforeActor) || !reflect.DeepEqual(state.Players["b"], beforeTarget) || !recallIDsEqual(state.Rooms[2].PlayerIDs, beforeSource) {
		t.Fatalf("destination apply failure mutated actor=%+v target=%+v source=%v", state.Players["a"], state.Players["b"], state.Rooms[2].PlayerIDs)
	}
}

func TestPlanApplyRecallSameRoomStillCommitsVisit(t *testing.T) {
	s := recallTestState()
	actor := s.Players["a"]
	actor.Body.RoomID = recallSquareRoom
	s.Players["a"] = actor
	s.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}}}
	dest := s.Rooms[recallSquareRoom]
	dest.PlayerIDs = []string{"a"}
	s.Rooms[recallSquareRoom] = dest
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || next.Players["a"].Body.RoomID != recallSquareRoom || next.Rooms[recallSquareRoom].Resource.BeenHere != 1 {
		t.Fatalf("result=%+v beenhere=%d ids=%v", result, next.Rooms[recallSquareRoom].Resource.BeenHere, next.Rooms[recallSquareRoom].PlayerIDs)
	}
	if next.Players["a"].Body.MPCurrent != 10 {
		t.Fatalf("mp=%d", next.Players["a"].Body.MPCurrent)
	}
}

func TestPlanApplyRecallUnmigratedSpawnRefreshFailsClosed(t *testing.T) {
	s := recallTestState()
	room := s.Rooms[recallSquareRoom]
	room.Resource.PermanentMonsters[0] = LegacyTimer{Interval: 1, LastTime: 0, Misc: 12}
	s.Rooms[recallSquareRoom] = room
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyRecallUnmigratedSourceNPCFailsClosed(t *testing.T) {
	s := recallTestState()
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}}}
	room := s.Rooms[1]
	room.NPCIDs = []string{"wolf"}
	s.Rooms[1] = room
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyRecallDeactivatesLastSourceNPC(t *testing.T) {
	s := recallTestState()
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}}}
	s.ActiveNPCIDs = []string{"wolf"}
	room := s.Rooms[1]
	room.NPCIDs = []string{"wolf"}
	s.Rooms[1] = room
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || len(next.ActiveNPCIDs) != 0 || next.Players["a"].Body.RoomID != recallSquareRoom {
		t.Fatalf("result=%+v active=%v", result, next.ActiveNPCIDs)
	}
}

func TestPlanApplyRecallActivatesEmptyDestNPCs(t *testing.T) {
	s := recallOccupiedSourceState()
	s.NPCs = map[string]NPCState{
		"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: recallSquareRoom}},
		"wolf":  {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}},
	}
	s.ActiveNPCIDs = []string{"wolf"}
	source := s.Rooms[1]
	source.NPCIDs = []string{"wolf"}
	s.Rooms[1] = source
	dest := s.Rooms[recallSquareRoom]
	dest.NPCIDs = []string{"guard"}
	s.Rooms[recallSquareRoom] = dest
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !containsString(next.ActiveNPCIDs, "guard") || !containsString(next.ActiveNPCIDs, "wolf") {
		t.Fatalf("result=%+v active=%v", result, next.ActiveNPCIDs)
	}
	if next.ActiveNPCIDs[0] != "guard" {
		t.Fatalf("dest add_active order=%v", next.ActiveNPCIDs)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestPlanApplyRecallOccupiedDestDoesNotActivateNPCs(t *testing.T) {
	s := recallOccupiedSourceState()
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", Type: 0, Class: 4, Level: 4, RoomID: recallSquareRoom}, Online: true}
	s.NPCs = map[string]NPCState{"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: recallSquareRoom}}}
	s.ActiveNPCIDs = []string{"guard"}
	dest := s.Rooms[recallSquareRoom]
	dest.PlayerIDs = []string{"c"}
	dest.NPCIDs = []string{"guard"}
	s.Rooms[recallSquareRoom] = dest
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || len(next.ActiveNPCIDs) != 1 || next.ActiveNPCIDs[0] != "guard" {
		t.Fatalf("occupied dest reordered active=%v result=%+v", next.ActiveNPCIDs, result)
	}
}

func TestPlanApplyRecallUnmigratedDestNPCFailsClosed(t *testing.T) {
	s := recallTestState()
	s.NPCs = map[string]NPCState{"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: recallSquareRoom}}}
	dest := s.Rooms[recallSquareRoom]
	dest.NPCIDs = []string{"guard"}
	s.Rooms[recallSquareRoom] = dest
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyRecallDarkDestUnmigratedItemsFailsClosed(t *testing.T) {
	s := recallTestState()
	dest := s.Rooms[recallSquareRoom]
	dest.Resource.Flags[1] |= 1 << 0 // RF 8 always-dark
	s.Rooms[recallSquareRoom] = dest
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplyRecallActivatesDestAndDeactivatesSource(t *testing.T) {
	s := recallTestState()
	s.NPCs = map[string]NPCState{
		"wolf":  {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}},
		"guard": {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: recallSquareRoom}},
		"bear":  {Body: LegacyMonster{Name: "곰", Type: 1, RoomID: recallSquareRoom}},
	}
	s.ActiveNPCIDs = []string{"wolf"}
	source := s.Rooms[1]
	source.NPCIDs = []string{"wolf"}
	s.Rooms[1] = source
	dest := s.Rooms[recallSquareRoom]
	dest.NPCIDs = []string{"guard", "bear"}
	s.Rooms[recallSquareRoom] = dest
	p, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || containsString(next.ActiveNPCIDs, "wolf") {
		t.Fatalf("source NPC stayed active: %+v active=%v", result, next.ActiveNPCIDs)
	}
	if len(next.ActiveNPCIDs) != 2 || next.ActiveNPCIDs[0] != "bear" || next.ActiveNPCIDs[1] != "guard" {
		t.Fatalf("dest add_active order=%v", next.ActiveNPCIDs)
	}
}

func recallOccupiedSourceState() State {
	s := recallTestState()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true}
	source := s.Rooms[1]
	source.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = source
	return s
}
