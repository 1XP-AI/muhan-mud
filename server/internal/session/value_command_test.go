package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func valueCommandFixture(t *testing.T, roomFlag int) []byte {
	t.Helper()
	var flags [8]byte
	flags[roomFlag/8] |= 1 << (roomFlag % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: flags}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword-1": {Object: world.LegacyObject{Name: "검", Value: 100}},
						"sword-2": {Object: world.LegacyObject{Name: "검", Value: 300001}},
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

func admitValueOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseValueLineAdmitsAliasesAndPositiveOccurrence(t *testing.T) {
	for _, line := range []string{"가치 검", "가격 검 2", `가치 "긴 검" 3`} {
		command, ok := ParseValueLine(line)
		if !ok || command.Name == "" || command.Occurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	if command, ok := ParseValueLine("가격 검 2"); !ok || command.Alias != "가격" || command.Name != "검" || command.Occurrence != 2 {
		t.Fatalf("parsed=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"value 검", "가치", "가치 검 0", "가치 검 -1", "가치 검 x", "가치 검 1 extra"} {
		if _, ok := ParseValueLine(line); ok {
			t.Fatalf("unsupported value line accepted: %q", line)
		}
	}
}

func TestExecuteValueLinePersistsTypedReadOnlyResultAndReplays(t *testing.T) {
	store := &departureStore{state: valueCommandFixture(t, world.RoomPawnFlag)}
	owners, lease := admitValueOwner(t)
	before := append([]byte(nil), store.state...)

	first, err := owners.ExecuteValueLine(context.Background(), store, "w", "value-1", lease, "가격 검 2")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ValueResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "value" || result.Mode != world.ValueModePawn || result.ItemID != "sword-2" || result.Occurrence != 2 || result.Quote != 100000 || !strings.Contains(result.Response, "100000냥") {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(before) {
		t.Fatal("value changed the world snapshot")
	}

	replay, err := owners.ExecuteValueLine(context.Background(), store, "w", "value-1", lease, "가격 검 2")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteValueLineSupportsRepairQuoteAndFailsBeforeReceipt(t *testing.T) {
	store := &departureStore{state: valueCommandFixture(t, world.RoomRepairFlag)}
	owners, lease := admitValueOwner(t)
	first, err := owners.ExecuteValueLine(context.Background(), store, "w", "value-repair-1", lease, "가치 검 1")
	if err != nil {
		t.Fatal(err)
	}
	var result world.ValueResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Mode != world.ValueModeRepair || result.Quote != 25 || !strings.Contains(result.Response, "25냥") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := owners.ExecuteValueLine(context.Background(), store, "w", "value-bad", lease, "가치 검 0"); err == nil || store.commits != 1 {
		t.Fatalf("malformed command committed: err=%v commits=%d", err, store.commits)
	}
}
