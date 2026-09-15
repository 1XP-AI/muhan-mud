package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func giveCommandFixture(t *testing.T, withNPC bool) []byte {
	t.Helper()
	actorItems := world.ItemCollection{
		Items: map[string]world.Item{
			"bag": {Object: world.LegacyObject{Name: "가방", Weight: 2}, Contents: []string{"gem"}},
			"gem": {Object: world.LegacyObject{Name: "보석", Weight: 1}},
		},
		Inventory: []string{"bag"},
	}
	targetItems := world.ItemCollection{Items: map[string]world.Item{"cloak": {Object: world.LegacyObject{Name: "망토", Weight: 1}}}, Inventory: []string{"cloak"}}
	room := world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
		PlayerIDs: []string{"actor", "target"},
	}
	s := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: room},
		Players: map[string]world.PlayerState{
			"actor":  {Body: world.LegacyMonster{Name: "앨리스", Type: 0, RoomID: 1, Gold: 100, Stats: [5]byte{0: 10}}, Online: true, Items: &actorItems},
			"target": {Body: world.LegacyMonster{Name: "밥", Type: 0, RoomID: 1, Gold: 5, Stats: [5]byte{0: 10}}, Online: true, Items: &targetItems},
		},
	}
	if withNPC {
		s.NPCs = map[string]world.NPCState{"vendor": {Body: world.LegacyMonster{Name: "상인", Type: 1, RoomID: 1}}}
		s.Rooms[1] = world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "target"}, NPCIDs: []string{"vendor"}}
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

func admitGiveOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseGiveLineAdmitsExactItemAndMoneySuffixForms(t *testing.T) {
	item, ok := ParseGiveLine("가방 밥 줘")
	if !ok || item.Alias != "줘" || item.Kind != world.GiveItem || item.Source != "가방" || item.ItemName != "가방" || item.TargetName != "밥" || item.ItemOccurrence != 1 || item.TargetOccurrence != 1 || item.Amount != 0 {
		t.Fatalf("item=%+v ok=%t", item, ok)
	}
	money, ok := ParseGiveLine("40냥 밥 줘")
	if !ok || money.Kind != world.GiveMoney || money.Source != "40냥" || money.Amount != 40 || money.ItemName != "" {
		t.Fatalf("money=%+v ok=%t", money, ok)
	}
	quoted, ok := ParseGiveLine(`"긴 가방" "밥" 줘`)
	if !ok || quoted.ItemName != "긴 가방" || quoted.TargetName != "밥" {
		t.Fatalf("quoted=%+v ok=%t", quoted, ok)
	}
}

func TestParseGiveLineRejectsWrongOrderMalformedMoneyAndExtraTokens(t *testing.T) {
	for _, line := range []string{
		"줘 가방 밥", "가방 밥", "가방 밥 줘 더", "가방 줘", "0냥 밥 줘", "-1냥 밥 줘", "냥 밥 줘", "1냥 밥 주어", "가방\n밥 줘", "가방 밥 \x1b줘",
	} {
		if _, ok := ParseGiveLine(line); ok {
			t.Fatalf("unsupported give line accepted: %q", line)
		}
	}
	if !IsGiveLine("  가방 밥 줘  ") {
		t.Fatal("surrounding terminal whitespace should be harmless")
	}
}

func TestExecuteGiveLinePersistsTypedItemResultAndReplays(t *testing.T) {
	store := &departureStore{state: giveCommandFixture(t, false)}
	owners, lease := admitGiveOwner(t)
	first, err := owners.ExecuteGiveLine(context.Background(), store, "w", "give-item-1", lease, "가방 밥 줘")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.GiveResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != world.GiveItem || result.ItemID != "bag" || result.TargetID != "target" || result.Response == result.TargetResponse || result.TargetResponse == result.ObserverResponse || result.Event == nil || result.Event.ExcludeTargetID != "target" {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	_, actorHasBag := saved.Players["actor"].Items.Items["bag"]
	_, targetHasBag := saved.Players["target"].Items.Items["bag"]
	if actorHasBag || !targetHasBag {
		t.Fatal("item subtree was not persisted atomically")
	}
	replay, err := owners.ExecuteGiveLine(context.Background(), store, "w", "give-item-1", lease, "가방 밥 줘")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteGiveLinePersistsGoldAndRejectsNPCWithoutCommit(t *testing.T) {
	store := &departureStore{state: giveCommandFixture(t, false)}
	owners, lease := admitGiveOwner(t)
	first, err := owners.ExecuteGiveLine(context.Background(), store, "w", "give-money-1", lease, "40냥 밥 줘")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.GiveResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Kind != world.GiveMoney || result.Amount != 40 || result.GoldBefore != 100 || result.GoldAfter != 60 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.Gold != 60 || saved.Players["target"].Body.Gold != 45 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}

	npcStore := &departureStore{state: giveCommandFixture(t, true)}
	npcOwners, npcLease := admitGiveOwner(t)
	_, npcErr := npcOwners.ExecuteGiveLine(context.Background(), npcStore, "w", "give-npc-1", npcLease, "1냥 상인 줘")
	if npcErr == nil || !errors.Is(npcErr, world.ErrGiveNPCPending) || npcStore.commits != 0 {
		t.Fatalf("NPC money err=%v commits=%d", npcErr, npcStore.commits)
	}
	if !strings.Contains(npcErr.Error(), "NPC") {
		t.Fatalf("NPC error should retain branch reason: %v", npcErr)
	}
}
