package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func admitShopPurchaseConnection(t *testing.T, connector *WorldConnector) *worldConnection {
	t.Helper()
	lease, err := connector.owners.Acquire("actor-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &worldConnection{game: connector, lease: lease, ready: true}
}

func TestWorldConnectorSubmitDispatchesShopPurchaseByCanonicalNameAndAlias(t *testing.T) {
	state := shopPurchaseState(t)
	player := state.Players["actor-1"]
	player.Body.Gold = 200
	state.Players["actor-1"] = player
	store, _ := shopPurchaseStore(t, state)
	connector := newShopPurchaseConnector(t, store, func() (string, error) { return "owned-shield", nil })
	connection := admitShopPurchaseConnection(t, connector)

	text, err := connection.Submit(context.Background(), "구입 방패")
	if err != nil || !strings.Contains(text, "방패") {
		t.Fatalf("purchase output=%q err=%v", text, err)
	}
	savedRaw, _, _, _, _, commits := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if commits != 1 || saved.Players["actor-1"].Body.Gold != 130 || len(saved.Players["actor-1"].Items.Inventory) != 2 {
		t.Fatalf("commits=%d saved player=%+v", commits, saved.Players["actor-1"])
	}

	// A display-name prefix resolves through the source selector contract while
	// still allocating only the server-owned copied item identity.
	secondState := shopPurchaseState(t)
	secondStore, _ := shopPurchaseStore(t, secondState)
	secondConnector := newShopPurchaseConnector(t, secondStore, func() (string, error) { return "unused", nil })
	secondConnection := admitShopPurchaseConnection(t, secondConnector)
	prefixPurchase, err := secondConnection.Submit(context.Background(), "사 방")
	if err != nil || prefixPurchase != "당신은 방패을(를) 샀습니다.\r\n" {
		t.Fatalf("prefix stock name=%q err=%v", prefixPurchase, err)
	}
	stateRaw, _, _, _, _, commits := secondStore.snapshot()
	savedPrefix, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || savedPrefix.Players["actor-1"].Body.Gold != 30 || !shopPurchaseHasItem(savedPrefix.Players["actor-1"].Items.Inventory, "unused") {
		t.Fatalf("prefix selector did not resolve canonical stock commits=%d state=%+v err=%v", commits, savedPrefix, err)
	}
}

func TestWorldConnectorSubmitShopPurchaseResolvesPositiveOccurrenceWithoutTrustingStockID(t *testing.T) {
	state := shopPurchaseState(t)
	storage := state.Rooms[11]
	storage.Items.Items["stock-third"] = world.Item{Object: world.LegacyObject{Name: "검", Value: 10, Weight: 1}}
	storage.Items.Inventory = append(storage.Items.Inventory, "stock-third")
	state.Rooms[11] = storage
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store, _ := shopPurchaseStore(t, state)
	connector := newShopPurchaseConnector(t, store, func() (string, error) { return "owned-second", nil })
	connection := admitShopPurchaseConnection(t, connector)

	text, err := connection.Submit(context.Background(), "사 검 2")
	if err != nil || !strings.Contains(text, "검") {
		t.Fatalf("occurrence purchase output=%q err=%v", text, err)
	}
	savedRaw, request, _, _, _, commits := store.snapshot()
	if commits != 1 {
		t.Fatalf("commits=%d", commits)
	}
	var canonical struct {
		ActorID    string `json:"actor_id"`
		Name       string `json:"name"`
		Occurrence int    `json:"occurrence"`
		StockID    string `json:"stock_id"`
	}
	if err := json.Unmarshal(request, &canonical); err != nil {
		t.Fatal(err)
	}
	if canonical.ActorID != "actor-1" || canonical.Name != "검" || canonical.Occurrence != 2 || canonical.StockID != "" {
		t.Fatalf("durable request=%s", request)
	}
	saved, err := world.DecodeState(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor-1"].Body.Gold != 90 {
		t.Fatalf("saved gold=%d", saved.Players["actor-1"].Body.Gold)
	}
}

func TestWorldConnectorSubmitShopPurchaseKeySelectorSkipsInvisibleAndReplays(t *testing.T) {
	state := shopPurchaseStateWithObserver(t, func(s *world.State) {
		storage := s.Rooms[11]
		invisible := [8]byte{}
		invisible[2/8] |= 1 << (2 % 8) // OINVIS
		storage.Items.Items["stock-hidden"] = world.Item{Object: world.LegacyObject{Name: "숨은물건", Keys: [3]string{"needle", "", ""}, Value: 10, Weight: 1, Flags: invisible}}
		storage.Items.Items["stock-visible-first"] = world.Item{Object: world.LegacyObject{Name: "보이는첫물건", Keys: [3]string{"needle", "", ""}, Value: 11, Weight: 1}}
		storage.Items.Items["stock-visible-second"] = world.Item{Object: world.LegacyObject{Name: "보이는둘째물건", Keys: [3]string{"needle", "", ""}, Value: 12, Weight: 1}}
		storage.Items.Inventory = append(storage.Items.Inventory, "stock-hidden", "stock-visible-first", "stock-visible-second")
		s.Rooms[11] = storage
	})
	allocated := 0
	store, actor, observer := shopPurchaseSubmitReady(t, state, func() (string, error) {
		allocated++
		return "owned-selector", nil
	})
	first, err := actor.Submit(context.Background(), "사 needle 2")
	if err != nil || first != "당신은 보이는둘째물건을(를) 샀습니다.\r\n" || store.commits != 1 || allocated != 1 {
		t.Fatalf("first=%q err=%v commits=%d allocations=%d", first, err, store.commits, allocated)
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice이 보이는둘째물건을(를) 샀습니다.\r\n" {
			t.Fatalf("broadcast=%q", event)
		}
	default:
		t.Fatal("selector purchase did not broadcast")
	}
	savedRaw, commits := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil || commits != 1 || saved.Players["actor-1"].Body.Gold != 88 || !shopPurchaseHasItem(saved.Players["actor-1"].Items.Inventory, "owned-selector") {
		t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
	}
	replay, err := actor.Submit(context.Background(), "사 needle 2")
	if err != nil || replay != first || store.commits != 1 || allocated != 1 {
		t.Fatalf("replay=%q err=%v commits=%d allocations=%d", replay, err, store.commits, allocated)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("replay fanned out %q", event)
	default:
	}
}

