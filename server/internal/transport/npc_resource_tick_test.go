package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type npcIdentityTickCatalog struct {
	monsters    map[int16]world.LegacyMonster
	monsterCall int
	objectCall  int
	mu          sync.Mutex
}

func (c *npcIdentityTickCatalog) Monster(id int16) (world.LegacyMonster, error) {
	c.mu.Lock()
	c.monsterCall++
	c.mu.Unlock()
	template, ok := c.monsters[id]
	if !ok {
		return world.LegacyMonster{}, errors.New("missing NPC template")
	}
	return template, nil
}

func (c *npcIdentityTickCatalog) Object(int16) (world.LegacyObject, error) {
	c.mu.Lock()
	c.objectCall++
	c.mu.Unlock()
	return world.LegacyObject{}, errors.New("unexpected NPC object lookup")
}

func npcIdentityTickCatalogFixture() *npcIdentityTickCatalog {
	return &npcIdentityTickCatalog{monsters: map[int16]world.LegacyMonster{
		11: {Name: "늑대", Type: 1, HPMax: 10, HPCurrent: 10},
		12: {Name: "개미", Type: 1, HPMax: 11, HPCurrent: 11},
	}}
}

func npcIdentityTickState(t *testing.T, canonicalNPCs bool) json.RawMessage {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{
					LegacyRoomHeader:  world.LegacyRoomHeader{ID: 1},
					PermanentMonsters: [10]world.LegacyTimer{{LastTime: 0, Interval: 1, Misc: 11}, {LastTime: 0, Interval: 1, Misc: 12}},
				},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
				PlayerIDs: []string{"player"},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}},
			},
		},
		Players: map[string]world.PlayerState{
			"player": {Body: world.LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
	}
	if canonicalNPCs {
		room := s.Rooms[1]
		room.NPCIDs = []string{"old"}
		s.Rooms[1] = room
		s.NPCs = map[string]world.NPCState{
			"old": {
				Body:    world.LegacyMonster{Name: "제일뒤", Type: 1, RoomID: 1},
				Enemies: []world.NPCEnemy{},
			},
		}
		s.ActiveNPCIDs = []string{"old"}
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

func npcIdentityTickRoll(low, _ int) int { return low }

func TestRunNPCResourceTickPersistsIdentityAndDeterministicOrders(t *testing.T) {
	store := &connectorCommandStore{state: npcIdentityTickState(t, true)}
	catalog := npcIdentityTickCatalogFixture()
	allocated := []string{"npc-slot-0", "npc-slot-1"}
	allocateAt := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-resource-world", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: catalog,
		Roll: npcIdentityTickRoll,
		Allocate: func() (string, error) {
			id := allocated[allocateAt]
			allocateAt++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, ran, err := connector.RunNPCResourceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || receipt.Replayed || store.commits != 1 || store.command != "npc-resources-2" {
		t.Fatalf("receipt=%+v ran=%v command=%q commits=%d err=%v", receipt, ran, store.command, store.commits, err)
	}
	var summary npcResourcePhaseSummary
	if err := json.Unmarshal(receipt.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Now != 40 || !reflect.DeepEqual(summary.SpawnedRooms, []int16{1}) || !reflect.DeepEqual(summary.SpawnedNPCIDs, allocated) || !reflect.DeepEqual(summary.Unmigrated, []int16{2}) {
		t.Fatalf("summary=%+v", summary)
	}
	if catalog.monsterCall != 2 || catalog.objectCall != 0 {
		t.Fatalf("catalog calls monster=%d object=%d", catalog.monsterCall, catalog.objectCall)
	}

	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	room := saved.Rooms[1]
	if !reflect.DeepEqual(room.NPCIDs, []string{"npc-slot-1", "npc-slot-0", "old"}) {
		t.Fatalf("room identity order=%#v", room.NPCIDs)
	}
	if !reflect.DeepEqual(saved.ActiveNPCIDs, []string{"npc-slot-1", "npc-slot-0", "old"}) {
		t.Fatalf("active identity order=%#v", saved.ActiveNPCIDs)
	}
	for slot, id := range allocated {
		npc := saved.NPCs[id]
		want := world.NPCPermanentOrigin{RoomID: 1, Slot: uint8(slot)}
		if npc.PermanentOrigin == nil || *npc.PermanentOrigin != want || npc.Body.RoomID != 1 {
			t.Fatalf("spawn %s origin/body=%+v", id, npc)
		}
	}
	second, ran, err := connector.RunNPCResourceTick(context.Background(), 20*time.Second)
	if err != nil || ran || second.Response != nil || store.commits != 1 {
		t.Fatalf("duplicate receipt=%+v ran=%v commits=%d err=%v", second, ran, store.commits, err)
	}
}

func TestRunNPCResourceTickSkipsUnmigratedWithoutAnonymousAllocation(t *testing.T) {
	initialRaw := npcIdentityTickState(t, false)
	store := &connectorCommandStore{state: initialRaw}
	catalog := npcIdentityTickCatalogFixture()
	allocCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-resource-unmigrated", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: catalog,
		Allocate: func() (string, error) { allocCalls++; return "anonymous", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, ran, err := connector.RunNPCResourceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || store.commits != 1 {
		t.Fatalf("receipt=%+v ran=%v commits=%d err=%v", receipt, ran, store.commits, err)
	}
	var summary npcResourcePhaseSummary
	if err := json.Unmarshal(receipt.Response, &summary); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(summary.Unmigrated, []int16{1, 2}) || len(summary.SpawnedNPCIDs) != 0 || allocCalls != 0 || catalog.monsterCall != 0 {
		t.Fatalf("summary=%+v allocations=%d catalog=%d", summary, allocCalls, catalog.monsterCall)
	}
	if got, _ := store.snapshot(); string(got) != string(initialRaw) {
		t.Fatal("unmigrated tick changed state")
	}
}

func TestRunNPCResourceTickRetriesExactPendingCommand(t *testing.T) {
	base := &connectorCommandStore{state: npcIdentityTickState(t, true)}
	store := &retryTickStore{base: base, failOnce: true}
	allocateAt := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-resource-retry", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: npcIdentityTickCatalogFixture(),
		Roll: npcIdentityTickRoll,
		Allocate: func() (string, error) {
			id := "retry-npc-" + string(rune('0'+allocateAt))
			allocateAt++
			return id, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCResourceTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("first ran=%v err=%v", ran, err)
	}
	if _, ran, err := connector.RunNPCResourceTick(context.Background(), 20*time.Second); err != nil || !ran {
		t.Fatalf("retry ran=%v err=%v", ran, err)
	}
	if len(store.ids) != 2 || store.ids[0] != store.ids[1] || store.ids[0] != "npc-resources-2" {
		t.Fatalf("ids=%v", store.ids)
	}
	if len(store.requests) != 2 || string(store.requests[0]) != string(store.requests[1]) || !strings.Contains(string(store.requests[0]), "npc-resource-phase") || !strings.Contains(string(store.requests[0]), `"now":40`) {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
}

func TestRunNPCResourceSchedulerStopsAfterContextCancellation(t *testing.T) {
	base := &connectorCommandStore{state: npcIdentityTickState(t, true)}
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelOnCommitStore{connectorCommandStore: base, cancel: cancel}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-resource-scheduler", MaxSessions: 1,
		Clock: func() (int32, int) { return 40, 12 }, Catalog: npcIdentityTickCatalogFixture(),
		Roll: npcIdentityTickRoll, Allocate: func() (string, error) { return "scheduler-npc", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.RunNPCResourceScheduler(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if base.commits != 1 || base.command != "npc-resources-0" {
		t.Fatalf("commits=%d command=%q", base.commits, base.command)
	}
}

func TestNPCResourceIntervalRejectsInvalidCadence(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		if _, err := npcResourceIntervalSeconds(interval); err == nil {
			t.Fatalf("interval %s accepted", interval)
		}
	}
	if _, err := npcResourceIntervalSeconds(time.Second); err != nil {
		t.Fatal(err)
	}
}
