package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesLegacyGroupAlias(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "social-alias-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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

	group, err := connection.Submit(context.Background(), "그룹")
	if err != nil {
		t.Fatalf("그룹 err=%v", err)
	}
	party, err := connection.Submit(context.Background(), "무리")
	if err != nil {
		t.Fatalf("무리 err=%v", err)
	}
	if group != party {
		t.Fatalf("alias response mismatch: 그룹=%q 무리=%q", group, party)
	}
	if _, commits := store.snapshot(); commits != 2 {
		t.Fatalf("expected one durable receipt per command, commits=%d", commits)
	}
}

func TestWorldConnectorSubmitDispatchesLongWhoThroughSocialHandlerAndReplays(t *testing.T) {
	initialState := connectorLongWhoState()
	initial, err := json.Marshal(initialState)
	if err != nil {
		t.Fatal(err)
	}
	// Submit generates connection-local command IDs. This store models the
	// durable command table's replay result for the second identical request.
	store := &groupTalkConnectorStore{state: append(json.RawMessage(nil), initial...)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "social-who-long-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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
	connection := &worldConnection{game: connector, lease: lease, ready: true}

	output, err := connection.Submit(context.Background(), "누구 l")
	stateAfter, receipt, commits := store.snapshot()
	if err != nil || !strings.Contains(output, "종족") || !strings.Contains(output, "Player30") || receipt == nil || commits != 1 {
		t.Fatalf("long who output=%q err=%v commits=%d", output, err, commits)
	}
	var storedOutput string
	if err := json.Unmarshal(receipt.Response, &storedOutput); err != nil || storedOutput != output {
		t.Fatalf("long who receipt=%q err=%v want=%q", storedOutput, err, output)
	}
	if string(stateAfter) != string(initial) {
		t.Fatal("long who changed the world snapshot")
	}

	replay, err := connection.Submit(context.Background(), "누구 l")
	if err != nil || replay != output {
		t.Fatalf("long who replay=%q err=%v want=%q", replay, err, output)
	}
	if _, _, commits = store.snapshot(); commits != 1 {
		t.Fatalf("long who replay committed again: commits=%d", commits)
	}
}

func connectorLongWhoState() world.State {
	ids := make([]string, 0, 31)
	players := make(map[string]world.PlayerState, 31)
	for i := 0; i < 31; i++ {
		id := "actor"
		name := "Alice"
		if i > 0 {
			id = fmt.Sprintf("player-%02d", i)
			name = fmt.Sprintf("Player%02d", i)
		}
		ids = append(ids, id)
		players[id] = world.PlayerState{
			Body:   world.LegacyMonster{Name: name, Type: 0, Class: 4, RoomID: 1},
			Online: true,
		}
	}
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: ids,
		}},
		Players: players,
	}
}
