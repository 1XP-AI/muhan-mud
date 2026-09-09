package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func studyCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			Items:     &world.ItemCollection{Items: map[string]world.Item{"floor": {Object: world.LegacyObject{Name: "돌"}}}, Inventory: []string{"floor"}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 4, Level: 10, RoomID: 1,
				},
				Online: true,
				Items: &world.ItemCollection{Items: map[string]world.Item{
					"scroll-1": {Object: world.LegacyObject{Name: "두루마리", Type: world.StudyScrollType, DiceCount: 1, MagicPower: 1}},
				}, Inventory: []string{"scroll-1"}},
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseStudyLineAndAliases(t *testing.T) {
	cases := []struct {
		line       string
		alias      string
		name       string
		occurrence int
	}{
		{line: "배워 두루마리", alias: "배워", name: "두루마리", occurrence: 1},
		{line: "연마 두루마리 2", alias: "연마", name: "두루마리", occurrence: 2},
		{line: `배워 "긴 비법서" 3`, alias: "배워", name: "긴 비법서", occurrence: 3},
	}
	for _, tc := range cases {
		command, ok := ParseStudyLine(tc.line)
		if !ok || command.Alias != tc.alias || command.Name != tc.name || command.Occurrence != tc.occurrence {
			t.Fatalf("line=%q command=%+v ok=%v", tc.line, command, ok)
		}
		if !IsStudyLine(tc.line) || !IsLearnLine(tc.line) {
			t.Fatalf("alias helper rejected %q", tc.line)
		}
	}
	for _, line := range []string{"배워", "연마", "배워 두루마리 0", "배워 두루마리 nope", "배워\n두루마리", "study 두루마리"} {
		if _, ok := ParseStudyLine(line); ok {
			t.Fatalf("invalid study line accepted: %q", line)
		}
	}
}

func TestExecuteStudyLinePersistsLearnAndReplaysWithoutMutation(t *testing.T) {
	store := &departureStore{state: studyCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteStudyLine(context.Background(), store, "w", "study-1", lease, "배워 두루마리")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.StudyResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Learned || !result.Broadcast || result.ItemID != "scroll-1" || result.SpellName != "회복" || result.Event == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Players["a"].Items.Items) != 0 || saved.Players["a"].Body.Spells[0]&1 == 0 {
		t.Fatalf("saved study state=%+v", saved.Players["a"])
	}
	replay, err := owners.ExecuteStudyLine(context.Background(), store, "w", "study-1", lease, "배워 두루마리")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteStudyLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: studyCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteStudyLine(context.Background(), store, "w", "study-bad", lease, "연마"); err == nil || store.commits != 0 {
		t.Fatalf("unsupported study reached receipt: err=%v commits=%d", err, store.commits)
	}
}
