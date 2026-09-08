package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesWelcomeDocument(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "welcome-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		HelpFS: fstest.MapFS{"welcome": &fstest.MapFile{Data: []byte("무한대전에 오신 것을 환영합니다.")}},
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
	text, err := connection.Submit(context.Background(), "환영")
	if err != nil || !strings.Contains(text, "무한대전에 오신 것을 환영합니다") {
		t.Fatalf("welcome text=%q err=%v", text, err)
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}
