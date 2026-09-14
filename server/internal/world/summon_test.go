package world

import (
	"errors"
	"strings"
	"testing"
)

func summonSpellBits() [16]byte {
	var spells [16]byte
	spells[castSummonSpell/8] |= 1 << uint(castSummonSpell%8)
	return spells
}

func summonTestState() State {
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
					HPMax: 100, HPCurrent: 100, MPMax: 80, MPCurrent: 80,
					Spells: summonSpellBits(),
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

func summonRolls(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	i := 0
	return func(low, high int) int {
		if i >= len(values) {
			t.Fatalf("unexpected extra roll at %d", i)
		}
		if low != 1 || high != 100 {
			t.Fatalf("unexpected summon bounds %d..%d", low, high)
		}
		value := values[i]
		i++
		return value
	}
}

func TestIsSummonCastSpellMatchesSourcePrefix(t *testing.T) {
	if !IsSummonCastSpell("소환") || !IsSummonCastSpell("소") {
		t.Fatal("expected unique SSUMMO prefixes to match")
	}
	if IsSummonCastSpell("회복") || IsSummonCastSpell("천리안") || IsSummonCastSpell("") {
		t.Fatal("non-summon tokens matched SSUMMO")
	}
	if !IsTargetedCastSpell("소환") || !IsTargetedCastSpell("천리안") || IsTargetedCastSpell("회복") {
		t.Fatal("targeted cast admission mismatch")
	}
}

func TestPlanApplySummonGatesFollowSourceOrder(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*State)
		target   string
		want     string
		wantHide bool
		roll     int
	}{
		{name: "unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			s.Players["a"] = actor
		}, target: "Bob", want: "그런 주술을 터득하지 못했습니다"},
		{name: "mana", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.MPCurrent = 49
			s.Players["a"] = actor
		}, target: "Bob", want: "도력이 부족합니다"},
		{name: "self", target: "", want: "자신을 소환하다뇨", roll: 51},
		{name: "missing", target: "Carol", want: "그런 사람을 못 찾습니다", roll: 51},
		{name: "self-name", target: "Alice", want: "그런 사람을 못 찾습니다", roll: 51},
		{name: "offline", mutate: func(s *State) {
			s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Carol", Type: 0, RoomID: 1}, Online: false}
		}, target: "Carol", want: "그런 사람을 못 찾습니다", roll: 51},
		{name: "dm-invis", mutate: func(s *State) {
			target := s.Players["b"]
			setSettingFlag(&target.Body, summonDMInvisibleFlag, true)
			s.Players["b"] = target
		}, target: "Bob", want: "그런 사람을 못 찾습니다", roll: 51},
		{name: "hidden-unlearned", mutate: func(s *State) {
			actor := s.Players["a"]
			actor.Body.Spells = [16]byte{}
			setSettingFlag(&actor.Body, castHiddenFlag, true)
			s.Players["a"] = actor
		}, target: "Bob", want: "그런 주술을 터득하지 못했습니다", wantHide: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := summonTestState()
			if tc.mutate != nil {
				tc.mutate(&s)
			}
			roll := tc.roll
			var rng func(int, int) int
			if roll != 0 {
				rng = summonRolls(t, roll)
			} else {
				rng = summonRolls(t)
			}
			p, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: tc.target, Roll: rng})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Succeeded || result.Broadcast || !strings.Contains(result.Response, tc.want) {
				t.Fatalf("result=%+v", result)
			}
			if tc.wantHide {
				body := next.Players["a"].Body
				if !result.Changed || !result.HiddenCleared || flag(body.Flags[:], castHiddenFlag) {
					t.Fatalf("hidden not cleared: %+v", result)
				}
				return
			}
			if next.Players["b"].Body.RoomID != 2 {
				t.Fatalf("target moved on gate: %+v", next.Players["b"].Body)
			}
			if result.MPDelta != 0 && tc.name != "self" && tc.name != "missing" && tc.name != "self-name" && tc.name != "offline" && tc.name != "dm-invis" {
				t.Fatalf("unexpected mp on pre-charge gate %+v", result)
			}
		})
	}
}

