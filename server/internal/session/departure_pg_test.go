package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"os"
	"testing"
	"time"
)

// Drop the successful commit response AND its immediate receipt recovery read.
type lostDepartureReply struct {
	*storage.Postgres
	hide    bool
	commits int
}

func (s *lostDepartureReply) ReadWorldReceipt(ctx context.Context, w, c string, r json.RawMessage) (storage.WorldReceipt, error) {
	if s.hide {
		s.hide = false
		return storage.WorldReceipt{}, errors.New("injected receipt read unavailable")
	}
	return s.Postgres.ReadWorldReceipt(ctx, w, c, r)
}
func (s *lostDepartureReply) CommitWorldCommand(ctx context.Context, w, c string, r json.RawMessage, v int64, state, response json.RawMessage) (storage.WorldReceipt, error) {
	s.commits++
	_, err := s.Postgres.CommitWorldCommand(ctx, w, c, r, v, state, response)
	if err != nil {
		return storage.WorldReceipt{}, err
	}
	s.hide = true
	return storage.WorldReceipt{}, errors.New("injected lost commit response")
}

func TestPostgresDepartureRecoversLostReplyBeforeRelease(t *testing.T) {
	dsn := os.Getenv("MUHAN_DEPARTURE_TEST_DATABASE_URL")
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
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30}, Online: true}}}
	raw, _ := json.Marshal(s)
	if err := pg.CreateWorld(ctx, "departure", raw); err != nil {
		t.Fatal(err)
	}
	store := &lostDepartureReply{Postgres: pg}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	first, err := owners.Depart(ctx, store, "departure", "boot-1-exit-1", lease)
	if err == nil || len(first.Response) != 0 || !owners.Owns(lease) {
		t.Fatal("uncertain departure released")
	}
	if _, err := owners.Acquire("a"); err == nil {
		t.Fatal("admitted while unresolved")
	}
	saved, err := pg.LoadWorld(ctx, "departure")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 1 || got.Players["a"].Online || got.Players["a"].Body.HPCurrent != 30 || len(got.Rooms[1].PlayerIDs) != 0 {
		t.Fatal("departure not durably committed")
	}
	retry, err := owners.Depart(ctx, store, "departure", "boot-1-exit-1", lease)
	if err != nil || !retry.Replayed || retry.Revision != 1 || owners.Owns(lease) || store.commits != 1 {
		t.Fatalf("%+v %v commits%d", retry, err, store.commits)
	}
	if _, err := owners.Acquire("a"); err != nil {
		t.Fatal(err)
	}
}
