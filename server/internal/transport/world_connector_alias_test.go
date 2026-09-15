package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitExpandsStoredAliasBeforeDispatch(t *testing.T) {
	const roomID int16 = 200
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{roomID: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "광장"}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body:    world.LegacyMonster{Name: "Alice", Type: 0, RoomID: roomID},
				Online:  true,
				Aliases: []world.PlayerAlias{{Alias: "t", Process: "시간"}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "alias-world", Clock: func() (int32, int) { return 100, 12 },
		WallClock: func() time.Time {
			return time.Date(2026, time.September, 9, 11, 5, 22, 0, time.FixedZone("PST", -8*60*60))
		},
		MaxSessions: 1,
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
	connection := &worldConnection{game: connector, lease: lease, ready: true}

	first, err := connection.Submit(context.Background(), "t")
	if err != nil || !strings.Contains(first, "현재 시간") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first, err, store.commits)
	}
	repeated, err := connection.Submit(context.Background(), "!")
	if err != nil || repeated != first || store.commits != 2 {
		t.Fatalf("repeat=%q first=%q err=%v commits=%d", repeated, first, err, store.commits)
	}
}
