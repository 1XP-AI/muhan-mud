package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesShopListAndSell(t *testing.T) {
	var shopFlags, storageFlags [8]byte
	shopFlags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	shopFlags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	storageFlags[12/8] |= 1 << (12 % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: shopFlags}},
				PlayerIDs: []string{"a"},
			},
			201: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 201, Name: "저장고", Flags: storageFlags}},
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"stock": {Object: world.LegacyObject{Name: "기존", Type: 13, Value: 80, Weight: 1}}},
					Inventory: []string{"stock"},
				},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100},
				Online: true,
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검", Type: 13, Value: 100, Weight: 2}}},
					Inventory: []string{"sword"},
				},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "shop-marketplace-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	listing, err := connection.Submit(context.Background(), "품목")
	if err != nil || !strings.Contains(listing, "기존") || store.commits != 1 {
		t.Fatalf("listing=%q err=%v commits=%d", listing, err, store.commits)
	}
	sold, err := connection.Submit(context.Background(), "팔아 검")
	if err != nil || !strings.Contains(sold, "50냥") || store.commits != 2 {
		t.Fatalf("sold=%q err=%v commits=%d", sold, err, store.commits)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 2 || saved.Players["a"].Body.Gold != 150 || saved.Rooms[201].Items.Items["sword"].Object.Name != "검" {
		t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
	}
	unsupported, err := connection.Submit(context.Background(), "팔아")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 2 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
