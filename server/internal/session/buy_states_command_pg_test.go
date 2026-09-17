package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresBuyStatesCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_BUY_STATES_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required; set MUHAN_BUY_STATES_TEST_DATABASE_URL")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	root := storage.NewPostgres(db)
	if err := root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	const actorID = "caretaker"
	worldID := fmt.Sprintf("buy-states-%d", time.Now().UTC().UnixNano())
	bootID := "buy-states-boot-" + worldID
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			actorID: {
				Body: world.LegacyMonster{
					Name:       "초인",
					RoomID:     1,
					Class:      world.BuyStatesCaretakerClass,
					Level:      1,
					Experience: 102000000,
					Gold:       5000000,
					HPMax:      100,
					HPCurrent:  42,
					MPMax:      80,
					MPCurrent:  21,
					Stats:      [5]byte{10, 10, 10, 10, 10},
				},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	if err := initial.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}

	writer, startup, err := engine.StartWorld(ctx, root, worldID, bootID)
	if err != nil {
		t.Fatal(err)
	}
	if writer == nil || startup.Replayed || startup.Revision != 1 {
		t.Fatalf("unexpected startup receipt: %+v", startup)
	}

	var owners Ownership
	lease, err := owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := owners.EnterWorld(ctx, writer, worldID, "buy-states-enter-"+worldID, lease, 100, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Replayed || entry.Revision != 2 {
		t.Fatalf("unexpected world admission receipt: %+v", entry)
	}

	const commandID = "buy-states-apply"
	draws := 0
	first, err := owners.ExecuteBuyStatesLineWithOptions(ctx, writer, worldID, commandID, lease, "체력 향상", BuyStatesOptions{
		Roll: func(low, high int) int {
			draws++
			if low != 0 || high != 3 {
				t.Fatalf("vitality roll bounds %d..%d", low, high)
			}
			if draws > 2 {
				t.Fatalf("unexpected extra vitality draw %d", draws)
			}
			return []int{1, 3}[draws-1]
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || first.Revision != 3 || draws != 2 {
		t.Fatalf("unexpected first receipt=%+v draws=%d", first, draws)
	}
	var result world.BuyStatesResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.BuyStatesApplied || !result.Changed || result.Stat != "체력" || result.Amount != 2 || result.Cost != 2000000 || result.Gain != 9 || result.ExperienceBefore != 102000000 || result.ExperienceAfter != 100000000 || result.GoldBefore != 5000000 || result.GoldAfter != 3000000 || result.HPMaxBefore != 100 || result.HPMaxAfter != 109 || result.HPCurrentBefore != 42 || result.HPCurrentAfter != 109 || result.MPMaxBefore != 80 || result.MPMaxAfter != 80 || result.MPCurrentBefore != 21 || result.MPCurrentAfter != 21 {
		t.Fatalf("unexpected buy-states result: %+v", result)
	}
	if len(result.Rolls) != 2 || len(result.GrowthRolls) != 2 || result.Rolls[0] != 1 || result.Rolls[1] != 3 || result.Rolls[0] != result.GrowthRolls[0] || result.Rolls[1] != result.GrowthRolls[1] {
		t.Fatalf("unexpected roll evidence: %+v", result)
	}

	firstSnapshot, err := writer.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if firstSnapshot.Revision != first.Revision {
		t.Fatalf("durable revision=%d receipt revision=%d", firstSnapshot.Revision, first.Revision)
	}
	saved, err := world.DecodeState(firstSnapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players[actorID]
	if !actor.Online || actor.Body.Experience != 100000000 || actor.Body.Gold != 3000000 || actor.Body.HPMax != 109 || actor.Body.HPCurrent != 109 || actor.Body.MPMax != 80 || actor.Body.MPCurrent != 21 || actor.Body.Stats != [5]byte{10, 10, 10, 10, 10} || actor.Body.DiceCount != 4 || actor.Body.DiceSides != 4 || actor.Body.DicePlus != 4 {
		t.Fatalf("durable caretaker transition: %+v", actor.Body)
	}
	if actor.Items == nil || len(actor.Items.Items) != 0 || saved.Rooms[1].Items == nil || len(saved.Rooms[1].Items.Items) != 0 || len(saved.Rooms[1].PlayerIDs) != 1 || saved.Rooms[1].PlayerIDs[0] != actorID {
		t.Fatalf("canonical room/item state changed unexpectedly: %+v", saved.Rooms[1])
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	if err := db2.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	root2 := storage.NewPostgres(db2)
	restarted, startupReplay, err := engine.StartWorld(ctx, root2, worldID, bootID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted == nil || !startupReplay.Replayed || startupReplay.Revision != startup.Revision {
		t.Fatalf("unexpected same-boot startup replay: %+v", startupReplay)
	}

	var replayOwners Ownership
	replayLease, err := replayOwners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := replayOwners.Admit(replayLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	replay, err := replayOwners.ExecuteBuyStatesLineWithOptions(ctx, restarted, worldID, commandID, replayLease, "체력 향상", BuyStatesOptions{
		Roll: func(int, int) int {
			t.Fatal("buy_states replay consumed a random draw")
			return 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Revision != first.Revision || !bytes.Equal(replay.Response, first.Response) || draws != 2 {
		t.Fatalf("replay changed receipt or draw count: replay=%+v first=%+v draws=%d", replay, first, draws)
	}
	afterReplay, err := restarted.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	if afterReplay.Revision != firstSnapshot.Revision || !bytes.Equal(afterReplay.State, firstSnapshot.State) {
		t.Fatalf("replay changed durable world: before revision=%d after revision=%d", firstSnapshot.Revision, afterReplay.Revision)
	}
}
