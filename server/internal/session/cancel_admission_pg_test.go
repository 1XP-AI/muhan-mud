package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresCancelAdmissionResolvesEveryDurableOutcome(t *testing.T) {
	dsn := os.Getenv("MUHAN_DEPARTURE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	pg := storage.NewPostgres(db)
	if err = pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"missing", "offline", "online"} {
		t.Run(mode, func(t *testing.T) {
			state := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}}}, Players: map[string]world.PlayerState{}}
			if mode != "missing" {
				state.Players["a"] = world.PlayerState{Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30}, Online: mode == "online"}
			}
			if mode == "online" {
				room := state.Rooms[1]
				room.PlayerIDs = []string{"a"}
				state.Rooms[1] = room
			}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err = pg.CreateWorld(ctx, mode, raw); err != nil {
				t.Fatal(err)
			}
			var owners Ownership
			lease, err := owners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			store := &lostDepartureReply{Postgres: pg}
			if _, err = owners.CancelWorldAdmission(ctx, store, mode, "cancel", lease); err == nil || !owners.Owns(lease) {
				t.Fatal("unconfirmed cancellation released")
			}
			if owners.Release(lease) {
				t.Fatal("release bypassed pending cancellation")
			}
			if _, err = owners.Acquire("a"); err == nil {
				t.Fatal("duplicate owner")
			}
			receipt, err := owners.CancelWorldAdmission(ctx, store, mode, "cancel", lease)
			if err != nil || !receipt.Replayed || receipt.Revision != 1 || store.commits != 1 || owners.Owns(lease) {
				t.Fatalf("%+v %v", receipt, err)
			}
			snap, err := pg.LoadWorld(ctx, mode)
			if err != nil {
				t.Fatal(err)
			}
			got, err := world.DecodeState(snap.State)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Players) != len(state.Players) || len(got.Rooms[1].PlayerIDs) != 0 || got.Players["a"].Online {
				t.Fatal("bad cancellation state")
			}
			if mode != "missing" && got.Players["a"].Body.HPCurrent != 30 {
				t.Fatal("cancellation changed player")
			}
		})
	}
}
