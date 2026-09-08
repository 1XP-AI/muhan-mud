package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"strings"
	"testing"
	"time"
)

type movementSpawnCatalog struct{}

func (movementSpawnCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, fmt.Errorf("unexpected monster")
}
func (movementSpawnCatalog) Object(int16) (world.LegacyObject, error) {
	return world.LegacyObject{Name: "리젠검"}, nil
}

func TestPostgresMovementExecutionAndReplay(t *testing.T) {
	dsn := os.Getenv("MUHAN_ENGINE_TEST_DATABASE_URL")
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
	p := storage.NewPostgres(db)
	if err = p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	initial, err := json.Marshal(world.State{
		Version:     1,
		Invitations: map[int16][]string{7: {"a"}},
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{"source-floor": {Object: world.LegacyObject{Name: "stone"}}}, Inventory: []string{"source-floor"}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Special: 7, Flags: [8]byte{0, 0, 0, 0, 0, 1}}, PermanentObjects: [10]world.LegacyTimer{{Misc: 2}}}, Items: &world.ItemCollection{Items: map[string]world.Item{"floor-box": {Object: world.LegacyObject{Name: "box"}, Contents: []string{"floor-gem"}}, "floor-gem": {}}, Inventory: []string{"floor-box"}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 30, Class: 4, Level: 1, Stats: [5]byte{10, 10}}, Online: true,
			Items: &world.ItemCollection{Items: map[string]world.Item{
				"bag":   {Object: world.LegacyObject{Name: "bag"}, Contents: []string{"coin"}},
				"coin":  {Object: world.LegacyObject{Name: "coin", Value: 1}},
				"sword": {Object: world.LegacyObject{Name: "sword", Wear: 20, Armor: 7, Adjustment: 2}},
			}, Inventory: []string{"bag"}, Ready: [20]string{19: "sword"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.CreateWorld(ctx, "movement", initial); err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"actor":"a","go":"북"}`)
	calls := 0
	allocations := 0
	spawnID := "source-floor" // First attempt deliberately collides with another room.
	reduce := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		calls++
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		actor := s.Players["a"].Body
		sourcePlayers, err := s.RoomPlayers(actor.RoomID)
		if err != nil {
			return nil, nil, err
		}
		destinationPlayers, err := s.RoomPlayers(2)
		if err != nil {
			return nil, nil, err
		}
		destination := s.Rooms[2].Resource
		next, proposal, err := s.TransferWithIDs(world.TransferInput{
			ActorID: "a", SourcePlayers: sourcePlayers,
			Movement: world.MovementInput{Source: s.Rooms[actor.RoomID].Resource, Destination: &destination,
				Occupants: destinationPlayers, Hidden: actor.Flags[0]&2 != 0,
				Prefix: "북", Occurrence: 1, Traversal: world.TraversalInput{HP: int(actor.HPCurrent), Class: actor.Class}, Visitor: world.DestinationVisitor{Class: actor.Class, Level: actor.Level}},
		}, movementSpawnCatalog{}, nil, func() (string, error) { allocations++; return spawnID, nil })
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(proposal)
		return state, response, err
	}
	if _, err := Execute(ctx, p, "movement", "move-1", request, reduce); err == nil {
		t.Fatal("persisted colliding spawn")
	}
	unchanged, err := p.LoadWorld(ctx, "movement")
	if err != nil || unchanged.Revision != 0 {
		t.Fatalf("partial failed move: %+v %v", unchanged, err)
	}
	before, err := world.DecodeState(unchanged.State)
	if err != nil || before.Players["a"].Body.RoomID != 1 || len(before.Rooms[2].Items.Items) != 2 || before.Rooms[1].Resource.Track != "" {
		t.Fatal("failed move changed state")
	}
	// The failed request must not have a successful receipt. Same command ID
	// now succeeds as a NEW reduction with the corrected internal allocator.
	spawnID = "move-1-spawn-1"
	calls, allocations = 0, 0
	r, err := Execute(ctx, p, "movement", "move-1", request, reduce)
	if err != nil || r.Revision != 1 || r.Replayed {
		t.Fatalf("%+v %v", r, err)
	}
	// New store instance reads the committed receipt before any second reduction.
	replay, err := Execute(ctx, storage.NewPostgres(db), "movement", "move-1", request, reduce)
	if err != nil || !replay.Replayed || calls != 1 || allocations != 1 || string(replay.Response) != string(r.Response) || !strings.Contains(string(r.Response), "리젠검") {
		t.Fatalf("%+v %v calls%d", replay, err, calls)
	}
	state, err := p.LoadWorld(ctx, "movement")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(state.State)
	if err == nil && (len(got.Invitations[7]) != 1 || got.Invitations[7][0] != "a") {
		t.Fatal("persisted invitation IDs lost")
	}
	if err != nil || got.Players["a"].Body.RoomID != 2 || state.Revision != 1 || got.Players["a"].Body.HPCurrent != 30 || got.Rooms[1].Resource.Track != "북" || len(got.Rooms[1].PlayerIDs) != 0 || len(got.Rooms[2].PlayerIDs) != 1 || got.Rooms[2].PlayerIDs[0] != "a" || got.Rooms[2].Resource.BeenHere != 1 {
		t.Fatalf("%+v %v", state, err)
	}
	// Simulate loss of the process's connection pool, not an actual DB restart.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted := storage.NewPostgres(db2)
	recoveryCalls := 0
	recoverState := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		recoveryCalls++
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, ids, err := s.RecoverOffline()
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(ids)
		return state, response, err
	}
	restartRequest := json.RawMessage(`{"kind":"cold-start","boot":"fixture-boot"}`)
	recovered, err := Execute(ctx, restarted, "movement", "restart-1", restartRequest, recoverState)
	if err != nil || recovered.Revision != 2 || recovered.Replayed {
		t.Fatalf("%+v %v", recovered, err)
	}
	repeated, err := Execute(ctx, restarted, "movement", "restart-1", restartRequest, recoverState)
	if err != nil || !repeated.Replayed || recoveryCalls != 1 || string(repeated.Response) != string(recovered.Response) {
		t.Fatalf("%+v %v", repeated, err)
	}
	saved, err := restarted.LoadWorld(ctx, "movement")
	if err != nil {
		t.Fatal(err)
	}
	offline, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 2 || offline.Players["a"].Online || offline.Players["a"].Body.RoomID != 2 || offline.Players["a"].Body.HPCurrent != 30 || len(offline.Rooms[2].PlayerIDs) != 0 || offline.Rooms[2].Resource.BeenHere != 1 {
		t.Fatalf("recovery lost gameplay state: %+v %v", offline, err)
	}
	loginCalls := 0
	enter := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		loginCalls++
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, entry, err := s.EnterSavedPlayerWithIDs("a", world.SceneOptions{}, movementSpawnCatalog{}, 1, nil, func() (string, error) { t.Fatal("existing spawn allocated again"); return "", nil })
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(entry.Scene)
		return state, response, err
	}
	loginRequest := json.RawMessage(`{"kind":"enter","actor":"a","session":"fixture-session"}`)
	joined, err := Execute(ctx, restarted, "movement", "login-1", loginRequest, enter)
	if err != nil || joined.Revision != 3 || joined.Replayed {
		t.Fatalf("%+v %v", joined, err)
	}
	joinedAgain, err := Execute(ctx, restarted, "movement", "login-1", loginRequest, enter)
	if err != nil || !joinedAgain.Replayed || loginCalls != 1 {
		t.Fatalf("%+v %v", joinedAgain, err)
	}
	final, err := restarted.LoadWorld(ctx, "movement")
	if err != nil {
		t.Fatal(err)
	}
	online, err := world.DecodeState(final.State)
	// An invitation grants movement entry, not residence on login: the legacy
	// login path sends non-spouses to room 1 even when invited to the property.
	if err != nil || final.Revision != 3 || !online.Players["a"].Online || online.Players["a"].Body.RoomID != 1 || len(online.Rooms[1].PlayerIDs) != 1 || len(online.Rooms[2].PlayerIDs) != 0 || online.Rooms[2].Resource.BeenHere != 1 || len(online.Invitations[7]) != 1 || online.Invitations[7][0] != "a" {
		t.Fatalf("%+v %v", online, err)
	}
	items := online.Players["a"].Items
	floor := online.Rooms[2].Items
	if floor == nil || len(floor.Items) != 3 || len(floor.Inventory) != 2 || floor.Items["floor-box"].Contents[0] != "floor-gem" || floor.Items["move-1-spawn-1"].Object.Name != "리젠검" || len(online.Rooms[2].Resource.Objects) != 0 || len(online.Rooms[1].Items.Items) != 1 || allocations != 1 {
		t.Fatalf("floor identity lost or duplicated: %+v", floor)
	}
	if int8(online.Players["a"].Body.Armor) != 93 || int8(online.Players["a"].Body.Thaco) != 18 {
		t.Fatal("equipment stats not persisted")
	}
	if items == nil || len(items.Items) != 3 || items.Ready[19] != "sword" || len(items.Inventory) != 1 || items.Inventory[0] != "bag" || len(items.Items["bag"].Contents) != 1 || items.Items["bag"].Contents[0] != "coin" {
		t.Fatalf("item identity lost: %+v", items)
	}
}
