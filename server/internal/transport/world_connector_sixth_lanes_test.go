package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesPowerAccurateAndMeditateLanes(t *testing.T) {
	const roomID int16 = 201
	actors := []string{"power", "accurate", "meditate"}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{roomID: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "수련장"}},
			PlayerIDs: actors,
		}},
		Players: map[string]world.PlayerState{
			"power":    {Body: world.LegacyMonster{Name: "Power", Type: 0, Class: world.PowerClass, Level: 4, RoomID: roomID, Stats: [5]byte{10, 15}}, Online: true},
			"accurate": {Body: world.LegacyMonster{Name: "Accurate", Type: 0, Class: world.AccurateThiefClass, Level: 4, RoomID: roomID, Stats: [5]byte{10, 15}, Thaco: 20}, Online: true},
			"meditate": {Body: world.LegacyMonster{Name: "Meditate", Type: 0, Class: world.ClericClass, Level: 4, RoomID: roomID, Stats: [5]byte{10, 15, 10, 10, 15}}, Online: true},
		},
	}
	accurate := state.Players["accurate"]
	accurate.Items = &world.ItemCollection{Items: map[string]world.Item{
		"blade": {Object: world.LegacyObject{Name: "검", Type: 1}},
	}}
	accurate.Items.Ready[world.WieldSlotIndex] = "blade"
	state.Players["accurate"] = accurate
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "sixth-lanes", Clock: func() (int32, int) { return 2000, 12 }, MaxSessions: len(actors),
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
		{actorID: "power", line: "기공집결", want: "가부좌"},
		{actorID: "accurate", line: "살기충전", want: "피를 먹입니다"},
		{actorID: "meditate", line: "참선", want: "새롭게"},
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
	if saved.Players["power"].Body.Stats[0] != 13 || saved.Players["power"].Body.Flags[world.PowerFlag/8]&(1<<(world.PowerFlag%8)) == 0 {
		t.Fatalf("power=%+v", saved.Players["power"].Body)
	}
	if saved.Players["accurate"].Body.Thaco != 17 || saved.Players["accurate"].Body.Flags[world.AccurateFlag/8]&(1<<(world.AccurateFlag%8)) == 0 {
		t.Fatalf("accurate=%+v", saved.Players["accurate"].Body)
	}
	if saved.Players["meditate"].Body.Stats[3] != 13 || saved.Players["meditate"].Body.Flags[world.MeditateFlag/8]&(1<<(world.MeditateFlag%8)) == 0 {
		t.Fatalf("meditate=%+v", saved.Players["meditate"].Body)
	}
}
