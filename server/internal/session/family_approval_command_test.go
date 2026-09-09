package session

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyApprovalSessionFixture(t *testing.T) []byte {
	t.Helper()
	room := world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"applicant", "boss"}}
	applicant := world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Gold: 100000}
	applicant.Daily[world.FamilyDailySlot].Max = 2
	applicant.Flags[world.FamilyPendingFlag/8] |= 1 << (world.FamilyPendingFlag % 8)
	boss := world.LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1, Gold: 100000}
	boss.Daily[world.FamilyDailySlot].Max = 2
	boss.Flags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	boss.Flags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: room},
		Players: map[string]world.PlayerState{"applicant": {Body: applicant, Online: true}, "boss": {Body: boss, Online: true}},
		Family:  &world.FamilyState{Members: map[int16][]world.FamilyMember{2: {{ID: "boss", Name: "Boss", Class: 4}}}},
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

func familyApprovalSessionCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss", Fee: 3}}}
}

func admitFamilyBoss(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("boss")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestExecuteFamilyApprovalPersistsAndReplaysReceipt(t *testing.T) {
	store := &departureStore{state: familyApprovalSessionFixture(t)}
	owners, lease := admitFamilyBoss(t)
	catalog := familyApprovalSessionCatalog()
	first, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-approve-1", lease, "가입허가 Alice", catalog)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FamilyMutationResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.FamilyMutationApprove || result.ActorID != "boss" || result.TargetID != "applicant" || result.GoldTransferred != 30000 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["boss"].Body.Gold != 70000 || saved.Players["applicant"].Body.Gold != 130000 || len(saved.Family.Members[2]) != 2 {
		t.Fatalf("saved=%+v family=%+v", saved.Players, saved.Family)
	}
	replay, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-approve-1", lease, "가입허가 Alice", catalog)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
