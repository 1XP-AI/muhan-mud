package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesMailReadAndDelete(t *testing.T) {
	var flags [8]byte
	flags[world.RoomPostOfficeFlag/8] |= 1 << (world.RoomPostOfficeFlag % 8)
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}},
			PlayerIDs: []string{"recipient"},
		}},
		Players: map[string]world.PlayerState{
			"recipient": {Body: world.LegacyMonster{Name: "받는이", RoomID: 1}, Online: true},
			"sender":    {Body: world.LegacyMonster{Name: "보내는이", RoomID: 1}, Online: false},
		},
		Mailboxes: map[string][]world.MailMessage{"recipient": {
			{ID: "mail-1", SenderID: "sender", Body: "우체국 본문\n", Timestamp: time.Date(2026, 9, 9, 1, 2, 3, 0, time.UTC)},
		}},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "mail-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("recipient")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	output, err := connection.Submit(context.Background(), "편지받기")
	if err != nil || !strings.Contains(output, "우체국 본문") || !strings.Contains(output, "보내는이") {
		t.Fatalf("read output=%q err=%v", output, err)
	}
	if store.commits != 1 {
		t.Fatalf("read commits=%d", store.commits)
	}
	output, err = connection.Submit(context.Background(), "편지삭제")
	if err != nil || !strings.Contains(output, "편지가 삭제되었습니다") {
		t.Fatalf("delete output=%q err=%v", output, err)
	}
	if store.commits != 2 {
		t.Fatalf("delete commits=%d", store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Mailboxes["recipient"]) != 0 {
		t.Fatalf("saved mailbox=%v err=%v", saved.Mailboxes["recipient"], err)
	}
	unsupported, err := connection.Submit(context.Background(), "편지보내기")
	if err != nil || !strings.Contains(unsupported, "아직") || store.commits != 2 {
		t.Fatalf("unsupported=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
