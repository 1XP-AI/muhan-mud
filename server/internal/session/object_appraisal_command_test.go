package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func objectAppraisalCommandFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: class, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword-1": {Object: world.LegacyObject{Name: "검", ShotsMax: 9, ShotsCurrent: 7, DiceCount: 2, DiceSides: 6, DicePlus: 3}},
						"sword-2": {Object: world.LegacyObject{Name: "검", ShotsMax: 5, ShotsCurrent: 4}},
					},
					Inventory: []string{"sword-1", "sword-2"},
				},
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func objectAppraisalOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseObjectAppraisalLineAdmitsExactAliasAndPositiveOccurrence(t *testing.T) {
	for _, line := range []string{"감정 검", "감정 검 2", `감정 "긴 검" 3`, "  감정 검  "} {
		command, ok := ParseObjectAppraisalLine(line)
		if !ok || command.Alias != "감정" || command.Name == "" || command.Occurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	if command, ok := ParseObjectAppraisalLine("감정 검 2"); !ok || command.Name != "검" || command.Occurrence != 2 {
		t.Fatalf("parsed=%+v ok=%t", command, ok)
	}
	for _, line := range []string{
		"감", "감정", "감정 검 0", "감정 검 -1", "감정 검 x", "감정 검 1 extra", "감정\n검", "감정 검\x00",
	} {
		if _, ok := ParseObjectAppraisalLine(line); ok {
			t.Fatalf("unsupported object appraisal line accepted: %q", line)
		}
	}
}

func TestExecuteObjectAppraisalLinePersistsTypedReadOnlyResultAndReplays(t *testing.T) {
	store := &departureStore{state: objectAppraisalCommandFixture(t, 8)}
	owners, lease := objectAppraisalOwner(t)
	before := append([]byte(nil), store.state...)

	first, err := owners.ExecuteObjectAppraisalLine(context.Background(), store, "w", "appraisal-1", lease, "감정 검 2")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ObjectAppraisalResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "object_appraisal" || result.ItemID != "sword-2" || result.ItemName != "검" || result.Occurrence != 2 || result.ShotsCurrent != 4 {
		t.Fatalf("result=%+v", result)
	}
	if result.Response == "" || !strings.Contains(result.Response, "사용회수 4") {
		t.Fatalf("result response=%q", result.Response)
	}
	if string(store.state) != string(before) {
		t.Fatal("object appraisal changed the world snapshot")
	}

	replay, err := owners.ExecuteAppraisalLine(context.Background(), store, "w", "appraisal-1", lease, "감정 검 2")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteObjectAppraisalLineFailsClosedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: objectAppraisalCommandFixture(t, 4)}
	owners, lease := objectAppraisalOwner(t)
	if _, err := owners.ExecuteObjectAppraisalLine(context.Background(), store, "w", "appraisal-unauthorized", lease, "감정 검"); err == nil || err != world.ErrObjectAppraisalUnauthorized || store.commits != 0 {
		t.Fatalf("unauthorized appraisal committed or lost error: err=%v commits=%d", err, store.commits)
	}
	for _, line := range []string{"감정", "감정 검 0", "감 검"} {
		if _, err := owners.ExecuteObjectAppraisalLine(context.Background(), store, "w", "appraisal-bad-"+line, lease, line); err == nil || err != ErrUnsupportedObjectAppraisalLine {
			t.Fatalf("malformed line=%q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("malformed line created receipt: %q", line)
		}
	}
}
