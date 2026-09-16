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

type npcAITickSchedulerFake struct {
	calls     []string
	intervals []time.Duration
}

func (r *npcAITickSchedulerFake) RunNPCMaintenanceTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "maintenance")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 1}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCResourceTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "resource")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 2}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCCombatTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "combat")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 3}, true, nil
}

func (r *npcAITickSchedulerFake) RunNPCAggressiveTargetTick(_ context.Context, interval time.Duration) (storage.WorldReceipt, bool, error) {
	r.calls = append(r.calls, "aggressive-target")
	r.intervals = append(r.intervals, interval)
	return storage.WorldReceipt{Revision: 4}, true, nil
}

func TestNPCWorldSchedulerRunsOptionalAggressiveTargetAfterCombat(t *testing.T) {
	runner := &npcAITickSchedulerFake{}
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("result=%+v ran=%v err=%v", result, ran, err)
	}
	if got, want := runner.calls, []string{"maintenance", "resource", "combat", "aggressive-target"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase order=%v want=%v", got, want)
	}
	if got, want := runner.intervals, []time.Duration{time.Second, 20 * time.Second, 2 * time.Second, 2 * time.Second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("phase intervals=%v want=%v", got, want)
	}
	if result.AggressiveTarget.Revision != 4 || !result.AggressiveTargetRan {
		t.Fatalf("aggressive target result=%+v", result)
	}
}

func TestNPCWorldSchedulerKeepsThreePhaseRunnerClosedToOptionalPhase(t *testing.T) {
	runner := newNPCWorldSchedulerFake()
	scheduler, err := NewNPCWorldScheduler(runner, time.Second, 20*time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result, ran, err := scheduler.RunOnce(context.Background())
	if err != nil || !ran {
		t.Fatalf("result=%+v ran=%v err=%v", result, ran, err)
	}
	if len(runner.calls) != 3 || result.AggressiveTargetRan || result.AggressiveTarget.Revision != 0 {
		t.Fatalf("legacy runner was widened: calls=%v result=%+v", runner.calls, result)
	}
}

func TestNPCAggressiveTargetCannotSwingUntilNextPass(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	rollCalls := 0
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, func(low, _ int) int {
		rollCalls++
		return low
	})

	before, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-before-acquisition", 1, now)
	if err != nil || before.Replayed {
		t.Fatalf("before combat receipt=%+v err=%v", before, err)
	}
	beforeSummary := decodeNPCAggressiveCombatSummary(t, before.Response)
	if len(beforeSummary.Attacks) != 0 || len(beforeSummary.Skipped) != 1 {
		t.Fatalf("before acquisition combat=%+v", beforeSummary)
	}

	acquired, err := connector.RunNPCAggressiveTargetPhase(context.Background(), "npc-aggressive-target-direct", 1, now)
	if err != nil || acquired.Replayed {
		t.Fatalf("acquisition receipt=%+v err=%v", acquired, err)
	}
	acquisition := decodeNPCAggressiveTargetResult(t, acquired.Response)
	if len(acquisition.Events) != 1 || acquisition.Events[0].NPCID != "wolf-1" || acquisition.Events[0].TargetID != "target" {
		t.Fatalf("acquisition=%+v", acquisition)
	}
	if got := npcAITickStoredState(t, store).NPCs["wolf-1"].Enemies; len(got) != 1 || got[0].Target != (world.EntityRef{Kind: "player", ID: "target"}) {
		t.Fatalf("acquired enemies=%+v", got)
	}

	now++
	after, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-next-pass", 2, now)
	if err != nil || after.Replayed {
		t.Fatalf("next combat receipt=%+v err=%v", after, err)
	}
	afterSummary := decodeNPCAggressiveCombatSummary(t, after.Response)
	if len(afterSummary.Attacks) != 1 || afterSummary.Attacks[0].NPCID != "wolf-1" || afterSummary.Attacks[0].PlayerID != "target" {
		t.Fatalf("next-pass combat=%+v", afterSummary)
	}
	if rollCalls == 0 {
		t.Fatal("acquisition/combat did not use the injected deterministic RNG")
	}
}

