package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestWebSocketWorldConnectorDispatchesDirectionAfterLogin(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"private-id": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "web-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewGameHandler(ctx, accountStub{}, []string{"https://mud.test"}, connector))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	var output Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Alice", "pw1234"} {
		if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(ctx, conn, &output); err != nil {
			t.Fatal(err)
		}
	}
	if output.Closed || !strings.Contains(output.Text, "출발지") {
		t.Fatalf("login did not open world: %+v", output)
	}
	if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: "8"}); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	if output.Closed || !strings.Contains(output.Text, "도착지") {
		t.Fatalf("direction did not reach world connector: %+v", output)
	}
	conn.CloseNow()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		_, commits := store.snapshot()
		if len(connector.PendingCleanup()) == 0 && commits >= 3 {
			break
		}
		select {
		case <-deadline.C:
			_, commits := store.snapshot()
			t.Fatalf("socket cleanup incomplete: pending=%d commits=%d", len(connector.PendingCleanup()), commits)
		case <-time.After(10 * time.Millisecond):
		}
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || saved.Players["private-id"].Online || saved.Players["private-id"].Body.RoomID != 2 || len(saved.Rooms[2].PlayerIDs) != 0 {
		t.Fatalf("web world state not departed: %+v %v", saved, err)
	}
}
