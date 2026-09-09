package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type resourceTickCatalog struct {
	object      world.LegacyObject
	objectCalls int
}

func (c *resourceTickCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("unexpected NPC catalog lookup")
}

func (c *resourceTickCatalog) Object(int16) (world.LegacyObject, error) {
	c.objectCalls++
	return c.object, nil
}

func resourceTickState(t *testing.T, npcPending bool) json.RawMessage {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader: world.LegacyRoomHeader{ID: 1},
					PermanentObjects: [10]world.LegacyTimer{{LastTime: 0, Interval: 1, Misc: 8}},
				},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}},
			},
		},
		Players: map[string]world.PlayerState{},
	}
	if npcPending {
		room := s.Rooms[1]
		room.Resource.PermanentMonsters[0] = world.LegacyTimer{LastTime: 0, Interval: 1, Misc: 7}
		room.NPCIDs = []string{"wolf"}
		s.Rooms[1] = room
		s.NPCs = map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
		}
		s.ActiveNPCIDs = []string{"wolf"}
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRunRoomResourceTickPersistsCanonicalFloorAndSkipsUnmigratedRooms(t *testing.T) {
	store := &connectorCommandStore{state: resourceTickState(t, false)}
	catalog := &resourceTickCatalog{object: world.LegacyObject{Name: "검"}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "room-resource-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: catalog,
		Allocate: func() (string, error) { return "floor-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := connector.RunRoomResourceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, store.commits, err)
	}
	var summary roomResourcePhaseSummary
	if err := json.Unmarshal(first.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Refreshed) != 1 || summary.Refreshed[0] != 1 || len(summary.Unmigrated) != 1 || summary.Unmigrated[0] != 2 {
		t.Fatalf("summary=%+v", summary)
	}
	if catalog.objectCalls != 1 {
		t.Fatalf("object catalog calls=%d", catalog.objectCalls)
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	room := saved.Rooms[1]
	if len(room.Items.Inventory) != 1 || room.Items.Inventory[0] != "floor-1" || room.Items.Items["floor-1"].Object.Name != "검" {
		t.Fatalf("saved floor=%+v", room.Items)
	}
	second, ran, err := connector.RunRoomResourceTick(context.Background(), 20*time.Second)
	if err != nil || ran || second.Response != nil || store.commits != 1 {
		t.Fatalf("duplicate=%+v ran=%v commits=%d err=%v", second, ran, store.commits, err)
	}
}

func TestRunRoomResourceTickLeavesDueNPCForIdentityPhase(t *testing.T) {
	store := &connectorCommandStore{state: resourceTickState(t, true)}
	catalog := &resourceTickCatalog{object: world.LegacyObject{Name: "검"}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "room-resource-npc-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: catalog,
		Allocate: func() (string, error) { return "floor-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, ran, err := connector.RunRoomResourceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || store.commits != 1 {
		t.Fatalf("receipt=%+v ran=%v commits=%d err=%v", receipt, ran, store.commits, err)
	}
	var summary roomResourcePhaseSummary
	if err := json.Unmarshal(receipt.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.NPCPending) != 1 || summary.NPCPending[0] != 1 || len(summary.Refreshed) != 0 {
		t.Fatalf("summary=%+v", summary)
	}
	if catalog.objectCalls != 0 {
		t.Fatalf("floor refresh ran before NPC boundary: object calls=%d", catalog.objectCalls)
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Rooms[1].Resource.PermanentObjects[0].LastTime != 0 || len(saved.Rooms[1].Items.Items) != 0 {
		t.Fatal("due NPC room was partially refreshed")
	}
}

func TestRunRoomResourceTickRetriesExactPendingCommand(t *testing.T) {
	base := &connectorCommandStore{state: resourceTickState(t, false)}
	store := &retryTickStore{base: base, failOnce: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "room-resource-retry-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: &resourceTickCatalog{object: world.LegacyObject{Name: "검"}},
		Allocate: func() (string, error) { return "floor-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunRoomResourceTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("first ran=%v err=%v", ran, err)
	}
	if _, ran, err := connector.RunRoomResourceTick(context.Background(), 20*time.Second); err != nil || !ran {
		t.Fatalf("retry ran=%v err=%v", ran, err)
	}
	if len(store.ids) != 2 || store.ids[0] != store.ids[1] || store.ids[0] != "room-resources-2" {
		t.Fatalf("ids=%v", store.ids)
	}
	if len(store.requests) != 2 || string(store.requests[0]) != string(store.requests[1]) || !strings.Contains(string(store.requests[0]), "room-resource-phase") {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
}
