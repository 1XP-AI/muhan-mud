package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDMEnemyState(class byte) world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor", "observer"},
				NPCIDs:    []string{"orc-first", "orc-second"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor":    {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "관찰자", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"orc-first":  {Body: world.LegacyMonster{Name: "오크", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "observer"}}}},
			"orc-second": {Body: world.LegacyMonster{Name: "오크", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{},
	}
}

func admitConnectorDMEnemy(t *testing.T, connector *WorldConnector, actorID string) *worldConnection {
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

func TestWorldConnectorSubmitDispatchesDMEnemyReadOnlyAndSuppressesReplay(t *testing.T) {
	state := connectorDMEnemyState(12)
	initial, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: initial}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-enemy", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMEnemy(t, connector, "actor")
	observer := admitConnectorDMEnemy(t, connector, "observer")

	output, err := actor.Submit(context.Background(), "오크 *enemy")
	want := "오크의 적들:\n관찰자.\n"
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("enemy=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("DM enemy changed the world snapshot")
	}
	select {
	case event := <-observer.events:
		t.Fatalf("unexpected DM enemy fan-out %q", event)
	default:
	}

	replay, err := actor.Submit(context.Background(), "오크 *적")
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

func TestWorldConnectorSubmitDMEnemyMortalIsUnknownAndMalformedIsNotCommitted(t *testing.T) {
	state := connectorDMEnemyState(4)
	initial, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: initial}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-enemy-mortal", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := admitConnectorDMEnemy(t, connector, "actor")

	output, err := actor.Submit(context.Background(), "오크 *적")
	if err != nil || output != world.DMEnemyUnknownResponse("*적") || store.commits != 1 {
		t.Fatalf("mortal=%q err=%v commits=%d", output, err, store.commits)
	}
	stateAfter, _ := store.snapshot()
	if !bytes.Equal(stateAfter, initial) {
		t.Fatal("unauthorized DM enemy changed the world snapshot")
	}
	if output, err := actor.Submit(context.Background(), "오크 *enemy extra"); err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 1 {
		t.Fatalf("malformed=%q err=%v commits=%d", output, err, store.commits)
	}
}
