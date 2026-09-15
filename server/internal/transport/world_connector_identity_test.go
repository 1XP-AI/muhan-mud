package transport

import (
	"context"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/game"
	"github.com/1XP-Inc/muhan-mud/server/internal/session"
)

func TestWorldConnectorRejectsDuplicateActiveIdentityAndReconnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const worldID = "memory-g2-identity"
	store := newMemoryRegistrationWorld(t, worldID)
	accounts := session.NewWorldAccounts(store, store, worldID)
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
	if _, err := accounts.Register(ctx, "Alice", []byte("pw1234"), draft); err != nil {
		t.Fatal(err)
	}
	character, err := accounts.Authenticate(ctx, "ALICE", []byte("pw1234"))
	if err != nil {
		t.Fatal(err)
	}
	if character.ID == "" || !character.Linked || character.WorldID != worldID || character.WorldPlayerID != character.ID {
		t.Fatalf("authentication did not return the linked world identity: %+v", character)
	}

	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     worldID,
		Clock:       func() (int32, int) { return 100, 12 },
		MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	first, scene, err := connector.Open(ctx, character)
	if err != nil || first == nil || scene == "" {
		t.Fatalf("first world admission failed: scene=%q connection=%v err=%v", scene, first != nil, err)
	}
	defer first.Close(ctx)

	beforeDuplicate, state, _ := store.snapshot(t)
	if len(state.Players) != 1 || !state.Players[character.ID].Online || len(state.Rooms[1].PlayerIDs) != 1 || state.Rooms[1].PlayerIDs[0] != character.ID {
		t.Fatalf("first admission did not establish one live identity: revision=%d players=%+v room=%+v", beforeDuplicate.Revision, state.Players, state.Rooms[1].PlayerIDs)
	}

	duplicate, _, duplicateErr := connector.Open(ctx, character)
	if duplicateErr == nil || duplicate != nil {
		t.Fatalf("duplicate active identity was admitted: connection=%v err=%v", duplicate != nil, duplicateErr)
	}
	afterDuplicate, state, _ := store.snapshot(t)
	if afterDuplicate.Revision != beforeDuplicate.Revision || len(state.Players) != 1 || !state.Players[character.ID].Online || len(state.Rooms[1].PlayerIDs) != 1 || state.Rooms[1].PlayerIDs[0] != character.ID {
		t.Fatalf("duplicate admission changed durable identity/state: before=%d after=%d players=%+v room=%+v", beforeDuplicate.Revision, afterDuplicate.Revision, state.Players, state.Rooms[1].PlayerIDs)
	}

	first.Close(ctx)
	g2WaitOffline(t, ctx, connector, store, character.ID)
	_, state, _ = store.snapshot(t)
	offline := state.Players[character.ID]
	if offline.Online || len(state.Players) != 1 || len(state.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("first connection cleanup did not leave one offline character: players=%+v room=%+v", state.Players, state.Rooms[1].PlayerIDs)
	}

	reconnected, scene, err := connector.Open(ctx, character)
	if err != nil || reconnected == nil || scene == "" {
		t.Fatalf("same verified identity could not reconnect: scene=%q connection=%v err=%v", scene, reconnected != nil, err)
	}
	defer reconnected.Close(ctx)
	_, state, account := store.snapshot(t)
	reconnectedPlayer, ok := state.Players[character.ID]
	if !ok || !reconnectedPlayer.Online || len(state.Players) != 1 || len(state.Rooms[1].PlayerIDs) != 1 || state.Rooms[1].PlayerIDs[0] != character.ID {
		t.Fatalf("reconnect did not restore one live identity: account=%+v players=%+v room=%+v", account, state.Players, state.Rooms[1].PlayerIDs)
	}
	if account.id != character.ID || account.worldPlayer != character.ID || reconnectedPlayer.Body.Name != offline.Body.Name || reconnectedPlayer.Body.Level != offline.Body.Level || reconnectedPlayer.Body.HPCurrent != offline.Body.HPCurrent || reconnectedPlayer.Body.MPCurrent != offline.Body.MPCurrent || reconnectedPlayer.Body.Gold != offline.Body.Gold || reconnectedPlayer.Items == nil || offline.Items == nil || len(reconnectedPlayer.Items.Items) != len(offline.Items.Items) {
		t.Fatalf("reconnect changed verified identity/state: account=%+v before=%+v after=%+v", account, offline, reconnectedPlayer)
	}

	reconnected.Close(ctx)
	g2WaitOffline(t, ctx, connector, store, character.ID)
	final, state, _ := store.snapshot(t)
	if final.Revision != 5 || len(state.Players) != 1 || state.Players[character.ID].Online || len(state.Rooms[1].PlayerIDs) != 0 {
		t.Fatalf("unexpected final reconnect lifecycle: revision=%d players=%+v room=%+v", final.Revision, state.Players, state.Rooms[1].PlayerIDs)
	}
}
