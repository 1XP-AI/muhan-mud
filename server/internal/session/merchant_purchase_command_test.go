package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func merchantPurchaseCommandFixture(t *testing.T) ([]byte, world.MerchantOffers) {
	t.Helper()
	var flags [8]byte
	flags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"merchant"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: nil},
			},
		},
		NPCs: map[string]world.NPCState{
			"merchant": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 200, Flags: flags}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw, world.MerchantOffers{
		"merchant": {{Name: "검", Value: 25, Weight: 1, Contents: []world.LegacyObject{{Name: "보석", Weight: 1}}}},
	}
}

func TestParseMerchantPurchaseLineUsesSuffixAndPositiveOccurrences(t *testing.T) {
	for _, line := range []string{"상인 검 구입", "상인 검 2 3 구입", `"상인" "긴 검" 2 1 구입`} {
		command, ok := ParseMerchantPurchaseLine(line)
		if !ok || command.NPCName == "" || command.ItemName == "" || command.NPCOccurrence < 1 || command.ItemOccurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	command, ok := ParseMerchantPurchaseLine("상인 검 2 3 구입")
	if !ok || command.NPCName != "상인" || command.ItemName != "검" || command.NPCOccurrence != 2 || command.ItemOccurrence != 3 {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	parsed, err := ParseCommand("상인 검 구입")
	if err != nil || parsed.Kind != CommandMerchantPurchase {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"구입 상인 검", "상인 검", "상인 검 0 1 구입", "상인 검 1 구입", "상인 검 1 1", "상인\n검 구입"} {
		if _, ok := ParseMerchantPurchaseLine(line); ok {
			t.Fatalf("unsupported merchant purchase accepted: %q", line)
		}
	}
}

func TestExecuteMerchantPurchaseLinePersistsNestedRewardAndReplays(t *testing.T) {
	state, offers := merchantPurchaseCommandFixture(t)
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMerchantPurchaseLine(context.Background(), store, "w", "merchant-1", lease, "상인 검 구입", offers)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MerchantPurchaseResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "merchant-purchase" || result.NPCID != "merchant" || result.Price != 25 || result.GoldBefore != 100 || result.GoldAfter != 75 || result.RewardRootID != "merchant-merchant-1-1" || len(result.PurchasedIDs) != 2 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Gold != 75 || saved.Players["actor"].Items.Items[result.RewardRootID].Contents[0] != result.PurchasedIDs[1] {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteMerchantPurchaseLine(context.Background(), store, "w", "merchant-1", lease, "상인 검 구입", offers)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteMerchantPurchaseLineFailsClosedWithoutOfferMigration(t *testing.T) {
	state, _ := merchantPurchaseCommandFixture(t)
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteMerchantPurchaseLine(context.Background(), store, "w", "merchant-unresolved", lease, "상인 검 구입"); err == nil || store.commits != 0 {
		t.Fatalf("unresolved merchant purchase committed: err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteMerchantPurchaseLine(context.Background(), store, "w", "merchant-bad", lease, "상인 검 0 1 구입", world.MerchantOffers{}); err == nil || store.commits != 0 {
		t.Fatalf("malformed merchant purchase committed: err=%v commits=%d", err, store.commits)
	}
}
