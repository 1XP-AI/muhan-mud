package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyNewsSessionFlag(body *world.LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyNewsSessionFixture(t *testing.T) []byte {
	t.Helper()
	room := world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"member", "boss"}}
	member := world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}
	member.Daily[world.FamilyDailySlot].Max = 2
	familyNewsSessionFlag(&member, world.FamilyMemberFlag, true)
	boss := world.LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	boss.Daily[world.FamilyDailySlot].Max = 2
	familyNewsSessionFlag(&boss, world.FamilyMemberFlag, true)
	familyNewsSessionFlag(&boss, world.FamilyBossFlag, true)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: room},
		Players: map[string]world.PlayerState{
			"member": {Body: member, Online: true},
			"boss":   {Body: boss, Online: true},
		},
		FamilyNews: &world.FamilyNewsState{Bodies: map[int16]string{}},
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

func familyNewsSessionCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
	}}
}

func admitFamilyNewsOwner(t *testing.T, actorID string) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire(actorID)
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseFamilyNewsLineAdmitsSourceForms(t *testing.T) {
	tests := []struct {
		line   string
		action world.FamilyNewsAction
	}{
		{line: "패거리공지", action: world.FamilyNewsView},
		{line: "  패거리공지  ", action: world.FamilyNewsView},
		{line: "패거리공지 a extra", action: world.FamilyNewsView},
		{line: "패거리공지 a", action: world.FamilyNewsAppend},
		{line: "패거리공지 A", action: world.FamilyNewsAppend},
		{line: "패거리공지 append", action: world.FamilyNewsAppend},
		{line: "패거리공지 d", action: world.FamilyNewsDelete},
		{line: "패거리공지 D", action: world.FamilyNewsDelete},
		{line: "패거리공지 delete", action: world.FamilyNewsDelete},
		{line: "패거리공지 x", action: world.FamilyNewsInvalid},
	}
	for _, tc := range tests {
		command, ok := ParseFamilyNewsLine(tc.line)
		if !ok || command.Action != tc.action || !IsFamilyNewsLine(tc.line) {
			t.Fatalf("line=%q command=%+v ok=%t", tc.line, command, ok)
		}
	}
	for _, line := range []string{"패거리공지\n", "패거리공지\x00a", string([]byte{0xff}), "공지", "패거리말"} {
		if _, ok := ParseFamilyNewsLine(line); ok || IsFamilyNewsLine(line) {
			t.Fatalf("unsupported family news line accepted: %q", line)
		}
	}
}

func TestParseCommandClassifiesFamilyNewsAliases(t *testing.T) {
	for _, line := range []string{"패거리공지", "패거리공지 a", "패거리공지 d", "패거리공지 x"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandFamilyNews {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteFamilyNewsViewAndDeleteReplayWithoutDuplicateCommit(t *testing.T) {
	store := &departureStore{state: familyNewsSessionFixture(t)}
	owners, lease := admitFamilyNewsOwner(t, "boss")
	catalog := familyNewsSessionCatalog()

	missing, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-view-1", lease, "패거리공지", catalog)
	if err != nil || missing.Replayed || store.commits != 1 {
		t.Fatalf("view=%+v err=%v commits=%d", missing, err, store.commits)
	}
	var viewed world.FamilyNewsResult
	if err := json.Unmarshal(missing.Response, &viewed); err != nil {
		t.Fatal(err)
	}
	if viewed.Action != world.FamilyNewsView || viewed.Response != world.FamilyNewsMissingResponse || viewed.Changed {
		t.Fatalf("viewed=%+v", viewed)
	}
	viewReplay, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-view-1", lease, "패거리공지", catalog)
	if err != nil || !viewReplay.Replayed || store.commits != 1 || !bytes.Equal(viewReplay.Response, missing.Response) {
		t.Fatalf("view replay=%+v err=%v commits=%d", viewReplay, err, store.commits)
	}

	appendReceipt, err := owners.ExecuteFamilyNewsAppendLine(context.Background(), store, "w", "family-news-append-1", lease, "공지 본문", catalog)
	if err != nil || appendReceipt.Replayed || store.commits != 2 {
		t.Fatalf("append=%+v err=%v commits=%d", appendReceipt, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.FamilyNews.Bodies[2], "공지 본문\n") || !strings.HasPrefix(saved.FamilyNews.Bodies[2], world.FamilyNewsHeader) {
		t.Fatalf("saved body=%q", saved.FamilyNews.Bodies[2])
	}
	appendReplay, err := owners.ExecuteFamilyNewsAppendLine(context.Background(), store, "w", "family-news-append-1", lease, "공지 본문", catalog)
	if err != nil || !appendReplay.Replayed || store.commits != 2 || !bytes.Equal(appendReplay.Response, appendReceipt.Response) {
		t.Fatalf("append replay=%+v err=%v commits=%d", appendReplay, err, store.commits)
	}

	deleted, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-delete-1", lease, "패거리공지 d", catalog)
	if err != nil || deleted.Replayed || store.commits != 3 {
		t.Fatalf("delete=%+v err=%v commits=%d", deleted, err, store.commits)
	}
	var result world.FamilyNewsResult
	if err := json.Unmarshal(deleted.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.FamilyNewsDelete || result.Response != world.FamilyNewsDeletedResponse || !result.Changed {
		t.Fatalf("delete result=%+v", result)
	}
	saved, err = world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := saved.FamilyNews.Bodies[2]; ok {
		t.Fatalf("news survived delete: %+v", saved.FamilyNews)
	}
	replay, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-delete-1", lease, "패거리공지 d", catalog)
	if err != nil || !replay.Replayed || store.commits != 3 || !bytes.Equal(replay.Response, deleted.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	if _, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-append-start", lease, "패거리공지 a", catalog); !errors.Is(err, ErrFamilyNewsAppendContinuationRequired) {
		t.Fatalf("append start err=%v", err)
	}
}

func TestExecuteFamilyNewsRejectsUnmigratedLedgerBeforeReceipt(t *testing.T) {
	body := world.LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	body.Daily[world.FamilyDailySlot].Max = 2
	familyNewsSessionFlag(&body, world.FamilyMemberFlag, true)
	familyNewsSessionFlag(&body, world.FamilyBossFlag, true)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"boss"}}},
		Players: map[string]world.PlayerState{"boss": {Body: body, Online: true}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitFamilyNewsOwner(t, "boss")
	if _, err := owners.ExecuteFamilyNewsLine(context.Background(), store, "w", "family-news-unresolved", lease, "패거리공지", familyNewsSessionCatalog()); !errors.Is(err, world.ErrFamilyNewsUnresolved) || store.commits != 0 {
		t.Fatalf("unmigrated err=%v commits=%d", err, store.commits)
	}
}
