package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func boundedLaneConnection(t *testing.T, store *connectorCommandStore, state world.State, ids ...string) (*WorldConnector, []*worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "bounded-lanes",
		Clock:       func() (int32, int) { return 8, 12 },
		Roll:        func(_, _ int) int { return 1 },
		MaxSessions: len(ids),
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, len(ids))
	for _, id := range ids {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
		connections = append(connections, connection)
		connector.connections[connection] = struct{}{}
	}
	return connector, connections
}

func TestWorldConnectorSubmitDispatchesAliasAndPersistsOrder(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor")
	output, err := connections[0].Submit(context.Background(), "줄임말 북 북쪽")
	if err != nil || !strings.Contains(output, "설정") {
		t.Fatalf("alias output=%q err=%v", output, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Players["actor"].Aliases) != 1 || saved.Players["actor"].Aliases[0].Process != "북쪽" || store.commits != 1 {
		t.Fatalf("alias state=%+v err=%v commits=%d", saved.Players["actor"].Aliases, err, store.commits)
	}
}

func TestWorldConnectorSubmitDispatchesBurnAndPublishesEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Gold: 10, Experience: 20}, Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{"coin": {Object: world.LegacyObject{Name: "동전"}}}, Inventory: []string{"coin"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor", "observer")
	output, err := connections[0].Submit(context.Background(), "태워 동전")
	if err != nil || !strings.Contains(output, "태웠습니다") {
		t.Fatalf("burn output=%q err=%v", output, err)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "동전") {
			t.Fatalf("burn event=%q", event)
		}
	default:
		t.Fatal("burn observer event missing")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 || saved.Players["actor"].Body.Gold != 100014 || store.commits != 1 {
		t.Fatalf("burn state=%+v err=%v commits=%d", saved.Players["actor"], err, store.commits)
	}
}

func TestWorldConnectorSubmitDispatchesStudyAndPublishesEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor", "observer"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Level: 10}, Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{"scroll": {Object: world.LegacyObject{Name: "두루마리", Type: world.StudyScrollType, DiceCount: 1, MagicPower: 1}}}, Inventory: []string{"scroll"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor", "observer")
	output, err := connections[0].Submit(context.Background(), "배워 두루마리")
	if err != nil || !strings.Contains(output, "연마") {
		t.Fatalf("study output=%q err=%v", output, err)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "두루마리") {
			t.Fatalf("study event=%q", event)
		}
	default:
		t.Fatal("study observer event missing")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 || !worldSpellKnown(saved.Players["actor"].Body.Spells, 0) || store.commits != 1 {
		t.Fatalf("study state=%+v err=%v commits=%d", saved.Players["actor"], err, store.commits)
	}
}

func worldSpellKnown(spells [16]byte, index int) bool {
	return spells[index/8]&(1<<uint(index%8)) != 0
}
