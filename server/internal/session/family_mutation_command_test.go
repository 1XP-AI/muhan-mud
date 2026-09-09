package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyMutationSessionFlag(body *world.LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyMutationSessionFixture(t *testing.T, pending, active bool) []byte {
	t.Helper()
	room := world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"applicant", "boss"}}
	applicant := world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1, Gold: 1234}
	if pending || active {
		applicant.Daily[world.FamilyDailySlot].Max = 2
	}
	familyMutationSessionFlag(&applicant, world.FamilyPendingFlag, pending)
	familyMutationSessionFlag(&applicant, world.FamilyMemberFlag, active)
	boss := world.LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	boss.Daily[world.FamilyDailySlot].Max = 2
	familyMutationSessionFlag(&boss, world.FamilyMemberFlag, true)
	familyMutationSessionFlag(&boss, world.FamilyBossFlag, true)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: room},
		Players: map[string]world.PlayerState{
			"applicant": {Body: applicant, Online: true},
			"boss":      {Body: boss, Online: true},
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

func familyMutationSessionCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
	}}
}

func admitFamilyMutationOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("applicant")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseFamilyMutationLineAdmitsBoundedSourceForms(t *testing.T) {
	tests := []struct {
		line       string
		action     world.FamilyMutationAction
		familyName string
		targetName string
	}{
		{line: "패거리가입 청룡", action: world.FamilyMutationApply, familyName: "청룡"},
		{line: "  패거리탈퇴  ", action: world.FamilyMutationWithdraw},
		{line: "가입허가 Alice", action: world.FamilyMutationApprove, targetName: "Alice"},
		{line: "패거리추방 Alice", action: world.FamilyMutationExpel, targetName: "Alice"},
	}
	for _, tc := range tests {
		command, ok := ParseFamilyMutationLine(tc.line)
		if !ok || command.Action != tc.action || command.FamilyName != tc.familyName || command.TargetName != tc.targetName || !IsFamilyMutationLine(tc.line) {
			t.Fatalf("line=%q command=%+v ok=%t", tc.line, command, ok)
		}
	}
	for _, line := range []string{
		"패거리가입",
		"패거리가입 청룡 예",
		"패거리탈퇴 Alice",
		"가입허가 Alice 더",
		"패거리추방",
		"패거리추방 Alice 더",
		"패거리가입\n청룡",
		"패거리가입\x00청룡",
		string([]byte{0xff}),
	} {
		if _, ok := ParseFamilyMutationLine(line); ok || IsFamilyMutationLine(line) {
			t.Fatalf("unsupported family mutation line accepted: %q", line)
		}
	}
}

