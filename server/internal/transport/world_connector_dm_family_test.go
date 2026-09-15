package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type dmFamilyTransportCatalog struct{}

func (dmFamilyTransportCatalog) Object(int16) (world.LegacyObject, error) {
	return world.LegacyObject{Name: "초인의 돌", Weight: 1, ShotsMax: 10, ShotsCurrent: 10}, nil
}

func (dmFamilyTransportCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{
		Name: "침략자", Type: 1, Class: 4, Level: 1, Armor: 100,
		HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4,
	}, nil
}

func TestWorldConnectorSubmitDispatchesDMMoonstoneAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	dm := world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"dm", "player"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"dm":     {Body: dm, Online: true},
			"player": {Body: world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	n := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-family", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 2,
		Catalog: dmFamilyTransportCatalog{}, Roll: func(lo, hi int) int { return lo },
		Allocate: func() (string, error) { n++; return "stone-1", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"dm", "player"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = conn
		connector.connections[conn] = struct{}{}
	}
	output, err := connections["dm"].Submit(context.Background(), "*떨어져라")
	want := world.DMMoonstoneBroadcast("광장")
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections["player"].events:
		if event != want {
			t.Fatalf("event=%q", event)
		}
	default:
		t.Fatal("player missing moonstone broadcast")
	}
	replay, err := connections["dm"].Submit(context.Background(), "*떨어져라")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay commits=%d", commits)
	}
	select {
	case event := <-connections["player"].events:
		t.Fatalf("replay fanned out: %q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesDMInvasionCombatReady(t *testing.T) {
	store := &connectorCommandStore{}
	dm := world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}
	fighter := world.LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, Level: 1, RoomID: 3601,
		Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40,
		DiceCount: 1, DiceSides: 5,
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			3601: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3601, Name: "무적존"}},
				PlayerIDs: []string{"fighter", "player"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"dm":      {Body: dm, Online: true},
			"fighter": {Body: fighter, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"player":  {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, RoomID: 3601}, Online: true},
		},
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	n := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "dm-invasion", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 3,
		Catalog: dmFamilyTransportCatalog{}, Roll: func(lo, hi int) int { return lo },
		Allocate: func() (string, error) { n++; return fmt.Sprintf("inv-%d", n), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"dm", "fighter", "player"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = conn
		connector.connections[conn] = struct{}{}
	}
	output, err := connections["dm"].Submit(context.Background(), "*침공")
	want := world.DMInvasionBroadcast1 + world.DMInvasionBroadcast2
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	var spawned []string
	for id, npc := range saved.NPCs {
		if npc.Enemies == nil {
			t.Fatalf("transport persisted NPC %q with nil Enemies", id)
		}
		spawned = append(spawned, id)
	}
	if len(spawned) != 10 {
		t.Fatalf("spawned=%d", len(spawned))
	}
	if _, _, err := saved.PlanNPCMeleeAttack("fighter", spawned[0], func(_, hi int) int { return hi }); err != nil {
		t.Fatalf("melee against transport invasion NPC: %v", err)
	}
	select {
	case event := <-connections["player"].events:
		if event != world.DMInvasionBroadcast1 && event != world.DMInvasionBroadcast2 {
			t.Fatalf("event=%q", event)
		}
	default:
		t.Fatal("player missing invasion broadcast")
	}
}
