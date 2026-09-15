package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresDivorceRequestAcceptPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIVORCE_TEST_DATABASE_URL")
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
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	raw := divorceSessionFixture(t)
	worldID := fmt.Sprintf("divorce-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	var owners Ownership
	aliceLease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(aliceLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	bobLease, err := owners.Acquire("bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(bobLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	request, err := owners.ExecuteDivorceLine(ctx, store, worldID, "divorce-request-1", aliceLease, "이혼")
	if err != nil || request.Replayed || request.Revision != 1 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	var requested world.DivorceResult
	if err := json.Unmarshal(request.Response, &requested); err != nil {
		t.Fatal(err)
	}
	if requested.Action != world.DivorceRequest || !strings.Contains(requested.Response, "이혼신청") {
		t.Fatalf("request result=%+v", requested)
	}

	accept, err := owners.ExecuteDivorceLine(ctx, store, worldID, "divorce-accept-1", bobLease, "이혼")
	if err != nil || accept.Replayed || accept.Revision != request.Revision+1 {
		t.Fatalf("accept=%+v err=%v", accept, err)
	}
	var accepted world.DivorceResult
	if err := json.Unmarshal(accept.Response, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Action != world.DivorceAccept || !accepted.Broadcast || !strings.Contains(accepted.BroadcastText, "이혼을 하였습니다") {
		t.Fatalf("accepted result=%+v", accepted)
	}

	replay, err := owners.ExecuteDivorceLine(ctx, store, worldID, "divorce-accept-1", bobLease, "이혼")
	if err != nil || !replay.Replayed || replay.Revision != accept.Revision || string(replay.Response) != string(accept.Response) {
		t.Fatalf("accept replay=%+v err=%v", replay, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	for id, key := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		body := saved.Players[id].Body
		if world.PlayerFlagSet(body, world.MarriageActiveFlag) || world.PlayerFlagSet(body, world.MarriagePendingFlag) || world.PlayerFlagSet(body, world.MarriageDivorcePendingFlag) || body.Keys[world.MarriageSpouseKeyIndex] != key {
			t.Fatalf("saved divorce state id=%s body=%+v", id, body)
		}
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipt count=%d", receipts)
	}
}

func TestPostgresMarriageSendPersistsRenderedEventAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIVORCE_TEST_DATABASE_URL")
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
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("marriage-send-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, divorceSessionFixture(t)); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteMarriageSendLine(ctx, store, worldID, "marriage-send-1", lease, "hello 사랑말")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.MarriageSendResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "bob" || result.Message != "hello" || !result.Delivered || result.Event == nil || !strings.Contains(result.Event.Text, "Alice님이 당신에게") {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteMarriageSendLine(ctx, store, worldID, "marriage-send-1", lease, "hello 사랑말")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt count=%d", receipts)
	}
}
