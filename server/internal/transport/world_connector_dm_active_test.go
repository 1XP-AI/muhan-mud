package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDMActiveState(class byte) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor", "other"},
				NPCIDs:    []string{"room-first", "room-second"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"other": {Body: world.LegacyMonster{Name: "다른사람", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"room-first":  {Body: world.LegacyMonster{Name: "방순서", Type: 1, RoomID: 1}},
			"room-second": {Body: world.LegacyMonster{Name: "활성순서", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{"room-second", "room-first"},
	}
}

func admitConnectorDMActive(t *testing.T, connector *WorldConnector, actorID string) *worldConnection {
	t.Helper()
	lease, err := connector.owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[connection] = struct{}{}
	return connection
}

func TestWorldConnectorSubmitDispatchesDMActiveReadOnlyAndSuppressesReplay(t *testing.T) {
	state := connectorDMActiveState(12)
	initial, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: initial}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-active", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMActive(t, connector, "actor")
	other := admitConnectorDMActive(t, connector, "other")

	output, err := actor.Submit(context.Background(), "*active")
	want := world.DMActiveHeaderResponse + "   활성순서.\n   방순서.\n"
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("active=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("DM active changed the world snapshot")
	}
	select {
	case event := <-other.events:
		t.Fatalf("unexpected DM active fan-out %q", event)
	default:
	}

	replay, err := actor.Submit(context.Background(), "*활성")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-other.events:
		t.Fatalf("unexpected replay fan-out %q", event)
	default:
	}
}

func TestWorldConnectorSubmitDMActiveMortalIsTypedNoOp(t *testing.T) {
	initial, err := json.Marshal(connectorDMActiveState(4))
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: initial}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-active-mortal", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMActive(t, connector, "actor")

	output, err := actor.Submit(context.Background(), "*활성")
	if err != nil || output != world.DMActiveUnknownResponse("*활성") || store.commits != 1 {
		t.Fatalf("mortal=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("unauthorized DM active changed the world snapshot")
	}
	if output, err := actor.Submit(context.Background(), "*active npc"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("argument form=%q err=%v commits=%d", output, err, store.commits)
	}
}
