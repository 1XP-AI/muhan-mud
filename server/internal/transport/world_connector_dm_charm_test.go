package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDMCharmState(class byte) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor", "observer"},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "먼방"}},
				PlayerIDs: []string{"target"},
			},
			3: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "NPC방"}},
				PlayerIDs: []string{"player-charmer"},
				NPCIDs:    []string{"npc-charmer"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor":          {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"observer":       {Body: world.LegacyMonster{Name: "관찰자", Type: 0, Class: 4, RoomID: 1}, Online: true},
			"target":         {Body: world.LegacyMonster{Name: "대상자", Type: 0, Class: 4, RoomID: 2}, Online: true, CharmRefs: []world.EntityRef{{Kind: "player", ID: "player-charmer"}, {Kind: "npc", ID: "npc-charmer"}}},
			"player-charmer": {Body: world.LegacyMonster{Name: "플레이어최면", Type: 0, Class: 4, RoomID: 3}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"npc-charmer": {Body: world.LegacyMonster{Name: "NPC최면", Type: 1, RoomID: 3}},
		},
		ActiveNPCIDs: []string{},
	}
}

func admitConnectorDMCharm(t *testing.T, connector *WorldConnector, actorID string) *worldConnection {
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

func TestWorldConnectorSubmitDispatchesDMCharmReadOnlyAndSuppressesFanoutReplay(t *testing.T) {
	state := connectorDMCharmState(12)
	initial, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: initial}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-charm", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMCharm(t, connector, "actor")
	observer := admitConnectorDMCharm(t, connector, "observer")

	output, err := actor.Submit(context.Background(), "대상자 *charm")
	want := "대상자의 피최면자:\n플레이어최면.\nNPC최면.\n"
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("charm=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("DM charm changed the world snapshot")
	}
	select {
	case event := <-observer.events:
		t.Fatalf("unexpected DM charm fan-out %q", event)
	default:
	}

	replay, err := actor.Submit(context.Background(), "대상자 *최면")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("unexpected replay fan-out %q", event)
	default:
	}
}

func TestWorldConnectorSubmitDMCharmMortalGateAndMissingTargetBoundary(t *testing.T) {
	initial, err := json.Marshal(connectorDMCharmState(4))
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: initial}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-charm-mortal", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMCharm(t, connector, "actor")

	output, err := actor.Submit(context.Background(), "없는대상 *최면")
	if err != nil || output != world.DMCharmUnknownResponse("*최면") || store.commits != 1 {
		t.Fatalf("mortal=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("unauthorized DM charm changed the world snapshot")
	}

	if output, err := actor.Submit(context.Background(), "*charm extra"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("malformed=%q err=%v commits=%d", output, err, store.commits)
	}

	// An authorized missing target fails before ExecuteGame and therefore
	// leaves no receipt or snapshot commit.
	authorizedRaw, err := json.Marshal(connectorDMCharmState(12))
	if err != nil {
		t.Fatal(err)
	}
	authorizedStore := &connectorCommandStore{state: authorizedRaw}
	authorizedConnector, err := NewWorldConnector(WorldConnectorConfig{
		Store: authorizedStore, WorldID: "dm-charm-missing", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizedActor := admitConnectorDMCharm(t, authorizedConnector, "actor")
	if output, err := authorizedActor.Submit(context.Background(), "없는대상 *charm"); err != nil || output != "없는대상은 없습니다.\n" || authorizedStore.commits != 0 {
		t.Fatalf("missing target=%q err=%v commits=%d", output, err, authorizedStore.commits)
	}
}
