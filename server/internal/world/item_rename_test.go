package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func itemRenameTestFlag(bit uint) [8]byte {
	var flags [8]byte
	flags[bit/8] |= 1 << (bit % 8)
	return flags
}

func itemRenameState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
				Items: &ItemCollection{
					Items: map[string]Item{
						"sword-1": {Object: LegacyObject{Name: "검", Flags: itemRenameTestFlag(ItemRenameChangeNameFlag)}},
						"sword-2": {Object: LegacyObject{Name: "검", Flags: itemRenameTestFlag(ItemRenameChangeNameFlag)}},
						"bag":     {Object: LegacyObject{Name: "가방", Flags: itemRenameTestFlag(ItemRenameChangeNameFlag)}, Contents: []string{"nested"}},
						"nested":  {Object: LegacyObject{Name: "검", Flags: itemRenameTestFlag(ItemRenameChangeNameFlag)}},
					},
					Inventory: []string{"sword-1", "sword-2", "bag"},
				},
			},
		},
	}
}

func TestValidateItemRenameNameUsesUTF8ControlWhitespaceAndByteBoundaries(t *testing.T) {
	if err := ValidateItemRenameName(strings.Repeat("a", ItemRenameNameMaxBytes)); err != nil {
		t.Fatalf("exact byte boundary rejected: %v", err)
	}
	for _, name := range []string{
		"", " leading", "trailing ", "\t", "a\tb", "a\u00a0b", "line\nfeed", string([]byte{0xff}),
	} {
		if err := ValidateItemRenameName(name); err == nil {
			t.Fatalf("unsafe item name accepted: %q", name)
		}
	}
	if err := ValidateItemRenameName(strings.Repeat("a", ItemRenameNameMaxBytes+1)); !errors.Is(err, ErrItemRenameNameTooLong) {
		t.Fatalf("long item name error=%v", err)
	}
	if err := ValidateItemRenameName("새 검"); err != nil {
		t.Fatalf("internal ASCII space should remain source-compatible: %v", err)
	}
}

func TestPlanApplyItemRenameSelectsDirectRootAndUpdatesFlagsAtomically(t *testing.T) {
	s := itemRenameState()
	proposal, err := s.PlanItemRename("actor", "검", 2, "새 검")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ItemID != "sword-2" || proposal.OldName != "검" || proposal.Occurrence != 2 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyItemRename(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "item_rename" || result.ItemID != "sword-2" || result.ItemName != "새 검" || result.OldName != "검" || result.NewName != "새 검" || !result.Broadcast || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if result.Response != "\n이름 명명 되었습니다.\r\n" || !strings.Contains(result.Event.Text, "Alice이") {
		t.Fatalf("response/event=%+v", result)
	}
	item := next.Players["actor"].Items.Items["sword-2"]
	if item.Object.Name != "새 검" || flag(item.Object.Flags[:], ItemRenameChangeNameFlag) || !flag(item.Object.Flags[:], ItemRenameNamedFlag) {
		t.Fatalf("renamed item=%+v", item)
	}
	original := s.Players["actor"].Items.Items["sword-2"]
	if original.Object.Name != "검" || !flag(original.Object.Flags[:], ItemRenameChangeNameFlag) {
		t.Fatal("proposal/apply mutated the source snapshot")
	}
	if next.Players["actor"].Items.Items["nested"].Object.Name != "검" {
		t.Fatal("nested item was unexpectedly renamed")
	}
}

func TestItemRenameFailsClosedForMissingPermissionNestedEquippedAndLegacyItems(t *testing.T) {
	s := itemRenameState()
	if _, err := s.PlanItemRename("actor", "가방", 1, "상자"); err != nil {
		t.Fatalf("canonical direct root with children should remain selectable: %v", err)
	}
	if _, err := s.PlanItemRename("actor", "검", 3, "새검"); !errors.Is(err, ErrItemRenameItemMissing) {
		t.Fatalf("nested-only occurrence was selected: %v", err)
	}

	withoutPermission := s.clone()
	item := withoutPermission.Players["actor"].Items.Items["sword-1"]
	item.Object.Flags = [8]byte{}
	withoutPermission.Players["actor"].Items.Items["sword-1"] = item
	if _, err := withoutPermission.PlanItemRename("actor", "검", 1, "새검"); !errors.Is(err, ErrItemRenameForbidden) {
		t.Fatalf("OCNAME-less item error=%v", err)
	}

	equipped := s.clone()
	player := equipped.Players["actor"]
	delete(player.Items.Items, "sword-1")
	delete(player.Items.Items, "sword-2")
	player.Items.Inventory = []string{"bag"}
	player.Items.Ready[0] = "sword-1"
	player.Items.Items["sword-1"] = s.Players["actor"].Items.Items["sword-1"]
	equipped.Players["actor"] = player
	if _, err := equipped.PlanItemRename("actor", "검", 1, "새검"); !errors.Is(err, ErrItemRenameItemMissing) {
		t.Fatalf("equipped item was searched as a direct root: %v", err)
	}

	legacy := s.clone()
	legacyPlayer := legacy.Players["actor"]
	legacyPlayer.Items = nil
	legacyPlayer.Body.Inventory = []LegacyObject{{Name: "검", Flags: itemRenameTestFlag(ItemRenameChangeNameFlag)}}
	legacy.Players["actor"] = legacyPlayer
	if _, err := legacy.PlanItemRename("actor", "검", 1, "새검"); err == nil {
		t.Fatal("legacy inventory unexpectedly entered canonical rename boundary")
	}
}

func TestApplyItemRenameRejectsStaleProposalWithoutMutation(t *testing.T) {
	s := itemRenameState()
	proposal, err := s.PlanItemRename("actor", "검", 1, "새검")
	if err != nil {
		t.Fatal(err)
	}
	stale := s.clone()
	item := stale.Players["actor"].Items.Items[proposal.ItemID]
	item.Object.Description = "changed after planning"
	stale.Players["actor"].Items.Items[proposal.ItemID] = item
	before := stale.Players["actor"].Items.Items[proposal.ItemID]
	if _, _, err := stale.ApplyItemRename(proposal); err == nil {
		t.Fatal("stale proposal was applied")
	}
	if !reflect.DeepEqual(stale.Players["actor"].Items.Items[proposal.ItemID], before) {
		t.Fatal("stale apply mutated source state")
	}
}
