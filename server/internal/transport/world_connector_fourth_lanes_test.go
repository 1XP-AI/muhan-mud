package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorFourthLanesFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 8, Level: 1, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword": {Object: world.LegacyObject{Name: "검", Type: 0, ShotsMax: 4, ShotsCurrent: 3, DiceCount: 1, DiceSides: 1, DicePlus: 1}},
						"armor": {Object: world.LegacyObject{Name: "갑옷", Type: 5, Armor: 4, Wear: 1}},
					},
					Inventory: []string{"sword", "armor"},
				},
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesCompareAndAppraisal(t *testing.T) {
	store := connectorFourthLanesFixture(t)
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "fourth-lanes", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	compare, err := connection.Submit(context.Background(), "비교 검")
	if err != nil || !strings.Contains(compare, "누구나 무장") {
		t.Fatalf("compare=%q err=%v", compare, err)
	}
	appraisal, err := connection.Submit(context.Background(), "감정 검")
	if err != nil || !strings.Contains(appraisal, "이름: 검") || !strings.Contains(appraisal, "사용회수 3") {
		t.Fatalf("appraisal=%q err=%v", appraisal, err)
	}
	if store.commits != 2 {
		t.Fatalf("commits=%d want=2", store.commits)
	}
}

func TestWorldConnectorSubmitDispatchesItemRename(t *testing.T) {
	var flags [8]byte
	flags[world.ItemRenameChangeNameFlag/8] |= 1 << (world.ItemRenameChangeNameFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"actor"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword": {Object: world.LegacyObject{Name: "검", Flags: flags}},
				}, Inventory: []string{"sword"}},
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "rename-lane", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "검 새검 명명")
	if err != nil || !strings.Contains(output, "명명 되었습니다") || store.commits != 1 {
		t.Fatalf("rename=%q err=%v commits=%d", output, err, store.commits)
	}
	savedRaw, _ := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil {
		t.Fatal(err)
	}
	item := saved.Players["actor"].Items.Items["sword"]
	if item.Object.Name != "새검" || item.Object.Flags[world.ItemRenameChangeNameFlag/8]&(1<<(world.ItemRenameChangeNameFlag%8)) != 0 || item.Object.Flags[world.ItemRenameNamedFlag/8]&(1<<(world.ItemRenameNamedFlag%8)) == 0 {
		t.Fatalf("renamed item=%+v", item.Object)
	}
}
