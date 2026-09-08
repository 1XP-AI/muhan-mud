package session

import (
	"context"
	"encoding/json"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
	"strings"
	"testing"
)

func TestExecuteLookLineReturnsCurrentRoomThroughReceipt(t *testing.T) {
	s := world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}, Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}}}
	raw, _ := json.Marshal(s)
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	first, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-1", lease, "봐", 12)
	if err != nil {
		t.Fatal(err)
	}
	var scene string
	if err := json.Unmarshal(first.Response, &scene); err != nil || !strings.Contains(scene, "광장") {
		t.Fatalf("%q %v", scene, err)
	}
	again, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-1", lease, "봐", 12)
	if err != nil || !again.Replayed || store.commits != 1 {
		t.Fatal("look receipt reexecuted")
	}
	if string(store.state) != string(raw) {
		t.Fatal("look mutated world")
	}
	for _, line := range []string{"검 조사", "조사 검", "북", "봐\n봐"} {
		if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "other", lease, line, 12); err == nil {
			t.Fatalf("unsupported command accepted %q", line)
		}
	}
}
