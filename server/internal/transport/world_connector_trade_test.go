package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesLegacySuffixTrade(t *testing.T) {
	var npcFlags [8]byte
	npcFlags[37/8] |= 1 << (37 % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "교역방"}},
			PlayerIDs: []string{"a"},
			NPCIDs:    []string{"npc"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"offered": {Object: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13, ShotsMax: 10, ShotsCurrent: 10}}}, Inventory: []string{"offered"}}},
		},
		NPCs: map[string]world.NPCState{
			"npc": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: npcFlags}, TradeOffers: []world.NPCTradeOffer{{Wanted: world.LegacyObject{Name: "사과", Keys: [3]string{"apple"}, Type: 13}, Reward: &world.LegacyObject{Name: "보상검", Type: 13}}}},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "trade-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
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
	output, err := connection.Submit(context.Background(), "사과 상인 교환")
	if err != nil || !strings.Contains(output, "보상검") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Players["a"].Items.Inventory) != 1 || saved.Players["a"].Items.Inventory[0] == "offered" {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	unsupported, err := connection.Submit(context.Background(), "교환 사과 상인")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 1 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
