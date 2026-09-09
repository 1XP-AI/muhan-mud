package transport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func npcMaintenanceTickState(t *testing.T) json.RawMessage {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}, Traffic: 100},
				PlayerIDs: []string{"hero"},
				NPCIDs:    []string{"wanderer"},
			},
		},
		Players: map[string]world.PlayerState{
			"hero": {Body: world.LegacyMonster{Name: "영웅", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wanderer": {
				Body:    world.LegacyMonster{Name: "방황자", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, MPMax: 10, MPCurrent: 10},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"wanderer"},
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

func TestRunNPCMaintenanceTickPersistsAndSuppressesDuplicate(t *testing.T) {
	base := &connectorCommandStore{state: npcMaintenanceTickState(t)}
	store := &retryTickStore{base: base}
	clockNow := int32(103)
	rollCalls := 0
	roll := func(low, high int) int {
		rollCalls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected wander range %d..%d", low, high)
		}
		return 1
	}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-maintenance-world", MaxSessions: 1,
		Clock: func() (int32, int) { return clockNow, 12 }, Roll: roll,
	})
	if err != nil {
		t.Fatal(err)
	}

	first, ran, err := connector.RunNPCMaintenanceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed || base.commits != 1 {
		t.Fatalf("first=%+v ran=%v commits=%d err=%v", first, ran, base.commits, err)
	}
	if len(store.ids) != 1 || store.ids[0] != "npc-maintenance-5" {
		t.Fatalf("command IDs=%v", store.ids)
	}
	var request npcMaintenanceTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-maintenance" || request.Slot != 5 || request.Now != 100 {
		t.Fatalf("request=%+v", request)
	}
	var result world.NPCMaintenanceResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Now != 100 || len(result.Actions) != 1 || !result.Actions[0].Wandered || len(result.ActiveNPCIDs) != 0 {
		t.Fatalf("result=%+v", result)
	}
	if rollCalls != 1 {
		t.Fatalf("roll calls=%d", rollCalls)
	}

	duplicate, ran, err := connector.RunNPCMaintenanceTick(context.Background(), 20*time.Second)
	if err != nil || ran || duplicate.Response != nil || base.commits != 1 {
		t.Fatalf("duplicate=%+v ran=%v commits=%d err=%v", duplicate, ran, base.commits, err)
	}
	stateRaw, _ := base.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.NPCs) != 0 || !reflect.DeepEqual(saved.ActiveNPCIDs, []string{}) {
		t.Fatalf("saved NPC state=%+v active=%v", saved.NPCs, saved.ActiveNPCIDs)
	}
}

func TestRunNPCMaintenanceTickReplaysDurableReceiptAcrossConnector(t *testing.T) {
	base := &connectorCommandStore{state: npcMaintenanceTickState(t)}
	clockNow := int32(103)
	rollCalls := 0
	roll := func(int, int) int {
		rollCalls++
		return 1
	}
	config := WorldConnectorConfig{
		Store: base, WorldID: "npc-maintenance-replay", MaxSessions: 1,
		Clock: func() (int32, int) { return clockNow, 12 }, Roll: roll,
	}
	firstConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	first, ran, err := firstConnector.RunNPCMaintenanceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed {
		t.Fatalf("first=%+v ran=%v err=%v", first, ran, err)
	}
	secondConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	replay, ran, err := secondConnector.RunNPCMaintenanceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed || base.commits != 1 {
		t.Fatalf("replay=%+v ran=%v commits=%d err=%v", replay, ran, base.commits, err)
	}
	if string(replay.Response) != string(first.Response) || rollCalls != 1 {
		t.Fatalf("replay response/rolls differ: response=%v rolls=%d", string(replay.Response), rollCalls)
	}
}

func TestRunNPCMaintenanceTickRetainsExactPendingRequest(t *testing.T) {
	base := &connectorCommandStore{state: npcMaintenanceTickState(t)}
	store := &retryTickStore{base: base, failOnce: true}
	clockNow := int32(103)
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-maintenance-retry", MaxSessions: 1,
		Clock: func() (int32, int) { return clockNow, 12 }, Roll: func(int, int) int { return 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCMaintenanceTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("first ran=%v err=%v", ran, err)
	}
	clockNow = 119
	retry, ran, err := connector.RunNPCMaintenanceTick(context.Background(), 20*time.Second)
	if err != nil || !ran || retry.Replayed || base.commits != 1 {
		t.Fatalf("retry=%+v ran=%v commits=%d err=%v", retry, ran, base.commits, err)
	}
	if len(store.ids) != 2 || store.ids[0] != "npc-maintenance-5" || store.ids[1] != store.ids[0] {
		t.Fatalf("command IDs=%v", store.ids)
	}
	if len(store.requests) != 2 || string(store.requests[0]) != string(store.requests[1]) {
		t.Fatalf("requests differ: %s / %s", store.requests[0], store.requests[1])
	}
	var request npcMaintenanceTickRequest
	if err := json.Unmarshal(store.requests[1], &request); err != nil {
		t.Fatal(err)
	}
	if request.Slot != 5 || request.Now != 100 {
		t.Fatalf("retry request=%+v", request)
	}
}

func TestRunNPCMaintenanceSchedulerStopsAfterContextCancellation(t *testing.T) {
	base := &connectorCommandStore{state: npcMaintenanceTickState(t)}
	ctx, cancel := context.WithCancel(context.Background())
	store := &cancelOnCommitStore{connectorCommandStore: base, cancel: cancel}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-maintenance-scheduler", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Roll: func(int, int) int { return 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.RunNPCMaintenanceScheduler(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if base.commits != 1 || base.command != "npc-maintenance-0" {
		t.Fatalf("commits=%d command=%q", base.commits, base.command)
	}
}

func TestNPCMaintenanceIntervalRejectsInvalidCadence(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second, 500 * time.Millisecond} {
		if _, err := npcMaintenanceIntervalSeconds(interval); err == nil {
			t.Fatalf("interval %s accepted", interval)
		}
	}
	if _, err := npcMaintenanceIntervalSeconds(time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestRunNPCMaintenanceTickHonorsCanceledContext(t *testing.T) {
	base := &connectorCommandStore{state: npcMaintenanceTickState(t)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: base, WorldID: "npc-maintenance-canceled", MaxSessions: 1,
		Clock: func() (int32, int) { return 103, 12 }, Roll: func(int, int) int { return 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ran, err := connector.RunNPCMaintenanceTick(ctx, time.Second); !errors.Is(err, context.Canceled) || ran || base.commits != 0 {
		t.Fatalf("canceled tick ran=%v commits=%d err=%v", ran, base.commits, err)
	}
}