func shopPurchaseHasItem(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func shopPurchaseStateWithObserver(t *testing.T, mutate func(*world.State)) world.State {
	t.Helper()
	state := shopPurchaseState(t)
	room := state.Rooms[10]
	room.PlayerIDs = []string{"actor-1", "actor-2"}
	state.Rooms[10] = room
	state.Players["actor-2"] = world.PlayerState{
		Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 10},
		Online: true,
		Items:  &world.ItemCollection{Items: map[string]world.Item{}},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&state)
	}
	return state
}

func shopPurchaseSubmitReady(t *testing.T, state world.State, allocate func() (string, error)) (*familyMutationReplayStore, *worldConnection, *worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "shop-buy-world", Clock: func() (int32, int) { return 100, 12 },
		Allocate: allocate, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, id := range []string{"actor-1", "actor-2"} {
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

func TestWorldConnectorSubmitShopPurchaseCParityAndReplayDoesNotFanOut(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		mutate func(*world.State)
		want   string
	}{
		{
			name: "not shop",
			line: "사 검",
			mutate: func(s *world.State) {
				room := s.Rooms[10]
				room.Resource.Flags = [8]byte{}
				room.Resource.Flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
				s.Rooms[10] = room
			},
			want: world.ShopPurchaseNotShopResponse,
		},
		{
			name: "bare 사",
			line: "사",
			want: world.ShopPurchaseAskWhatResponse,
		},
		{
			name: "missing storage",
			line: "사 검",
			mutate: func(s *world.State) {
				delete(s.Rooms, 11)
			},
			want: world.ShopPurchaseNoStockResponse,
		},
		{
			name: "name missing",
			line: "사 없는물건",
			want: world.ShopPurchaseNotSoldResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, actor, observer := shopPurchaseSubmitReady(t, shopPurchaseStateWithObserver(t, tc.mutate), func() (string, error) {
				t.Fatal("allocator called for C-print buy")
				return "", nil
			})
			output, err := actor.Submit(context.Background(), tc.line)
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d want=%q", output, err, store.commits, tc.want)
			}
			select {
			case event := <-observer.events:
				t.Fatalf("C-print buy fanned out %q", event)
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

func TestWorldConnectorSubmitShopPurchaseSuccessBroadcastsOnce(t *testing.T) {
	allocated := []string{"owned-root", "owned-child"}
	store, actor, observer := shopPurchaseSubmitReady(t, shopPurchaseStateWithObserver(t, nil), func() (string, error) {
		if len(allocated) == 0 {
			t.Fatal("extra allocation")
			return "", nil
		}
		id := allocated[0]
		allocated = allocated[1:]
		return id, nil
	})
	output, err := actor.Submit(context.Background(), "사 검")
	if err != nil || output != "당신은 검을(를) 샀습니다.\r\n" || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice이 검을(를) 샀습니다.\r\n" {
			t.Fatalf("broadcast=%q", event)
		}
	default:
		t.Fatal("successful buy did not broadcast")
	}
	replay, err := actor.Submit(context.Background(), "사 검")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("replay fanned out %q", event)
	default:
	}
}

func TestWorldConnectorSubmitShopPurchaseUnmigratedStorageFailClosed(t *testing.T) {
	store, actor, _ := shopPurchaseSubmitReady(t, shopPurchaseStateWithObserver(t, func(s *world.State) {
		room := s.Rooms[11]
		room.Items = nil
		s.Rooms[11] = room
	}), func() (string, error) {
		t.Fatal("allocator called for unmigrated storage")
		return "", nil
	})
	if _, err := actor.Submit(context.Background(), "사 검"); err == nil {
		t.Fatal("unmigrated storage unexpectedly purchased")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed buy committed=%d", store.commits)
	}
}
