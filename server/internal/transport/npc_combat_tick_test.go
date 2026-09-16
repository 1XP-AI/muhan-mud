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

// npcCombatLethalTickFixture admits the canonical death graph used by the
// PLAYER branch of creature.c:die: source/respawn floors, family-war state,
// and an ID-owned wielded item.  The base ordering fixture intentionally
// omits these domains so ordinary combat remains small and fail-closed.
func npcCombatLethalTickFixture(t *testing.T) world.State {
	t.Helper()
	state := npcCombatTickFixture(t)
	state.War = &world.FamilyWar{}

	source := state.Rooms[1]
	source.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	state.Rooms[1] = source
	state.Rooms[1008] = world.RoomState{
		Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1008}},
		Items:    &world.ItemCollection{Items: map[string]world.Item{}},
	}

	player := state.Players["player-b"]
	player.Body.Class = 4
	player.Body.Level = 1
	player.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	player.Body.HPMax = 30
	player.Body.HPCurrent = 1
	player.Body.MPMax = 8
	player.Body.MPCurrent = 2
	player.Body.Experience = 100
	player.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"weapon": {Object: world.LegacyObject{Name: "sword"}},
		},
		Ready: [20]string{19: "weapon"},
	}
	state.Players["player-b"] = player
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

type npcCombatTickSpawnCatalog struct{}

func (npcCombatTickSpawnCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{Name: "소환", Type: 1, Class: 4, Level: 1, HPMax: 10, HPCurrent: 10}, nil
}

func (npcCombatTickSpawnCatalog) Object(int16) (world.LegacyObject, error) {
	return world.LegacyObject{Name: "spawned"}, nil
}

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
	return newNPCCombatTickConnectorWithDeps(t, store, now, nil, nil)
}

