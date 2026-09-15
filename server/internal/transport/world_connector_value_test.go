package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesValueAndKeepsSnapshotReadOnly(t *testing.T) {
	var flags [8]byte
	flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: flags}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword-1": {Object: world.LegacyObject{Name: "검", Value: 100}},
					"sword-2": {Object: world.LegacyObject{Name: "검", Value: 300001}},
				}, Inventory: []string{"sword-1", "sword-2"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "value-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
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
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "가격 검 2")
	if err != nil || !strings.Contains(output, "100000냥") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Items.Items["sword-2"].Object.Value != 300001 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	unsupported, err := connection.Submit(context.Background(), "가격 검 0")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 1 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func valueConnectorState(t *testing.T, mutate func(*world.State)) world.State {
	t.Helper()
	var flags [8]byte
	flags[world.RoomPawnFlag/8] |= 1 << (world.RoomPawnFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{200: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 200, Name: "전당포", Flags: flags}},
			PlayerIDs: []string{"a", "b"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 200},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"sword-1": {Object: world.LegacyObject{Name: "검", Value: 100}},
					"sword-2": {Object: world.LegacyObject{Name: "검", Value: 300001}},
				}, Inventory: []string{"sword-1", "sword-2"}},
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

func valueConnectorReady(t *testing.T, state world.State) (*familyMutationReplayStore, *worldConnection, *worldConnection) {
	t.Helper()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "value-leftover-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2})
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

func TestWorldConnectorSubmitValueCParityAndReplayDoesNotFanOut(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		mutate func(*world.State)
		want   string
	}{
		{
			name: "not service",
			line: "가치 검",
			mutate: func(s *world.State) {
				room := s.Rooms[200]
				room.Resource.Flags = [8]byte{}
				s.Rooms[200] = room
			},
			want: world.ValueNotServiceResponse,
		},
		{
			name: "bare 가치",
			line: "가치",
			want: world.ValueAskWhatResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, actor, observer := valueConnectorReady(t, valueConnectorState(t, tc.mutate))
			before, _ := store.snapshot()
			output, err := actor.Submit(context.Background(), tc.line)
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("output=%q err=%v commits=%d want=%q", output, err, store.commits, tc.want)
			}
			stateRaw, commits := store.snapshot()
			saved, err := world.DecodeState(stateRaw)
			if err != nil || commits != 1 || saved.Players["a"].Items.Items["sword-2"].Object.Value != 300001 {
				t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
			}
			if string(stateRaw) != string(before) {
				t.Fatal("C-print value changed world snapshot")
			}
			select {
			case event := <-observer.events:
				t.Fatalf("value leftover fanned out %q", event)
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

func TestWorldConnectorSubmitValueNotHoldingClearsPHIDDNAndReplayDoesNotRecommit(t *testing.T) {
	store, actor, observer := valueConnectorReady(t, valueConnectorState(t, func(s *world.State) {
		player := s.Players["a"]
		player.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
		s.Players["a"] = player
	}))
	output, err := actor.Submit(context.Background(), "가치 방패")
	if err != nil || output != world.ValueNotHoldingResponse || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 1 || saved.Players["a"].Items.Items["sword-2"].Object.Value != 300001 || saved.Players["a"].Body.Flags[0]&(1<<1) != 0 {
		t.Fatalf("saved=%+v err=%v commits=%d", saved, err, commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("not-holding leftover fanned out %q", event)
	default:
	}
	replay, err := actor.Submit(context.Background(), "가치 방패")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	replayRaw, replayCommits := store.snapshot()
	replayed, err := world.DecodeState(replayRaw)
	if err != nil || replayCommits != 1 || replayed.Players["a"].Body.Flags[0]&(1<<1) != 0 || replayed.Players["a"].Items.Items["sword-2"].Object.Value != 300001 {
		t.Fatalf("replay re-cleared or re-committed: %+v err=%v commits=%d", replayed, err, replayCommits)
	}
}

func TestWorldConnectorSubmitValueQuotesAndReplayDoesNotFanOut(t *testing.T) {
	store, actor, observer := valueConnectorReady(t, valueConnectorState(t, nil))
	output, err := actor.Submit(context.Background(), "가격 검 2")
	if err != nil || !strings.Contains(output, "100000냥") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("quote fanned out %q", event)
	default:
	}
	replay, err := actor.Submit(context.Background(), "가격 검 2")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("replay fanned out %q", event)
	default:
	}

	repairStore, repairActor, repairObserver := valueConnectorReady(t, valueConnectorState(t, func(s *world.State) {
		var flags [8]byte
		flags[world.RoomRepairFlag/8] |= 1 << (world.RoomRepairFlag % 8)
		room := s.Rooms[200]
		room.Resource.Flags = flags
		s.Rooms[200] = room
	}))
	repairOut, err := repairActor.Submit(context.Background(), "가치 검")
	if err != nil || !strings.Contains(repairOut, "25냥") || repairStore.commits != 1 {
		t.Fatalf("repair=%q err=%v commits=%d", repairOut, err, repairStore.commits)
	}
	select {
	case event := <-repairObserver.events:
		t.Fatalf("repair quote fanned out %q", event)
	default:
	}
}

func TestWorldConnectorSubmitValuePrefixAndKeySelectorReplaysWithoutDuplicateCommit(t *testing.T) {
	store, actor, observer := valueConnectorReady(t, valueConnectorState(t, func(s *world.State) {
		player := s.Players["a"]
		first := player.Items.Items["sword-1"]
		first.Object.Name = "NeedleFirst"
		first.Object.Keys[0] = "needle-zero"
		player.Items.Items["sword-1"] = first
		second := player.Items.Items["sword-2"]
		second.Object.Name = "SecondBlade"
		second.Object.Keys[1] = "needle-one"
		player.Items.Items["sword-2"] = second
		s.Players["a"] = player
	}))
	line := `가격 "NEEDLE" 2`
	output, err := actor.Submit(context.Background(), line)
	if err != nil || !strings.Contains(output, "SecondBlade") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	replay, err := actor.Submit(context.Background(), line)
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("value selector fanned out %q", event)
	default:
	}
}

func TestWorldConnectorSubmitValueUnmigratedItemsFailClosed(t *testing.T) {
	store, actor, _ := valueConnectorReady(t, valueConnectorState(t, func(s *world.State) {
		actor := s.Players["a"]
		actor.Items = nil
		s.Players["a"] = actor
	}))
	if _, err := actor.Submit(context.Background(), "가치 검"); err == nil {
		t.Fatal("unmigrated items unexpectedly valued")
	}
	if store.commits != 0 {
		t.Fatalf("fail-closed value committed=%d", store.commits)
	}
}
