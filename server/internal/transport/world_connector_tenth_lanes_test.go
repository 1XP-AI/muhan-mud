package transport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorTrainingFlags(class byte) (flags [8]byte) {
	flags[world.TrainingRoomFlag/8] |= 1 << (world.TrainingRoomFlag % 8)
	if class > 0 && class <= 8 {
		bits := class - 1
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				bit := world.TrainingRoomFlag + 3 - i
				flags[bit/8] |= 1 << (bit % 8)
			}
		}
	}
	return flags
}

func TestWorldConnectorSubmitDispatchesTimeThroughNewProjection(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, state, "actor")
	connector.config.Clock = func() (int32, int) { return 25, 1 }
	connector.config.WallClock = func() time.Time {
		return time.Date(2026, time.January, 2, 23, 4, 5, 0, time.UTC)
	}
	output, err := connections[0].Submit(context.Background(), "시간")
	if err != nil || !strings.Contains(output, "현재 시간: 오전 1시.") || !strings.Contains(output, "실제 시간: Fri Jan  2 15:04:05 2026 (PST).") || store.commits != 1 {
		t.Fatalf("time output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 1 {
		t.Fatalf("time state=%+v err=%v", saved.Players["actor"], err)
	}
}

func TestWorldConnectorSubmitDispatchesTrainingAndPersistsProgression(t *testing.T) {
	class := byte(4)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: connectorTrainingFlags(class)}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: 1, Experience: 128, Gold: 100}, Online: true},
		},
	}
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, state, "actor")
	output, err := connections[0].Submit(context.Background(), "수련")
	if err != nil || !strings.Contains(output, "축하합니다! 당신의 레벨이 올랐습니다!") || store.commits != 1 {
		t.Fatalf("training output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["actor"].Body
	if actor.Level != 2 || actor.Gold != 94 {
		t.Fatalf("training actor=%+v", actor)
	}
}

func TestWorldConnectorSubmitDispatchesSelectionWithoutMutatingWorld(t *testing.T) {
	var merchantFlags [8]byte
	merchantFlags[world.MerchantPurchaseFlag/8] |= 1 << (world.MerchantPurchaseFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"actor"},
			NPCIDs:    []string{"merchant"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"merchant": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 1, Flags: merchantFlags}},
		},
	}
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, state, "actor")
	connector.config.MerchantOffers = world.MerchantOffers{
		"merchant": {{Name: "검", Value: 7, Weight: 1}, {Name: "보석", Value: 25, Weight: 1}},
	}
	before := append([]byte(nil), store.state...)
	output, err := connections[0].Submit(context.Background(), "선택 상인")
	if err != nil || !strings.Contains(output, "상인의 물건들:") || !strings.Contains(output, "10냥") || !strings.Contains(output, "25냥") || store.commits != 1 {
		t.Fatalf("selection output=%q err=%v commits=%d", output, err, store.commits)
	}
	if string(store.state) != string(before) {
		t.Fatal("selection mutated world state")
	}
}
