package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func descriptionCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Description: "기대어 "},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseDescriptionLineUsesFinalSuffixAndCByteBoundary(t *testing.T) {
	command, ok := ParseDescriptionLine("기대어 서 묘사")
	if !ok || command.Description != "기대어 서" || command.Text != command.Description || command.Clear {
		t.Fatalf("description command=%+v ok=%t", command, ok)
	}
	clear, ok := ParseDescriptionLine("묘사")
	if !ok || !clear.Clear || clear.Description != "" || clear.Text != "" {
		t.Fatalf("clear command=%+v ok=%t", clear, ok)
	}
	for _, line := range []string{
		"묘사 서 있습니다",
		"서 있습니다",
		"서\t있습니다 묘사",
		"서 있습니다\n묘사",
		string([]byte{'\xff'}) + " 묘사",
		strings.Repeat("a", world.MaxDescriptionLineBytes) + " 묘사",
	} {
		if _, ok := ParseDescriptionLine(line); ok {
			t.Fatalf("unsupported description line accepted: %q", line)
		}
	}
}

func TestExecuteDescriptionLinePersistsAndReplaysAtomicDescription(t *testing.T) {
	store := &departureStore{state: descriptionCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteDescriptionLine(context.Background(), store, "w", "description-1", lease, "바르게 서다 묘사")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DescriptionResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "description" || result.ActorID != "a" || result.Description != "바르게 서다 " || result.Cleared || !strings.Contains(result.Response, "바르게 서다 서 있습니다") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Description != "바르게 서다 " {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}

	replay, err := owners.ExecuteDescriptionLine(context.Background(), store, "w", "description-1", lease, "바르게 서다 묘사")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	cleared, err := owners.ExecuteDescriptionLine(context.Background(), store, "w", "description-2", lease, "묘사")
	if err != nil || cleared.Replayed || store.commits != 2 || !strings.Contains(string(cleared.Response), "당신은 서 있습니다") {
		t.Fatalf("clear=%+v err=%v commits=%d", cleared, err, store.commits)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Description != "" {
		t.Fatalf("cleared state=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteDescriptionLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: descriptionCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteDescriptionLine(context.Background(), store, "w", "description-bad", lease, "서 있습니다"); err == nil || store.commits != 0 {
		t.Fatalf("malformed description committed: err=%v commits=%d", err, store.commits)
	}
}
