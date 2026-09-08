package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresDeathExecutionAndReplay(t *testing.T) {
	testPostgresDeath(t, false, false, false)
}

func TestPostgresRepeatedVitalsDeathAndReplay(t *testing.T) {
	testPostgresDeath(t, true, false, false)
}

func TestPostgresPlayerUpdateAndReplay(t *testing.T) {
	testPostgresDeath(t, true, true, false)
}

func TestPostgresDirectionalFallDeathAndReplay(t *testing.T) {
	testPostgresDeath(t, false, false, true)
}

func testPostgresDeath(t *testing.T, vitals, wholeUpdate, directional bool) {
	worldID := "death"
	wantHP, wantVisits, wantRolls := int16(100), int32(1), 1
	if vitals {
		worldID = "vitals-death"
		wantHP = 5
		wantVisits = 2
		wantRolls = 4
	}
	if wholeUpdate {
		worldID = "player-update"
	}
	if directional {
		worldID = "directional-death"
		wantRolls = 3
	}
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
	empty := func() *world.ItemCollection { return &world.ItemCollection{Items: map[string]world.Item{}} }
	s := world.State{Version: 1, War: &world.FamilyWar{}, Rooms: map[int16]world.RoomState{
		1:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, Items: empty(), PlayerIDs: []string{"a"}},
		1008: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1008}}, Items: empty()},
	}, Players: map[string]world.PlayerState{"a": {
		Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Level: 1, Class: 4, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, MPMax: 80, MPCurrent: 2}, Online: true,
		Items: &world.ItemCollection{Items: map[string]world.Item{"weapon": {Object: world.LegacyObject{Name: "sword"}, Contents: []string{"gem"}}, "gem": {}}, Ready: [20]string{19: "weapon"}},
	}}}
	respawnRoom := s.Rooms[1008]
	respawnRoom.NPCIDs = []string{"respawn-old"}
	respawnRoom.Resource.PermanentMonsters[0] = world.LegacyTimer{Misc: 3}
	s.Rooms[1008] = respawnRoom
	s.NPCs = map[string]world.NPCState{"respawn-old": {Body: world.LegacyMonster{Name: "same", Type: 1, RoomID: 1008, HPCurrent: 77}}}
	wantRolls += 2 // first entry spawns one permanent NPC; repeated death must not duplicate it
	if vitals {
		p := s.Players["a"]
		p.Body.HPMax = 5
		p.Body.HPCurrent = 1
		p.Body.Flags[2] |= 1
		s.Players["a"] = p
		r := s.Rooms[1]
		r.Resource.Flags[3] |= 1
		s.Rooms[1] = r
	}
	if directional {
		p := s.Players["a"]
		p.Body.HPCurrent = 5
		s.Players["a"] = p
		r := s.Rooms[1]
		r.Resource.Exits = []world.LegacyExit{{Name: "북", Destination: 2, Flags: [4]byte{0, 1}}}
		s.Rooms[1] = r
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	calls, rolls := 0, 0
	allocations := 0
	reduce := func(raw json.RawMessage) (json.RawMessage, json.RawMessage, error) {
		calls++
		s, err := world.DecodeState(raw)
		if err != nil {
			return nil, nil, err
		}
		var next world.State
		var result any
		roll := func(lo, hi int) int { rolls++; return lo }
		allocate := func() (string, error) { allocations++; return "respawn-new", nil }
		catalog := npcMovementSpawnCatalog{}
		if directional {
			next, result, err = s.DirectionalStep(world.TransferInput{ActorID: "a", Movement: world.MovementInput{Prefix: "북", Now: 100, Traversal: world.TraversalInput{Class: 4}, Visitor: world.DestinationVisitor{Class: 4, Level: 1}}}, catalog, roll, allocate)
		} else if wholeUpdate {
			next, result, err = s.UpdatePlayer("a", 100, world.SceneOptions{}, catalog, roll, allocate)
		} else if vitals {
			next, result, err = s.TickPlayerVitals("a", 100, world.SceneOptions{}, catalog, roll, allocate)
		} else {
			next, result, err = s.PlanPlayerDeath("a", "a", 100, world.SceneOptions{}, catalog, roll, allocate)
		}
		if err != nil {
			return nil, nil, err
		}
		state, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		response, err := json.Marshal(result)
		return state, response, err
	}
	request := json.RawMessage(`{"kind":"death","victim":"a","attacker":"a","event":"lethal-1"}`)
	if directional {
		request = json.RawMessage(`{"kind":"directional","actor":"a","token":"북","now":100}`)
	}
	if vitals {
		request = json.RawMessage(`{"kind":"vitals","actor":"a","tick":100}`)
	}
	if wholeUpdate {
		request = json.RawMessage(`{"kind":"player-update","actor":"a","tick":100}`)
	}
	first, err := Execute(ctx, store, worldID, "lethal-1", request, reduce)
	if err != nil || first.Revision != 1 || first.Replayed {
		t.Fatalf("%+v %v", first, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	restarted := storage.NewPostgres(db2)
	again, err := Execute(ctx, restarted, worldID, "lethal-1", request, reduce)
	if err != nil || !again.Replayed || calls != 1 || allocations != 1 || rolls != wantRolls || string(again.Response) != string(first.Response) {
		t.Fatalf("%+v %v calls%d rolls%d", again, err, calls, rolls)
	}
	saved, err := restarted.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Players["a"]
	if len(got.NPCs) != 2 || len(got.Rooms[1008].NPCIDs) != 2 || got.Rooms[1008].NPCIDs[0] != "respawn-old" || got.Rooms[1008].NPCIDs[1] != "respawn-new" || got.NPCs["respawn-old"].Body.HPCurrent != 77 || got.NPCs["respawn-new"].Enemies == nil || len(got.Rooms[1008].Resource.Monsters) != 0 {
		t.Fatal("respawn NPC IDs lost or duplicated")
	}
	floor := got.Rooms[1].Items
	if saved.Revision != 1 || p.Body.RoomID != 1008 || p.Body.HPCurrent != wantHP || p.Body.MPCurrent != 8 || p.Body.Timers[11].LastTime != 100 || p.Body.Timers[11].Interval != 604800 || len(p.Items.Items) != 0 || len(floor.Items) != 2 || floor.Items["weapon"].Contents[0] != "gem" || floor.Items["gem"].Object.Flags[1]&3 != 3 || len(got.Rooms[1].PlayerIDs) != 0 || len(got.Rooms[1008].PlayerIDs) != 1 || got.Rooms[1008].Resource.BeenHere != wantVisits {
		t.Fatalf("partial death state %+v", got)
	}
	var receipt world.PlayerDeathResult
	if directional {
		var step world.DirectionalStepResult
		if err := json.Unmarshal(again.Response, &step); err != nil || step.Death == nil || !step.Death.BroadcastDeath || step.Death.Entry.Room.ID != 1008 || !step.Transfer.Movement.Traversal.Dead || step.Transfer.Entry != nil {
			t.Fatalf("lost fall/death result %+v %v", step, err)
		}
		want, err := got.CurrentScene("a", 0)
		if err != nil || step.Death.Entry.Scene != want {
			t.Fatalf("replayed respawn scene does not match saved state: %v", err)
		}
		return
	}
	if vitals {
		var result world.VitalsTransitionResult
		if wholeUpdate {
			var update world.PlayerUpdateResult
			if err := json.Unmarshal(again.Response, &update); err != nil {
				t.Fatal(err)
			}
			result = update.Vitals
			if !update.SaveDue || p.Body.Timers[28].Interval != 100 || p.Body.Timers[28].LastTime != 100 || p.Body.Timers[22].LastTime != 100 {
				t.Fatal("update phase timers missing")
			}
		} else if err := json.Unmarshal(again.Response, &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Deaths) != 2 || result.Deaths[0].AfterMessages != 1 || result.Deaths[1].AfterMessages != 2 || p.Body.Timers[8].LastTime != 100 {
			t.Fatalf("lost ordered deaths %+v %v", result, err)
		}
		return
	}
	if err := json.Unmarshal(again.Response, &receipt); err != nil || !receipt.BroadcastDeath || receipt.Entry.Room.ID != 1008 {
		t.Fatalf("lost result %+v %v", receipt, err)
	}
	want, err := got.CurrentScene("a", 0)
	if err != nil || receipt.Entry.Scene != want {
		t.Fatalf("replayed death scene does not match saved state: %v", err)
	}
}