func TestPlanApplySummonRandomFailConsumesFiftyWithoutTimer(t *testing.T) {
	s := summonTestState()
	p, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 50)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SpellFailed || result.Succeeded || result.Broadcast || result.Event != nil || next.Players["a"].Body.MPCurrent != 30 {
		t.Fatalf("result=%+v mp=%d", result, next.Players["a"].Body.MPCurrent)
	}
	if result.MPDelta != -50 || next.Players["a"].Body.Timers[castSpellTimerIndex].LastTime != 0 || !strings.Contains(result.Response, "소환에 실패") {
		t.Fatalf("timer/response %+v %q", next.Players["a"].Body.Timers[castSpellTimerIndex], result.Response)
	}
	if next.Players["b"].Body.RoomID != 2 {
		t.Fatal("random fail moved target")
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("replayed summon fail err=%v", err)
	}
}

func TestPlanApplySummonStaffClassUsesHundredCost(t *testing.T) {
	s := summonTestState()
	actor := s.Players["a"]
	actor.Body.Class = castInvincible
	actor.Body.MPCurrent = 120
	s.Players["a"] = actor
	p, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: summonRolls(t, 51)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.Cost != 100 || result.MPDelta != -100 || next.Players["a"].Body.MPCurrent != 20 {
		t.Fatalf("result=%+v mp=%d", result, next.Players["a"].Body.MPCurrent)
	}
	failState := summonTestState()
	failActor := failState.Players["a"]
	failActor.Body.Class = summonCaretakerClass
	failActor.Body.MPCurrent = 120
	failState.Players["a"] = failActor
	fail, err := failState.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 1)})
	if err != nil {
		t.Fatal(err)
	}
	failNext, failResult, err := failState.ApplyCast(fail)
	if err != nil {
		t.Fatal(err)
	}
	if !failResult.SpellFailed || failResult.MPDelta != -50 || failNext.Players["a"].Body.MPCurrent != 70 {
		t.Fatalf("staff fail result=%+v mp=%d", failResult, failNext.Players["a"].Body.MPCurrent)
	}
}

func TestPlanApplySummonChargedRoomGatesKeepMana(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*State)
		want   string
	}{
		{name: "noteleport", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Flags[summonNoTeleportFlag/8] |= 1 << (summonNoTeleportFlag % 8)
			s.Rooms[1] = room
		}, want: "공중으로 빨려듭니다"},
		{name: "one-player", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Flags[summonOnePlayerFlag/8] |= 1 << (summonOnePlayerFlag % 8)
			s.Rooms[1] = room
		}, want: "공중으로 빨려듭니다"},
		{name: "family", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Flags[summonFamilyRoomFlag/8] |= 1 << (summonFamilyRoomFlag % 8)
			s.Rooms[1] = room
		}, want: "패거리 가입자가 아닙니다"},
		{name: "only-family", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Flags[summonOneFamilyFlag/8] |= 1 << (summonOneFamilyFlag % 8)
			room.Resource.Special = 7
			s.Rooms[1] = room
		}, want: "이곳에 올수 없습니다"},
		{name: "low-level", mutate: func(s *State) {
			room := s.Rooms[1]
			room.Resource.LowLevel = 20
			s.Rooms[1] = room
		}, want: "주문이 실패했습니다"},
		{name: "nosummon", mutate: func(s *State) {
			target := s.Players["b"]
			setSettingFlag(&target.Body, summonNoSummonFlag, true)
			s.Players["b"] = target
		}, want: "소환 거부"},
		{name: "noleave", mutate: func(s *State) {
			room := s.Rooms[2]
			room.Resource.Flags[summonNoLeaveFlag/8] |= 1 << (summonNoLeaveFlag % 8)
			s.Rooms[2] = room
		}, want: "소환 주문이 Bob을 찾지 못합니다"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := summonTestState()
			tc.mutate(&s)
			p, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 51)})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyCast(p)
			if err != nil {
				t.Fatal(err)
			}
			if result.Succeeded || result.Broadcast || result.SpellFailed || result.MPDelta != -50 || next.Players["a"].Body.MPCurrent != 30 {
				t.Fatalf("result=%+v mp=%d", result, next.Players["a"].Body.MPCurrent)
			}
			if !strings.Contains(result.Response, tc.want) || next.Players["b"].Body.RoomID != 2 || next.Players["a"].Body.Timers[castSpellTimerIndex].LastTime != 0 {
				t.Fatalf("response=%q room=%d timer=%+v", result.Response, next.Players["b"].Body.RoomID, next.Players["a"].Body.Timers[castSpellTimerIndex])
			}
		})
	}
}

