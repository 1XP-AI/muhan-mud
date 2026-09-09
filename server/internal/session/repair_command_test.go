package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func repairCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: [8]byte{world.RoomRepairFlag / 8: 1 << (world.RoomRepairFlag % 8)}}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, RoomID: 1, Gold: 100,
					Stats: [5]byte{0, 0, 0, 0, 0},
				},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword": {Object: world.LegacyObject{
							Name: "검", Type: 0, Value: 100, ShotsMax: 30, ShotsCurrent: 0,
							DicePlus: 5, Adjustment: 2,
							Flags: [8]byte{14 / 8: 1 << (14 % 8)},
						}},
					},
					Inventory: []string{"sword"},
				},
			},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[1/8] |= 1 << (1 % 8)
	s.Players["actor"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseRepairLineAdmitsExactNameAndPositiveOccurrence(t *testing.T) {
	for _, line := range []string{"수리 검", "수리 검 2", `수리 "긴 검" 3`} {
		command, ok := ParseRepairLine(line)
		if !ok || command.Name == "" || command.Occurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	if command, ok := ParseRepairLine("수리 검 2"); !ok || command.Name != "검" || command.Occurrence != 2 {
		t.Fatalf("parsed=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"repair 검", "수리", "수리 검 0", "수리 검 -1", "수리 검 x", "수리 검 1 extra", "수리\n검"} {
		if _, ok := ParseRepairLine(line); ok {
			t.Fatalf("unsupported repair line accepted: %q", line)
		}
	}
}

func TestExecuteRepairLinePersistsStateAndReplaysWithoutRandom(t *testing.T) {
	store := &departureStore{state: repairCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	rolls := []int{100, 1, 9}
	first, err := owners.ExecuteRepairLine(context.Background(), store, "w", "repair-1", lease, "수리 검", func(low, high int) int {
		if len(rolls) == 0 {
			t.Fatal("repair replayed RNG during first execution")
		}
		value := rolls[0]
		rolls = rolls[1:]
		if (len(rolls) == 2 && (low != 1 || high != 100)) || (len(rolls) == 1 && (low != 1 || high != 50)) || (len(rolls) == 0 && (low != 5 || high != 9)) {
			t.Fatalf("unexpected random range=%d..%d", low, high)
		}
		return value
	})
	if err != nil || first.Replayed || store.commits != 1 || len(rolls) != 0 {
		t.Fatalf("first=%+v err=%v commits=%d rolls=%v", first, err, store.commits, rolls)
	}
	var result world.RepairResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.ItemID != "sword" || result.Cost != 25 || result.GoldAfter != 75 || !result.AdjustmentCleared || result.ShotsCurrent != 9 || result.Response == "" {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Gold != 75 || saved.Players["actor"].Items.Items["sword"].Object.ShotsCurrent != 9 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}

	replay, err := owners.ExecuteRepairLine(context.Background(), store, "w", "repair-1", lease, "수리 검", func(int, int) int { t.Fatal("repair replay consumed RNG"); return 1 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteRepairLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: repairCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteRepairLine(context.Background(), store, "w", "repair-bad", lease, "수리 검 0", func(int, int) int { t.Fatal("malformed repair consumed RNG"); return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("malformed repair committed: err=%v commits=%d", err, store.commits)
	}
}
