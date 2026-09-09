package transport

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresSixthLanesPowerAccurateMeditatePersistAndReplay(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	const roomID int16 = 201
	players := []string{"power", "accurate", "meditate"}
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{roomID: serviceRoomState(roomID, "수련장", [8]byte{}, players...)},
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
	worldID := fmt.Sprintf("service-sixth-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	roll := func(low, high int) int {
		if low == 1 && high == 100 {
			return 1
		}
		return high
	}

	powerLease := serviceCommandLease(t, owners, "power")
	firstPower, err := owners.ExecutePowerLine(ctx, store, worldID, "sixth-power-1", powerLease, "기공집결", 2000, roll)
	if err != nil || firstPower.Replayed || firstPower.Revision != 1 {
		t.Fatalf("power=%+v err=%v", firstPower, err)
	}
	replayPower, err := owners.ExecutePowerLine(ctx, store, worldID, "sixth-power-1", powerLease, "기공집결", 2000, func(int, int) int { t.Fatal("power replay rerolled"); return 0 })
	if err != nil || !replayPower.Replayed || string(replayPower.Response) != string(firstPower.Response) {
		t.Fatalf("power replay=%+v err=%v", replayPower, err)
	}

	accurateLease := serviceCommandLease(t, owners, "accurate")
	accurateReceipt, err := owners.ExecuteAccurateLine(ctx, store, worldID, "sixth-accurate-1", accurateLease, "살기충전", 2000, roll)
	if err != nil || accurateReceipt.Replayed || accurateReceipt.Revision != 2 {
		t.Fatalf("accurate=%+v err=%v", accurateReceipt, err)
	}
	meditateLease := serviceCommandLease(t, owners, "meditate")
	meditateReceipt, err := owners.ExecuteMeditateLine(ctx, store, worldID, "sixth-meditate-1", meditateLease, "참선", 2000, roll)
	if err != nil || meditateReceipt.Replayed || meditateReceipt.Revision != 3 {
		t.Fatalf("meditate=%+v err=%v", meditateReceipt, err)
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil || snapshot.Revision != 3 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["power"].Body.Stats[0] != 13 || saved.Players["power"].Body.Flags[world.PowerFlag/8]&(1<<(world.PowerFlag%8)) == 0 {
		t.Fatalf("power state=%+v", saved.Players["power"].Body)
	}
	if saved.Players["accurate"].Body.Thaco != 17 || saved.Players["accurate"].Body.Flags[world.AccurateFlag/8]&(1<<(world.AccurateFlag%8)) == 0 {
		t.Fatalf("accurate state=%+v", saved.Players["accurate"].Body)
	}
	if saved.Players["meditate"].Body.Stats[3] != 13 || saved.Players["meditate"].Body.Flags[world.MeditateFlag/8]&(1<<(world.MeditateFlag%8)) == 0 {
		t.Fatalf("meditate state=%+v", saved.Players["meditate"].Body)
	}
}
