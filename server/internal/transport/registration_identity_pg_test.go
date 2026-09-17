package transport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/engine"
	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func loadPGIdentityState(t *testing.T, ctx context.Context, store *storage.Postgres, worldID string) (storage.WorldSnapshot, world.State) {
	t.Helper()
	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, state
}

func waitPGIdentityOffline(t *testing.T, ctx context.Context, store *storage.Postgres, worldID string, connector *WorldConnector, actorID string) storage.WorldSnapshot {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot, state := loadPGIdentityState(t, ctx, store, worldID)
		player, playerOK := state.Players[actorID]
		room, roomOK := state.Rooms[1]
		if playerOK && !player.Online && roomOK && len(room.PlayerIDs) == 0 && len(connector.PendingCleanup()) == 0 {
			return snapshot
		}
		select {
		case <-ctx.Done():
			t.Fatalf("durable identity cleanup did not become visible: pending=%d player=%+v room=%+v", len(connector.PendingCleanup()), player, room.PlayerIDs)
		case <-ticker.C:
		}
	}
}

func cleanupPGIdentityConnection(t *testing.T, connection GameConnection) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		connection.Close(cleanupCtx)
	})
}

func TestPostgresRegistrationIdentityRejectsDuplicateActiveAndReconnects(t *testing.T) {
	dsn := os.Getenv("MUHAN_IDENTITY_REGISTRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required; set MUHAN_IDENTITY_REGISTRATION_TEST_DATABASE_URL")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	root := storage.NewPostgres(db)
	if err := root.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("identity-registration-%d", time.Now().UnixNano())
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{},
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
	writer, startup, err := engine.StartWorld(ctx, root, worldID, "identity-registration-boot-"+worldID)
	if err != nil {
		t.Fatal(err)
	}
	if startup.Replayed || startup.Revision != 1 {
		t.Fatalf("unexpected isolated world startup receipt: %+v", startup)
	}

	const characterName = "Alice"
	const password = "pw1234"
	draft, err := game.BuildCreation(game.CreationChoices{
		Male:       true,
		Class:      4,
		Stats:      game.Stats{12, 10, 12, 10, 10},
		Weapon:     1,
		RaceChoice: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts := session.NewWorldAccounts(root, writer, worldID)
	characterID, err := accounts.Register(ctx, characterName, []byte(password), draft)
	if err != nil {
		t.Fatal(err)
	}
	character, err := accounts.Authenticate(ctx, "ALICE", []byte(password))
	if err != nil {
		t.Fatal(err)
	}
	if character.ID != characterID || character.Name != characterName || !character.Linked || character.WorldID != worldID || character.WorldPlayerID != characterID {
		t.Fatalf("authentication did not return the linked world identity: registered=%q authenticated=%+v", characterID, character)
	}

	registeredSnapshot, registeredState := loadPGIdentityState(t, ctx, writer, worldID)
	registeredPlayer, ok := registeredState.Players[characterID]
	if !ok || registeredPlayer.Online || registeredPlayer.Items == nil || len(registeredState.Players) != 1 || len(registeredState.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("registration did not persist one offline canonical character: revision=%d players=%+v room=%+v", registeredSnapshot.Revision, registeredState.Players, registeredState.Rooms[1].PlayerIDs)
	}
	if registeredSnapshot.Revision != startup.Revision+1 {
		t.Fatalf("registration revision=%d startup=%d", registeredSnapshot.Revision, startup.Revision)
	}

	clock := func() (int32, int) { return 100, 12 }
	firstConnector, err := NewWorldConnector(WorldConnectorConfig{Store: writer, WorldID: worldID, Clock: clock, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	secondConnector, err := NewWorldConnector(WorldConnectorConfig{Store: writer, WorldID: worldID, Clock: clock, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}

	first, scene, err := firstConnector.Open(ctx, character)
	if err != nil || first == nil || scene == "" {
		t.Fatalf("first world admission failed: scene=%q connection=%v err=%v", scene, first != nil, err)
	}
	cleanupPGIdentityConnection(t, first)
	beforeDuplicate, beforeDuplicateState := loadPGIdentityState(t, ctx, writer, worldID)
	beforePlayer, ok := beforeDuplicateState.Players[characterID]
	if !ok || !beforePlayer.Online || len(beforeDuplicateState.Players) != 1 || len(beforeDuplicateState.Rooms[1].PlayerIDs) != 1 || beforeDuplicateState.Rooms[1].PlayerIDs[0] != characterID {
		t.Fatalf("first admission did not establish one live identity: revision=%d players=%+v room=%+v", beforeDuplicate.Revision, beforeDuplicateState.Players, beforeDuplicateState.Rooms[1].PlayerIDs)
	}

	duplicate, _, duplicateErr := secondConnector.Open(ctx, character)
	if duplicateErr == nil {
		t.Fatalf("duplicate active identity was admitted: connection=%v", duplicate != nil)
	}
	// Open retains a failed admission lease until its outcome is resolved. This
	// rejection is known before any commit, so release only that process-local
	// reservation instead of issuing a no-op cleanup receipt that would advance
	// the world revision or log out the first active connection.
	for _, lease := range secondConnector.owners.Ordered() {
		if !secondConnector.owners.Release(lease) {
			t.Fatalf("failed duplicate admission lease was not released: %+v", lease)
		}
	}
	afterDuplicate, afterDuplicateState := loadPGIdentityState(t, ctx, writer, worldID)
	if afterDuplicate.Revision != beforeDuplicate.Revision || !reflect.DeepEqual(afterDuplicateState.Players, beforeDuplicateState.Players) || !reflect.DeepEqual(afterDuplicateState.Rooms[1].PlayerIDs, beforeDuplicateState.Rooms[1].PlayerIDs) {
		t.Fatalf("duplicate admission changed durable identity/state: before=%d after=%d beforePlayers=%+v afterPlayers=%+v beforeRoom=%+v afterRoom=%+v", beforeDuplicate.Revision, afterDuplicate.Revision, beforeDuplicateState.Players, afterDuplicateState.Players, beforeDuplicateState.Rooms[1].PlayerIDs, afterDuplicateState.Rooms[1].PlayerIDs)
	}

	first.Close(ctx)
	offlineSnapshot := waitPGIdentityOffline(t, ctx, writer, worldID, firstConnector, characterID)
	_, offlineState := loadPGIdentityState(t, ctx, writer, worldID)
	offlinePlayer, ok := offlineState.Players[characterID]
	if !ok || offlinePlayer.Online || len(offlineState.Players) != 1 || len(offlineState.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("first connection cleanup did not leave one offline character: revision=%d players=%+v room=%+v", offlineSnapshot.Revision, offlineState.Players, offlineState.Rooms[1].PlayerIDs)
	}
	if offlineSnapshot.Revision != beforeDuplicate.Revision+1 {
		t.Fatalf("departure revision=%d before=%d", offlineSnapshot.Revision, beforeDuplicate.Revision)
	}

	reconnected, scene, err := secondConnector.Open(ctx, character)
	if err != nil || reconnected == nil || scene == "" {
		t.Fatalf("same verified identity could not reconnect: scene=%q connection=%v err=%v", scene, reconnected != nil, err)
	}
	cleanupPGIdentityConnection(t, reconnected)
	reconnectedSnapshot, reconnectedState := loadPGIdentityState(t, ctx, writer, worldID)
	reconnectedPlayer, ok := reconnectedState.Players[characterID]
	if !ok || !reconnectedPlayer.Online || len(reconnectedState.Players) != 1 || len(reconnectedState.Rooms[1].PlayerIDs) != 1 || reconnectedState.Rooms[1].PlayerIDs[0] != characterID {
		t.Fatalf("reconnect did not restore exactly one active occupant: revision=%d players=%+v room=%+v", reconnectedSnapshot.Revision, reconnectedState.Players, reconnectedState.Rooms[1].PlayerIDs)
	}
	if reconnectedSnapshot.Revision != offlineSnapshot.Revision+1 || !reflect.DeepEqual(reconnectedPlayer.Body, offlinePlayer.Body) || !reflect.DeepEqual(reconnectedPlayer.Items, offlinePlayer.Items) {
		t.Fatalf("reconnect changed canonical identity state: account=%+v offline=%+v reconnected=%+v", character, offlinePlayer, reconnectedPlayer)
	}

	reconnected.Close(ctx)
	finalSnapshot := waitPGIdentityOffline(t, ctx, writer, worldID, secondConnector, characterID)
	_, finalState := loadPGIdentityState(t, ctx, writer, worldID)
	if finalSnapshot.Revision != reconnectedSnapshot.Revision+1 || len(finalState.Players) != 1 || finalState.Players[characterID].Online || len(finalState.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("unexpected final identity cleanup: revision=%d players=%+v room=%+v", finalSnapshot.Revision, finalState.Players, finalState.Rooms[1].PlayerIDs)
	}
}