func TestNPCAggressiveTargetTickRetainsExactPendingRequestAcrossRetry(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	store.failCommitOnce = true
	now := int32(100)
	clockCalls := 0
	connector := newNPCAITickConnector(t, store, func() (int32, int) {
		clockCalls++
		return now, 12
	}, func(low, _ int) int { return low })

	first, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err == nil || !ran || first.Replayed {
		t.Fatalf("uncertain first receipt=%+v ran=%v err=%v", first, ran, err)
	}
	if store.commitAttempts != 1 || store.commits != 0 {
		t.Fatalf("first commit attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	if len(store.commands) != 1 || len(store.requests) != 1 {
		t.Fatalf("first durable boundary commands=%v requests=%v", store.commands, store.requests)
	}

	now = 500
	second, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || second.Replayed {
		t.Fatalf("retry receipt=%+v ran=%v err=%v", second, ran, err)
	}
	if clockCalls != 1 {
		t.Fatalf("retry resampled clock %d times", clockCalls)
	}
	if store.commitAttempts != 2 || store.commits != 1 {
		t.Fatalf("retry commit attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	if store.commands[0] != store.commands[1] || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("retry changed durable boundary commands=%v requests=%q", store.commands, store.requests)
	}
	var request npcAggressiveTargetTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-aggressive-target-phase" || request.Slot != 100 || request.Now != 100 {
		t.Fatalf("request=%+v", request)
	}
}

func TestNPCAggressiveTargetTickReplaysWithoutRNGOrFanout(t *testing.T) {
	state := npcAITickState(t, 1)
	store := newNPCAITickStore(t, state)
	now := int32(100)
	rollCalls := 0
	roll := func(low, _ int) int {
		rollCalls++
		return low
	}
	firstConnector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, roll)
	firstConnections := installNPCAITickConnections(t, firstConnector, "target", "observer", "away")
	first, ran, err := firstConnector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first receipt=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 1 || store.commits != 1 {
		t.Fatalf("first RNG/commit calls=%d/%d", rollCalls, store.commits)
	}
	if got := <-firstConnections["target"].events; got == "" {
		t.Fatal("first target event empty")
	}
	if got := <-firstConnections["observer"].events; got == "" {
		t.Fatal("first observer event empty")
	}

	secondConnector := newNPCAITickConnector(t, store, func() (int32, int) { return now, 12 }, roll)
	secondConnections := installNPCAITickConnections(t, secondConnector, "target", "observer", "away")
	second, ran, err := secondConnector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || !second.Replayed {
		t.Fatalf("replay receipt=%+v ran=%v err=%v", second, ran, err)
	}
	if rollCalls != 1 || store.commits != 1 {
		t.Fatalf("replay repeated RNG/commit calls=%d/%d", rollCalls, store.commits)
	}
	for id, connection := range secondConnections {
		select {
		case event := <-connection.events:
			t.Fatalf("replay delivered duplicate to %s: %q", id, event)
		default:
		}
	}
}

func TestNPCAggressiveTargetFanoutCommitsFirstAndPreservesOrderAndExclusion(t *testing.T) {
	state := npcAITickState(t, 2)
	store := newNPCAITickStore(t, state)
	connector := newNPCAITickConnector(t, store, func() (int32, int) { return 100, 12 }, func(low, _ int) int { return low })
	connections := installNPCAITickConnections(t, connector, "target", "observer", "away")

	receipt, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
	if err != nil || !ran || receipt.Replayed {
		t.Fatalf("receipt=%+v ran=%v err=%v", receipt, ran, err)
	}
	if store.commits != 1 || store.commitAttempts != 1 {
		t.Fatalf("commit-before-fanout attempts=%d commits=%d", store.commitAttempts, store.commits)
	}
	result := decodeNPCAggressiveTargetResult(t, receipt.Response)
	if len(result.Events) != 2 {
		t.Fatalf("events=%+v", result.Events)
	}
	for i, event := range result.Events {
		if event.RoomID != 1 || event.ExcludeTargetID != event.TargetID || event.TargetID != "target" {
			t.Fatalf("event[%d]=%+v", i, event)
		}
		if got := <-connections["target"].events; got != event.TargetText {
			t.Fatalf("target event[%d]=%q want=%q", i, got, event.TargetText)
		}
		if got := <-connections["observer"].events; got != event.RoomText {
			t.Fatalf("observer event[%d]=%q want=%q", i, got, event.RoomText)
		}
	}
	for id, connection := range connections {
		select {
		case event := <-connection.events:
			t.Fatalf("unexpected extra event for %s: %q", id, event)
		default:
		}
	}
}

func TestNPCAggressiveTargetTickFailsClosedForNilOrInvalidWorldState(t *testing.T) {
	validWithoutCanonicalNPCs := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "빈 방"}}},
		},
		Players: map[string]world.PlayerState{},
	}
	validRaw, err := json.Marshal(validWithoutCanonicalNPCs)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "nil snapshot", raw: nil, want: ""},
		{name: "null snapshot", raw: json.RawMessage(`null`), want: "null"},
		{name: "canonical NPCs unresolved", raw: validRaw, want: "valid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &npcAITickStore{state: append(json.RawMessage(nil), tc.raw...), receipts: map[string]npcAITickStoredReceipt{}}
			connector := newNPCAITickConnector(t, store, func() (int32, int) { return 100, 12 }, func(int, int) int {
				t.Fatal("invalid state consumed RNG")
				return 0
			})
			before := append(json.RawMessage(nil), store.state...)
			receipt, ran, err := connector.RunNPCAggressiveTargetTick(context.Background(), time.Second)
			if err == nil || !ran || !reflect.DeepEqual(receipt, storage.WorldReceipt{}) {
				t.Fatalf("accepted invalid state receipt=%+v ran=%v err=%v", receipt, ran, err)
			}
			if store.commits != 0 || !bytes.Equal(store.state, before) {
				t.Fatalf("invalid state partially committed commits=%d before=%q after=%q", store.commits, before, store.state)
			}
		})
	}
}

