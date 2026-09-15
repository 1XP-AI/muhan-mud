package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDMFollowState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm", "player"},
				NPCIDs:    []string{"wolf-1"},
			},
		},
		Players: map[string]world.PlayerState{
			"dm":     {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}, Online: true},
			"player": {Body: world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf-1": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{},
	}
}

func TestWorldConnectorSubmitDispatchesDMFollowAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorDMFollowState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-follow", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"dm", "player"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = conn
		connector.connections[conn] = struct{}{}
	}

	output, err := connections["dm"].Submit(context.Background(), "늑대 *따르기")
	want := world.DMFollowAttachResponse("늑대")
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("attach=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections["player"].events:
		t.Fatalf("unexpected fan-out %q", event)
	default:
	}

	replay, err := connections["dm"].Submit(context.Background(), "늑대 *따르기")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}

	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := saved.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "dm" || !world.PlayerFlagSet(npc.Body, 46) {
		t.Fatalf("saved npc=%+v", npc)
	}
}

func TestWorldConnectorSubmitDMFollowUnknownForMortal(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorDMFollowState())
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-follow-mortal", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("player")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
	connector.connections[conn] = struct{}{}

	output, err := conn.Submit(context.Background(), "늑대 *따르기")
	state, commits := store.snapshot()
	if err != nil || output != world.DMFollowUnknownResponse("*따르기") || commits != 1 {
		t.Fatalf("mortal=%q err=%v commits=%d", output, err, commits)
	}
	saved, err := world.DecodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	npc := saved.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "" || world.PlayerFlagSet(npc.Body, 46) {
		t.Fatalf("mortal mutated npc=%+v", npc)
	}
}
