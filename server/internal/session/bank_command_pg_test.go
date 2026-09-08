package session

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresBankMoneyCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_BANK_MONEY_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(bankCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("bank-money-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, mustJSON(state)); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-money-1", lease, "입금 250냥")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "250") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-money-1", lease, "입금 250냥")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	second, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-money-2", lease, "출금 모두")
	if err != nil || second.Replayed || second.Revision != 2 || !strings.Contains(string(second.Response), "250") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || saved.Players["a"].Body.Gold != 1000 || saved.BankAccounts["a"].Balance != 0 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}

func TestPostgresBankItemCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_BANK_MONEY_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(bankCommandItemFixture())
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("bank-item-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, mustJSON(state)); err != nil {
		t.Fatal(err)
	}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-item-1", lease, "보관물 검")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "검") {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-item-1", lease, "보관물 검")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	second, err := owners.ExecuteBankLine(ctx, store, worldID, "bank-item-2", lease, "받아 검")
	if err != nil || second.Replayed || second.Revision != 2 || !strings.Contains(string(second.Response), "검") {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil || len(saved.BankAccounts["a"].Items.Inventory) != 0 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}
