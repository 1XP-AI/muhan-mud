package transport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcCombatTickFixture intentionally puts active order and room NPC order in
// different orders.  C's update_active order is first_active (ActiveNPCIDs),
// while room/player slices remain the authoritative membership indexes.
func npcCombatTickFixture(t *testing.T) world.State {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전투실"}},
				PlayerIDs: []string{"player-b", "player-a"},
				NPCIDs:    []string{"npc-a", "npc-b"},
			},
		},
		Players: map[string]world.PlayerState{
			"player-a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 30, HPCurrent: 30, Armor: 70},
				Online: true,
			},
			"player-b": {
				Body:   world.LegacyMonster{Name: "Bob", RoomID: 1, HPMax: 30, HPCurrent: 30, Armor: 70},
				Online: true,
			},
		},
		NPCs: map[string]world.NPCState{
			"npc-a": {
				Body: world.LegacyMonster{
					Name: "늑대A", Type: 1, Class: 4, RoomID: 1,
					HPMax: 20, HPCurrent: 20, Thaco: 20, DiceCount: 1, DiceSides: 4,
				},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "player-a"}, Damage: 0}},
			},
			"npc-b": {
				Body: world.LegacyMonster{
					Name: "늑대B", Type: 1, Class: 4, RoomID: 1,
					HPMax: 20, HPCurrent: 20, Thaco: 20, DiceCount: 1, DiceSides: 4,
				},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "player-b"}, Damage: 0}},
			},
		},
		// src/update.c:add_active removes then prepends, so this order is
		// authoritative even though room.NPCIDs has the opposite order.
		ActiveNPCIDs: []string{"npc-b", "npc-a"},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func npcCombatTickRoll(_ int, high int) int { return high }

type npcCombatTickStore struct {
	mu sync.Mutex

	state    json.RawMessage
	revision int64
	receipt  *storage.WorldReceipt
	command  string
	request  json.RawMessage

	commitAttempts int
	commits        int
	commands       []string
	requests       []json.RawMessage
	failOnce       bool
}

