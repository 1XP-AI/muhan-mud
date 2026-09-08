package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorSettingsFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b", "c"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesSettingsResponse(t *testing.T) {
	store := connectorSettingsFixture(t)
	_, actor, _, _ := connectorThreeWorldConnections(t, store, "settings-world")
	text, err := actor.Submit(context.Background(), "설정 색")
	if err != nil || !strings.Contains(text, "색        :  사용 ") || store.commits != 1 {
		t.Fatalf("settings response=%q err=%v commits=%d", text, err, store.commits)
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || saved.Players["a"].Body.Flags[26/8]&(1<<(26%8)) == 0 {
		t.Fatalf("settings state=%+v err=%v", saved.Players["a"], err)
	}
}
