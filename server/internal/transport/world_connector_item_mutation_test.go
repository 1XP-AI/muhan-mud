package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func lastTokenItemMutationState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"floor-sword": {Object: world.LegacyObject{Name: "검"}},
				}, Inventory: []string{"floor-sword"}},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
}

func collectionHasInventoryName(c *world.ItemCollection, name string) bool {
	if c == nil {
		return false
	}
	for _, id := range c.Inventory {
		if c.Items[id].Object.Name == name {
			return true
		}
	}
	return false
}

func TestWorldConnectorSubmitLastTokenItemMutationAndReplay(t *testing.T) {
	raw, err := json.Marshal(lastTokenItemMutationState())
	if err != nil {
		t.Fatal(err)
	}
	replayStore := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	replayConnector, err := NewWorldConnector(WorldConnectorConfig{
		Store: replayStore, WorldID: "item-suffix-replay", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	replayConn := admitConnectorPlayers(t, replayConnector, []string{"a"})
	taken, err := replayConn["a"].Submit(context.Background(), "검 주워")
	if err != nil || !strings.Contains(taken, "주웠습니다") || replayStore.commits != 1 {
		t.Fatalf("take=%q err=%v commits=%d", taken, err, replayStore.commits)
	}
	replay, err := replayConn["a"].Submit(context.Background(), "검 주워")
	if err != nil || replay != taken {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := replayStore.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}

	raw, err = json.Marshal(lastTokenItemMutationState())
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "item-suffix", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
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

	taken, err = connection.Submit(context.Background(), "검 주워")
	if err != nil || !strings.Contains(taken, "주웠습니다") || store.commits != 1 {
		t.Fatalf("take=%q err=%v commits=%d", taken, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if collectionHasInventoryName(saved.Rooms[1].Items, "검") || !collectionHasInventoryName(saved.Players["a"].Items, "검") {
		t.Fatalf("take locations room=%+v player=%+v", saved.Rooms[1].Items, saved.Players["a"].Items)
	}

	dropped, err := connection.Submit(context.Background(), "검 버려")
	if err != nil || !strings.Contains(dropped, "버렸습니다") || store.commits != 2 {
		t.Fatalf("drop=%q err=%v commits=%d", dropped, err, store.commits)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if !collectionHasInventoryName(saved.Rooms[1].Items, "검") || collectionHasInventoryName(saved.Players["a"].Items, "검") {
		t.Fatalf("drop locations room=%+v player=%+v", saved.Rooms[1].Items, saved.Players["a"].Items)
	}
}

func TestWorldConnectorSubmitLastTokenItemMutationFailsClosed(t *testing.T) {
	raw, err := json.Marshal(lastTokenItemMutationState())
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "item-suffix-fail", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
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

	if _, err := connection.Submit(context.Background(), "없는검 주워"); store.commits != 0 {
		t.Fatalf("missing item committed err=%v commits=%d", err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || !collectionHasInventoryName(saved.Rooms[1].Items, "검") {
		t.Fatalf("missing mutated room=%+v actor=%+v err=%v", saved.Rooms[1].Items, saved.Players["a"], err)
	}

	unmigrated := lastTokenItemMutationState()
	player := unmigrated.Players["a"]
	player.Items = nil
	unmigrated.Players["a"] = player
	raw, err = json.Marshal(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	unmigratedStore := &connectorCommandStore{state: raw}
	unmigratedConnector, err := NewWorldConnector(WorldConnectorConfig{
		Store: unmigratedStore, WorldID: "item-suffix-unmigrated", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	unmigratedLease, err := unmigratedConnector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := unmigratedConnector.owners.Admit(unmigratedLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	unmigratedConn := &worldConnection{game: unmigratedConnector, lease: unmigratedLease, ready: true}
	if _, err := unmigratedConn.Submit(context.Background(), "검 주워"); unmigratedStore.commits != 0 {
		t.Fatalf("unmigrated committed err=%v commits=%d", err, unmigratedStore.commits)
	}
	saved, err = world.DecodeState(unmigratedStore.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("unmigrated moved actor=%+v err=%v", saved.Players["a"], err)
	}
}