func TestExecuteFamilyMutationJoinPersistsAndReplays(t *testing.T) {
	initial := familyMutationSessionFixture(t, false, false)
	store := &departureStore{state: initial}
	owners, lease := admitFamilyMutationOwner(t)
	first, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-join-1", lease, "패거리가입 청룡", familyMutationSessionCatalog())
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FamilyMutationResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.FamilyMutationApply || result.ActorID != "applicant" || result.BossID != "boss" || result.FamilyID != 2 || result.FamilyName != "청룡" || !result.Changed || !result.BossNotificationPending || result.Response != world.FamilyApplicationResponse {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	applicant := saved.Players["applicant"].Body
	if !familyMutationSessionHasFlag(applicant, world.FamilyPendingFlag) || familyMutationSessionHasFlag(applicant, world.FamilyMemberFlag) || applicant.Daily[world.FamilyDailySlot].Max != 2 || applicant.Gold != 1234 {
		t.Fatalf("saved applicant=%+v", applicant)
	}

	replay, err := owners.ExecuteFamilyJoinLine(context.Background(), store, "w", "family-join-1", lease, "패거리가입 청룡", familyMutationSessionCatalog())
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFamilyMutationPendingWithdrawalPersistsAndReplays(t *testing.T) {
	store := &departureStore{state: familyMutationSessionFixture(t, true, false)}
	owners, lease := admitFamilyMutationOwner(t)
	first, err := owners.ExecuteFamilyWithdrawalLine(context.Background(), store, "w", "family-withdraw-1", lease, "패거리탈퇴", familyMutationSessionCatalog())
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FamilyMutationResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.FamilyMutationWithdraw || result.ActorID != "applicant" || result.FamilyID != 2 || result.BeforeFamilyID != 2 || result.AfterFamilyID != 0 || !result.Changed || result.BossNotificationPending || result.Response != world.FamilyWithdrawalResponse {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	applicant := saved.Players["applicant"].Body
	if familyMutationSessionHasFlag(applicant, world.FamilyPendingFlag) || familyMutationSessionHasFlag(applicant, world.FamilyMemberFlag) || applicant.Daily[world.FamilyDailySlot].Max != 0 || applicant.Gold != 1234 {
		t.Fatalf("saved applicant=%+v", applicant)
	}

	replay, err := owners.ExecuteFamilyWithdrawalLine(context.Background(), store, "w", "family-withdraw-1", lease, "패거리탈퇴", familyMutationSessionCatalog())
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFamilyMutationFailsClosedForInteractiveApprovalAndActiveLeave(t *testing.T) {
	store := &departureStore{state: familyMutationSessionFixture(t, false, false)}
	owners, lease := admitFamilyMutationOwner(t)
	if _, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-bare", lease, "패거리가입", familyMutationSessionCatalog()); !errors.Is(err, ErrUnsupportedFamilyMutationLine) || !errors.Is(err, ErrFamilyMutationSelectionRequired) || store.commits != 0 {
		t.Fatalf("bare join err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-approve", lease, "가입허가 Alice", familyMutationSessionCatalog()); !errors.Is(err, ErrUnsupportedFamilyMutationLine) || !errors.Is(err, world.ErrFamilyMutationApprovalUnsupported) || store.commits != 0 {
		t.Fatalf("approval err=%v commits=%d", err, store.commits)
	}

	activeStore := &departureStore{state: familyMutationSessionFixture(t, false, true)}
	activeOwners, activeLease := admitFamilyMutationOwner(t)
	if _, err := activeOwners.ExecuteFamilyMutationLineWithCatalog(context.Background(), activeStore, "w", "family-active-leave", activeLease, "패거리탈퇴", familyMutationSessionCatalog()); !errors.Is(err, world.ErrFamilyMutationFeeUnavailable) || activeStore.commits != 0 {
		t.Fatalf("active leave err=%v commits=%d", err, activeStore.commits)
	}
}

func TestExecuteFamilyMutationFailsClosedBeforeReceiptForMissingCatalogOrBoss(t *testing.T) {
	store := &departureStore{state: familyMutationSessionFixture(t, false, false)}
	owners, lease := admitFamilyMutationOwner(t)
	if _, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-missing-catalog", lease, "패거리가입 청룡", world.FamilyCatalog{}); !errors.Is(err, world.ErrFamilyCatalogUnavailable) || store.commits != 0 {
		t.Fatalf("catalog err=%v commits=%d", err, store.commits)
	}

	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	delete(state.Players, "boss")
	room := state.Rooms[1]
	room.PlayerIDs = []string{"applicant"}
	state.Rooms[1] = room
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteFamilyMutationLineWithCatalog(context.Background(), store, "w", "family-missing-boss", lease, "패거리가입 청룡", familyMutationSessionCatalog()); !errors.Is(err, world.ErrFamilyMutationBossUnavailable) || store.commits != 0 {
		t.Fatalf("boss err=%v commits=%d", err, store.commits)
	}
}

func familyMutationSessionHasFlag(body world.LegacyMonster, bit uint) bool {
	return body.Flags[bit/8]&(1<<(bit%8)) != 0
}
