package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func shopMarketplaceCommandFixture(t *testing.T) []byte {
	t.Helper()
	var pawnFlags, storageFlags [8]byte
	pawnFlags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	pawnFlags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	storageFlags[12/8] |= 1 << (12 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: pawnFlags}},
				PlayerIDs: []string{"a"},
			},
			201: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 201, Name: "전당포 저장고", Flags: storageFlags}},
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"stock": {Object: world.LegacyObject{Name: "기존", Type: 13, Value: 80, Weight: 1}}},
					Inventory: []string{"stock"},
				},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Stats: [5]byte{10}},
				Online: true,
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검", Type: 13, Value: 100, Weight: 2}}},
					Inventory: []string{"sword"},
				},
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseShopMarketplaceLineUsesOnlySourceAliasesAndExplicitOccurrence(t *testing.T) {
	if action, ok := parseShopMarketplaceLine("품목"); !ok || action.kind != "list" || action.occurrence != 1 {
		t.Fatalf("list action=%+v ok=%t", action, ok)
	}
	if action, ok := parseShopMarketplaceLine(`팔아 "검" 2`); !ok || action.kind != "sell" || action.name != "검" || action.occurrence != 2 {
		t.Fatalf("sell action=%+v ok=%t", action, ok)
	}
	for _, line := range []string{"list", "sell 검", "팔아", "팔아 검 0", "팔아 검 x", "품목 재고"} {
		if _, ok := parseShopMarketplaceLine(line); ok {
			t.Fatalf("unsupported shop line accepted: %q", line)
		}
	}
	parsed, err := ParseCommand("팔아 검 2")
	if err != nil || parsed.Kind != CommandShopSell {
		t.Fatalf("ParseCommand sell=%+v err=%v", parsed, err)
	}
	parsed, err = ParseCommand("품목")
	if err != nil || parsed.Kind != CommandShopList {
		t.Fatalf("ParseCommand list=%+v err=%v", parsed, err)
	}
}

func admitShopMarketplaceOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestExecuteShopLineListIsReadOnlyAndReplaysDeterministically(t *testing.T) {
	store := &departureStore{state: shopMarketplaceCommandFixture(t)}
	owners, lease := admitShopMarketplaceOwner(t)
	before := append([]byte(nil), store.state...)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-1", lease, "품목")
	if err != nil || first.Replayed || !strings.Contains(string(first.Response), "기존") || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	if string(store.state) != string(before) {
		t.Fatal("list changed world snapshot")
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-1", lease, "품목")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineSellPersistsAndReplaysWithoutReevaluatingAllocator(t *testing.T) {
	store := &departureStore{state: shopMarketplaceCommandFixture(t)}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-1", lease, "팔아 검")
	if err != nil || first.Replayed || !strings.Contains(string(first.Response), "50냥") || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 150 || len(saved.Players["a"].Items.Inventory) != 0 || !containsShopCommandID(saved.Rooms[201].Items.Inventory, "sword") {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-1", lease, "팔아 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-conflict", lease, "팔아 없음"); err == nil {
		t.Fatal("missing item unexpectedly sold")
	}
	if store.commits != 1 {
		t.Fatalf("failed sale created commit=%d", store.commits)
	}
}

func containsShopCommandID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
