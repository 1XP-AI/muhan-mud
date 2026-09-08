package session

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteSocialLinePersistsReadOnlyReceiptAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-1", lease, "누구")
	if err != nil || !strings.Contains(string(first.Response), "Alice") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-1", lease, "누구")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
