package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestWorldConnectorSubmitDispatchesUseAndPublishesEvent(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "약방"}},
			PlayerIDs: []string{"actor", "observer"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"potion": {Object: world.LegacyObject{Name: "회복약", Type: world.DrinkPotionType, MagicPower: 1, ShotsCurrent: 1}},
				}, Inventory: []string{"potion"}},
			},
			"observer": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, RoomID: 1, Level: 1, HPMax: 30, HPCurrent: 30}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, state, "actor", "observer")
	connector.config.Roll = func(_, _ int) int { return 5 }
	output, err := connections[0].Submit(context.Background(), "사용 회복약")
	if err != nil || !strings.Contains(output, "회복약") || store.commits != 1 {
		t.Fatalf("use output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "회복약") {
			t.Fatalf("use observer event=%q", event)
		}
	default:
		t.Fatal("use observer event missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("use actor received duplicate event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesChangeClass(t *testing.T) {
	flags := [8]byte{}
	flags[world.ChangeClassRoomFlag/8] |= 1 << (world.ChangeClassRoomFlag % 8)
	// Destination class 5 is encoded by the first class bit after RTRAIN; the
	// source folds RTRAIN+1 through +3 in increasing order.
	flags[(world.ChangeClassRoomFlag+1)/8] |= 1 << ((world.ChangeClassRoomFlag + 1) % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전직소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 2, Experience: 100000},
			Online: true,
		}},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor")
	output, err := connections[0].Submit(context.Background(), "직업전환 예")
	if err != nil || !strings.Contains(output, "직업이 전환") || store.commits != 1 {
		t.Fatalf("change-class output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Class != 5 || saved.Players["actor"].Body.Experience != 0 {
		t.Fatalf("change-class state=%+v err=%v", saved.Players["actor"].Body, err)
	}
}

func connectorChangeClassState(experience int32) world.State {
	flags := [8]byte{}
	flags[world.ChangeClassRoomFlag/8] |= 1 << (world.ChangeClassRoomFlag % 8)
	// Destination class 5 is encoded by the first class bit after RTRAIN.
	flags[(world.ChangeClassRoomFlag+1)/8] |= 1 << ((world.ChangeClassRoomFlag + 1) % 8)
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "전직소", Flags: flags}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Level: 2, Experience: experience},
			Online: true,
		}},
	}
}

func TestWorldConnectorBareChangeClassUsesLocalConfirmationBeforeReceipt(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorChangeClassState(100000), "actor")

	start, err := connections[0].Submit(context.Background(), "직업전환")
	if err != nil || start != world.ChangeClassPromptResponse || store.commits != 0 || connections[0].compose == nil {
		t.Fatalf("start=%q err=%v commits=%d compose=%+v", start, err, store.commits, connections[0].compose)
	}
	commandID := connections[0].compose.commandID
	if commandID == "" {
		t.Fatal("local confirmation command ID missing")
	}

	cancel, err := connections[0].Submit(context.Background(), "아니오")
	if err != nil || cancel != session.ChangeClassCancelResponse || store.commits != 0 || connections[0].compose != nil {
		t.Fatalf("cancel=%q err=%v commits=%d compose=%+v", cancel, err, store.commits, connections[0].compose)
	}

	start, err = connections[0].Submit(context.Background(), "직업전환")
	if err != nil || start != world.ChangeClassPromptResponse || store.commits != 0 || connections[0].compose == nil {
		t.Fatalf("second start=%q err=%v commits=%d compose=%+v", start, err, store.commits, connections[0].compose)
	}
	secondCommandID := connections[0].compose.commandID
	if secondCommandID == "" || secondCommandID == commandID {
		t.Fatalf("confirmation command ID was reused: first=%q second=%q", commandID, secondCommandID)
	}
	confirmed, err := connections[0].Submit(context.Background(), "예")
	if err != nil || confirmed != world.ChangeClassSuccessResponse || store.commits != 1 || connections[0].compose != nil {
		t.Fatalf("confirmed=%q err=%v commits=%d compose=%+v", confirmed, err, store.commits, connections[0].compose)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Class != 5 || saved.Players["actor"].Body.Experience != 0 {
		t.Fatalf("saved actor=%+v err=%v", saved.Players["actor"].Body, err)
	}
	if connections[0].compose != nil {
		t.Fatal("completed change-class draft retained")
	}
}

func TestWorldConnectorBareChangeClassGateIsReceiptFree(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorChangeClassState(99999), "actor")
	output, err := connections[0].Submit(context.Background(), "직업전환")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 || connections[0].compose != nil {
		t.Fatalf("output=%q err=%v commits=%d compose=%+v", output, err, store.commits, connections[0].compose)
	}
}

func TestWorldConnectorChangeClassConfirmationRetainsStableReceiptOnTransientStoreFailure(t *testing.T) {
	raw, err := json.Marshal(connectorChangeClassState(100000))
	if err != nil {
		t.Fatal(err)
	}
	base := &connectorCommandStore{state: raw}
	store := &composeTransientStore{base: base, fail: true}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "change-class-retry", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
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
	connection := &worldConnection{game: connector, lease: lease, ready: true}
	if _, err := connection.Submit(context.Background(), "직업전환"); err != nil {
		t.Fatal(err)
	}
	commandID := connection.compose.commandID
	first, err := connection.Submit(context.Background(), "예")
	if err != nil || first != session.ChangeClassRetryResponse || connection.compose == nil || store.commitSeen != 0 {
		t.Fatalf("first=%q err=%v compose=%+v commits=%d", first, err, connection.compose, store.commitSeen)
	}
	second, err := connection.Submit(context.Background(), "예")
	if err != nil || second != world.ChangeClassSuccessResponse || connection.compose != nil || store.commitSeen != 1 || len(store.attempts) != 2 || store.attempts[0] != commandID || store.attempts[1] != commandID {
		t.Fatalf("second=%q err=%v compose=%+v seen=%d attempts=%q", second, err, connection.compose, store.commitSeen, store.attempts)
	}
}
