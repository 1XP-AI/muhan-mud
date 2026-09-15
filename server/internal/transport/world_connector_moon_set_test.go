package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorMoonSetState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			7: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 7, Name: "숲"}},
				PlayerIDs: []string{"actor", "observer"},
			},
			8: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 8, Name: "동굴"}},
				PlayerIDs: []string{"away"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 7},
				Online: true,
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"stone": {Object: world.LegacyObject{Name: "초인의 돌", Keys: [3]string{"귀환석", "", ""}, Value: 1001}}},
					Inventory: []string{"stone"},
				},
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 7},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: []string{}},
			},
			"away": {
				Body:   world.LegacyMonster{Name: "Carol", Type: 0, RoomID: 8},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}, Inventory: []string{}},
			},
		},
	}
}

func TestWorldConnectorSubmitDispatchesMoonSetAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorMoonSetState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "moon-set", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 3,
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

	output, err := connections["actor"].Submit(context.Background(), "귀환 기억")
	want := world.MoonSetBindResponse()
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("bind=%q err=%v commits=%d", output, err, store.commits)
	}
	events := world.MoonSetBindEvents(7, "actor", "Alice")
	for i, wantEvent := range events {
		select {
		case event := <-connections["observer"].events:
			if event != wantEvent.Text {
				t.Fatalf("observer event %d=%q want=%q", i, event, wantEvent.Text)
			}
		default:
			t.Fatalf("observer missing event %d", i)
		}
	}
	select {
	case event := <-connections["away"].events:
		t.Fatalf("other room received %q", event)
	default:
	}

	replay, err := connections["actor"].Submit(context.Background(), "귀환 기억")
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

	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	item := saved.Players["actor"].Items.Items["stone"]
	if item.Object.Value != 7 || item.Object.Keys[1] != "숲" || item.Object.Description != "숲의 광경이 어른거립니다." {
		t.Fatalf("saved item=%+v", item.Object)
	}
}
