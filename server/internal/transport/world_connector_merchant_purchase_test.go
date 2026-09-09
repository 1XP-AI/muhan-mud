package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesMerchantPurchaseWithConfiguredOffers(t *testing.T) {
	var merchantFlags [8]byte
	merchantFlags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "시장"}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"merchant"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		NPCs: map[string]world.NPCState{
			"merchant": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: merchantFlags}},
		},
	}
	offers := world.MerchantOffers{
		"merchant": {{Name: "검", Value: 25, Weight: 1, Contents: []world.LegacyObject{{Name: "보석", Weight: 1}}}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}

	accepted, ok := session.ParseMerchantPurchaseLine("상인 검 구입")
	if !ok || accepted.NPCName != "상인" || accepted.ItemName != "검" || accepted.NPCOccurrence != 1 || accepted.ItemOccurrence != 1 {
		t.Fatalf("accepted merchant suffix=%+v ok=%t", accepted, ok)
	}
	parsed, err := session.ParseCommand("상인 검 구입")
	if err != nil || parsed.Kind != session.CommandMerchantPurchase {
		t.Fatalf("central parser=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"구입 상인 검", "상인 검", "상인 검 0 1 구입"} {
		if _, ok := session.ParseMerchantPurchaseLine(line); ok {
			t.Fatalf("unsupported merchant suffix accepted: %q", line)
		}
	}

	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:          store,
		WorldID:        "merchant-world",
		Clock:          func() (int32, int) { return 100, 12 },
		MaxSessions:    1,
		MerchantOffers: offers,
	})
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
	output, err := connection.Submit(context.Background(), "상인 검 구입")
	if err != nil || store.commits != 1 || !strings.Contains(output, "25냥") || !strings.Contains(output, "검") {
		t.Fatalf("merchant submit output=%q err=%v commits=%d", output, err, store.commits)
	}

	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	player := saved.Players["a"]
	if commits != 1 || player.Body.Gold != 75 || player.Items == nil || len(player.Items.Inventory) != 1 || len(player.Items.Items) != 2 {
		t.Fatalf("merchant state=%+v commits=%d", player, commits)
	}
	rootID := player.Items.Inventory[0]
	root, ok := player.Items.Items[rootID]
	if !ok || root.Object.Name != "검" || len(root.Contents) != 1 {
		t.Fatalf("purchased root=%+v id=%q", root, rootID)
	}
	child, ok := player.Items.Items[root.Contents[0]]
	if !ok || child.Object.Name != "보석" {
		t.Fatalf("purchased child=%+v", child)
	}

	unsupported, err := connection.Submit(context.Background(), "구입 상인 검")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 1 {
		t.Fatalf("unsupported merchant prefix output=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
