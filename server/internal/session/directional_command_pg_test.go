package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresDirectionalCommandPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(directionalCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, "directional-session", mustJSON(state)); err != nil {
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
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-session", "move-1", lease, "8", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, store, "directional-session", "move-1", lease, "8", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-session")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || len(got.Rooms[1].PlayerIDs) != 0 || len(got.Rooms[2].PlayerIDs) != 1 {
		t.Fatalf("saved direction state revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalArrivalTrapPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(directionalCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	r := state.Rooms[2]
	r.Resource.Trap = world.TrapDart
	state.Rooms[2] = r
	p := state.Players["a"]
	p.Body.Stats[1] = 1
	state.Players["a"] = p
	if err := store.CreateWorld(ctx, "directional-trap", mustJSON(state)); err != nil {
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
	rolls := []int{100, 4}
	roll := func(low, high int) int {
		if low != 1 || (high != 100 && high != 10) {
			t.Fatalf("unexpected trap roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-trap", "trap-1", lease, "8", 100, 12, world.SceneOptions{}, nil, roll, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-trap", "trap-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(int, int) int { t.Fatal("replay consumed trap RNG"); return 0 }, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-trap")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || got.Players["a"].Body.HPCurrent != 26 || got.Players["a"].Body.Flags[2]&(1<<0) == 0 {
		t.Fatalf("saved trap state revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalFollowerPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	state, err := world.DecodeState(directionalCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	state.Players["b"] = world.PlayerState{Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	r := state.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	state.Rooms[1] = r
	state, err = state.FollowPlayer("b", "a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWorld(ctx, "directional-follower", mustJSON(state)); err != nil {
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
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-follower", "follow-1", lease, "8", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-follower", "follow-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(int, int) int { t.Fatal("replay consumed follower RNG"); return 0 }, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-follower")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || got.Players["b"].Body.RoomID != 2 || len(got.Rooms[1].PlayerIDs) != 0 || len(got.Rooms[2].PlayerIDs) != 2 || got.Players["b"].FollowingID != "a" || len(got.Players["a"].FollowerIDs) != 1 || got.Players["a"].FollowerIDs[0] != "b" {
		t.Fatalf("saved follower state revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalNPCChasePersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}, PermanentMonsters: [10]world.LegacyTimer{{Misc: 77, LastTime: 1, Interval: 10}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}, NPCIDs: []string{"guard"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
		NPCs: map[string]world.NPCState{"guard": {
			Body:            world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Stats: [5]byte{0, 10}, Flags: [8]byte{1, 2}},
			Enemies:         []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: -1}},
			PermanentOrigin: &world.NPCPermanentOrigin{RoomID: 1, Slot: 0},
		}},
		ActiveNPCIDs: []string{"guard"},
	}
	if err := store.CreateWorld(ctx, "directional-npc-chase", mustJSON(state)); err != nil {
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
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-npc-chase", "npc-chase-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(low, high int) int {
		if low != 1 || high != 50 {
			t.Fatalf("unexpected NPC chase roll %d..%d", low, high)
		}
		return 1
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-npc-chase", "npc-chase-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(int, int) int { t.Fatal("replay consumed NPC chase RNG"); return 0 }, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-npc-chase")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	guard := got.NPCs["guard"]
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || guard.Body.RoomID != 2 || guard.Body.Flags[0]&1 != 0 || got.Rooms[1].Resource.PermanentMonsters[0].LastTime != 100 || len(got.Rooms[1].NPCIDs) != 0 || len(got.Rooms[2].NPCIDs) != 1 {
		t.Fatalf("saved NPC chase revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalAlarmPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	emptyItems := func() *world.ItemCollection { return &world.ItemCollection{Items: map[string]world.Item{}} }
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: emptyItems()},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "경보실", Trap: world.TrapAlarm, TrapExit: 3}}, Items: emptyItems()},
			3: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "경비실"}, PermanentMonsters: [10]world.LegacyTimer{{Misc: 77, LastTime: 1, Interval: 10}}}, Items: emptyItems(), NPCIDs: []string{"guard"}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 1, 10, 10, 10}}, Online: true, Items: emptyItems()}},
		NPCs: map[string]world.NPCState{"guard": {
			Body:            world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 3, Flags: [8]byte{1}},
			Enemies:         []world.NPCEnemy{},
			PermanentOrigin: &world.NPCPermanentOrigin{RoomID: 3, Slot: 0},
		}},
		ActiveNPCIDs: []string{},
	}
	if err := store.CreateWorld(ctx, "directional-alarm", mustJSON(state)); err != nil {
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
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-alarm", "alarm-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("unexpected alarm roll %d..%d", low, high)
		}
		return 100
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-alarm", "alarm-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(int, int) int { t.Fatal("replay consumed alarm RNG"); return 0 }, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-alarm")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	guard := got.NPCs["guard"]
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || guard.Body.RoomID != 2 || guard.Body.Flags[0]&1 != 0 || guard.Body.Flags[0]&(1<<6) == 0 || got.Rooms[3].Resource.PermanentMonsters[0].LastTime != 100 || len(got.Rooms[3].NPCIDs) != 0 || len(got.Rooms[2].NPCIDs) != 1 {
		t.Fatalf("saved alarm revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalAlarmSpawnsDueNPCPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	emptyItems := func() *world.ItemCollection { return &world.ItemCollection{Items: map[string]world.Item{}} }
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: emptyItems()},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "경보실", Trap: world.TrapAlarm, TrapExit: 3}}, Items: emptyItems()},
			3: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "경비실"}, PermanentMonsters: [10]world.LegacyTimer{{Misc: 77, LastTime: 1, Interval: 10}}}, Items: emptyItems()},
		},
		Players:      map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 1, 10, 10, 10}}, Online: true, Items: emptyItems()}},
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
	}
	if err := store.CreateWorld(ctx, "directional-alarm-spawn", mustJSON(state)); err != nil {
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
	rolls := []int{100, 1, 0}
	roll := func(low, high int) int {
		if len(rolls) == 0 {
			t.Fatalf("unexpected extra alarm spawn roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		if value < low || value > high {
			t.Fatalf("alarm spawn roll %d outside %d..%d", value, low, high)
		}
		return value
	}
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-alarm-spawn", "alarm-spawn-1", lease, "8", 100, 12, world.SceneOptions{}, pgAlarmSpawnCatalog{}, roll, func() (string, error) { return "guard-spawned", nil })
	if err != nil || len(rolls) != 0 {
		t.Fatalf("first alarm spawn receipt=%+v rolls=%v err=%v", first, rolls, err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-alarm-spawn", "alarm-spawn-1", lease, "8", 100, 12, world.SceneOptions{}, pgAlarmSpawnCatalog{}, func(int, int) int { t.Fatal("replay consumed alarm spawn RNG"); return 0 }, func() (string, error) { t.Fatal("replay allocated alarm NPC"); return "", nil })
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-alarm-spawn")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	guard := got.NPCs["guard-spawned"]
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || guard.Body.RoomID != 2 || guard.Body.Flags[0]&1 != 0 || guard.Body.Flags[0]&(1<<6) == 0 || got.Rooms[3].Resource.PermanentMonsters[0].LastTime != 100 || len(got.Rooms[3].NPCIDs) != 0 || len(got.Rooms[2].NPCIDs) != 1 || got.ActiveNPCIDs[0] != "guard-spawned" {
		t.Fatalf("saved alarm spawn revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func TestPostgresDirectionalMDMFOLPersistsAndReplays(t *testing.T) {
	dsn := os.Getenv("MUHAN_DIRECTION_TEST_DATABASE_URL")
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
	emptyItems := func() *world.ItemCollection { return &world.ItemCollection{Items: map[string]world.Item{}} }
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a", "b"}, Items: emptyItems(), NPCIDs: []string{"guard"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: emptyItems()},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: emptyItems(), FollowerIDs: []string{"b"}, NPCFollowerIDs: []string{"guard"}, FollowerRefs: []world.EntityRef{{Kind: "player", ID: "b"}, {Kind: "npc", ID: "guard"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, FollowingID: "a", Items: emptyItems()},
		},
		NPCs: map[string]world.NPCState{"guard": {
			Body:              world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Flags: [8]byte{1, 0, 0, 0, 0, 64}},
			Enemies:           []world.NPCEnemy{},
			FollowingPlayerID: "a",
		}},
		ActiveNPCIDs: []string{"guard"},
	}
	if err := store.CreateWorld(ctx, "directional-mdmfol", mustJSON(state)); err != nil {
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
	first, err := owners.ExecuteDirectionalLine(ctx, store, "directional-mdmfol", "mdmfol-1", lease, "8", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := owners.ExecuteDirectionalLine(ctx, storage.NewPostgres(db), "directional-mdmfol", "mdmfol-1", lease, "8", 100, 12, world.SceneOptions{}, nil, func(int, int) int { t.Fatal("replay consumed MDMFOL RNG"); return 0 }, nil)
	if err != nil || !replay.Replayed || string(first.Response) != string(replay.Response) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	saved, err := store.LoadWorld(ctx, "directional-mdmfol")
	if err != nil {
		t.Fatal(err)
	}
	got, err := world.DecodeState(saved.State)
	guard := got.NPCs["guard"]
	if err != nil || saved.Revision != 1 || got.Players["a"].Body.RoomID != 2 || got.Players["b"].Body.RoomID != 2 || guard.Body.RoomID != 2 || guard.Body.Flags[0]&1 != 0 || got.Players["a"].NPCFollowerIDs[0] != "guard" || got.Players["a"].FollowerRefs[0] != (world.EntityRef{Kind: "player", ID: "b"}) || got.Players["a"].FollowerRefs[1] != (world.EntityRef{Kind: "npc", ID: "guard"}) || got.Rooms[1].Resource.PermanentMonsters[0].LastTime != 0 || len(got.Rooms[1].NPCIDs) != 0 || len(got.Rooms[2].NPCIDs) != 1 {
		t.Fatalf("saved MDMFOL revision=%d state=%+v err=%v", saved.Revision, got, err)
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

type pgAlarmSpawnCatalog struct{}

func (pgAlarmSpawnCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{Name: "Guard", Type: 1}, nil
}

func (pgAlarmSpawnCatalog) Object(int16) (world.LegacyObject, error) {
	return world.LegacyObject{Name: "검"}, nil
}
