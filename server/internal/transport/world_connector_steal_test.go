package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesStealAndPublishesCommittedReveal(t *testing.T) {
	var actorFlags [8]byte
	actorFlags[1/8] |= 1 << (1 % 8) // PHIDDN
	actorFlags[2/8] |= 1 << (2 % 8) // PINVIS
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "방"}},
			PlayerIDs: []string{"a", "o"},
			NPCIDs:    []string{"goblin"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 8, Level: 20, Stats: [5]byte{10, 10, 10, 10, 10}, Flags: actorFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"o": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1, Class: 4, Level: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"goblin": {Body: world.LegacyMonster{Name: "고블린", Type: 1, RoomID: 1, Class: 4, Level: 1}, Items: &world.ItemCollection{Items: map[string]world.Item{"pouch": {Object: world.LegacyObject{Name: "주머니", Type: 1}}}, Inventory: []string{"pouch"}}, Enemies: []world.NPCEnemy{}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "steal-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2,
		Roll: func(_, high int) int { return high - 99 },
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, actorID := range []string{"a", "o"} {
		lease, acquireErr := connector.owners.Acquire(actorID)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()

	output, err := connections[0].Submit(context.Background(), "훔쳐 주머니 고블린")
	if err != nil || !strings.Contains(output, "훔쳤습니다") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "모습이 서서히 드러납니다") {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("committed steal reveal was not projected")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("actor received duplicate event=%q", event)
	default:
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.NPCs["goblin"].Items.Inventory) != 0 || !strings.Contains(saved.Players["a"].Items.Items["pouch"].Object.Name, "주머니") || saved.Players["a"].Body.Timers[world.StealTimerIndex].LastTime != 100 {
		t.Fatalf("saved actor=%+v npc=%+v", saved.Players["a"], saved.NPCs["goblin"])
	}
}

func TestWorldConnectorStealPlayerFailureDoesNotDuplicateTargetWarning(t *testing.T) {
	var actorFlags [8]byte
	actorFlags[28/8] |= 1 << (28 % 8) // PCHAOS
	var targetFlags [8]byte
	targetFlags[28/8] |= 1 << (28 % 8) // PCHAOS
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "방"}},
			PlayerIDs: []string{"a", "b", "o"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 8, Level: 20, Stats: [5]byte{10, 10, 10, 10, 10}, Flags: actorFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, Flags: targetFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"ring": {Object: world.LegacyObject{Name: "반지", Type: 1}}}, Inventory: []string{"ring"}}},
			"o": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1, Class: 4, Level: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "steal-player-failure-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
		Roll: func(_, high int) int { return high },
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 3)
	for _, actorID := range []string{"a", "b", "o"} {
		lease, acquireErr := connector.owners.Acquire(actorID)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()

	output, err := connections[0].Submit(context.Background(), "훔쳐 반지 Bob")
	if err != nil || !strings.Contains(output, "실패하였습니다") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case warning := <-connections[1].events:
		if !strings.Contains(warning, "당신에게서 반지") {
			t.Fatalf("target warning=%q", warning)
		}
	}
	select {
	case duplicate := <-connections[1].events:
		t.Fatalf("target received duplicate room event=%q", duplicate)
	default:
	}
	select {
	case roomEvent := <-connections[2].events:
		if !strings.Contains(roomEvent, "훔치려고 합니다") {
			t.Fatalf("observer room event=%q", roomEvent)
		}
	default:
		t.Fatal("observer room event missing")
	}
}
