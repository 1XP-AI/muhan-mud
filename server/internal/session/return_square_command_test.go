package session

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func returnSquareSessionState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			7:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 7}}, PlayerIDs: []string{"actor"}},
			1001: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001}}},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Mina", RoomID: 7, MPCurrent: 12}, Online: true},
		},
	}
}

func TestParseReturnSquareLineAdmitsOnlyExactAliases(t *testing.T) {
	for _, line := range []string{"귀환", "귀", "  귀환  "} {
		command, ok := ParseReturnSquareLine(line)
		if !ok || (command.Alias != "귀환" && command.Alias != "귀") {
			t.Fatalf("line=%q command=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"", "귀환 지금", "귀환!", "귀환\n", "귀환\x00", "귀환\u200b"} {
		if _, ok := ParseReturnSquareLine(line); ok {
			t.Fatalf("unsupported line accepted: %q", line)
		}
	}
}

func TestExecuteReturnSquareLinePersistsTypedResultAndReplays(t *testing.T) {
	initial := returnSquareSessionState()
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteReturnSquareLine(context.Background(), store, "world", "return-1", lease, "귀환")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ReturnSquareResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Moved || result.DestinationRoomID != 1001 || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 1001 || len(saved.Rooms[7].PlayerIDs) != 0 || !reflect.DeepEqual(saved.Rooms[1001].PlayerIDs, []string{"actor"}) {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}

	again, err := owners.ExecuteReturnSquareLine(context.Background(), store, "world", "return-1", lease, "귀환")
	if err != nil || !again.Replayed || store.commits != 1 || !reflect.DeepEqual(again.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", again, err, store.commits)
	}
}

func TestExecuteReturnSquareLineCommitsDenialAsNoOpReceipt(t *testing.T) {
	state := returnSquareSessionState()
	actor := state.Players["actor"]
	actor.FollowerIDs = []string{"actor-follower"}
	state.Players["actor"] = actor
	state.Players["actor-follower"] = world.PlayerState{Body: world.LegacyMonster{Name: "F", RoomID: 7}, Online: true, FollowingID: "actor"}
	room := state.Rooms[7]
	room.PlayerIDs = []string{"actor", "actor-follower"}
	state.Rooms[7] = room
	raw, _ := json.Marshal(state)
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	receipt, err := owners.ExecuteReturnSquareLine(context.Background(), store, "world", "return-denied", lease, "귀")
	if err != nil || receipt.Replayed || store.commits != 1 {
		t.Fatalf("receipt=%+v err=%v commits=%d", receipt, err, store.commits)
	}
	var result world.ReturnSquareResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil || result.Moved || result.Response != "먼저 그룹에서 나오세요.\r\n" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if string(store.state) != string(raw) {
		t.Fatal("denial changed world bytes")
	}
}
