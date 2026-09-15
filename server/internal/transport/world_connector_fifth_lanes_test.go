package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesAbilityAndTitleLanes(t *testing.T) {
	const roomID int16 = 200
	actors := []string{"haste", "pray", "prepare", "up-dmg", "title"}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			roomID: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "광장"}},
				PlayerIDs: actors,
			},
		},
		Players: map[string]world.PlayerState{
			"haste":   {Body: world.LegacyMonster{Name: "Haste", Type: 0, Class: world.RangerClass, Level: 4, RoomID: roomID}, Online: true},
			"pray":    {Body: world.LegacyMonster{Name: "Pray", Type: 0, Class: world.ClericClass, Level: 4, RoomID: roomID}, Online: true},
			"prepare": {Body: world.LegacyMonster{Name: "Prepare", Type: 0, Class: 4, Level: 4, RoomID: roomID}, Online: true},
			"up-dmg":  {Body: world.LegacyMonster{Name: "UpDmg", Type: 0, Class: world.InvincibleClass, Level: 4, RoomID: roomID}, Online: true},
			"title":   {Body: world.LegacyMonster{Name: "Title", Type: 0, Class: 4, Level: 4, RoomID: roomID}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "fifth-lanes", Clock: func() (int32, int) { return 2000, 12 }, MaxSessions: len(actors),
		Roll: func(low, high int) int {
			if low == 1 && high == 100 {
				return 1
			}
			return high
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make(map[string]*worldConnection, len(actors))
	for _, actorID := range actors {
		lease, acquireErr := connector.owners.Acquire(actorID)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections[actorID] = &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()

	tests := []struct {
		actorID string
		line    string
		want    string
	}{
		{actorID: "haste", line: "활보법", want: "민첩"},
		{actorID: "pray", line: "신원법", want: "신앙심"},
		{actorID: "prepare", line: "경계", want: "함정"},
		{actorID: "up-dmg", line: "잠력격발", want: "잠력을"},
		{actorID: "title", line: "칭호 새 수호자", want: "새 수호자"},
	}
	for _, tt := range tests {
		output, submitErr := connections[tt.actorID].Submit(context.Background(), tt.line)
		if submitErr != nil || !strings.Contains(output, tt.want) {
			t.Fatalf("actor=%s line=%q output=%q err=%v", tt.actorID, tt.line, output, submitErr)
		}
	}
	if store.commits != len(tests) {
		t.Fatalf("commits=%d want=%d", store.commits, len(tests))
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["title"].Title != "새 수호자" {
		t.Fatalf("title=%q", saved.Players["title"].Title)
	}
	if !strings.Contains(saved.Players["prepare"].Body.Name, "Prepare") {
		t.Fatal("prepare actor missing after durable commands")
	}
}
