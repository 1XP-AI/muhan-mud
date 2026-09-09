package session

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

// TestPostgresBoundedLanesPersistAndReplay keeps the three persistence-aware
// command lanes in one disposable database batch. It is intentionally opt-in:
// local feature lanes use the in-memory receipt tests, while this one check is
// run at the integration boundary with MUHAN_BOUNDED_LANES_TEST_DATABASE_URL.
func TestPostgresBoundedLanesPersistAndReplay(t *testing.T) {
	dsn := os.Getenv("MUHAN_BOUNDED_LANES_TEST_DATABASE_URL")
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
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		state     func(*testing.T) []byte
		commandID string
		line      string
		execute   func(context.Context, *Ownership, *storage.Postgres, string, string, SessionLease, string) (storage.WorldReceipt, error)
		verify    func(*testing.T, world.State)
	}{
		{
			name:      "alias",
			state:     aliasCommandState,
			commandID: "bounded-alias-1",
			line:      "줄임말 two 둘째",
			execute: func(ctx context.Context, owners *Ownership, store *storage.Postgres, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
				return owners.ExecuteAliasLine(ctx, store, worldID, commandID, lease, line)
			},
			verify: func(t *testing.T, state world.State) {
				aliases := state.Players["a"].Aliases
				if len(aliases) != 2 || aliases[1].Alias != "two" || aliases[1].Process != "둘째" {
					t.Fatalf("aliases=%+v", aliases)
				}
			},
		},
		{
			name:      "burn",
			state:     burnCommandFixture,
			commandID: "bounded-burn-1",
			line:      "태워 동전",
			execute: func(ctx context.Context, owners *Ownership, store *storage.Postgres, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
				return owners.ExecuteBurnLineWithOptions(ctx, store, worldID, commandID, lease, line, BurnOptions{Now: 8, Roll: func(int, int) int { return 1 }})
			},
			verify: func(t *testing.T, state world.State) {
				actor := state.Players["actor"]
				if actor.Body.Gold != 100014 || actor.Body.Experience != 31 || actor.Items == nil || len(actor.Items.Items) != 0 {
					t.Fatalf("burn actor=%+v", actor)
				}
			},
		},
		{
			name:      "study",
			state:     studyCommandFixture,
			commandID: "bounded-study-1",
			line:      "배워 두루마리",
			execute: func(ctx context.Context, owners *Ownership, store *storage.Postgres, worldID, commandID string, lease SessionLease, line string) (storage.WorldReceipt, error) {
				return owners.ExecuteStudyLine(ctx, store, worldID, commandID, lease, line)
			},
			verify: func(t *testing.T, state world.State) {
				actor := state.Players["a"]
				if actor.Items == nil || len(actor.Items.Items) != 0 || actor.Body.Spells[0]&1 == 0 {
					t.Fatalf("study actor=%+v", actor)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.state(t)
			state, err := world.DecodeState(raw)
			if err != nil {
				t.Fatal(err)
			}
			worldID := fmt.Sprintf("bounded-%s-%d", tc.name, time.Now().UnixNano())
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CreateWorld(ctx, worldID, encoded); err != nil {
				t.Fatal(err)
			}
			actorID := "a"
			switch tc.name {
			case "burn":
				actorID = "actor"
			case "study":
				actorID = "a"
			}
			var owners Ownership
			lease, err := owners.Acquire(actorID)
			if err != nil {
				t.Fatal(err)
			}
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			first, err := tc.execute(ctx, &owners, store, worldID, tc.commandID, lease, tc.line)
			if err != nil || first.Replayed || first.Revision != 1 {
				t.Fatalf("first=%+v err=%v", first, err)
			}
			replay, err := tc.execute(ctx, &owners, store, worldID, tc.commandID, lease, tc.line)
			if err != nil || !replay.Replayed || replay.Revision != first.Revision || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			snapshot, err := store.LoadWorld(ctx, worldID)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := world.DecodeState(snapshot.State)
			if err != nil {
				t.Fatal(err)
			}
			tc.verify(t, saved)
		})
	}
}
