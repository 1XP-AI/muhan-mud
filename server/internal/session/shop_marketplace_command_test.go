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
	if action, ok := parseShopMarketplaceLine("팔아"); !ok || action.kind != "sell" || action.name != "" || action.occurrence != 1 {
		t.Fatalf("bare sell action=%+v ok=%t", action, ok)
	}
	for _, line := range []string{"list", "sell 검", "팔아 검 0", "팔아 검 x", "팔아 검\x00", "품목 재고"} {
		if _, ok := parseShopMarketplaceLine(line); ok {
			t.Fatalf("unsupported shop line accepted: %q", line)
		}
	}
	parsed, err := ParseCommand("팔아 검 2")
	if err != nil || parsed.Kind != CommandShopSell {
		t.Fatalf("ParseCommand sell=%+v err=%v", parsed, err)
	}
	parsed, err = ParseCommand("팔아")
	if err != nil || parsed.Kind != CommandShopSell {
		t.Fatalf("ParseCommand bare sell=%+v err=%v", parsed, err)
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

func shopMarketplaceMutatedFixture(t *testing.T, mutate func(*world.State)) []byte {
	t.Helper()
	s, err := world.DecodeState(shopMarketplaceCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&s)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteShopLineListNotShopSucceedsWithCResponseAndReplays(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		room := s.Rooms[200]
		room.Resource.Flags[world.RoomShopFlag/8] &^= 1 << (world.RoomShopFlag % 8)
		s.Rooms[200] = room
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-not-shop", lease, "품목")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopListResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopListNotShopResponse || strings.Contains(result.Response, "전당포") {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-not-shop", lease, "품목")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineListMissingStorageSucceedsWithCResponse(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		delete(s.Rooms, 201)
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-no-stock", lease, "품목")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopListResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopListNoStockResponse {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-no-stock", lease, "품목")
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineListEmptyCatalogMatchesCHeader(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		room := s.Rooms[201]
		room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
		s.Rooms[201] = room
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-empty", lease, "품목")
	if err != nil || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopListResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopListHeaderResponse || strings.Contains(result.Response, "없음") {
		t.Fatalf("result=%+v", result)
	}
}

func TestExecuteShopLineListUnmigratedStorageFailClosed(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		room := s.Rooms[201]
		room.Items = nil
		s.Rooms[201] = room
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	if _, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-list-unmigrated", lease, "품목"); err == nil {
		t.Fatal("unmigrated storage unexpectedly listed")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed list committed=%d", store.commits)
	}
}

func hideShopMarketplaceActor(s *world.State) {
	actor := s.Players["a"]
	actor.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
	s.Players["a"] = actor
}

func shopMarketplaceActorHidden(s world.State) bool {
	return s.Players["a"].Body.Flags[1/8]&(1<<(1%8)) != 0
}

func TestExecuteShopLineSellPersistsAndReplaysWithoutReevaluatingAllocator(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, hideShopMarketplaceActor)}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-1", lease, "팔아 검")
	if err != nil || first.Replayed || !strings.Contains(string(first.Response), "50냥") || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 150 || len(saved.Players["a"].Items.Inventory) != 0 || !containsShopCommandID(saved.Rooms[201].Items.Inventory, "sword") || shopMarketplaceActorHidden(saved) {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-1", lease, "팔아 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || shopMarketplaceActorHidden(replayed) || replayed.Players["a"].Body.Gold != 150 {
		t.Fatalf("replay re-cleared or re-committed: %+v err=%v commits=%d", replayed, err, store.commits)
	}
}

func TestExecuteShopLineSellPreservesKeyPrefixOccurrenceAndReplays(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		actor := s.Players["a"]
		actor.Items.Items["key-first"] = world.Item{Object: world.LegacyObject{Name: "도구1", Keys: [3]string{"", "needle", ""}, Type: 13, Value: 100, Weight: 1}}
		actor.Items.Items["key-second"] = world.Item{Object: world.LegacyObject{Name: "도구2", Keys: [3]string{"", "needle", ""}, Type: 13, Value: 100, Weight: 1}}
		actor.Items.Inventory = append(actor.Items.Inventory, "key-first", "key-second")
		s.Players["a"] = actor
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-key-prefix", lease, "팔아 needle 2")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopSaleResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ShopSaleSoldAction || result.ItemID != "key-second" || result.Payout != 50 {
		t.Fatalf("key-prefix result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 150 || containsShopCommandID(saved.Players["a"].Items.Inventory, "key-second") || !containsShopCommandID(saved.Rooms[201].Items.Inventory, "key-second") {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-key-prefix", lease, "팔아 needle 2")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineSellNotPawnSucceedsWithCResponseAndReplays(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		room := s.Rooms[200]
		room.Resource.Flags[world.RoomPawnFlag/8] &^= 1 << (world.RoomPawnFlag % 8)
		s.Rooms[200] = room
		hideShopMarketplaceActor(s)
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	before := append([]byte(nil), store.state...)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-not-pawn", lease, "팔아 검")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopSaleResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopSaleNotPawnResponse || result.Action != world.ShopSaleNotPawnAction || strings.Contains(result.Response, "상점") {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(before) {
		t.Fatal("not-pawn sell changed world snapshot")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 100 || !containsShopCommandID(saved.Players["a"].Items.Inventory, "sword") || !shopMarketplaceActorHidden(saved) {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-not-pawn", lease, "팔아 검")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineSellAskWhatSucceedsWithCResponseAndReplays(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, hideShopMarketplaceActor)}
	owners, lease := admitShopMarketplaceOwner(t)
	before := append([]byte(nil), store.state...)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-ask-what", lease, "팔아")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopSaleResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopSaleAskWhatResponse || result.Action != world.ShopSaleAskWhatAction {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(before) {
		t.Fatal("ask-what sell changed world snapshot")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !shopMarketplaceActorHidden(saved) {
		t.Fatalf("ask-what cleared PHIDDN: %+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-ask-what", lease, "팔아")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteShopLineSellNotHoldingSucceedsWithCResponseAndReplays(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, hideShopMarketplaceActor)}
	owners, lease := admitShopMarketplaceOwner(t)
	first, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-not-holding", lease, "팔아 없음")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%s err=%v commits=%d", first.Response, err, store.commits)
	}
	var result world.ShopSaleResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != world.ShopSaleNotHoldingResponse || result.Action != world.ShopSaleNotHoldingAction {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Gold != 100 || !containsShopCommandID(saved.Players["a"].Items.Inventory, "sword") || shopMarketplaceActorHidden(saved) {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	replay, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-not-holding", lease, "팔아 없음")
	if err != nil || !replay.Replayed || string(replay.Response) != string(first.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || shopMarketplaceActorHidden(replayed) || replayed.Players["a"].Body.Gold != 100 || !containsShopCommandID(replayed.Players["a"].Items.Inventory, "sword") {
		t.Fatalf("replay re-cleared or re-committed: %+v err=%v commits=%d", replayed, err, store.commits)
	}
}

func TestExecuteShopLineSellUnmigratedItemsFailClosed(t *testing.T) {
	store := &departureStore{state: shopMarketplaceMutatedFixture(t, func(s *world.State) {
		actor := s.Players["a"]
		actor.Items = nil
		s.Players["a"] = actor
	})}
	owners, lease := admitShopMarketplaceOwner(t)
	if _, err := owners.ExecuteShopLine(context.Background(), store, "w", "shop-sell-unmigrated", lease, "팔아 검"); err == nil {
		t.Fatal("unmigrated items unexpectedly sold")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed sell committed=%d", store.commits)
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
