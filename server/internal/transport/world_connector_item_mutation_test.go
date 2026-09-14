package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func lastTokenItemMutationState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"floor-sword": {Object: world.LegacyObject{Name: "검"}},
				}, Inventory: []string{"floor-sword"}},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
}

func collectionHasInventoryName(c *world.ItemCollection, name string) bool {
	if c == nil {
		return false
	}
	for _, id := range c.Inventory {
		if c.Items[id].Object.Name == name {
			return true
		}
	}
	return false
}

func collectionHasInventoryID(c *world.ItemCollection, id string) bool {
	if c == nil {
		return false
	}
	for _, got := range c.Inventory {
		if got == id {
			return true
		}
	}
	return false
}

func collectionHasContainedID(c *world.ItemCollection, containerID, childID string) bool {
	if c == nil {
		return false
	}
	item, ok := c.Items[containerID]
	if !ok {
		return false
	}
	for _, got := range item.Contents {
		if got == childID {
			return true
		}
	}
	return false
}

func itemMutationConnectorReady(t *testing.T, state world.State, worldID string) (*connectorCommandStore, *worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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
	return store, &worldConnection{game: connector, lease: lease, ready: true}
}

func twoGemBagItemMutationState() world.State {
	s := lastTokenItemMutationState()
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 2}, Contents: []string{"floor-gem-1", "floor-gem-2"}},
			"floor-gem-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag"}},
	}
	return s
}

func twoBagItemMutationState() world.State {
	s := lastTokenItemMutationState()
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag-1": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-1"}},
			"floor-bag-2": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-2"}},
			"floor-gem-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag-1", "floor-bag-2"}},
	}
	return s
}

func twoBagThreeGemItemMutationState() world.State {
	s := lastTokenItemMutationState()
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag-1":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 1}, Contents: []string{"floor-gem-1-1"}},
			"floor-bag-2":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 3}, Contents: []string{"floor-gem-2-1", "floor-gem-2-2", "floor-gem-2-3"}},
			"floor-gem-1-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-2": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-3": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag-1", "floor-bag-2"}},
	}
	return s
}

func threeGemTwoBagDropItemMutationState() world.State {
	s := lastTokenItemMutationState()
	room := s.Rooms[1]
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"bag-1": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 0}},
		"bag-2": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 0}},
		"gem-1": {Object: world.LegacyObject{Name: "보석"}},
		"gem-2": {Object: world.LegacyObject{Name: "보석"}},
		"gem-3": {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"bag-1", "bag-2", "gem-1", "gem-2", "gem-3"}}
	s.Players["a"] = player
	return s
}

