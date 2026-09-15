package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseUseLineIsExactlyOneUnquotedItemToken(t *testing.T) {
	got, ok := ParseUseLine("사용 검")
	if !ok || got.Alias != "사용" || got.ItemName != "검" || got.Occurrence != 1 {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	for _, line := range []string{
		"사용", "사용 모두", "사용 검 2", `사용 "긴 검"`, "사용 검 칼", "사용\n검", "use 검",
	} {
		if _, ok := ParseUseLine(line); ok {
			t.Fatalf("invalid use line accepted: %q", line)
		}
	}
}

func useCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{"a": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10},
			Online: true,
			Items: &world.ItemCollection{Items: map[string]world.Item{
				"potion": {Object: world.LegacyObject{Name: "회복약", Type: world.DrinkPotionType, MagicPower: 1, ShotsCurrent: 1}},
			}, Inventory: []string{"potion"}},
		}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteUseLinePersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: useCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteUseLineWithOptions(context.Background(), store, "w", "use-1", lease, "사용 회복약", UseOptions{Now: 10, Roll: func(low, high int) int {
		calls++
		if low != 1 || high != 6 {
			t.Fatalf("roll range %d..%d", low, high)
		}
		return 5
	}})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.UseResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Consumed || result.ItemID != "potion" || result.Event == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteUseLineWithOptions(context.Background(), store, "w", "use-1", lease, "사용 회복약", UseOptions{Now: 10, Roll: func(int, int) int {
		calls++
		return 1
	}})
	if err != nil || !replay.Replayed || store.commits != 1 || calls != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
	if _, err := owners.ExecuteUseLine(context.Background(), store, "w", "use-2", lease, "사용 모두", 10, nil); !errors.Is(err, ErrUnsupportedUseLine) {
		t.Fatalf("unsupported line err=%v", err)
	}
}
