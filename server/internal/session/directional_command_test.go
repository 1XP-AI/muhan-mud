package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func directionalCommandFixture() []byte {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteDirectionalLineCommitsCanonicalMovementAndReplays(t *testing.T) {
	store := &departureStore{state: directionalCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-1", lease, "  8  북문", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.Response), "도착지") || store.commits != 1 {
		t.Fatalf("response=%q commits=%d", first.Response, store.commits)
	}
	state, err := world.DecodeState(store.state)
	if err != nil || state.Players["a"].Body.RoomID != 2 || len(state.Rooms[1].PlayerIDs) != 0 || len(state.Rooms[2].PlayerIDs) != 1 {
		t.Fatalf("movement state %+v %v", state, err)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-1", lease, "  8  북문", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectionalLineRejectsNonMovementWithoutCommit(t *testing.T) {
	store := &departureStore{state: directionalCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "bad", lease, "봐", 100, 12, world.SceneOptions{}, nil, nil, nil); err == nil || store.commits != 0 {
		t.Fatalf("unsupported direction committed: %v commits=%d", err, store.commits)
	}
}
