package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorBuyStatesState(class byte, experience, gold int32, inventory []world.LegacyObject) world.State {
	actor := world.LegacyMonster{
		Name: "초인", Type: 0, Class: class, RoomID: 1,
		Experience: experience, Gold: gold, HPMax: 100, MPMax: 80,
		Stats: [5]byte{10, 10, 10, 10, 10},
	}
	if inventory != nil {
		actor.Inventory = inventory
	}
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: actor, Online: true},
		},
	}
}

func TestWorldConnectorSubmitDispatchesBuyStatesPromptAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorBuyStatesState(world.BuyStatesCaretakerClass, 101000000, 2000000, nil))
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "buy-states", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "향상")
	if err != nil || output != world.BuyStatesPromptResponse || store.commits != 1 {
		t.Fatalf("prompt=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000 || saved.Players["actor"].Body.Experience != 101000000 {
		t.Fatalf("prompt mutated gold/exp=%+v", saved.Players["actor"].Body)
	}

	replay, err := conn.Submit(context.Background(), "향상")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
}

func TestWorldConnectorSubmitBuyStatesNonCaretakerIsTypedNoOp(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorBuyStatesState(4, 101000000, 2000000, nil))
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "buy-states-fighter", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "향상")
	if err != nil || output != world.BuyStatesNotCaretakerResponse || store.commits != 1 {
		t.Fatalf("not-caretaker=%q err=%v commits=%d", output, err, store.commits)
	}
}

func TestWorldConnectorSubmitBuyStatesPrefixAndUnmigratedStayClosed(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorBuyStatesState(world.BuyStatesCaretakerClass, 101000000, 2000000, nil))
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "buy-states-prefix", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "향상 체력")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("prefix=%q err=%v commits=%d", output, err, store.commits)
	}

	unmigrated, err := json.Marshal(connectorBuyStatesState(world.BuyStatesCaretakerClass, 101000000, 2000000, []world.LegacyObject{{Name: "금화"}}))
	if err != nil {
		t.Fatal(err)
	}
	store.state = unmigrated
	output, err = conn.Submit(context.Background(), "체력 향상")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("unmigrated=%q err=%v commits=%d", output, err, store.commits)
	}

	ready, err := json.Marshal(connectorBuyStatesState(world.BuyStatesCaretakerClass, 101000000, 2000000, nil))
	if err != nil {
		t.Fatal(err)
	}
	store.state = ready
	output, err = conn.Submit(context.Background(), "체력 향상")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("apply pending=%q err=%v commits=%d", output, err, store.commits)
	}
}
