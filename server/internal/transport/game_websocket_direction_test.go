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

func loginWorldConnectorSocket(t *testing.T, ctx context.Context, connector *WorldConnector) (*websocket.Conn, Output) {
	t.Helper()
	server := httptest.NewServer(NewGameHandler(ctx, accountStub{}, []string{"https://mud.test"}, connector))
	t.Cleanup(server.Close)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
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
	return conn, output
}

func submitWorldConnectorLine(t *testing.T, ctx context.Context, conn *websocket.Conn, line, want string) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
		t.Fatal(err)
	}
	var output Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	if output.Closed || !strings.Contains(output.Text, want) {
		t.Fatalf("line %q did not change room to %q: %+v", line, want, output)
	}
}

func TestWebSocketWorldConnectorMovesWithKoreanNorthAndGoAfterLogin(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "출발지",
					Exits: []world.LegacyExit{
						{Name: "북", Destination: 2},
						{Name: "동굴", Destination: 3},
					},
				}},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 2, Name: "북쪽방",
					Exits: []world.LegacyExit{{Name: "남", Destination: 1}},
				}},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
			3: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 3, Name: "동굴방",
					Exits: []world.LegacyExit{{Name: "광장", Destination: 1}},
				}},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"private-id": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "live-move", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := loginWorldConnectorSocket(t, ctx, connector)
	submitWorldConnectorLine(t, ctx, conn, "북", "북쪽방")
	submitWorldConnectorLine(t, ctx, conn, "남", "출발지")
	submitWorldConnectorLine(t, ctx, conn, "가 동굴", "동굴방")
	conn.CloseNow()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		stateRaw, commits := store.snapshot()
		saved, decodeErr := world.DecodeState(stateRaw)
		if decodeErr == nil && len(connector.PendingCleanup()) == 0 && commits >= 5 && !saved.Players["private-id"].Online {
			if saved.Players["private-id"].Body.RoomID != 3 {
				t.Fatalf("logged-in 북/가 did not persist cave room: %+v", saved.Players["private-id"])
			}
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("socket cleanup incomplete: pending=%d commits=%d err=%v", len(connector.PendingCleanup()), commits, decodeErr)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
