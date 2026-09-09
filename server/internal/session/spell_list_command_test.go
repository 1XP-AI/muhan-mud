package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteSpellListPersistsReadOnlyProjectionAndReplays(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Body.Spells[0] |= 1<<0 | 1<<1
	actor.Body.Spells[6] |= 1 << 7
	state.Players["a"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteSpellList(context.Background(), store, "w", "spell-list-1", lease)
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SpellListResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "spell_list" || result.Changed || len(result.Spells) != 3 || result.Spells[0].Name != "삭풍" || result.Spells[1].Name != "이혼대법" || result.Spells[2].Name != "회복" || result.Response != "\n주문: 삭풍, 이혼대법, 회복.\n" {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(raw) {
		t.Fatal("spell list changed world snapshot")
	}

	replay, err := owners.ExecuteSpellList(context.Background(), store, "w", "spell-list-1", lease)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSpellListRequiresCanonicalOnlineActorBeforeReceipt(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Online = false
	state.Players["a"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteSpellList(context.Background(), store, "w", "spell-list-offline", lease); err == nil || store.commits != 0 {
		t.Fatalf("offline actor err=%v commits=%d", err, store.commits)
	}
}
