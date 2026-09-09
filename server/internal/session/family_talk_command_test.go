package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyTalkCommandFixture(t *testing.T) []byte {
	t.Helper()
	member := func(name string, room int16) world.PlayerState {
		body := world.LegacyMonster{Name: name, Type: 0, RoomID: room}
		body.Flags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
		body.Daily[world.FamilyDailySlot].Max = 2
		return world.PlayerState{Body: body, Online: true}
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "member"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}, PlayerIDs: []string{"remote"}},
		},
		Players: map[string]world.PlayerState{
			"actor":  member("Actor", 1),
			"member": member("Member", 1),
			"remote": member("Remote", 2),
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func familyTalkCommandCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "두목"},
	}}
}

func admitFamilyTalkOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseFamilyTalkLineAdmitsOriginalAliases(t *testing.T) {
	for _, test := range []struct {
		line, alias, message string
	}{
		{line: "패거리말 안녕하세요", alias: "패거리말", message: "안녕하세요"},
		{line: "] 안녕하세요", alias: "]", message: "안녕하세요"},
		{line: "패거리말", alias: "패거리말", message: ""},
	} {
		command, ok := ParseFamilyTalkLine(test.line)
		if !ok || command.Alias != test.alias || command.Message != test.message {
			t.Fatalf("line=%q command=%+v ok=%t", test.line, command, ok)
		}
	}
	for _, line := range []string{"패거리말x hello", "]x hello", "hello 패거리말", "패거리말\nhello", ""} {
		if _, ok := ParseFamilyTalkLine(line); ok {
			t.Fatalf("unsupported family-talk line accepted: %q", line)
		}
	}
}

func TestExecuteFamilyTalkLinePersistsEventsAndReplaysWithoutMutation(t *testing.T) {
	store := &departureStore{state: familyTalkCommandFixture(t)}
	owners, lease := admitFamilyTalkOwner(t)
	before := append([]byte(nil), store.state...)

	first, err := owners.ExecuteFamilyTalkLine(context.Background(), store, "w", "family-talk-1", lease, "패거리말 안녕하세요", familyTalkCommandCatalog())
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FamilyTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "family-talk" || result.ActorID != lease.ActorID || result.FamilyID != 2 || result.FamilyName != "청룡" || result.Message != "안녕하세요" || !result.Broadcast || len(result.Events) != 3 {
		t.Fatalf("result=%+v", result)
	}
	if result.ActorID != lease.ActorID || result.Events[0].RecipientID != "actor" || result.Events[1].RecipientID != "member" || result.Events[2].RecipientID != "remote" {
		t.Fatalf("event order=%+v actor=%q", result.Events, result.ActorID)
	}
	if !strings.Contains(result.Events[0].Text, "Actor>>>") {
		t.Fatalf("event text=%q", result.Events[0].Text)
	}
	if string(store.state) != string(before) {
		t.Fatal("family talk changed world snapshot")
	}

	replay, err := owners.ExecuteFamilyTalkLine(context.Background(), store, "w", "family-talk-1", lease, "패거리말 안녕하세요", familyTalkCommandCatalog())
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
