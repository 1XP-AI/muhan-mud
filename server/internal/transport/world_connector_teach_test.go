package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesTeachAndProjectsRecipients(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor", "target", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: world.TeachMageClass, Level: 20, RoomID: 1, Spells: [16]byte{1 << 1}},
				Online: true,
			},
			"target":   {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor", "target", "observer")

	output, err := connections[0].Submit(context.Background(), "가르쳐 Bob 삭풍")
	if err != nil || !strings.Contains(output, "삭풍") || store.commits != 1 {
		t.Fatalf("teach output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "삭풍") {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target teach event missing")
	}
	select {
	case event := <-connections[2].events:
		if !strings.Contains(event, "Bob") || !strings.Contains(event, "삭풍") {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("observer teach event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("actor received duplicate event=%q", event)
	default:
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["target"].Body.Spells[0]&(1<<1) == 0 || store.commits != 1 {
		t.Fatalf("teach state=%+v err=%v commits=%d", saved.Players["target"], err, store.commits)
	}
}
