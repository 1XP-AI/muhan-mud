package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func repairConnectorState(t *testing.T, mutate func(*world.State)) world.State {
	t.Helper()
	var flags [8]byte
	flags[world.RepairRoomFlag/8] |= 1 << (world.RepairRoomFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "수리점", Flags: flags}},
			PlayerIDs: []string{"a", "b"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, RoomID: 200, Gold: 100,
					Flags: [8]byte{1 / 8: 1 << (1 % 8)},
				},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword": {Object: world.LegacyObject{Name: "검", Type: 4, Value: 100, ShotsMax: 30}},
				}, Inventory: []string{"sword"}},
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

func repairConnectorReady(t *testing.T, state world.State) (*familyMutationReplayStore, *worldConnection, *worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "repair-leftover-world", Clock: func() (int32, int) { return 100, 12 },
		Roll: func(int, int) int {
			t.Fatal("repair leftover consumed connector RNG")
			return 1
		},
		MaxSessions: 2,
	})
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

func TestWorldConnectorSubmitRepairCParityAndReplayDoesNotFanOut(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		mutate func(*world.State)
		want   string
	}{
		{
			name: "bare 수리",
			line: "수리",
			want: world.RepairAskWhatResponse,
		},
		{
			name: "not repair shop",
			line: "수리 검",
			mutate: func(s *world.State) {
				room := s.Rooms[200]
				room.Resource.Flags = [8]byte{}
				s.Rooms[200] = room
			},
			want: world.RepairNotRepairResponse,
		},
		{
			name: "not holding",
			line: "수리 방패",
			want: world.RepairNotHoldingResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, actor, observer := repairConnectorReady(t, repairConnectorState(t, tc.mutate))
			before, _ := store.snapshot()
			output, err := actor.Submit(context.Background(), tc.line)
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d want=%q", output, err, store.commits, tc.want)
			}
			stateRaw, commits := store.snapshot()
			saved, err := world.DecodeState(stateRaw)
			if err != nil || commits != 1 || saved.Players["a"].Body.Gold != 100 || saved.Players["a"].Body.Flags[0]&(1<<1) == 0 {
				t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
			}
			if string(stateRaw) != string(before) {
				t.Fatal("C-print repair changed world snapshot")
			}
			select {
			case event := <-observer.events:
				t.Fatalf("repair leftover fanned out %q", event)
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

func TestWorldConnectorSubmitRepairSuccessStillChargesAndReplays(t *testing.T) {
	raw, err := json.Marshal(repairConnectorState(t, func(s *world.State) {
		actor := s.Players["a"]
		item := actor.Items.Items["sword"]
		item.Object.Name = "NeedleBlade"
		item.Object.Keys[1] = "needle-key"
		actor.Items.Items["sword"] = item
		s.Players["a"] = actor
	}))
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "repair-success-world", Clock: func() (int32, int) { return 100, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			if low == 1 && high == 100 {
				return 100
			}
			return high
		}, MaxSessions: 2,
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
	observerLease, err := connector.owners.Acquire("b")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(observerLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	observer := &worldConnection{game: connector, lease: observerLease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.connections[observer] = struct{}{}
	connector.mu.Unlock()

	line := `수리 "NEEDLE"`
	output, err := actor.Submit(context.Background(), line)
	if err != nil || !strings.Contains(output, "NeedleBlade를 되돌려 줍니다") || store.commits != 1 || rollCalls != 2 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	savedRaw, commits := store.snapshot()
	saved, err := world.DecodeState(savedRaw)
	if err != nil || commits != 1 || saved.Players["a"].Body.Gold != 75 || saved.Players["a"].Items.Items["sword"].Object.ShotsCurrent != 27 {
		t.Fatalf("saved=%+v err=%v commits=%d", saved.Players["a"], err, commits)
	}
	if saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatal("successful repair did not F_CLR PHIDDN")
	}
	replay, err := actor.Submit(context.Background(), line)
	if err != nil || replay != output || store.commits != 1 || rollCalls != 2 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
}
