package transport

import (
	"context"
	"encoding/json"
	"fmt"
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
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200, Gold: 100, Flags: [8]byte{1 << 1}}, // PHIDDN
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
	if err != nil || commits != 2 || saved.Players["a"].Body.Gold != 150 || saved.Rooms[201].Items.Items["sword"].Object.Name != "검" || saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
	}
	askWhat, err := connection.Submit(context.Background(), "팔아")
	if err != nil || askWhat != world.ShopSaleAskWhatResponse || store.commits != 3 {
		t.Fatalf("ask-what=%q err=%v commits=%d", askWhat, err, store.commits)
	}
}

func shopListConnectorState(t *testing.T, mutate func(*world.State)) world.State {
	t.Helper()
	var shopFlags, storageFlags [8]byte
	shopFlags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	storageFlags[12/8] |= 1 << (12 % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "상점", Flags: shopFlags}},
				PlayerIDs: []string{"a", "b"},
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
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"b": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 200},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	if mutate != nil {
		mutate(&state)
	}
	return state
}

func shopListConnectorReady(t *testing.T, state world.State) (*familyMutationReplayStore, *worldConnection, *worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "shop-list-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, id := range []string{"a", "b"} {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connections = append(connections, &worldConnection{
			game:   connector,
			lease:  lease,
			ready:  true,
			events: make(chan string, 8),
		})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	return store, connections[0], connections[1]
}

func TestWorldConnectorSubmitShopListCParityAndReplayDoesNotFanOut(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*world.State)
		want   string
	}{
		{
			name: "not shop",
			mutate: func(s *world.State) {
				room := s.Rooms[200]
				room.Resource.Flags = [8]byte{}
				room.Resource.Flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
				s.Rooms[200] = room
			},
			want: world.ShopListNotShopResponse,
		},
		{
			name: "missing storage",
			mutate: func(s *world.State) {
				delete(s.Rooms, 201)
			},
			want: world.ShopListNoStockResponse,
		},
		{
			name: "empty catalog",
			mutate: func(s *world.State) {
				room := s.Rooms[201]
				room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
				s.Rooms[201] = room
			},
			want: world.ShopListHeaderResponse,
		},
		{
			name: "stocked catalog",
			want: world.ShopListHeaderResponse + "\r\n   " + fmt.Sprintf("%-30s", "기존") + "   가격: 80",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, actor, observer := shopListConnectorReady(t, shopListConnectorState(t, tc.mutate))
			output, err := actor.Submit(context.Background(), "품목")
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d want=%q", output, err, store.commits, tc.want)
			}
			select {
			case event := <-observer.events:
				t.Fatalf("list fanned out %q", event)
			default:
			}
			replay, err := actor.Submit(context.Background(), "품목")
			if err != nil || replay != output || store.commits != 1 {
				t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
			}
			select {
			case event := <-observer.events:
				t.Fatalf("replay fanned out %q", event)
			default:
			}
		})
	}
}

func shopSellConnectorState(t *testing.T, mutate func(*world.State)) world.State {
	t.Helper()
	var pawnFlags, storageFlags [8]byte
	pawnFlags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	pawnFlags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
	storageFlags[12/8] |= 1 << (12 % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			200: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: pawnFlags}},
				PlayerIDs: []string{"a", "b"},
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
			"b": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 200},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	if mutate != nil {
		mutate(&state)
	}
	return state
}

func TestWorldConnectorSubmitShopSellCParityAndReplayDoesNotFanOut(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		mutate func(*world.State)
		want   string
	}{
		{
			name: "not pawn",
			line: "팔아 검",
			mutate: func(s *world.State) {
				room := s.Rooms[200]
				room.Resource.Flags = [8]byte{}
				room.Resource.Flags[world.RoomShopFlag/8] |= 1 << (world.RoomShopFlag % 8)
				s.Rooms[200] = room
			},
			want: world.ShopSaleNotPawnResponse,
		},
		{
			name: "bare 팔아",
			line: "팔아",
			want: world.ShopSaleAskWhatResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, actor, observer := shopListConnectorReady(t, shopSellConnectorState(t, tc.mutate))
			before, _ := store.snapshot()
			output, err := actor.Submit(context.Background(), tc.line)
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d want=%q", output, err, store.commits, tc.want)
			}
			if strings.Contains(output, "상점") {
				t.Fatalf("guessed RSHOPP shop text: %q", output)
			}
			stateRaw, commits := store.snapshot()
			saved, err := world.DecodeState(stateRaw)
			if err != nil || commits != 1 || saved.Players["a"].Body.Gold != 100 || !shopSellHasItem(saved.Players["a"].Items.Inventory, "sword") {
				t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
			}
			if string(stateRaw) != string(before) {
				t.Fatal("C-print sell changed world snapshot")
			}
			select {
			case event := <-observer.events:
				t.Fatalf("sell leftover fanned out %q", event)
			default:
			}
			replay, err := actor.Submit(context.Background(), tc.line)
			if err != nil || replay != output || store.commits != 1 {
				t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
			}
			select {
			case event := <-observer.events:
				t.Fatalf("replay fanned out %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitShopSellNotHoldingClearsPHIDDNAndReplayDoesNotRecommit(t *testing.T) {
	store, actor, observer := shopListConnectorReady(t, shopSellConnectorState(t, func(s *world.State) {
		player := s.Players["a"]
		player.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
		s.Players["a"] = player
	}))
	output, err := actor.Submit(context.Background(), "팔아 없음")
	if err != nil || output != world.ShopSaleNotHoldingResponse || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || saved.Players["a"].Body.Gold != 100 || !shopSellHasItem(saved.Players["a"].Items.Inventory, "sword") || saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("not-holding leftover fanned out %q", event)
	default:
	}
	replay, err := actor.Submit(context.Background(), "팔아 없음")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	replayRaw, replayCommits := store.snapshot()
	replayed, err := world.DecodeState(replayRaw)
	if err != nil || replayCommits != 1 || replayed.Players["a"].Body.Flags[0]&(1<<1) != 0 || replayed.Players["a"].Body.Gold != 100 {
		t.Fatalf("replay re-cleared or re-committed: %+v err=%v commits=%d", replayed, err, replayCommits)
	}
}

func shopSellHasItem(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestWorldConnectorSubmitShopSellUnmigratedItemsFailClosed(t *testing.T) {
	store, actor, _ := shopListConnectorReady(t, shopSellConnectorState(t, func(s *world.State) {
		actor := s.Players["a"]
		actor.Items = nil
		s.Players["a"] = actor
	}))
	if _, err := actor.Submit(context.Background(), "팔아 검"); err == nil {
		t.Fatal("unmigrated items unexpectedly sold")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed sell committed=%d", store.commits)
	}
}

func TestWorldConnectorSubmitShopListUnmigratedStorageFailClosed(t *testing.T) {
	store, actor, _ := shopListConnectorReady(t, shopListConnectorState(t, func(s *world.State) {
		room := s.Rooms[201]
		room.Items = nil
		s.Rooms[201] = room
	}))
	if _, err := actor.Submit(context.Background(), "품목"); err == nil {
		t.Fatal("unmigrated storage unexpectedly listed")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed list committed=%d", store.commits)
	}
}
