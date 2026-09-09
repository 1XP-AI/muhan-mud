package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteBroadcastLinePersistsAndReplaysWithoutRecharging(t *testing.T) {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"a"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 20, HPMax: 100, HPCurrent: 100, Flags: [8]byte{6: 1 << (49 % 8)}, Daily: [10]world.LegacyDaily{{Max: 2, Current: 2, LastTime: 1000}}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	options := world.BroadcastOptions{Now: 2000, LastAt: 0, GlobalAt: 0}
	first, err := owners.ExecuteBroadcastLine(context.Background(), store, "w", "broadcast-1", lease, "잡담 안녕하세요", options)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.BroadcastResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || !strings.Contains(result.Response, "Alice> 안녕하세요") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.HPCurrent != 98 || saved.Players["a"].Body.Daily[0].Current != 1 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"].Body, err)
	}
	replay, err := owners.ExecuteBroadcastLine(context.Background(), store, "w", "broadcast-1", lease, "잡담 안녕하세요", options)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBroadcastLineRejectsUnknownAliasBeforeReceipt(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBroadcastLine(context.Background(), store, "w", "broadcast-unsupported", lease, "외쳐 안녕", world.BroadcastOptions{}); err != ErrUnsupportedBroadcastLine || store.commits != 0 {
		t.Fatalf("err=%v commits=%d", err, store.commits)
	}
}
