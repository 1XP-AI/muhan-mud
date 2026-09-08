package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	_ "github.com/jackc/pgx/v5/stdlib"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Sequential-connection integration adapter, not the production retry supervisor.
type pgGameFixture struct {
	owners session.Ownership
	store  *storage.Postgres
	lease  session.SessionLease
	done   chan error
}

func (g *pgGameFixture) Open(ctx context.Context, c storage.Character) (GameConnection, string, error) {
	if c.ID == "" {
		return nil, "", fmt.Errorf("unexpected verified identity")
	}
	lease, err := g.owners.Acquire(c.ID)
	if err != nil {
		return nil, "", err
	}
	g.lease = lease
	r, err := g.owners.EnterWorld(ctx, g.store, "socket-world", fmt.Sprintf("socket-enter-%d", lease.Generation), lease, 100, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		return g, "", err
	}
	var entry world.RoomEntry
	err = json.Unmarshal(r.Response, &entry)
	return g, entry.Scene, err
}
func (g *pgGameFixture) Submit(ctx context.Context, line string) (string, error) {
	r, err := g.owners.ExecuteLookLine(ctx, g.store, "socket-world", fmt.Sprintf("socket-look-%d", g.lease.Generation), g.lease, line, 12)
	if err != nil {
		return "", err
	}
	var text string
	err = json.Unmarshal(r.Response, &text)
	return text, err
}
func (g *pgGameFixture) Close(ctx context.Context) {
	_, err := g.owners.Depart(ctx, g.store, "socket-world", fmt.Sprintf("socket-exit-%d", g.lease.Generation), g.lease)
	g.done <- err
}

func TestWebSocketPostgresWorldAdmissionLookDeparture(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCKET_WORLD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pg := storage.NewPostgres(db)
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{"floor": {Object: world.LegacyObject{Name: "검"}}}, Inventory: []string{"floor"}}}}, Players: map[string]world.PlayerState{"private-id": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.CreateWorld(ctx, "socket-world", raw); err != nil {
		t.Fatal(err)
	}
	game := &pgGameFixture{store: pg, done: make(chan error, 1)}
	server := httptest.NewServer(NewGameHandler(ctx, accountStub{}, []string{"https://mud.test"}, game))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var output Output
	if err := wsjson.Read(ctx, conn, &output); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"Alice", "pw1234", "봐"} {
		if err := wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
			t.Fatal(err)
		}
		if err := wsjson.Read(ctx, conn, &output); err != nil {
			t.Fatal(err)
		}
		if line != "Alice" && (output.Closed || output.Secret || !strings.Contains(output.Text, "광장") || !strings.Contains(output.Text, "검")) {
			t.Fatalf("%+v", output)
		}
	}
	conn.CloseNow()
	select {
	case err := <-game.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("departure timed out")
	}
	saved, err := pg.LoadWorld(ctx, "socket-world")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 3 || got.Players["private-id"].Online || got.Players["private-id"].Body.HPCurrent != 30 || len(got.Rooms[1].PlayerIDs) != 0 || got.Rooms[1].Resource.BeenHere != 1 || len(got.Rooms[1].Items.Items) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if game.owners.Owns(game.lease) {
		t.Fatal("departure lease retained")
	}
}
