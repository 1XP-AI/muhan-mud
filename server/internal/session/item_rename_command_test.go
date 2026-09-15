package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func itemRenameCommandFixture(t *testing.T) []byte {
	t.Helper()
	flags := [8]byte{}
	flags[world.ItemRenameChangeNameFlag/8] |= 1 << (world.ItemRenameChangeNameFlag % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword-1": {Object: world.LegacyObject{Name: "검", Flags: flags}},
						"sword-2": {Object: world.LegacyObject{Name: "검", Flags: flags}},
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

func itemRenameCommandOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseItemRenameLineAdmitsSuffixOccurrenceAndQuotedNames(t *testing.T) {
	cases := []struct {
		line, item, newName string
		occurrence          int
	}{
		{"검 새검 명명", "검", "새검", 1},
		{"검 2 새 검 명명", "검", "새 검", 2},
		{`"긴 검" 3 "전설의 검" 명명`, "긴 검", "전설의 검", 3},
		{"  검   새검   명명  ", "검", "새검", 1},
	}
	for _, tc := range cases {
		got, ok := ParseItemRenameLine(tc.line)
		if !ok || got.Alias != "명명" || got.ItemName != tc.item || got.Name != tc.item || got.NewName != tc.newName || got.Occurrence != tc.occurrence {
			t.Fatalf("ParseItemRenameLine(%q)=%+v ok=%t want=%+v", tc.line, got, ok, tc)
		}
	}
	for _, line := range []string{
		"명명", "검 명명", "검 0 새검 명명", "검 -1 새검 명명",
		"검 새검", "검 새검 명명 extra", "검 새\t검 명명", "검 \" 명명",
	} {
		if _, ok := ParseItemRenameLine(line); ok {
			t.Fatalf("unsupported item rename line accepted: %q", line)
		}
	}
	if _, ok := ParseItemRenameLine("검 " + strings.Repeat("a", world.ItemRenameNameMaxBytes+1) + " 명명"); ok {
		t.Fatal("overlong new name was accepted")
	}
}

func TestExecuteItemRenameLinePersistsTypedResultAndReplays(t *testing.T) {
	store := &departureStore{state: itemRenameCommandFixture(t)}
	owners, lease := itemRenameCommandOwner(t)

	first, err := owners.ExecuteItemRenameLine(context.Background(), store, "w", "rename-1", lease, "검 2 새 검 명명")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ItemRenameResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "item_rename" || result.ActorID != "actor" || result.ItemID != "sword-2" || result.OldName != "검" || result.NewName != "새 검" || result.ItemName != "새 검" || result.Occurrence != 2 || result.Event == nil || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	item := saved.Players["actor"].Items.Items["sword-2"]
	if item.Object.Name != "새 검" || item.Object.Flags[world.ItemRenameChangeNameFlag/8]&(1<<(world.ItemRenameChangeNameFlag%8)) != 0 || item.Object.Flags[world.ItemRenameNamedFlag/8]&(1<<(world.ItemRenameNamedFlag%8)) == 0 {
		t.Fatalf("saved item=%+v", item)
	}

	replay, err := owners.ExecuteRenameLine(context.Background(), store, "w", "rename-1", lease, "검 2 새 검 명명")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteItemRenameLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: itemRenameCommandFixture(t)}
	owners, lease := itemRenameCommandOwner(t)
	for i, line := range []string{"검", "검 명명", "검 0 새검 명명", "검 새\n검 명명"} {
		_, err := owners.ExecuteItemRenameLine(context.Background(), store, "w", "rename-bad-"+string(rune('a'+i)), lease, line)
		if !errors.Is(err, ErrUnsupportedItemRenameLine) {
			t.Fatalf("line=%q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("unsupported line created receipt: %q", line)
		}
	}
}