type npcAITickStoredReceipt struct {
	request json.RawMessage
	receipt storage.WorldReceipt
}

type npcAITickStore struct {
	mu sync.Mutex

	state    json.RawMessage
	revision int64
	receipts map[string]npcAITickStoredReceipt

	commands       []string
	requests       []json.RawMessage
	commitAttempts int
	commits        int
	failCommitOnce bool
}

func newNPCAITickStore(t *testing.T, state world.State) *npcAITickStore {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &npcAITickStore{state: raw, receipts: map[string]npcAITickStoredReceipt{}}
}

func (s *npcAITickStore) ReadWorldReceipt(_ context.Context, _ string, commandID string, request json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.receipts[commandID]
	if !ok {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	if !bytes.Equal(stored.request, request) {
		return storage.WorldReceipt{}, storage.ErrCommandConflict
	}
	receipt := stored.receipt
	receipt.Response = append(json.RawMessage(nil), receipt.Response...)
	receipt.Replayed = true
	return receipt, nil
}

func (s *npcAITickStore) LoadWorld(_ context.Context, _ string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{Revision: s.revision, State: append(json.RawMessage(nil), s.state...)}, nil
}

func (s *npcAITickStore) CommitWorldCommand(_ context.Context, _ string, commandID string, request json.RawMessage, expected int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitAttempts++
	s.commands = append(s.commands, commandID)
	s.requests = append(s.requests, append(json.RawMessage(nil), request...))
	if s.failCommitOnce {
		s.failCommitOnce = false
		return storage.WorldReceipt{}, errors.New("uncertain NPC aggressive target commit")
	}
	if expected != s.revision {
		return storage.WorldReceipt{}, storage.ErrWorldConflict
	}
	s.revision++
	s.state = append(json.RawMessage(nil), state...)
	receipt := storage.WorldReceipt{Revision: s.revision, Response: append(json.RawMessage(nil), response...)}
	s.receipts[commandID] = npcAITickStoredReceipt{
		request: append(json.RawMessage(nil), request...),
		receipt: receipt,
	}
	s.commits++
	return receipt, nil
}

func npcAITickStoredState(t *testing.T, store *npcAITickStore) world.State {
	t.Helper()
	store.mu.Lock()
	raw := append(json.RawMessage(nil), store.state...)
	store.mu.Unlock()
	state, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func newNPCAITickConnector(t *testing.T, store *npcAITickStore, clock func() (int32, int), roll func(int, int) int) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-aggressive-target-test-world",
		Clock:       clock,
		MaxSessions: 3,
		Roll:        roll,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connector
}

func installNPCAITickConnections(t *testing.T, connector *WorldConnector, ids ...string) map[string]*worldConnection {
	t.Helper()
	connections := make(map[string]*worldConnection, len(ids))
	for _, id := range ids {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
		connections[id] = connection
		connector.mu.Lock()
		connector.connections[connection] = struct{}{}
		connector.mu.Unlock()
	}
	return connections
}

func npcAITickState(t *testing.T, npcCount int) world.State {
	t.Helper()
	room := world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "AI 방"}},
		PlayerIDs: []string{"target", "observer"},
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: room,
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "먼 방"}},
				PlayerIDs: []string{"away"},
			},
		},
		Players: map[string]world.PlayerState{
			"target":   {Body: world.LegacyMonster{Name: "대상", Type: 0, RoomID: 1, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "관전자", Type: 0, RoomID: 1, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
			"away":     {Body: world.LegacyMonster{Name: "먼사람", Type: 0, RoomID: 2, Level: 8, HPMax: 100, HPCurrent: 100, Stats: [5]byte{10, 10, 10, 10, 24}}, Online: true},
		},
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
	}
	for i := 1; i <= npcCount; i++ {
		id := "wolf-" + string(rune('0'+i))
		name := "늑대" + string(rune('0'+i))
		body := world.LegacyMonster{
			Name: name, Type: 1, Class: 4, RoomID: 1, Level: 8,
			HPMax: 100, HPCurrent: 100, Thaco: 20, DiceCount: 1, DiceSides: 1,
			Stats: [5]byte{10, 10, 10, 10, 10},
		}
		body.Flags[6/8] |= 1 << (6 % 8) // MAGGRE
		state.NPCs[id] = world.NPCState{Body: body, Enemies: []world.NPCEnemy{}}
		room := state.Rooms[1]
		room.NPCIDs = append(room.NPCIDs, id)
		state.Rooms[1] = room
		state.ActiveNPCIDs = append(state.ActiveNPCIDs, id)
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func decodeNPCAggressiveTargetResult(t *testing.T, raw json.RawMessage) world.NPCAggressiveTargetResult {
	t.Helper()
	var result world.NPCAggressiveTargetResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeNPCAggressiveCombatSummary(t *testing.T, raw json.RawMessage) NPCCombatTickSummary {
	t.Helper()
	var summary NPCCombatTickSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}