func TestPlanApplySummonMovesTargetAndReplaysWithoutReroll(t *testing.T) {
	s := summonTestState()
	p, err := s.PlanCast("a", "소", CastOptions{Now: 100, Target: "bob", Hour: 12, Roll: summonRolls(t, 51)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "소환" || result.TargetID != "b" || result.TargetName != "Bob" || result.MPDelta != -50 {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["b"].Body.RoomID != 1 || !containsString(next.Rooms[1].PlayerIDs, "b") || containsString(next.Rooms[2].PlayerIDs, "b") {
		t.Fatalf("occupancy dest=%v source=%v room=%d", next.Rooms[1].PlayerIDs, next.Rooms[2].PlayerIDs, next.Players["b"].Body.RoomID)
	}
	if next.Rooms[1].Resource.BeenHere != 1 || next.Players["a"].Body.MPCurrent != 30 {
		t.Fatalf("beenhere=%d mp=%d", next.Rooms[1].Resource.BeenHere, next.Players["a"].Body.MPCurrent)
	}
	if next.Players["a"].Body.Timers[castSpellTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("timer=%+v", next.Players["a"].Body.Timers[castSpellTimerIndex])
	}
	if !strings.Contains(result.Response, "Bob을 소환") || !strings.Contains(result.TargetText, "Alice이 당신앞에") || !strings.Contains(result.TargetText, "숲") {
		t.Fatalf("response=%q target=%q", result.Response, result.TargetText)
	}
	if result.Event == nil || result.Event.ExcludeTargetID != "b" || !strings.Contains(result.Event.Text, "Bob이 나타났습니다") {
		t.Fatalf("event=%+v", result.Event)
	}
	if _, _, err := next.ApplyCast(p); !errors.Is(err, ErrCastStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestPlanApplySummonUnmigratedSourceNPCFailsClosed(t *testing.T) {
	s := summonTestState()
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 2}}}
	room := s.Rooms[2]
	room.NPCIDs = []string{"wolf"}
	s.Rooms[2] = room
	_, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 51)})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplySummonUnmigratedSpawnRefreshFailsClosed(t *testing.T) {
	s := summonTestState()
	room := s.Rooms[1]
	room.Resource.PermanentMonsters[0] = LegacyTimer{Interval: 1, LastTime: 0, Misc: 12}
	s.Rooms[1] = room
	_, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 51)})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlanApplySummonDeactivatesLastSourceNPC(t *testing.T) {
	s := summonTestState()
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 2}}}
	s.ActiveNPCIDs = []string{"wolf"}
	room := s.Rooms[2]
	room.NPCIDs = []string{"wolf"}
	s.Rooms[2] = room
	p, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Hour: 12, Roll: summonRolls(t, 51)})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyCast(p)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || len(next.ActiveNPCIDs) != 0 || next.Players["b"].Body.RoomID != 1 {
		t.Fatalf("result=%+v active=%v", result, next.ActiveNPCIDs)
	}
}

func TestPlanApplySummonAmbiguousNameFailsClosed(t *testing.T) {
	s := summonTestState()
	s.Players["c"] = PlayerState{Body: LegacyMonster{Name: "Bob", Type: 0, Class: 4, RoomID: 2}, Online: true}
	room := s.Rooms[2]
	room.PlayerIDs = append(room.PlayerIDs, "c")
	s.Rooms[2] = room
	_, err := s.PlanCast("a", "소환", CastOptions{Now: 100, Target: "Bob", Roll: summonRolls(t, 51)})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
