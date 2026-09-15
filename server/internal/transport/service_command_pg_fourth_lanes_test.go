package transport

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestPostgresFourthLanesCompareAppraisalAndRenamePersistAndReplay(t *testing.T) {
	_, store, ctx := serviceCommandPG(t)
	var renameFlags [8]byte
	renameFlags[world.ItemRenameChangeNameFlag/8] |= 1 << (world.ItemRenameChangeNameFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 8, RoomID: 1},
				Online: true,
				Items: &world.ItemCollection{
					Items: map[string]world.Item{
						"sword": {Object: world.LegacyObject{Name: "검", Type: 0, ShotsMax: 4, ShotsCurrent: 3, DiceCount: 1, DiceSides: 1, DicePlus: 1, Flags: renameFlags}},
						"armor": {Object: world.LegacyObject{Name: "갑옷", Type: 5, Armor: 4, Wear: 1}},
					},
					Inventory: []string{"sword", "armor"},
				},
			},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	worldID := fmt.Sprintf("service-fourth-%d", time.Now().UnixNano())
	if err := store.CreateWorld(ctx, worldID, raw); err != nil {
		t.Fatal(err)
	}
	owners := &session.Ownership{}
	lease := serviceCommandLease(t, owners, "actor")

	compare, err := owners.ExecuteCompareLine(ctx, store, worldID, "compare-pg-1", lease, "비교 검")
	if err != nil || compare.Replayed || compare.Revision != 1 {
		t.Fatalf("compare=%+v err=%v", compare, err)
	}
	var compareResult world.CompareResult
	if err := json.Unmarshal(compare.Response, &compareResult); err != nil || !compareResult.Success || !strings.Contains(compareResult.Response, "누구나 무장") {
		t.Fatalf("compare result=%+v err=%v", compareResult, err)
	}
	compareReplay, err := owners.ExecuteCompareLine(ctx, store, worldID, "compare-pg-1", lease, "비교 검")
	if err != nil || !compareReplay.Replayed || string(compareReplay.Response) != string(compare.Response) {
		t.Fatalf("compare replay=%+v err=%v", compareReplay, err)
	}

	appraisal, err := owners.ExecuteObjectAppraisalLine(ctx, store, worldID, "appraisal-pg-1", lease, "감정 검")
	if err != nil || appraisal.Replayed || appraisal.Revision != 2 {
		t.Fatalf("appraisal=%+v err=%v", appraisal, err)
	}
	var appraisalResult world.ObjectAppraisalResult
	if err := json.Unmarshal(appraisal.Response, &appraisalResult); err != nil || appraisalResult.ItemID != "sword" || !strings.Contains(appraisalResult.Response, "사용회수 3") {
		t.Fatalf("appraisal result=%+v err=%v", appraisalResult, err)
	}
	appraisalReplay, err := owners.ExecuteObjectAppraisalLine(ctx, store, worldID, "appraisal-pg-1", lease, "감정 검")
	if err != nil || !appraisalReplay.Replayed || string(appraisalReplay.Response) != string(appraisal.Response) {
		t.Fatalf("appraisal replay=%+v err=%v", appraisalReplay, err)
	}

	rename, err := owners.ExecuteItemRenameLine(ctx, store, worldID, "rename-pg-1", lease, "검 새검 명명")
	if err != nil || rename.Replayed || rename.Revision != 3 {
		t.Fatalf("rename=%+v err=%v", rename, err)
	}
	renameReplay, err := owners.ExecuteItemRenameLine(ctx, store, worldID, "rename-pg-1", lease, "검 새검 명명")
	if err != nil || !renameReplay.Replayed || string(renameReplay.Response) != string(rename.Response) {
		t.Fatalf("rename replay=%+v err=%v", renameReplay, err)
	}

	snapshot, err := store.LoadWorld(ctx, worldID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	item := saved.Players["actor"].Items.Items["sword"]
	if item.Object.Name != "새검" || item.Object.Flags[world.ItemRenameChangeNameFlag/8]&(1<<(world.ItemRenameChangeNameFlag%8)) != 0 || item.Object.Flags[world.ItemRenameNamedFlag/8]&(1<<(world.ItemRenameNamedFlag%8)) == 0 {
		t.Fatalf("saved renamed item=%+v", item.Object)
	}
}
