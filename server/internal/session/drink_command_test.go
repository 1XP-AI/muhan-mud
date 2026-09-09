package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func drinkCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{"a": {
			Body:   world.LegacyMonster{Name: "Alice", Type: 0, Level: 10, RoomID: 1, HPMax: 30, HPCurrent: 10},
			Online: true,
			Items:  &world.ItemCollection{Items: map[string]world.Item{"potion": {Object: world.LegacyObject{Name: "회복약", Type: world.DrinkPotionType, MagicPower: 1, ShotsCurrent: 1}}}, Inventory: []string{"potion"}},
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

func TestParseDrinkLineAdmitsOriginalAliases(t *testing.T) {
	tests := []struct {
		line       string
		alias      string
		name       string
		occurrence int
	}{
		{line: "먹어 회복약", alias: "먹어", name: "회복약", occurrence: 1},
		{line: "마셔 회복약 2", alias: "마셔", name: "회복약", occurrence: 2},
		{line: `먹어 "긴 물약" 3`, alias: "먹어", name: "긴 물약", occurrence: 3},
	}
	for _, tc := range tests {
		got, ok := ParseDrinkLine(tc.line)
		if !ok || got.Alias != tc.alias || got.ItemName != tc.name || got.Occurrence != tc.occurrence {
			t.Fatalf("line=%q got=%+v ok=%v", tc.line, got, ok)
		}
	}
	for _, line := range []string{"먹어", "마셔", "먹어 회복약 0", "먹어 회복약 bad", "먹어\n회복약", "drink 회복약"} {
		if _, ok := ParseDrinkLine(line); ok {
			t.Fatalf("invalid drink line accepted: %q", line)
		}
	}
}

func TestExecuteDrinkLinePersistsReceiptAndReplaysWithoutRandom(t *testing.T) {
	store := &departureStore{state: drinkCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteDrinkLineWithOptions(context.Background(), store, "w", "drink-1", lease, "먹어 회복약", DrinkOptions{Now: 10, Roll: func(low, high int) int {
		calls++
		if low != 1 || high != 6 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 5
	}})
	if err != nil || first.Replayed || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.DrinkResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Consumed || result.ItemID != "potion" || result.Event == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.HPCurrent != 15 || len(saved.Players["a"].Items.Items) != 0 {
		t.Fatalf("saved=%+v", saved.Players["a"])
	}
	replay, err := owners.ExecuteDrinkLineWithOptions(context.Background(), store, "w", "drink-1", lease, "먹어 회복약", DrinkOptions{Now: 10, Roll: func(int, int) int {
		t.Fatal("replay consumed random")
		return 1
	}})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