func (s *npcCombatTickStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || commandID != s.command {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(request, s.request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	r := *s.receipt
	r.Response = append(json.RawMessage(nil), r.Response...)
	r.Replayed = true
	return r, nil
}

func (s *npcCombatTickStore) LoadWorld(_ context.Context, _ string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcCombatTickStore) CommitWorldCommand(_ context.Context, _ string, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitAttempts++
	s.commands = append(s.commands, commandID)
	s.requests = append(s.requests, append(json.RawMessage(nil), request...))
	if s.failOnce {
		s.failOnce = false
		return storage.WorldReceipt{}, errors.New("uncertain combat commit")
	}
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	s.state = append(json.RawMessage(nil), state...)
	s.revision++
	s.command = commandID
	s.request = append(json.RawMessage(nil), request...)
	s.receipt = &storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.commits++
	return *s.receipt, nil
}

func newNPCCombatTickConnector(t *testing.T, store *npcCombatTickStore, now *int32) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-test-world",
		MaxSessions: 1,
		Clock: func() (int32, int) {
			return *now, 12
		},
		Roll: npcCombatTickRoll,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func encodeNPCCombatTickState(t *testing.T, state world.State) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeNPCCombatTickSummary(t *testing.T, raw json.RawMessage) NPCCombatTickSummary {
	t.Helper()
	var summary NPCCombatTickSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func (s *npcCombatTickStore) snapshot() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(json.RawMessage(nil), s.state...)
}

func TestPlanNPCCombatTickUsesCanonicalActiveRoomPlayerAndEnemyOrder(t *testing.T) {
	state := npcCombatTickFixture(t)
	original := encodeNPCCombatTickState(t, state)

	next, summary, err := PlanNPCCombatTick(state, 5, 100, npcCombatTickRoll)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Attacks) != 2 {
		t.Fatalf("summary=%+v", summary)
	}
	if got := []string{summary.Attacks[0].NPCID, summary.Attacks[1].NPCID}; !reflect.DeepEqual(got, []string{"npc-b", "npc-a"}) {
		t.Fatalf("active order=%v", got)
	}
	if got := []string{summary.Attacks[0].PlayerID, summary.Attacks[1].PlayerID}; !reflect.DeepEqual(got, []string{"player-b", "player-a"}) {
		t.Fatalf("enemy/player order=%v", got)
	}
	if summary.Attacks[0].Damage != 4 || summary.Attacks[1].Damage != 4 || next.Players["player-a"].Body.HPCurrent != 26 || next.Players["player-b"].Body.HPCurrent != 26 {
		t.Fatalf("summary=%+v next=%+v", summary, next)
	}
	if got := encodeNPCCombatTickState(t, state); !bytes.Equal(got, original) {
		t.Fatalf("planning mutated source: before=%s after=%s", original, got)
	}
}

func TestPlanNPCCombatTickRejectsUnknownUnresolvedAndBadRNGAtomically(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*world.State)
		roll   func(int, int) int
	}{
		{
			name: "unknown active identity",
			mutate: func(s *world.State) {
				s.ActiveNPCIDs = []string{"missing"}
			},
			roll: npcCombatTickRoll,
		},
		{
			name: "active order unresolved",
			mutate: func(s *world.State) {
				s.ActiveNPCIDs = nil
			},
			roll: npcCombatTickRoll,
		},
		{
			name: "late unresolved relation does not leak earlier candidate",
			mutate: func(s *world.State) {
				npc := s.NPCs["npc-a"]
				npc.Enemies = nil
				s.NPCs["npc-a"] = npc
			},
			roll: npcCombatTickRoll,
		},
		{
			name:   "invalid random value",
			mutate: func(*world.State) {},
			roll: func(low, high int) int {
				return low - 1
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := npcCombatTickFixture(t)
			test.mutate(&state)
			before := encodeNPCCombatTickState(t, state)
			next, summary, err := PlanNPCCombatTick(state, 2, 40, test.roll)
			if err == nil || !reflect.DeepEqual(next, world.State{}) || !reflect.DeepEqual(summary, NPCCombatTickSummary{}) {
				t.Fatalf("accepted partial plan next=%+v summary=%+v err=%v", next, summary, err)
			}
			if after := encodeNPCCombatTickState(t, state); !bytes.Equal(before, after) {
				t.Fatalf("error mutated source: before=%s after=%s", before, after)
			}
		})
	}
}

func TestRunNPCCombatTickPersistsSkipsAndReplaysExactSlot(t *testing.T) {
	state := npcCombatTickFixture(t)
	now := int32(103)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	connector := newNPCCombatTickConnector(t, store, &now)

	first, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if store.command != "npc-combat-5" || store.commits != 1 {
		t.Fatalf("command=%q commits=%d", store.command, store.commits)
	}
	var request npcCombatTickRequest
	if err := json.Unmarshal(store.request, &request); err != nil || request.Kind != "npc-combat-phase" || request.Slot != 5 || request.Now != 100 {
		t.Fatalf("request=%s err=%v", store.request, err)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil || saved.Players["player-a"].Body.HPCurrent != 26 || saved.Players["player-b"].Body.HPCurrent != 26 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}

	duplicate, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || ran || duplicate.Response != nil || store.commitAttempts != 1 {
		t.Fatalf("duplicate=%+v ran=%v attempts=%d err=%v", duplicate, ran, store.commitAttempts, err)
	}

	// A fresh connector does not trust process memory.  The same deterministic
	// command resolves to the immutable receipt and never attacks again.
	restarted := newNPCCombatTickConnector(t, store, &now)
	replay, ran, err := restarted.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed || store.commits != 1 {
		t.Fatalf("restart replay=%+v ran=%v commits=%d err=%v", replay, ran, store.commits, err)
	}
}

