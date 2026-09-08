package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDoorFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	room := state.Rooms[1]
	room.Resource.Exits = []world.LegacyExit{{Name: "북", Flags: [4]byte{40}}}
	state.Rooms[1] = room
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitDispatchesDoorResponseAndRoomEvent(t *testing.T) {
	store := connectorDoorFixture(t)
	_, actor, observer, _ := connectorThreeWorldConnections(t, store, "door-world")
	text, err := actor.Submit(context.Background(), "열어 북")
	if err != nil || !strings.Contains(text, "북쪽 출구를 열었습니다") || store.commits != 1 {
		t.Fatalf("door response=%q err=%v commits=%d", text, err, store.commits)
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "출구를 열었습니다") {
			t.Fatalf("door event=%q", event)
		}
	default:
		t.Fatal("door room event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own door event=%q", event)
	default:
	}
}

func connectorDoorKeyFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	store := connectorDoorFixture(t)
	stateRaw, _ := store.snapshot()
	state, err := world.DecodeState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	room := state.Rooms[1]
	room.Resource.Exits[0] = world.LegacyExit{Name: "북", Flags: [4]byte{0x3c}, Key: 7}
	state.Rooms[1] = room
	actor := state.Players["a"]
	actor.Body.Class = 8
	actor.Body.Level = 4
	actor.Body.Stats[1] = 20
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{
		"key-1": {Object: world.LegacyObject{Name: "열쇠", Type: 11, DiceCount: 7, ShotsCurrent: 2}},
	}, Inventory: []string{"key-1"}}
	state.Players["a"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	return store
}

func TestWorldConnectorSubmitDispatchesDoorKeyEventsAndPickResult(t *testing.T) {
	store := connectorDoorKeyFixture(t)
	connector, actor, observer, _ := connectorThreeWorldConnections(t, store, "door-key-world")
	connector.config.Roll = func(_, _ int) int { return 1 }
	text, err := actor.Submit(context.Background(), "풀어 북 열쇠")
	if err != nil || !strings.Contains(text, "찰칵") {
		t.Fatalf("unlock response=%q err=%v", text, err)
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice이 북쪽 출구를 풀었습니다.\r\n" {
			t.Fatalf("unlock event=%q", event)
		}
	default:
		t.Fatal("unlock room event missing")
	}
	text, err = actor.Submit(context.Background(), "잠궈 북 열쇠")
	if err != nil || !strings.Contains(text, "찰칵") {
		t.Fatalf("lock response=%q err=%v", text, err)
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice이 북쪽 출구를 잠궜습니다.\r\n" {
			t.Fatalf("lock event=%q", event)
		}
	default:
		t.Fatal("lock room event missing")
	}
	text, err = actor.Submit(context.Background(), "따 북")
	if err != nil || !strings.Contains(text, "문을 따는데 성공했습니다") {
		t.Fatalf("pick response=%q err=%v", text, err)
	}
	for _, want := range []string{"\nAlice이 북쪽 출구를 따려고 합니다.\r\n", "\nAlice이 문을 땄습니다.\r\n"} {
		select {
		case event := <-observer.events:
			if event != want {
				t.Fatalf("pick event=%q want=%q", event, want)
			}
		default:
			t.Fatalf("pick room event missing: %q", want)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own door-key event=%q", event)
	default:
	}
}
