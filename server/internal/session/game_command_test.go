package session

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGameExecutionBindsActorAndChecksAdmission(t *testing.T) {
	var owners Ownership
	lease, _ := owners.Acquire("a")
	store := &departureStore{state: json.RawMessage(`{}`)}
	calls := 0
	reduce := func(raw json.RawMessage, actor string) (json.RawMessage, json.RawMessage, error) {
		calls++
		if actor != "a" {
			t.Fatal("wrong actor")
		}
		return raw, json.RawMessage(`{"ok":true}`), nil
	}
	payload := json.RawMessage(`{"verb":"look","actor":"b"}`)
	if _, err := owners.ExecuteGame(context.Background(), store, "w", "boot-cmd-1", lease, payload, reduce); err == nil || calls != 0 || store.commits != 0 {
		t.Fatal("pending command reached reducer")
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteGame(context.Background(), store, "w", "boot-cmd-1", lease, payload, reduce)
	if err != nil || first.Revision != 1 || calls != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	replay, err := owners.ExecuteGame(context.Background(), store, "w", "boot-cmd-1", lease, payload, reduce)
	if err != nil || !replay.Replayed || calls != 1 || store.commits != 1 {
		t.Fatal("command replay reduced again")
	}
}