func TestWorldConnectorSubmitLastTokenItemMutationAndReplay(t *testing.T) {
	// Submit mints command-<rand> each call. Same-ID replay is the session
	// ExecuteItemMutationLine contract, not a transport store double.
	store, connection := itemMutationConnectorReady(t, lastTokenItemMutationState(), "item-suffix")

	taken, err := connection.Submit(context.Background(), "검 주워")
	if err != nil || !strings.Contains(taken, "주웠습니다") || store.commits != 1 {
		t.Fatalf("take=%q err=%v commits=%d", taken, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if collectionHasInventoryName(saved.Rooms[1].Items, "검") || !collectionHasInventoryName(saved.Players["a"].Items, "검") {
		t.Fatalf("take locations room=%+v player=%+v", saved.Rooms[1].Items, saved.Players["a"].Items)
	}

	dropped, err := connection.Submit(context.Background(), "검 버려")
	if err != nil || !strings.Contains(dropped, "버렸습니다") || store.commits != 2 {
		t.Fatalf("drop=%q err=%v commits=%d", dropped, err, store.commits)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if !collectionHasInventoryName(saved.Rooms[1].Items, "검") || collectionHasInventoryName(saved.Players["a"].Items, "검") {
		t.Fatalf("drop locations room=%+v player=%+v", saved.Rooms[1].Items, saved.Players["a"].Items)
	}
}

func TestWorldConnectorSubmitLastTokenItemMutationFailsClosed(t *testing.T) {
	store, connection := itemMutationConnectorReady(t, lastTokenItemMutationState(), "item-suffix-fail")

	if _, err := connection.Submit(context.Background(), "없는검 주워"); err == nil || store.commits != 0 {
		t.Fatalf("missing item committed err=%v commits=%d", err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || !collectionHasInventoryName(saved.Rooms[1].Items, "검") {
		t.Fatalf("missing mutated room=%+v actor=%+v err=%v", saved.Rooms[1].Items, saved.Players["a"], err)
	}

	unmigrated := lastTokenItemMutationState()
	player := unmigrated.Players["a"]
	player.Items = nil
	unmigrated.Players["a"] = player
	unmigratedStore, unmigratedConn := itemMutationConnectorReady(t, unmigrated, "item-suffix-unmigrated")
	if _, err := unmigratedConn.Submit(context.Background(), "검 주워"); err == nil || unmigratedStore.commits != 0 {
		t.Fatalf("unmigrated committed err=%v commits=%d", err, unmigratedStore.commits)
	}
	saved, err = world.DecodeState(unmigratedStore.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("unmigrated moved actor=%+v err=%v", saved.Players["a"], err)
	}
}

func TestWorldConnectorSubmitTakesNthItemFromNthContainer(t *testing.T) {
	store, connection := itemMutationConnectorReady(t, twoBagThreeGemItemMutationState(), "item-val1-val2-take")
	taken, err := connection.Submit(context.Background(), "가방 2 보석 3 꺼내")
	if err != nil || !strings.Contains(taken, "주웠습니다") || !strings.Contains(taken, "보석") || store.commits != 1 {
		t.Fatalf("take=%q err=%v commits=%d", taken, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if collectionHasInventoryID(saved.Players["a"].Items, "floor-gem-1-1") || collectionHasInventoryID(saved.Players["a"].Items, "floor-gem-2-1") || collectionHasInventoryID(saved.Players["a"].Items, "floor-gem-2-2") || !collectionHasInventoryID(saved.Players["a"].Items, "floor-gem-2-3") {
		t.Fatalf("took wrong gem player=%+v", saved.Players["a"].Items)
	}
	if !collectionHasContainedID(saved.Rooms[1].Items, "floor-bag-1", "floor-gem-1-1") || !collectionHasContainedID(saved.Rooms[1].Items, "floor-bag-2", "floor-gem-2-1") || !collectionHasContainedID(saved.Rooms[1].Items, "floor-bag-2", "floor-gem-2-2") || collectionHasContainedID(saved.Rooms[1].Items, "floor-bag-2", "floor-gem-2-3") {
		t.Fatalf("bag contents=%+v", saved.Rooms[1].Items)
	}
}

func TestWorldConnectorSubmitDropsNthItemIntoNthContainer(t *testing.T) {
	store, connection := itemMutationConnectorReady(t, threeGemTwoBagDropItemMutationState(), "item-val1-val2-drop")
	dropped, err := connection.Submit(context.Background(), "보석 3 가방 2 버려")
	if err != nil || !strings.Contains(dropped, "버렸습니다") || store.commits != 1 {
		t.Fatalf("drop=%q err=%v commits=%d", dropped, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop moved actor=%+v err=%v", saved.Players["a"], err)
	}
	if !collectionHasInventoryID(saved.Players["a"].Items, "gem-1") || !collectionHasInventoryID(saved.Players["a"].Items, "gem-2") || collectionHasInventoryID(saved.Players["a"].Items, "gem-3") {
		t.Fatalf("dropped wrong gem inventory=%+v", saved.Players["a"].Items)
	}
	if collectionHasContainedID(saved.Players["a"].Items, "bag-1", "gem-3") || !collectionHasContainedID(saved.Players["a"].Items, "bag-2", "gem-3") {
		t.Fatalf("dropped into wrong bag player=%+v", saved.Players["a"].Items)
	}
}

func TestWorldConnectorSubmitXOROccurrenceTake(t *testing.T) {
	val2Store, val2Conn := itemMutationConnectorReady(t, twoGemBagItemMutationState(), "item-xor-val2")
	fromBag, err := val2Conn.Submit(context.Background(), "가방 보석 2 꺼내")
	if err != nil || !strings.Contains(fromBag, "주웠습니다") || val2Store.commits != 1 {
		t.Fatalf("가방 보석 2 꺼내=%q err=%v commits=%d", fromBag, err, val2Store.commits)
	}
	fromBagState, err := world.DecodeState(val2Store.state)
	if err != nil || fromBagState.Players["a"].Body.RoomID != 1 {
		t.Fatalf("가방 보석 2 꺼내 moved actor=%+v err=%v", fromBagState.Players["a"], err)
	}
	if collectionHasInventoryID(fromBagState.Players["a"].Items, "floor-gem-1") || !collectionHasInventoryID(fromBagState.Players["a"].Items, "floor-gem-2") {
		t.Fatalf("가방 보석 2 꺼내 took first gem player=%+v", fromBagState.Players["a"].Items)
	}
	if !collectionHasContainedID(fromBagState.Rooms[1].Items, "floor-bag", "floor-gem-1") || collectionHasContainedID(fromBagState.Rooms[1].Items, "floor-bag", "floor-gem-2") {
		t.Fatalf("가방 보석 2 꺼내 bag contents=%+v", fromBagState.Rooms[1].Items.Items["floor-bag"])
	}

	val1Store, val1Conn := itemMutationConnectorReady(t, twoBagItemMutationState(), "item-xor-val1")
	fromSecond, err := val1Conn.Submit(context.Background(), "가방 2 보석 꺼내")
	if err != nil || !strings.Contains(fromSecond, "주웠습니다") || val1Store.commits != 1 {
		t.Fatalf("가방 2 보석 꺼내=%q err=%v commits=%d", fromSecond, err, val1Store.commits)
	}
	fromSecondState, err := world.DecodeState(val1Store.state)
	if err != nil || fromSecondState.Players["a"].Body.RoomID != 1 {
		t.Fatalf("가방 2 보석 꺼내 moved actor=%+v err=%v", fromSecondState.Players["a"], err)
	}
	if collectionHasInventoryID(fromSecondState.Players["a"].Items, "floor-gem-1") || !collectionHasInventoryID(fromSecondState.Players["a"].Items, "floor-gem-2") {
		t.Fatalf("가방 2 보석 꺼내 took from first bag player=%+v", fromSecondState.Players["a"].Items)
	}
	if !collectionHasContainedID(fromSecondState.Rooms[1].Items, "floor-bag-1", "floor-gem-1") || collectionHasContainedID(fromSecondState.Rooms[1].Items, "floor-bag-2", "floor-gem-2") {
		t.Fatalf("가방 2 보석 꺼내 2nd bag still held gem room=%+v", fromSecondState.Rooms[1].Items)
	}
}
