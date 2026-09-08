package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type npcMovementSpawnCatalog struct{ movementSpawnCatalog }

func (npcMovementSpawnCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{Name: "same", Type: 1}, nil
}

func TestPostgresNPCMovementIdentityAndReplay(t *testing.T) {
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
	store := storage.NewPostgres(db)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{
		1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, NPCIDs: []string{"n1", "n2"}},
		2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}, PermanentMonsters: [10]world.LegacyTimer{{Misc: 3}}}},
	}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, HPCurrent: 30}, Online: true}}, NPCs: map[string]world.NPCState{
		"n1": {Body: world.LegacyMonster{Name: "same", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: -1}}},
		"n2": {Body: world.LegacyMonster{Name: "same", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
	}}
	s.ActiveNPCIDs = []string{"n2", "n1"}
	initial, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateWorld(ctx, "npc-move", initial); err != nil {
		t.Fatal(err)
	}
	calls := 0
	allocations := 0
	reduce := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		calls++
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		dest := state.Rooms[2].Resource
		next, result, err := state.DirectionalTransferWithIDs(world.TransferInput{ActorID: "a", Movement: world.MovementInput{Prefix: "북", Destination: &dest, Traversal: world.TraversalInput{PassageOptions: world.PassageOptions{Fighting: true}}}}, npcMovementSpawnCatalog{}, func(lo, hi int) int { return lo }, func() (string, error) { allocations++; return "spawned", nil })
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return body, response, err
	}
	request := json.RawMessage(`{"actor":"a","direction":"북"}`)
	first, err := Execute(ctx, store, "npc-move", "move-1", request, reduce)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted := storage.NewPostgres(db2)
	again, err := Execute(ctx, restarted, "npc-move", "move-1", request, reduce)
	if err != nil || !again.Replayed || calls != 1 || allocations != 1 || string(first.Response) != string(again.Response) {
		t.Fatalf("replay %+v %v calls%d", again, err, calls)
	}
	saved, err := restarted.LoadWorld(ctx, "npc-move")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || len(got.NPCs) != 3 || len(got.Rooms[1].Resource.Monsters) != 0 || len(got.Rooms[1].NPCIDs) != 2 || got.Rooms[1].NPCIDs[0] != "n1" || got.Rooms[1].NPCIDs[1] != "n2" || got.NPCs["n1"].Enemies[0].Damage != -1 || got.NPCs["n2"].Enemies == nil {
		t.Fatalf("lost NPC state %+v %v", got, err)
	}
	if len(got.Rooms[2].NPCIDs) != 1 || got.Rooms[2].NPCIDs[0] != "spawned" || got.NPCs["spawned"].Body.RoomID != 2 || got.NPCs["spawned"].Enemies == nil || len(got.Rooms[2].Resource.Monsters) != 0 {
		t.Fatal("spawn identity not committed with movement")
	}
	writer, _, err := StartWorld(ctx, restarted, "npc-move", "npc-boot")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ActiveNPCIDs, []string{"spawned"}) {
		t.Fatalf("movement active order %v", got.ActiveNPCIDs)
	}
	recovered, err := writer.LoadWorld(ctx, "npc-move")
	if err != nil {
		t.Fatal(err)
	}
	offline, err := world.DecodeState(recovered.State)
	if err != nil || offline.ActiveNPCIDs == nil || len(offline.ActiveNPCIDs) != 0 {
		t.Fatalf("recovery active list %+v %v", offline.ActiveNPCIDs, err)
	}
	loginCalls := 0
	enter := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		loginCalls++
		state, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		next, result, err := state.EnterSavedPlayerWithIDs("a", world.SceneOptions{}, npcMovementSpawnCatalog{}, 1, nil, func() (string, error) { t.Fatal("existing NPC reallocated on login"); return "", nil })
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return body, response, err
	}
	loginRequest := json.RawMessage(`{"kind":"login","actor":"a"}`)
	if _, err = Execute(ctx, writer, "npc-move", "login-1", loginRequest, enter); err != nil {
		t.Fatal(err)
	}
	loginReplay, err := Execute(ctx, writer, "npc-move", "login-1", loginRequest, enter)
	if err != nil || !loginReplay.Replayed || loginCalls != 1 {
		t.Fatalf("login replay %v", err)
	}
	final, err := writer.LoadWorld(ctx, "npc-move")
	if err != nil {
		t.Fatal(err)
	}
	online, err := world.DecodeState(final.State)
	if !reflect.DeepEqual(online.ActiveNPCIDs, []string{"spawned"}) {
		t.Fatalf("login active order %v", online.ActiveNPCIDs)
	}
	if err != nil || final.Revision != 3 || !online.Players["a"].Online || online.Players["a"].Body.RoomID != 2 || len(online.NPCs) != 3 || len(online.Rooms[2].NPCIDs) != 1 || online.Rooms[2].NPCIDs[0] != "spawned" {
		t.Fatalf("NPC login state lost %v", err)
	}
}
