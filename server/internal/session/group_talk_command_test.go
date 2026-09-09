package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func groupTalkCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"leader", "one", "two"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]world.PlayerState{
			"leader": {
				Body:           world.LegacyMonster{Name: "Leader", Type: 0, RoomID: 1, Class: 4},
				Online:         true,
				FollowerIDs:    []string{"two", "one"},
				NPCFollowerIDs: []string{"wolf"},
				FollowerRefs:   []world.EntityRef{{Kind: "player", ID: "two"}, {Kind: "npc", ID: "wolf"}, {Kind: "player", ID: "one"}},
			},
			"one": {
				Body:        world.LegacyMonster{Name: "One", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowingID: "leader",
			},
			"two": {
				Body:        world.LegacyMonster{Name: "Two", Type: 0, RoomID: 1, Class: 4},
				Online:      true,
				FollowingID: "leader",
			},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{Name: "Wolf", Type: 1, RoomID: 1}, FollowingPlayerID: "leader"},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitGroupTalkOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("one")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseGroupTalkLineAdmitsAliasesAndCOrientedSuffix(t *testing.T) {
	for _, test := range []struct {
		line, alias, message string
	}{
		{line: "hello 그룹말", alias: "그룹말", message: "hello"},
		{line: "무리말 hello", alias: "무리말", message: "hello"},
		{line: "= hello world", alias: "=", message: "hello world"},
		{line: "그룹말", alias: "그룹말", message: ""},
	} {
		command, ok := ParseGroupTalkLine(test.line)
		if !ok || command.Alias != test.alias || command.Message != test.message {
			t.Fatalf("line=%q command=%+v ok=%t", test.line, command, ok)
		}
	}
	for _, line := range []string{"group hello", "그룹말x hello", "hello 그룹말x", "", "그룹말\nhello"} {
		if _, ok := ParseGroupTalkLine(line); ok {
			t.Fatalf("unsupported group-talk line accepted: %q", line)
		}
	}
}

func TestExecuteGroupTalkLinePersistsTypedEventsAndReplaysWithoutMutation(t *testing.T) {
	store := &departureStore{state: groupTalkCommandFixture(t)}
	owners, lease := admitGroupTalkOwner(t)
	before := append([]byte(nil), store.state...)

	first, err := owners.ExecuteGroupTalkLine(context.Background(), store, "w", "group-talk-1", lease, "hello 그룹말")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.GroupTalkResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "group-talk" || result.ActorID != "one" || result.LeaderID != "leader" || result.Message != "hello" || !result.Broadcast || len(result.Events) != 4 {
		t.Fatalf("result=%+v", result)
	}
	if result.Events[0].RecipientID != "two" || result.Events[1].RecipientID != "wolf" || result.Events[2].RecipientID != "one" || result.Events[3].RecipientID != "leader" {
		t.Fatalf("event order=%+v", result.Events)
	}
	if !strings.Contains(result.Events[0].Text, "One이 그룹원들에게") {
		t.Fatalf("event text=%q", result.Events[0].Text)
	}
	if string(store.state) != string(before) {
		t.Fatal("group talk changed world snapshot")
	}

	replay, err := owners.ExecuteGroupTalkLine(context.Background(), store, "w", "group-talk-1", lease, "hello 그룹말")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteGroupTalkLinePersistsEmptyResponseAndRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: groupTalkCommandFixture(t)}
	owners, lease := admitGroupTalkOwner(t)
	first, err := owners.ExecuteGroupTalkLine(context.Background(), store, "w", "group-talk-empty", lease, "그룹말")
	if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "무슨말") {
		t.Fatalf("empty=%+v err=%v commits=%d", first, err, store.commits)
	}
	if _, err := owners.ExecuteGroupTalkLine(context.Background(), store, "w", "group-talk-bad", lease, "그룹말 bad\ntext"); err == nil || store.commits != 1 {
		t.Fatalf("malformed line committed: err=%v commits=%d", err, store.commits)
	}
}
