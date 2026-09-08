package session

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteStatusLinePersistsNoOpReceiptAndReplays(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteStatusLine(context.Background(), store, "w", "status-1", lease, "점수")
	if err != nil || !strings.Contains(string(first.Response), "Alice") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteStatusLine(context.Background(), store, "w", "status-1", lease, "점수")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