func TestRunNPCCombatTickRetriesExactPendingCommandAndRequest(t *testing.T) {
	state := npcCombatTickFixture(t)
	now := int32(101)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state), failOnce: true}
	connector := newNPCCombatTickConnector(t, store, &now)

	if _, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("uncertain first attempt ran=%v err=%v", ran, err)
	}
	now = 119 // pending request must retain slot boundary 100, not this sample.
	receipt, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || receipt.Replayed || store.commits != 1 {
		t.Fatalf("retry=%+v ran=%v commits=%d err=%v", receipt, ran, store.commits, err)
	}
	if len(store.commands) != 2 || store.commands[0] != "npc-combat-5" || store.commands[1] != store.commands[0] {
		t.Fatalf("commands=%v", store.commands)
	}
	if len(store.requests) != 2 || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
	var request npcCombatTickRequest
	if err := json.Unmarshal(store.requests[1], &request); err != nil || request.Slot != 5 || request.Now != 100 {
		t.Fatalf("retry request=%s err=%v", store.requests[1], err)
	}
}

func TestRunNPCCombatPhaseRejectsStaleCommandAndErrorIsAtomic(t *testing.T) {
	state := npcCombatTickFixture(t)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	connector := newNPCCombatTickConnector(t, store, &now)

	if _, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-stale", 5, 100); err != nil {
		t.Fatal(err)
	}
	before := store.snapshot()
	if _, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-stale", 6, 120); !errors.Is(err, storage.ErrCommandConflict) {
		t.Fatalf("stale request err=%v", err)
	}
	if after := store.snapshot(); !bytes.Equal(before, after) || store.commits != 1 {
		t.Fatalf("stale changed state before=%s after=%s commits=%d", before, after, store.commits)
	}

	// A later unresolved NPC must discard the earlier in-memory attack.  The
	// durable store observes no commit and retains the original snapshot.
	broken := npcCombatTickFixture(t)
	npc := broken.NPCs["npc-a"]
	npc.Enemies = nil
	broken.NPCs["npc-a"] = npc
	brokenStore := &npcCombatTickStore{state: encodeNPCCombatTickState(t, broken)}
	brokenConnector := newNPCCombatTickConnector(t, brokenStore, &now)
	if _, err := brokenConnector.RunNPCCombatPhase(context.Background(), "npc-combat-broken", 5, 100); err == nil {
		t.Fatal("accepted unresolved relation")
	}
	if brokenStore.commits != 0 || !bytes.Equal(brokenStore.snapshot(), encodeNPCCombatTickState(t, broken)) {
		t.Fatalf("partial error commit=%d state=%s", brokenStore.commits, brokenStore.snapshot())
	}
}

func TestRunNPCCombatPhaseRecordsLethalPlayerFailClosed(t *testing.T) {
	state := npcCombatTickFixture(t)
	player := state.Players["player-b"]
	player.Body.HPCurrent = 1
	state.Players["player-b"] = player
	state.ActiveNPCIDs = []string{"npc-b"}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	connector := newNPCCombatTickConnector(t, store, &now)

	receipt, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-lethal", 5, 100)
	if err != nil || receipt.Replayed || store.commits != 1 {
		t.Fatalf("receipt=%+v commits=%d err=%v", receipt, store.commits, err)
	}
	summary := decodeNPCCombatTickSummary(t, receipt.Response)
	if len(summary.Attacks) != 0 || len(summary.FailClosed) != 1 || summary.FailClosed[0].NPCID != "npc-b" || summary.FailClosed[0].PlayerID != "player-b" {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil || saved.Players["player-b"].Body.HPCurrent != 1 {
		t.Fatalf("lethal damage was committed saved=%+v err=%v", saved, err)
	}
}

func TestRunNPCCombatTickSerializesConcurrentCallers(t *testing.T) {
	state := npcCombatTickFixture(t)
	now := int32(100)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	connector := newNPCCombatTickConnector(t, store, &now)

	const callers = 8
	var wait sync.WaitGroup
	results := make(chan struct {
		ran bool
		err error
	}, callers)
	for i := 0; i < callers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
			results <- struct {
				ran bool
				err error
			}{ran: ran, err: err}
		}()
	}
	wait.Wait()
	close(results)
	runs := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent tick err=%v", result.err)
		}
		if result.ran {
			runs++
		}
	}
	if runs != 1 || store.commits != 1 {
		t.Fatalf("runs=%d commits=%d", runs, store.commits)
	}
}
