package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func turnCommandFixture(t *testing.T) []byte {
	t.Helper()
	flags := [8]byte{}
	flags[world.TurnNPCUndeadFlag/8] |= 1 << (world.TurnNPCUndeadFlag % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"cleric"},
			NPCIDs:    []string{"undead"},
		}},
		Players: map[string]world.PlayerState{
			"cleric": {Body: world.LegacyMonster{
				Name: "아린", Type: world.TurnPlayerType, Class: world.TurnClericClass,
				Level: 10, Stats: [5]byte{10, 10, 10, 10, 10}, RoomID: 1,
				HPMax: 100, HPCurrent: 100,
			}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"undead": {Body: world.LegacyMonster{
				Name: "해골", Type: world.TurnMonsterType, Class: 4, Level: 10,
				RoomID: 1, HPMax: 100, HPCurrent: 100, Flags: flags,
			}, Enemies: []world.NPCEnemy{}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitTurnActor(t *testing.T, owners *Ownership) SessionLease {
	t.Helper()
	lease, err := owners.Acquire("cleric")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestParseTurnLineAdmitsPromptAndOneExactTargetToken(t *testing.T) {
	for _, tc := range []struct {
		line   string
		target string
		ok     bool
	}{
		{line: "방혼술", ok: true},
		{line: " 방혼술 해골 ", target: "해골", ok: true},
		{line: "방혼술 해골 추가", ok: false},
		{line: "방 혼술 해골", ok: false},
		{line: "방혼술 해 골", ok: false},
		{line: "방혼술 해골\n", ok: false},
		{line: "turn 해골", ok: false},
		{line: "\xff", ok: false},
	} {
		got, ok := ParseTurnLine(tc.line)
		if ok != tc.ok || (ok && got.Target != tc.target) {
			t.Fatalf("ParseTurnLine(%q)=%+v,%v want target=%q,%v", tc.line, got, ok, tc.target, tc.ok)
		}
		if ok && got.Occurrence != 1 {
			t.Fatalf("ParseTurnLine(%q) occurrence=%d", tc.line, got.Occurrence)
		}
	}
}

func TestExecuteTurnPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: turnCommandFixture(t)}
	var owners Ownership
	lease := admitTurnActor(t, &owners)
	calls := 0
	first, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-1", lease, "방혼술 해골", 100, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("turn random range=%d..%d", low, high)
		}
		if calls == 1 {
			return 1
		}
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.TurnResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != "turn" || !result.Authorized || !result.TargetFound || !result.Succeeded || result.Damage != 50 || result.TargetHP != 50 || result.Event == nil || !strings.Contains(result.Response, "50만큼") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["undead"].Body.HPCurrent != 50 || len(saved.NPCs["undead"].Enemies) != 1 || saved.NPCs["undead"].Enemies[0].Damage != 50 {
		t.Fatalf("saved npc=%+v", saved.NPCs["undead"])
	}
	replay, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-1", lease, "방혼술 해골", 9999, func(int, int) int {
		t.Fatal("turn replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || calls != 2 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
}

func TestExecuteTurnSupportsOriginalNoArgumentPromptWithoutRandomness(t *testing.T) {
	store := &departureStore{state: turnCommandFixture(t)}
	var owners Ownership
	lease := admitTurnActor(t, &owners)
	first, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-prompt", lease, "방혼술", 100, func(int, int) int {
		t.Fatal("turn prompt consumed RNG")
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.TurnResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.NoOp || result.TargetFound || !strings.Contains(result.Response, "누구에게") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-prompt", lease, "방혼술", 200, func(int, int) int { t.Fatal("replayed prompt consumed RNG"); return 1 }); err != nil {
		t.Fatal(err)
	}
	if store.commits != 1 {
		t.Fatalf("prompt replay committed again: %d", store.commits)
	}
}

func TestExecuteTurnFailsClosedBeforeReceiptForMalformedAndUnsafeDeath(t *testing.T) {
	store := &departureStore{state: turnCommandFixture(t)}
	var owners Ownership
	lease := admitTurnActor(t, &owners)
	if _, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-bad", lease, "방혼술 해골 extra", 100, func(int, int) int { return 1 }); !errors.Is(err, ErrUnsupportedTurnLine) || store.commits != 0 {
		t.Fatalf("malformed line reached receipt: err=%v commits=%d", err, store.commits)
	}
	s, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["undead"]
	npc.Body.HPCurrent = 1
	flags := npc.Body.Flags
	flags[world.TurnNPCPermanentFlag/8] |= 1 << (world.TurnNPCPermanentFlag % 8)
	npc.Body.Flags = flags
	s.NPCs["undead"] = npc
	store.state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := owners.ExecuteTurnLine(context.Background(), store, "world", "turn-unsafe", lease, "방혼술 해골", 100, func(int, int) int { calls++; return 1 }); !errors.Is(err, world.ErrTurnDeathTransitionPending) || store.commits != 0 || calls != 0 {
		t.Fatalf("unsafe turn was admitted: err=%v commits=%d calls=%d", err, store.commits, calls)
	}
}
