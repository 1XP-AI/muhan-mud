package session

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteQuitLinePersistsAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteQuitLine(context.Background(), store, "w", "quit-1", lease, "끝")
	if err != nil || !strings.Contains(string(first.Response), "종료") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteQuitLine(context.Background(), store, "w", "quit-1", lease, "끝")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteQuitLine(context.Background(), store, "w", "quit-2", lease, "봐"); err == nil {
		t.Fatal("non-quit line accepted")
	}
}
