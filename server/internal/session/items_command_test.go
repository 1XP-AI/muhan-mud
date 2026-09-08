package session

import (
	"context"
	"strings"
	"testing"
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