func newNPCCombatTickConnectorWithDeps(t *testing.T, store *npcCombatTickStore, now *int32, catalog world.SpawnCatalog, allocate func() (string, error)) *WorldConnector {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-test-world",
		MaxSessions: 1,
		Clock: func() (int32, int) {
			return *now, 12
		},
		Catalog:  catalog,
		Roll:     npcCombatTickRoll,
		Allocate: allocate,
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

func TestRunNPCCombatTickPersistsPoisonAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(103)
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-poison-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch high {
			case 20, 4:
				return high
			case 100:
				return 15
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 3 {
		t.Fatalf("first attack RNG calls=%d, want hit/damage/poison", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Hit || !summary.Attacks[0].Poisoned || summary.Attacks[0].Damage != 4 || summary.Attacks[0].PlayerHP != 26 {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"]
	if savedPlayer.Body.Flags[16/8]&(1<<(16%8)) == 0 {
		t.Fatalf("saved player omitted PPOISN: flags=%#x", savedPlayer.Body.Flags)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-poison-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(_, high int) int {
			replayCalls++
			return high
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	if replayCalls != 0 || rollCalls != 3 {
		t.Fatalf("replay consumed RNG: replayCalls=%d firstCalls=%d", replayCalls, rollCalls)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatTickPersistsMBEFUDAttenuationAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.DiceSides = 6
	npc.Body.Flags[51/8] |= 1 << (51 % 8) // MBEFUD
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(103)
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-befud-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch {
			case low == 1 && high == 20:
				return 20
			case low == 1 && high == 6:
				return 6
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 2 {
		t.Fatalf("first attack RNG calls=%d, want hit/damage", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Hit || summary.Attacks[0].Damage != 2 || summary.Attacks[0].PlayerHP != 28 {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["player-b"].Body.HPCurrent != 28 {
		t.Fatalf("saved player HP=%d want=28", saved.Players["player-b"].Body.HPCurrent)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-befud-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(_, high int) int {
			replayCalls++
			return high
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	if replayCalls != 0 || rollCalls != 2 {
		t.Fatalf("replay consumed RNG: replayCalls=%d firstCalls=%d", replayCalls, rollCalls)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatTickPersistsDiseaseAndBlindAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(103)
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-disease-blind-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch high {
			case 20, 4:
				return high
			case 100:
				return 10
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 4 {
		t.Fatalf("first attack RNG calls=%d, want hit/damage/disease/blind", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Hit || !summary.Attacks[0].Diseased || !summary.Attacks[0].Blinded || summary.Attacks[0].Damage != 4 || summary.Attacks[0].PlayerHP != 26 {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"]
	if !flagForNPCCombatTest(savedPlayer.Body.Flags[:], 41) || !flagForNPCCombatTest(savedPlayer.Body.Flags[:], 42) {
		t.Fatalf("saved player omitted disease/blind flags: flags=%#x", savedPlayer.Body.Flags)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-disease-blind-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(_, high int) int {
			replayCalls++
			return high
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	if replayCalls != 0 || rollCalls != 4 {
		t.Fatalf("replay consumed RNG: replayCalls=%d firstCalls=%d", replayCalls, rollCalls)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatTickPersistsBreathAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Level = 5                       // two level-band dice: ((5 + 3) / 4) == 2.
	for _, bit := range []uint{19, 28, 29} { // MBRETH + MBRWP1/MBRWP2 (acid branch)
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(103)
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-breath-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch {
			case low == 1 && high == 20:
				return 20
			case low == 1 && high == 30:
				return 4
			case low == 1 && high == 2:
				return 2
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	if rollCalls != 4 {
		t.Fatalf("first attack RNG calls=%d, want hit/trigger/two acid dice", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	attack := summary.Attacks[0]
	if !attack.Hit || !attack.BreathTriggered || attack.BreathType != world.NPCCombatBreathAcid || attack.BreathRoll != 4 || attack.BreathDiceCount != 2 || attack.BreathDiceSides != 2 || attack.BreathDicePlus != 1 || !attack.BreathPoisoned || attack.BreathResisted || attack.Damage != 5 || attack.PlayerHP != 25 {
		t.Fatalf("attack=%+v", attack)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"]
	if !flagForNPCCombatTest(savedPlayer.Body.Flags[:], 16) {
		t.Fatalf("saved player omitted acid PPOISN: flags=%#x", savedPlayer.Body.Flags)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-breath-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restarted.RunNPCCombatTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	if replayCalls != 0 || rollCalls != 4 {
		t.Fatalf("replay consumed RNG: replayCalls=%d firstCalls=%d", replayCalls, rollCalls)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatPhasePersistsLethalBreathAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatLethalTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Level = 1
	for _, bit := range []uint{19, 28, 29} { // MBRETH + MBRWP1/MBRWP2 (acid branch)
		npc.Body.Flags[bit/8] |= 1 << (bit % 8)
	}
	state.NPCs["npc-b"] = npc
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-breath-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch {
			case low == 1 && high == 20:
				return 20
			case low == 1 && high == 30:
				return 4
			case low == 1 && high == 2:
				return 2
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-breath-lethal", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d err=%v", first, store.commits, err)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Lethal || !summary.Attacks[0].BreathTriggered || summary.Attacks[0].BreathType != world.NPCCombatBreathAcid || !summary.Attacks[0].BreathPoisoned || summary.Attacks[0].Damage != 3 || summary.Attacks[0].PlayerHP >= 1 || len(summary.Deaths) != 1 || !summary.StoppedAfterDeath {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"]
	if savedPlayer.Body.RoomID != 1008 || savedPlayer.Body.HPCurrent < 1 {
		t.Fatalf("saved lethal breath state=%+v", savedPlayer)
	}
	if rollCalls != 3 {
		t.Fatalf("attack RNG calls=%d, want hit/trigger/acid die", rollCalls)
	}

	replay, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-breath-lethal", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || rollCalls != 3 {
		t.Fatalf("replay=%+v commits=%d RNG calls=%d err=%v", replay, store.commits, rollCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatPhasePersistsDissolveAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	player := state.Players["player-b"]
	player.Body.Class = 4
	player.Body.Level = 1
	player.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	player.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"held":  {Object: world.LegacyObject{Name: "held"}},
			"wield": {Object: world.LegacyObject{Name: "wield"}},
		},
		Ready: [20]string{16: "held", 19: "wield"},
	}
	state.Players["player-b"] = player
	npc := state.NPCs["npc-b"]
	npc.Body.Flags[35/8] |= 1 << (35 % 8) // MDISIT
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-dissolve-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch {
			case low == 1 && high == 20:
				return 20
			case low == 1 && high == 4:
				return 4
			case low == 1 && high == 100:
				return 15
			case low == 0 && high == 1:
				return 1
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-dissolve", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d err=%v", first, store.commits, err)
	}
	if rollCalls != 4 {
		t.Fatalf("attack RNG calls=%d, want hit/damage/dissolve/selection", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	attack := summary.Attacks[0]
	if !attack.Hit || !attack.DissolveSucceeded || !attack.Dissolved || attack.DissolveProtected || attack.DissolveRoll != 15 || attack.DissolveSelectionRoll != 1 || attack.DissolveCandidateCount != 2 || attack.DissolveReadySlot != 19 || attack.DissolveItemID != "wield" || attack.PlayerHP != 26 {
		t.Fatalf("attack=%+v", attack)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedItems := saved.Players["player-b"].Items
	if savedItems.Ready[16] != "held" || savedItems.Ready[19] != "" {
		t.Fatalf("saved ready=%v", savedItems.Ready)
	}
	if _, ok := savedItems.Items["wield"]; ok {
		t.Fatal("dissolved wield root remains in durable state")
	}
	stats, err := savedItems.CombatStats(saved.Players["player-b"].Body)
	if err != nil {
		t.Fatal(err)
	}
	if int8(saved.Players["player-b"].Body.Armor) != stats.Armor || int8(saved.Players["player-b"].Body.Thaco) != stats.Thaco {
		t.Fatalf("saved equipment stats not refreshed: body=%+v stats=%+v", saved.Players["player-b"].Body, stats)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-dissolve-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(context.Background(), "npc-combat-dissolve", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || replayCalls != 0 {
		t.Fatalf("replay=%+v commits=%d replay RNG calls=%d err=%v", replay, store.commits, replayCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatPhasePersistsLethalDissolveAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatLethalTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Flags[35/8] |= 1 << (35 % 8) // MDISIT
	state.NPCs["npc-b"] = npc
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-dissolve-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch {
			case low == 1 && high == 20:
				return 20
			case low == 1 && high == 4:
				return 4
			case low == 1 && high == 100:
				return 15
			case low == 0 && high == 0:
				return 0
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-dissolve-lethal", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d err=%v", first, store.commits, err)
	}
	if rollCalls != 4 {
		t.Fatalf("attack RNG calls=%d, want hit/damage/dissolve/selection", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Lethal || !summary.Attacks[0].DissolveSucceeded || !summary.Attacks[0].Dissolved || summary.Attacks[0].DissolveReadySlot != 19 || summary.Attacks[0].DissolveItemID != "weapon" || len(summary.Deaths) != 1 || !summary.StoppedAfterDeath {
		t.Fatalf("summary=%+v", summary)
	}
	if summary.Deaths[0].DroppedItemCount != 0 {
		t.Fatalf("lethal MDISIT counted dissolved item as floor drop: death=%+v", summary.Deaths[0])
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["player-b"].Body.RoomID != 1008 || saved.Players["player-b"].Body.HPCurrent < 1 || len(saved.Players["player-b"].Items.Items) != 0 || saved.Rooms[1].Items == nil {
		t.Fatalf("saved lethal dissolve state=%+v", saved)
	}
	sourceItems := saved.Rooms[1].Items
	if sourceItems == nil || len(sourceItems.Items) != 0 || len(sourceItems.Inventory) != 0 || sourceItems.Ready != [20]string{} {
		t.Fatalf("lethal MDISIT left a source-floor item graph: items=%+v", sourceItems)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-dissolve-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(context.Background(), "npc-combat-dissolve-lethal", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || replayCalls != 0 {
		t.Fatalf("replay=%+v commits=%d replay RNG calls=%d err=%v", replay, store.commits, replayCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func flagForNPCCombatTest(flags []byte, bit uint) bool {
	return flags[bit/8]&(1<<(bit%8)) != 0
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

func TestRunNPCCombatPhaseCommitsLethalPlayerDeathAndReplays(t *testing.T) {
	state := npcCombatLethalTickFixture(t)
	npc := state.NPCs["npc-b"]
	npc.Body.Flags[13/8] |= 1 << (13 % 8) // MPOISS
	npc.Body.Flags[34/8] |= 1 << (34 % 8) // MDISEA
	npc.Body.Flags[45/8] |= 1 << (45 % 8) // MBLNDR
	state.NPCs["npc-b"] = npc
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			if high == 100 {
				switch rollCalls {
				case 3:
					return 15 // MPOISS
				case 4, 5:
					return 10 // MDISEA/MBLNDR
				default:
					t.Fatalf("unexpected extra effect draw at call %d", rollCalls)
					return 0
				}
			}
			return high
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-lethal", 5, 100)
	if err != nil || receipt.Replayed || store.commits != 1 {
		t.Fatalf("receipt=%+v commits=%d err=%v", receipt, store.commits, err)
	}
	summary := decodeNPCCombatTickSummary(t, receipt.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Lethal || !summary.Attacks[0].Poisoned || !summary.Attacks[0].Diseased || !summary.Attacks[0].Blinded || summary.Attacks[0].PlayerHP >= 1 ||
		len(summary.FailClosed) != 0 || len(summary.Deaths) != 1 || !summary.StoppedAfterDeath {
		t.Fatalf("summary=%+v", summary)
	}
	death := summary.Deaths[0]
	if death.NPCID != "npc-b" || death.PlayerID != "player-b" || death.SourceRoomID != 1 || death.DestinationRoomID != 1008 ||
		!death.BroadcastDeath || death.ExperienceAfter >= death.ExperienceBefore || !death.EnemyRemoved {
		t.Fatalf("death=%+v", death)
	}
	if death.DroppedItemCount != 1 {
		t.Fatalf("ordinary lethal equipment loss dropped item count=%d, want 1", death.DroppedItemCount)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["player-b"].Body.RoomID != 1008 || saved.Players["player-b"].Body.HPCurrent < 1 ||
		len(saved.Players["player-b"].Items.Items) != 0 || len(saved.Rooms[1].Items.Items) != 1 ||
		len(saved.NPCs["npc-b"].Enemies) != 0 || !reflect.DeepEqual(saved.ActiveNPCIDs, []string{"npc-b"}) {
		t.Fatalf("saved lethal state=%+v", saved)
	}
	if rollCalls != 5 {
		t.Fatalf("attack RNG calls=%d, want exactly hit/damage/poison/disease/blind", rollCalls)
	}

	replay, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-lethal", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || rollCalls != 5 {
		t.Fatalf("replay=%+v commits=%d RNG calls=%d err=%v", replay, store.commits, rollCalls, err)
	}
	if !bytes.Equal(replay.Response, receipt.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", receipt.Response, replay.Response)
	}
}

func TestRunNPCCombatPhaseLethalDeathFailureRollsBackRespawnAndAllocator(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*world.State)
		deps   func() (world.SpawnCatalog, func() (string, error))
	}{
		{
			name: "missing respawn floor",
			mutate: func(state *world.State) {
				delete(state.Rooms, 1008)
			},
			deps: func() (world.SpawnCatalog, func() (string, error)) {
				return nil, nil
			},
		},
		{
			name: "allocator error after spawn planning",
			mutate: func(state *world.State) {
				respawn := state.Rooms[1008]
				respawn.Resource.PermanentMonsters[0] = world.LegacyTimer{LastTime: 0, Interval: 1, Misc: 7}
				state.Rooms[1008] = respawn
			},
			deps: func() (world.SpawnCatalog, func() (string, error)) {
				return npcCombatTickSpawnCatalog{}, func() (string, error) {
					return "", errors.New("allocator unavailable")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := npcCombatLethalTickFixture(t)
			test.mutate(&state)
			if err := state.Validate(); err != nil {
				t.Fatal(err)
			}
			before := encodeNPCCombatTickState(t, state)
			catalog, allocate := test.deps()
			store := &npcCombatTickStore{state: before}
			now := int32(100)
			connector := newNPCCombatTickConnectorWithDeps(t, store, &now, catalog, allocate)
			if _, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-lethal-failure-"+test.name, 5, 100); err == nil {
				t.Fatal("accepted incomplete lethal continuation")
			}
			if store.commits != 0 || !bytes.Equal(before, store.snapshot()) {
				t.Fatalf("lethal failure was not atomic commits=%d state=%s", store.commits, store.snapshot())
			}
		})
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

func TestRunNPCCombatPhasePersistsMissProgressionAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	player := state.Players["player-b"]
	player.Body.Experience = 1234
	player.Body.Proficiency = [5]int32{10, 20, 30, 40, 50}
	player.Body.Realm = [4]int32{1, 2, 3, 4}
	state.Players["player-b"] = player
	npc := state.NPCs["npc-b"]
	npc.Body.Thaco = 20
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-miss-progression-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 20 {
				t.Fatalf("unexpected random request %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-miss-progression", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 || rollCalls != 1 {
		t.Fatalf("first=%+v commits=%d RNG calls=%d err=%v", first, store.commits, rollCalls, err)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	attack := summary.Attacks[0]
	if attack.Hit || attack.Damage != 0 || attack.PlayerHP != 30 || attack.ExperienceBefore != 1234 || attack.ExperienceAfter != 1234 || attack.ProficiencyBefore != [5]int32{10, 20, 30, 40, 50} || attack.ProficiencyAfter != [5]int32{10, 20, 30, 40, 50} || attack.RealmBefore != [4]int32{1, 2, 3, 4} || attack.RealmAfter != [4]int32{1, 2, 3, 4} {
		t.Fatalf("attack=%+v", attack)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"].Body
	if savedPlayer.Experience != 1234 || savedPlayer.Proficiency != [5]int32{10, 20, 30, 40, 50} || savedPlayer.Realm != [4]int32{1, 2, 3, 4} || savedPlayer.HPCurrent != 30 {
		t.Fatalf("saved miss progression state=%+v", savedPlayer)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-miss-progression-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 20
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(context.Background(), "npc-combat-miss-progression", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || replayCalls != 0 {
		t.Fatalf("replay=%+v commits=%d replay RNG calls=%d err=%v", replay, store.commits, replayCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatPhasePersistsMENEDRAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatTickFixture(t)
	player := state.Players["player-b"]
	player.Body.Experience = 1000
	player.Body.Proficiency = [5]int32{10, 20, 30, 40, 50}
	player.Body.Realm = [4]int32{1, 2, 3, 4}
	state.Players["player-b"] = player
	npc := state.NPCs["npc-b"]
	npc.Body.Level = 5 // MENEDR band=(level+3)/4 == 2.
	npc.Body.DiceSides = 6
	npc.Body.Flags[30/8] |= 1 << (30 % 8) // MENEDR
	state.NPCs["npc-b"] = npc
	state.ActiveNPCIDs = []string{"npc-b"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-menedr-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch high {
			case 20:
				return 20
			case 100:
				return 9
			case 5:
				return 5
			case 6:
				return 6
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-menedr", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d err=%v", first, store.commits, err)
	}
	if rollCalls != 5 {
		t.Fatalf("attack RNG calls=%d, want hit/energy-roll/two-energy-dice/damage", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	attack := summary.Attacks[0]
	if !attack.Hit || !attack.EnergyDrainTriggered || attack.EnergyRoll != 9 || attack.EnergyBand != 2 || attack.EnergyDiceCount != 2 || attack.EnergyDiceSides != 5 || attack.EnergyDicePlus != 10 || attack.EnergyDrain != 20 || attack.ExperienceBefore != 1000 || attack.ExperienceAfter != 980 || attack.Damage != 6 || attack.PlayerHP != 24 {
		t.Fatalf("attack=%+v", attack)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	savedPlayer := saved.Players["player-b"].Body
	if savedPlayer.Experience != 980 || savedPlayer.Proficiency != [5]int32{8, 18, 28, 38, 1024} || savedPlayer.Realm != [4]int32{} || savedPlayer.HPCurrent != 24 {
		t.Fatalf("saved MENEDR state=%+v", savedPlayer)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-menedr-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(context.Background(), "npc-combat-menedr", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || replayCalls != 0 {
		t.Fatalf("replay=%+v commits=%d replay RNG calls=%d err=%v", replay, store.commits, replayCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}

func TestRunNPCCombatPhasePersistsLethalMENEDRAndReplaysWithoutRNG(t *testing.T) {
	state := npcCombatLethalTickFixture(t)
	player := state.Players["player-b"]
	player.Body.Proficiency = [5]int32{10, 20, 30, 40, 50}
	player.Body.Realm = [4]int32{1, 2, 3, 4}
	state.Players["player-b"] = player
	npc := state.NPCs["npc-b"]
	npc.Body.Level = 5                    // MENEDR band=(level+3)/4 == 2.
	npc.Body.Flags[30/8] |= 1 << (30 % 8) // MENEDR
	state.NPCs["npc-b"] = npc
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-menedr-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			switch high {
			case 20:
				return 20
			case 100:
				return 9
			case 5:
				return 5
			case 4:
				return 4
			default:
				t.Fatalf("unexpected random request %d..%d", low, high)
				return 0
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-menedr-lethal", 5, 100)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d err=%v", first, store.commits, err)
	}
	if rollCalls != 5 {
		t.Fatalf("lethal attack RNG calls=%d, want hit/energy-roll/two-energy-dice/damage", rollCalls)
	}
	summary := decodeNPCCombatTickSummary(t, first.Response)
	if len(summary.Attacks) != 1 || !summary.Attacks[0].Lethal || !summary.Attacks[0].EnergyDrainTriggered || summary.Attacks[0].EnergyDrain != 20 || summary.Attacks[0].ExperienceBefore != 100 || summary.Attacks[0].ExperienceAfter != 80 || len(summary.Deaths) != 1 || !summary.StoppedAfterDeath {
		t.Fatalf("summary=%+v", summary)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["player-b"].Body.Experience < 0 || saved.Players["player-b"].Body.HPCurrent < 1 {
		t.Fatalf("saved lethal MENEDR state=%+v", saved.Players["player-b"].Body)
	}

	replayCalls := 0
	restarted, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-menedr-lethal-world",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return 100, 12 },
		Roll: func(_, _ int) int {
			replayCalls++
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.RunNPCCombatPhase(context.Background(), "npc-combat-menedr-lethal", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 || replayCalls != 0 {
		t.Fatalf("replay=%+v commits=%d replay RNG calls=%d err=%v", replay, store.commits, replayCalls, err)
	}
	if !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay response changed: first=%s replay=%s", first.Response, replay.Response)
	}
}
