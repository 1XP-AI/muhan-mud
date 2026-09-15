package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func bribeCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"guard-one", "guard-two"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 500}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"guard-one": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Level: 8}},
			"guard-two": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1, Level: 0}},
		},
		ActiveNPCIDs: []string{"guard-one", "guard-two"},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseBribeLineUsesSuffixAndPositiveOccurrence(t *testing.T) {
	for _, tc := range []struct {
		line     string
		name     string
		amount   int32
		occur    int
		accepted bool
	}{
		{line: "Guard 100냥 뇌물", name: "Guard", amount: 100, occur: 1, accepted: true},
		{line: "Guard 2 100냥 뇌물", name: "Guard", amount: 100, occur: 2, accepted: true},
		{line: `"긴 경비" 1냥 뇌물`, name: "긴 경비", amount: 1, occur: 1, accepted: true},
		{line: "Guard 0 1냥 뇌물", accepted: false},
		{line: "Guard -1냥 뇌물", accepted: false},
		{line: "Guard 1냥", accepted: false},
		{line: "Guard 1원 뇌물", accepted: false},
		{line: "Guard 2147483648냥 뇌물", accepted: false},
		{line: "Guard\n1냥 뇌물", accepted: false},
	} {
		got, ok := ParseBribeLine(tc.line)
		if ok != tc.accepted || (ok && (got.NPCName != tc.name || got.Amount != tc.amount || got.NPCOccurrence != tc.occur)) {
			t.Fatalf("ParseBribeLine(%q)=%+v,%v want %+v,%v", tc.line, got, ok, tc, tc.accepted)
		}
	}
	if !IsBribeLine("Guard 1냥 뇌물") {
		t.Fatal("IsBribeLine rejected canonical suffix")
	}
}

func TestExecuteBribeLinePersistsAndReplaysStayAndLeave(t *testing.T) {
	store := &departureStore{state: bribeCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteBribeLine(context.Background(), store, "w", "bribe-1", lease, "Guard 100냥 뇌물")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var stay world.BribeResult
	if err := json.Unmarshal(first.Response, &stay); err != nil {
		t.Fatal(err)
	}
	if stay.NPCID != "guard-one" || !stay.Stayed || stay.GoldBefore != 500 || stay.GoldAfter != 400 || stay.Event == nil || !strings.Contains(stay.Response, "Guard") {
		t.Fatalf("stay=%+v", stay)
	}
	replay, err := owners.ExecuteBribeLine(context.Background(), store, "w", "bribe-1", lease, "Guard 100냥 뇌물")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	leave, err := owners.ExecuteBribeLine(context.Background(), store, "w", "bribe-2", lease, "Guard 2 1냥 뇌물")
	if err != nil || leave.Replayed || store.commits != 2 {
		t.Fatalf("leave=%+v err=%v commits=%d", leave, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 399 || saved.NPCs["guard-one"].Body.Gold != 100 {
		t.Fatalf("saved money=%+v", saved)
	}
	if _, ok := saved.NPCs["guard-two"]; ok || len(saved.Rooms[1].NPCIDs) != 1 || len(saved.ActiveNPCIDs) != 1 {
		t.Fatalf("saved leave indexes=%+v", saved)
	}
}

func TestExecuteBribeLineRejectsOverGoldWithoutCommit(t *testing.T) {
	store := &departureStore{state: bribeCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("actor")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteBribeLine(context.Background(), store, "w", "bribe-over", lease, "Guard 501냥 뇌물"); err == nil || store.commits != 0 {
		t.Fatalf("over-gold bribe committed: err=%v commits=%d", err, store.commits)
	}
}
