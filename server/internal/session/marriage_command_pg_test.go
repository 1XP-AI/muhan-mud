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

func TestPostgresMarriageRequestAcceptPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_MARRIAGE_TEST_DATABASE_URL")
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
	raw := marriageSessionFixture(t)
	if _, err := world.DecodeState(raw); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("marriage-%d", time.Now().UnixNano())
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

	first, err := owners.ExecuteMarriageLine(ctx, store, worldID, "marriage-request-1", aliceLease, "결혼 Bob")
	if err != nil || first.Replayed || first.Revision != 1 {
		t.Fatalf("request=%+v err=%v", first, err)
	}
	var request world.MarriageResult
	if err := json.Unmarshal(first.Response, &request); err != nil {
		t.Fatal(err)
	}
	if request.Action != world.MarriageRequest || !strings.Contains(request.Response, "Bob님에게 결혼을 신청") {
		t.Fatalf("request result=%+v", request)
	}

	accept, err := owners.ExecuteMarriageLine(ctx, store, worldID, "marriage-accept-1", bobLease, "결혼 Alice")
	if err != nil || accept.Replayed || accept.Revision != first.Revision+1 {
		t.Fatalf("accept=%+v err=%v", accept, err)
	}
	var accepted world.MarriageResult
	if err := json.Unmarshal(accept.Response, &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Action != world.MarriageAccept || !accepted.Broadcast || !strings.Contains(accepted.BroadcastText, "결혼을 하였습니다") {
		t.Fatalf("accept result=%+v", accepted)
	}

	replay, err := owners.ExecuteMarriageLine(ctx, store, worldID, "marriage-accept-1", bobLease, "결혼 Alice")
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
	if !world.PlayerFlagSet(saved.Players["alice"].Body, world.MarriageActiveFlag) || !world.PlayerFlagSet(saved.Players["bob"].Body, world.MarriageActiveFlag) || saved.Players["alice"].Body.Keys[world.MarriageSpouseKeyIndex] != "mBob" || saved.Players["bob"].Body.Keys[world.MarriageSpouseKeyIndex] != "mAlice" {
		t.Fatalf("saved marriage state=%+v", saved.Players)
	}
	var receipts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mud_go.world_commands WHERE world_id=$1`, worldID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipt count=%d", receipts)
	}
}
