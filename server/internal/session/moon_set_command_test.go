package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func moonSetCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			7: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 7, Name: "숲"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 7},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"stone-1": {Object: world.LegacyObject{Name: "초인의 돌", Value: 1001}},
						"stone-2": {Object: world.LegacyObject{Name: "초인의 돌", Value: 1001}},
					},
					Inventory: []string{"stone-1", "stone-2"},
				},
			},
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseMoonSetLineAdmitsSuffixForms(t *testing.T) {
	bare, ok := ParseMoonSetLine("  기억  ")
	if !ok || bare.ItemName != "" || bare.Occurrence != 1 || !IsMoonSetLine("기억") {
		t.Fatalf("bare=%+v ok=%t", bare, ok)
	}
	named, ok := ParseMoonSetLine("초인의 돌 기억")
	if !ok || named.ItemName != "초인의 돌" || named.Occurrence != 1 {
		t.Fatalf("named=%+v ok=%t", named, ok)
	}
	occ, ok := ParseMoonSetLine(`"초인의 돌" 2 기억`)
	if !ok || occ.ItemName != "초인의 돌" || occ.Occurrence != 2 {
		t.Fatalf("occurrence=%+v ok=%t", occ, ok)
	}
	spacedOcc, ok := ParseMoonSetLine("초인의 돌 2 기억")
	if !ok || spacedOcc.ItemName != "초인의 돌" || spacedOcc.Occurrence != 2 {
		t.Fatalf("spaced occurrence=%+v ok=%t", spacedOcc, ok)
	}
	for _, line := range []string{
		"기억 돌", "돌 0 기억", "돌 -1 기억", "돌 기억\n", string([]byte{0xff}),
	} {
		if _, ok := ParseMoonSetLine(line); ok || IsMoonSetLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesMoonSet(t *testing.T) {
	for _, line := range []string{"기억", "초인의 돌 기억", "초인의 돌 2 기억"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandMoonSet {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	parsed, err := ParseCommand("기억 돌")
	if err != nil || parsed.Kind != CommandUnknown {
		t.Fatalf("prefix=%+v err=%v", parsed, err)
	}
}

func TestExecuteMoonSetLineBindsAndReplaysWithoutDuplicateCommit(t *testing.T) {
	store := &departureStore{state: moonSetCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMoonSetLine(context.Background(), store, "w", "moon-set-1", lease, "초인의 돌 2 기억")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MoonSetResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.MoonSetBind || result.ItemID != "stone-2" || !result.Changed || len(result.Events) != 2 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	item := saved.Players["actor"].Items.Items["stone-2"]
	if item.Object.Value != 7 || item.Object.Keys[1] != "숲" || item.Object.Description != "숲의 광경이 어른거립니다." {
		t.Fatalf("saved item=%+v", item.Object)
	}
	replay, err := owners.ExecuteMoonSetLine(context.Background(), store, "w", "moon-set-1", lease, "초인의 돌 2 기억")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteMoonSetLine(context.Background(), store, "w", "moon-set-bad", lease, "기억 돌"); !errors.Is(err, ErrUnsupportedMoonSetLine) {
		t.Fatalf("unsupported err=%v", err)
	}
}

func TestExecuteMoonSetLineUsageDoesNotMutate(t *testing.T) {
	store := &departureStore{state: moonSetCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMoonSetLine(context.Background(), store, "w", "moon-set-usage", lease, "기억")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("usage=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.MoonSetResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != world.MoonSetUsage || result.Response != world.MoonSetUsageResponse {
		t.Fatalf("result=%+v", result)
	}
}
