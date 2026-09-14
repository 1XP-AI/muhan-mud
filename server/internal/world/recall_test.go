package world

import (
	"errors"
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
		t.Fatal("self-only SRECAL must not admit a three-token targeted form")
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

func TestPlanApplyRecallTargetedFormFailsClosed(t *testing.T) {
	s := recallTestState()
	_, err := s.PlanCast("a", "귀환", CastOptions{Now: 100, Hour: 12, Target: "Bob"})
	if !errors.Is(err, ErrCastSpellUnavailable) {
		t.Fatalf("err=%v", err)
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
