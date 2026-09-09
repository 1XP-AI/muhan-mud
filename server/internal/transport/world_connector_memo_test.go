package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesMemoToOfflineRecipient(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"alice"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
			"bob":   {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: false},
		},
		Memos: map[string][]world.CharacterMemo{},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "memo-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "메모 bOB 안녕, 오프라인 친구")
	if err != nil || !strings.Contains(output, world.MemoResponse) || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	savedRaw, _ := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil || len(saved.Memos["bob"]) != 1 || saved.Memos["bob"][0].SenderName != "Alice" || saved.Memos["bob"][0].Body != "안녕, 오프라인 친구" {
		t.Fatalf("saved=%+v err=%v", saved.Memos, err)
	}
}

func TestWorldConnectorMemoPreMigrationStateFailsClosedWithoutReceipt(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"alice"}},
		},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
			"bob":   {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: false},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "memo-unmigrated", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "메모 Bob hello")
	if err != nil || !strings.Contains(output, "아직 구현되지 않은 명령") || store.commits != 0 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
}
