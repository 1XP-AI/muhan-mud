package session

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteItemsLinePersistsReadOnlyReceiptAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteItemsLine(context.Background(), store, "w", "items-1", lease, "소지품")
	if err != nil || !strings.Contains(string(first.Response), "소지품") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteItemsLine(context.Background(), store, "w", "items-1", lease, "소지품")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteItemsLineAcceptsEquipmentAlias(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	receipt, err := owners.ExecuteItemsLine(context.Background(), store, "w", "items-2", lease, "장")
	if err != nil || !strings.Contains(string(receipt.Response), "걸치고") || store.commits != 1 {
		t.Fatalf("receipt=%+v err=%v commits=%d", receipt, err, store.commits)
	}
}

func TestExecuteItemsLineAcceptsLastTokenCatalogAndKeepsRoom(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	roomID := before.Players["a"].Body.RoomID
	for _, tt := range []struct {
		line string
		id   string
		want string
	}{
		{"검 소지품", "items-last-inv", "소지품"},
		{"검 장비", "items-last-eq", "걸치고"},
		{"검 장", "items-last-eq-alias", "걸치고"},
		{"소지품", "items-prefix-inv", "소지품"},
		{"장비", "items-prefix-eq", "걸치고"},
		{"장", "items-prefix-eq-alias", "걸치고"},
	} {
		receipt, execErr := owners.ExecuteItemsLine(context.Background(), store, "w", tt.id, lease, tt.line)
		if execErr != nil || !strings.Contains(string(receipt.Response), tt.want) {
			t.Fatalf("%q receipt=%q err=%v", tt.line, receipt.Response, execErr)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != roomID {
			t.Fatalf("%q RoomID=%+v err=%v", tt.line, saved.Players["a"], decodeErr)
		}
	}
	commits := store.commits
	first, err := owners.ExecuteItemsLine(context.Background(), store, "w", "items-last-replay", lease, "검 소지품")
	if err != nil || first.Replayed || store.commits != commits+1 {
		t.Fatalf("first last-token=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteItemsLine(context.Background(), store, "w", "items-last-replay", lease, "검 소지품")
	if err != nil || !replay.Replayed || store.commits != commits+1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay last-token=%+v err=%v commits=%d", replay, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != roomID {
		t.Fatalf("replay RoomID=%+v err=%v", saved.Players["a"], err)
	}
}
