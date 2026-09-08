package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func equipmentCommandFixture() []byte {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	p := s.Players["a"]
	p.Body.Class = 4
	p.Body.Level = 1
	p.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	p.Items = &world.ItemCollection{Items: map[string]world.Item{
		"armor": {Object: world.LegacyObject{Name: "갑옷", Wear: 1, Armor: 2, ShotsMax: 1, ShotsCurrent: 1}},
	}, Inventory: []string{"armor"}}
	s.Players["a"] = p
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteEquipmentLinePersistsAndReplays(t *testing.T) {
	store := &departureStore{state: equipmentCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteEquipmentLine(context.Background(), store, "w", "equip-1", lease, "입어 모두")
	if err != nil || !strings.Contains(string(first.Response), "입었습니다") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Items.Ready[0] != "armor" || len(saved.Players["a"].Items.Inventory) != 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"].Items, err)
	}
	replay, err := owners.ExecuteEquipmentLine(context.Background(), store, "w", "equip-1", lease, "입어 모두")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	second, err := owners.ExecuteEquipmentLine(context.Background(), store, "w", "equip-2", lease, "벗어 갑옷")
	if err != nil || !strings.Contains(string(second.Response), "벗었습니다") || store.commits != 2 {
		t.Fatalf("second=%q err=%v commits=%d", second.Response, err, store.commits)
	}
}

func TestParseEquipmentLineKeepsOccurrenceAndRejectsAmbiguousInput(t *testing.T) {
	got, ok := parseEquipmentLine("무장 검 2")
	if !ok || got.mode != world.EquipmentWield || got.remove || got.name != "검" || got.occurrence != 2 {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	allWear, ok := parseEquipmentLine("입어 모두")
	if !ok || !allWear.all || allWear.mode != world.EquipmentWear {
		t.Fatalf("all wear=%+v ok=%v", allWear, ok)
	}
	allRemove, ok := parseEquipmentLine("벗어 모두")
	if !ok || !allRemove.all || !allRemove.remove {
		t.Fatalf("all remove=%+v ok=%v", allRemove, ok)
	}
	for _, line := range []string{"입어", "입어 갑옷 0", "장비 갑옷", "벗어 갑옷 a"} {
		if _, ok := parseEquipmentLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}
