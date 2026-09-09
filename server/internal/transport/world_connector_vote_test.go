package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorVoteStopsBeforeBallotReceiptWhenAuthorityIsMissing(t *testing.T) {
	var flags [8]byte
	flags[world.VoteElectionRoomFlag/8] |= 1 << (world.VoteElectionRoomFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "투표소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body: world.LegacyMonster{
				Name:   "Alice",
				Type:   0,
				Class:  4,
				RoomID: 1,
				Timers: [45]world.LegacyTimer{world.VoteHoursTimerIndex: {Interval: 3 * 86400}},
			},
			Online: true,
		}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "vote-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
		VoteCatalog: world.VoteCatalog{Issue: world.VoteIssue{Number: 1, Prompt: "선택", Options: []string{"A"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 2)}
	connector.mu.Lock()
	connector.connections[connection] = struct{}{}
	connector.mu.Unlock()

	output, err := connection.Submit(context.Background(), "투표")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 || !connection.ready {
		t.Fatalf("output=%q err=%v commits=%d ready=%t", output, err, store.commits, connection.ready)
	}
}
