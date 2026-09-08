package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type connectorCommandStore struct {
	mu      sync.Mutex
	state   json.RawMessage
	receipt *storage.WorldReceipt
	command string
	commits int
}

func (s *connectorCommandStore) ReadWorldReceipt(_ context.Context, _ string, command string, _ json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.receipt == nil || s.command != command {
		return storage.WorldReceipt{}, sql.ErrNoRows
	}
	r := *s.receipt
	r.Replayed = true
	return r, nil
}
func (s *connectorCommandStore) LoadWorld(context.Context, string) (storage.WorldSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storage.WorldSnapshot{State: append(json.RawMessage(nil), s.state...)}, nil
}
func (s *connectorCommandStore) CommitWorldCommand(_ context.Context, _ string, command string, _ json.RawMessage, revision int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commits++
	s.state = append(json.RawMessage(nil), state...)
	s.command = command
	r := storage.WorldReceipt{Revision: revision + 1, Response: response}
	s.receipt = &r
	return r, nil
}

func (s *connectorCommandStore) snapshot() (json.RawMessage, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(json.RawMessage(nil), s.state...), s.commits
}

func TestWorldConnectorSubmitDispatchesDirectionThroughDurableCommand(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "w", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	text, err := connection.Submit(context.Background(), "8")
	stateRaw, commits := store.snapshot()
	if err != nil || text == "" || !strings.Contains(text, "도착지") || commits != 1 {
		t.Fatalf("direction submit text=%q err=%v commits=%d", text, err, commits)
	}
	saved, err := world.DecodeState(stateRaw)
	if err != nil || saved.Players["a"].Body.RoomID != 2 || len(saved.Rooms[1].PlayerIDs) != 0 || len(saved.Rooms[2].PlayerIDs) != 1 {
		t.Fatalf("connector did not persist movement: %+v %v", saved, err)
	}
}

func TestWorldConnectorRunsDurablePlayerVitalPhaseAndReplays(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 10, MPMax: 80, MPCurrent: 2},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	r := initial.Rooms[1]
	r.PlayerIDs = []string{"a"}
	initial.Rooms[1] = r
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "vitals-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := connector.RunPlayerVitalPhase(context.Background(), "vitals-1")
	if err != nil {
		t.Fatal(err)
	}
	var summary playerPhaseSummary
	if err := json.Unmarshal(first.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(summary.Actors[0], "a") || summary.Now != 100 || summary.Hour != 12 || store.commits != 1 {
		t.Fatalf("summary=%+v commits=%d", summary, store.commits)
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || saved.Players["a"].Body.HPCurrent <= 10 {
		t.Fatalf("player phase did not persist vitals: %+v %v", saved.Players["a"], err)
	}
	replay, err := connector.RunPlayerVitalPhase(context.Background(), "vitals-1")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestWorldConnectorSubmitDispatchesNPCAttackThroughDurableCommand(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, NPCIDs: []string{"wolf-id"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"wolf-id": {Body: world.LegacyMonster{Name: "늑대", RoomID: 1, Type: 1, Class: 4, Level: 1, Armor: 100, HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4}, Enemies: []world.NPCEnemy{}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "attack-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1, Roll: func(_, hi int) int { return hi }})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	text, err := connection.Submit(context.Background(), "공격 늑대")
	if err != nil || !strings.Contains(text, "5 만큼의 피해") {
		t.Fatalf("attack text=%q err=%v", text, err)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || saved.NPCs["wolf-id"].Body.HPCurrent != 45 {
		t.Fatalf("saved=%+v err=%v commits=%d", saved.NPCs["wolf-id"], err, commits)
	}
	status, err := connection.Submit(context.Background(), "점수")
	if err != nil || !strings.Contains(status, "Alice") {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestWorldConnectorSubmitDispatchesFollowAndLoseThroughDurableCommand(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "follow-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	text, err := connection.Submit(context.Background(), "따라 Bob")
	if err != nil || !strings.Contains(text, "Bob") {
		t.Fatalf("follow text=%q err=%v", text, err)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || saved.Players["a"].FollowingID != "b" || len(saved.Players["b"].FollowerIDs) != 1 {
		t.Fatalf("follow state=%+v err=%v commits=%d", saved.Players, err, commits)
	}
	text, err = connection.Submit(context.Background(), "내보내")
	if err != nil || !strings.Contains(text, "그만 따라다니기로") {
		t.Fatalf("lose text=%q err=%v", text, err)
	}
	stateRaw, commits = store.snapshot()
	saved, err = world.DecodeState(stateRaw)
	if err != nil || commits != 2 || saved.Players["a"].FollowingID != "" || len(saved.Players["b"].FollowerIDs) != 0 {
		t.Fatalf("lose state=%+v err=%v commits=%d", saved.Players, err, commits)
	}
}

func TestWorldConnectorSubmitDispatchesInventoryAndEquipmentThroughDurableCommand(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{"floor": {Object: world.LegacyObject{Name: "돌"}}}, Inventory: []string{"floor"}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷", Wear: 1, Armor: 2, ShotsMax: 1, ShotsCurrent: 1}}}, Inventory: []string{"armor"}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "items-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	inventory, err := connection.Submit(context.Background(), "소지품")
	if err != nil || !strings.Contains(inventory, "소지품") {
		t.Fatalf("inventory=%q err=%v", inventory, err)
	}
	equipment, err := connection.Submit(context.Background(), "장비")
	if err != nil || !strings.Contains(equipment, "걸치고") {
		t.Fatalf("equipment=%q err=%v", equipment, err)
	}
	say, err := connection.Submit(context.Background(), "말 안녕하세요")
	if err != nil || !strings.Contains(say, "좋습니다") {
		t.Fatalf("say=%q err=%v", say, err)
	}
	taken, err := connection.Submit(context.Background(), "주워 돌")
	if err != nil || !strings.Contains(taken, "주웠습니다") {
		t.Fatalf("take=%q err=%v", taken, err)
	}
	worn, err := connection.Submit(context.Background(), "입어 갑옷")
	if err != nil || !strings.Contains(worn, "입었습니다") {
		t.Fatalf("wear=%q err=%v", worn, err)
	}
	removed, err := connection.Submit(context.Background(), "벗어 갑옷")
	if err != nil || !strings.Contains(removed, "벗었습니다") {
		t.Fatalf("remove=%q err=%v", removed, err)
	}
	_, commits := store.snapshot()
	if commits != 6 {
		t.Fatalf("commits=%d", commits)
	}
}
