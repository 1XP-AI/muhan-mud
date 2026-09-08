package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestWorldConnectorPlayerVitalPhasePostgresPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_VITAL_PHASE_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("vital-phase-%d", time.Now().UnixNano())
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 10, MPMax: 80, MPCurrent: 2}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: pg, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, err := connector.RunPlayerVitalPhase(ctx, "vitals-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v", first)
	}
	saved, err := pg.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || got.Players["a"].Body.HPCurrent <= 10 {
		t.Fatalf("vital phase not persisted: %+v %v", got.Players["a"], err)
	}
	replay, err := connector.RunPlayerVitalPhase(ctx, "vitals-1")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
}

func TestWorldConnectorPlayerVitalTickReplaysAcrossConnectorRestart(t *testing.T) {
	dsn := os.Getenv("MUHAN_VITAL_PHASE_TEST_DATABASE_URL")
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
	worldID := fmt.Sprintf("vital-tick-%d", time.Now().UnixNano())
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 10, MPMax: 80, MPCurrent: 2}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	clock := func() (int32, int) { return 123, 12 }
	firstConnector, err := NewWorldConnector(WorldConnectorConfig{Store: pg, WorldID: worldID, Clock: clock, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := firstConnector.RunPlayerVitalTick(ctx, 20*time.Second)
	if err != nil || !ran || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	restartedConnector, err := NewWorldConnector(WorldConnectorConfig{Store: pg, WorldID: worldID, Clock: clock, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := restartedConnector.RunPlayerVitalTick(ctx, 20*time.Second)
	if err != nil || !ran || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v ran=%v err=%v", replay, ran, err)
	}
	snapshot, err := pg.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
