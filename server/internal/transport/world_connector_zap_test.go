package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorZapState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
				PlayerIDs: []string{"actor", "observer"},
				NPCIDs:    []string{"wolf"},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "동굴"}},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
				PlayerIDs: []string{"away"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"wand": {Object: world.LegacyObject{Name: "회복봉", Type: world.ZapWandType, ShotsCurrent: 2, ShotsMax: 2, MagicPower: 1}}},
					Inventory: []string{"wand"},
				},
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Level: 8, HPMax: 20, HPCurrent: 5},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: []string{}},
			},
			"away": {
				Body:   world.LegacyMonster{Name: "Carol", Type: 0, RoomID: 2, Level: 8, HPMax: 20, HPCurrent: 12},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: []string{}},
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 16, HPCurrent: 8}},
		},
	}
}

func TestWorldConnectorSubmitDispatchesZapAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorZapState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	rolls := []int{1, 4, 1, 4}
	at := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "zap", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 3,
		Roll: func(low, high int) int {
			if at >= len(rolls) {
				t.Fatalf("unexpected roll %d..%d", low, high)
			}
			n := rolls[at]
			at++
			return n
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"actor", "observer", "away"} {
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

	output, err := connections["actor"].Submit(context.Background(), "회복봉 Bob zap")
	want := world.ZapVigorTargetResponse("Bob")
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("zap=%q err=%v commits=%d", output, err, store.commits)
	}
	events := world.ZapVigorTargetEvents(1, "actor", "Alice", "observer", "Bob", true)
	select {
	case event := <-connections["observer"].events:
		if event != events[0].Text {
			t.Fatalf("target event=%q want=%q", event, events[0].Text)
		}
	default:
		t.Fatal("target missing event")
	}
	select {
	case event := <-connections["away"].events:
		t.Fatalf("other room received %q", event)
	default:
	}

	replay, err := connections["actor"].Submit(context.Background(), "회복봉 Bob zap")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-connections["observer"].events:
		t.Fatalf("replay fanned out: %q", event)
	default:
	}
	if at != 2 {
		t.Fatalf("replay re-rolled RNG: draws=%d", at)
	}

	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["observer"].Body.HPCurrent != 9 || saved.Players["actor"].Items.Items["wand"].Object.ShotsCurrent != 1 {
		t.Fatalf("saved actor=%+v observer=%+v", saved.Players["actor"], saved.Players["observer"])
	}
}
