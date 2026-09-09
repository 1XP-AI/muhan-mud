package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func serviceCommandPG(t *testing.T) (*sql.DB, *storage.Postgres, context.Context) {
	t.Helper()
	dsn := os.Getenv("MUHAN_SERVICE_COMMAND_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return db, store, ctx
}

func serviceCommandLease(t *testing.T, owners *session.Ownership, actorID string) session.SessionLease {
	t.Helper()
	lease, err := owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return lease
}

func serviceRoomState(id int16, name string, flags [8]byte, players ...string) world.RoomState {
	return world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: id, Name: name, Flags: flags}},
		PlayerIDs: players,
	}
}

func TestPostgresValueCommandPersistsReadOnlyReceiptAndReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	var flags [8]byte
	flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: serviceRoomState(200, "전당포", flags, "a")},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검", Value: 300001}}}, Inventory: []string{"sword"}}}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-value-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "a")
	first, err := owners.ExecuteValueLine(ctx, store, worldID, "value-pg-1", lease, "가격 검")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteValueLine(ctx, store, worldID, "value-pg-1", lease, "가격 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].Items.Items["sword"].Object.Value != 300001 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestPostgresRepairCommandPersistsMutationAndReplaysWithoutReroll(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	var flags [8]byte
	flags[world.RepairRoomFlag/8] |= 1 << (world.RepairRoomFlag % 8)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: serviceRoomState(200, "수리점", flags, "a")},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검", Type: 4, Value: 100, ShotsMax: 30}}}, Inventory: []string{"sword"}}}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-repair-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "a")
	roll := func(low, high int) int {
		if low == 1 && high == 100 {
			return 100
		}
		return high
	}
	first, err := owners.ExecuteRepairLineWithOptions(ctx, store, worldID, "repair-pg-1", lease, "수리 검", session.RepairOptions{Roll: roll})
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteRepairLineWithOptions(ctx, store, worldID, "repair-pg-1", lease, "수리 검", session.RepairOptions{Roll: func(int, int) int { t.Fatal("repair replay rerolled"); return 0 }})
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].Body.Gold != 75 || saved.Players["a"].Items.Items["sword"].Object.ShotsCurrent != 27 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestPostgresDirectMessagePersistsEventReceiptAndReplays(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{200: serviceRoomState(200, "광장", [8]byte{}, "a", "b")},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 200}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-message-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "a")
	first, err := owners.ExecuteDirectMessageLine(ctx, store, worldID, "message-pg-1", lease, "얘기 Bob 안녕")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.DirectMessageResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Event == nil || !strings.Contains(result.Event.Text, "안녕") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteDirectMessageLine(ctx, store, worldID, "message-pg-1", lease, "얘기 Bob 안녕")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}
