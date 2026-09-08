package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Observe socket cleanup completion without replacing any game behavior.
type observedConnector struct {
	GameConnector
	done chan error
}
type observedConnection struct {
	GameConnection
	done chan error
}

func (g *observedConnector) Open(ctx context.Context, c storage.Character) (GameConnection, string, error) {
	connection, scene, err := g.GameConnector.Open(ctx, c)
	if connection == nil {
		return nil, scene, err
	}
	return &observedConnection{connection, g.done}, scene, err
}
func (c *observedConnection) Close(ctx context.Context) { c.GameConnection.Close(ctx); c.done <- nil }

func TestWebSocketPostgresRegistrationAndRelogin(t *testing.T) {
	dsn := os.Getenv("MUHAN_SOCKET_REGISTRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg := storage.NewPostgres(db)
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	initial := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{}}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err = pg.CreateWorld(ctx, "socket-world", raw); err != nil {
		t.Fatal(err)
	}
	writer, _, err := engine.StartWorld(ctx, pg, "socket-world", "test-boot")
	if err != nil {
		t.Fatal(err)
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: writer, WorldID: "socket-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	game := &observedConnector{GameConnector: connector, done: make(chan error, 1)}
	server := httptest.NewServer(NewGameHandler(ctx, session.NewWorldAccounts(pg, writer, "socket-world"), []string{"https://mud.test"}, game))
	defer server.Close()
	conversations := [][]string{{"Alice", "예", "", "남", "4", "12 10 12 10 10", "1", "선", "7", "pw1234", "봐"}, {"ALICE", "pw1234", "봐"}}
	var characterID string
	for round, lines := range conversations {
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://mud.test"}}})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		var output Output
		if err = wsjson.Read(ctx, conn, &output); err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			if line == "pw1234" && !output.Secret {
				t.Fatal("password prompt not secret")
			}
			if err = wsjson.Write(ctx, conn, Input{Type: "line", Text: line}); err != nil {
				t.Fatal(err)
			}
			if err = wsjson.Read(ctx, conn, &output); err != nil {
				t.Fatal(err)
			}
			if output.Closed || strings.Contains(output.Text, "pw1234") {
				t.Fatalf("unexpected terminal output %+v", output)
			}
			if (line == "pw1234" || line == "봐") && (output.Secret || !strings.Contains(output.Text, "광장")) {
				t.Fatalf("no game scene %+v", output)
			}
		}
		if round == 1 {
			lock, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			var revision int64
			if err = lock.QueryRowContext(ctx, `SELECT revision FROM mud_go.worlds WHERE id='socket-world' FOR UPDATE`).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			short, stop := context.WithTimeout(ctx, 50*time.Millisecond)
			err = connector.Shutdown(short)
			stop()
			if err == nil || len(connector.PendingCleanup()) != 1 {
				t.Fatal("timed out shutdown discarded session")
			}
			if connection, _, err := connector.Open(ctx, storage.Character{ID: characterID}); err == nil || connection != nil {
				t.Fatal("incomplete shutdown accepted admission")
			}
			if err = lock.Rollback(); err != nil {
				t.Fatal(err)
			}
			if err := connector.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if connection, _, err := connector.Open(ctx, storage.Character{ID: characterID}); err == nil || connection != nil {
				t.Fatal("shutdown accepted new admission")
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
		verified, err := pg.Authenticate(ctx, "Alice", []byte("pw1234"))
		if err != nil {
			t.Fatal(err)
		}
		if len(connector.PendingCleanup()) != 0 {
			t.Fatal("socket cleanup remains pending")
		}
		if round == 0 {
			characterID = verified.ID
		}
		if verified.ID != characterID {
			t.Fatal("relogin changed identity")
		}
		saved, err := pg.LoadWorld(ctx, "socket-world")
		if err != nil {
			t.Fatal(err)
		}
		state, err := world.DecodeState(saved.State)
		if err != nil {
			t.Fatal(err)
		}
		p := state.Players[characterID]
		if saved.Revision != int64(5+round*3) || len(state.Players) != 1 || p.Online || p.Body.Level != 1 || p.Body.HPCurrent != 56 || p.Body.Gold != 500 || p.Items == nil || len(state.Rooms[1].PlayerIDs) != 0 || state.Rooms[1].Resource.BeenHere != int32(round+1) {
			t.Fatalf("bad saved character %+v revision%d", p, saved.Revision)
		}
	}
	var accounts, characters int
	if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM mud_go.accounts),(SELECT count(*) FROM mud_go.characters)`).Scan(&accounts, &characters); err != nil || accounts != 1 || characters != 1 {
		t.Fatalf("duplicate identities %d %d %v", accounts, characters, err)
	}
}
