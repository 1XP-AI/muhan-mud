package session

import (
	"context"
	"encoding/json"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"testing"
)

func TestEnterWorldPersistsBeforeReturningAndReplays(t *testing.T) {
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}}
	raw, _ := json.Marshal(s)
	store := &departureStore{state: raw, fail: true}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	r, err := owners.EnterWorld(context.Background(), store, "w", "boot-entry-1", lease, 100, world.SceneOptions{}, nil, nil, nil)
	if err == nil || len(r.Response) != 0 || !owners.Owns(lease) {
		t.Fatal("uncommitted admission returned")
	}
	store.fail = false
	r, err = owners.EnterWorld(context.Background(), store, "w", "boot-entry-1", lease, 100, world.SceneOptions{}, nil, nil, nil)
	if err != nil || r.Revision != 1 || r.Replayed {
		t.Fatalf("%+v %v", r, err)
	}
	again, err := owners.EnterWorld(context.Background(), store, "w", "boot-entry-1", lease, 100, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !again.Replayed || store.commits != 2 || string(r.Response) != string(again.Response) {
		t.Fatal("admission reexecuted")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !saved.Players["a"].Online || saved.Rooms[1].Resource.BeenHere != 1 || len(saved.Rooms[1].PlayerIDs) != 1 {
		t.Fatal("bad admitted state")
	}
}
