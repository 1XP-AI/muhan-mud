package session

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func followCommandFixture() []byte {
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Players["b"] = world.PlayerState{Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteFollowLinePersistsRelationAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-1", lease, "따라 Bob")
	if err != nil || !strings.Contains(string(first.Response), "Bob") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].FollowingID != "b" || len(saved.Players["b"].FollowerIDs) != 1 || saved.Players["b"].FollowerIDs[0] != "a" {
		t.Fatalf("follow state=%+v err=%v", saved.Players, err)
	}
	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-1", lease, "따라 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineStopsFollowingAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	follow, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-2", lease, "따라 Bob")
	if err != nil || !strings.Contains(string(follow.Response), "Bob") || store.commits != 1 {
		t.Fatalf("follow=%q err=%v commits=%d", follow.Response, err, store.commits)
	}

	expel, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-1", lease, "내보내")
	if err != nil || !strings.Contains(string(expel.Response), "그만 따라다니기로") || store.commits != 2 {
		t.Fatalf("expel=%q err=%v commits=%d", expel.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].FollowingID != "" || len(saved.Players["b"].FollowerIDs) != 0 {
		t.Fatalf("expelled state=%+v err=%v", saved.Players, err)
	}

	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-1", lease, "내보내")
	if err != nil || !replay.Replayed || store.commits != 2 || string(replay.Response) != string(expel.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineExpelsFollowerAndReplays(t *testing.T) {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.FollowPlayer("b", "a")
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	expel, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-2", lease, "내보내 Bob")
	if err != nil || !strings.Contains(string(expel.Response), "못 따라오도록") || store.commits != 1 {
		t.Fatalf("expel=%q err=%v commits=%d", expel.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["b"].FollowingID != "" || len(saved.Players["a"].FollowerIDs) != 0 {
		t.Fatalf("expelled state=%+v err=%v", saved.Players, err)
	}

	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-2", lease, "내보내 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(expel.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestParseFollowLineUsesPositiveVal1Occurrence(t *testing.T) {
	tests := []struct {
		line       string
		verb       string
		target     string
		occurrence int
		ok         bool
	}{
		{line: "따라 Bob", verb: "따라", target: "Bob", occurrence: 1, ok: true},
		{line: "따라 Bob 2", verb: "따라", target: "Bob", occurrence: 2, ok: true},
		{line: "내보내 Bob 3", verb: "내보내", target: "Bob", occurrence: 3, ok: true},
		{line: "내보내", verb: "내보내", occurrence: 1, ok: true},
		{line: "따라 Bob 0", ok: false},
		{line: "따라 Bob -1", ok: false},
		{line: "따라 Bob +2", ok: false},
		{line: "따라 Bob 2147483648", ok: false},
		{line: "따라 Bob nope", ok: false},
		{line: "따라 Bob 2 extra", ok: false},
		{line: "따라 Bob Smith", ok: false},
		{line: `따라 "Bob Smith"`, ok: false},
		{line: "내보내 Bob Smith", ok: false},
		{line: `내보내 "Bob Smith"`, ok: false},
		{line: "따라\nBob", ok: false},
	}
	for _, tc := range tests {
		got, ok := ParseFollowLine(tc.line)
		if ok != tc.ok {
			t.Fatalf("line=%q got=%+v ok=%v want=%v", tc.line, got, ok, tc.ok)
		}
		if tc.ok && (got.Verb != tc.verb || got.Target != tc.target || got.Occurrence != tc.occurrence) {
			t.Fatalf("line=%q got=%+v", tc.line, got)
		}
	}
}

func TestExecuteFollowLineRejectsQuotedOrMultiTokenNamesBeforeReceipt(t *testing.T) {
	for _, line := range []string{
		"따라 Bob Smith",
		`따라 "Bob Smith"`,
		"내보내 Bob Smith",
		`내보내 "Bob Smith"`,
	} {
		t.Run(line, func(t *testing.T) {
			store := &departureStore{state: followCommandFixture()}
			before := append([]byte(nil), store.state...)
			var owners Ownership
			lease, _ := owners.Acquire("a")
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if _, err := owners.ExecuteFollowLine(context.Background(), store, "w", "unsupported-target", lease, line); err == nil {
				t.Fatalf("line=%q unexpectedly accepted", line)
			}
			if store.commits != 0 || !bytes.Equal(before, store.state) {
				t.Fatalf("line=%q reached a receipt: commits=%d changed=%v", line, store.commits, !bytes.Equal(before, store.state))
			}
		})
	}
}

func addFollowCommandPlayer(t *testing.T, raw []byte, id, name string) []byte {
	t.Helper()
	s, err := world.DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	s.Players[id] = world.PlayerState{
		Body: world.LegacyMonster{
			Name: name, RoomID: 1, Type: 0, Class: 4, Level: 1,
			HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5,
		},
		Online: true,
		Items:  &world.ItemCollection{Items: map[string]world.Item{}},
	}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, id)
	s.Rooms[1] = r
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestExecuteFollowLineResolvesSameNameRoomOccurrenceInRoomOrder(t *testing.T) {
	state := addFollowCommandPlayer(t, followCommandFixture(), "b2", "Bob")
	s, err := world.DecodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	// Deliberately put b2 before b. The selector must use this canonical room
	// order instead of the randomized Players map order.
	room.PlayerIDs = []string{"a", "b2", "b"}
	s.Rooms[1] = room
	state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-room-occurrence", lease, "따라 Bob 2")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].FollowingID != "b" || saved.Players["b2"].FollowingID != "" {
		t.Fatalf("room occurrence selected wrong target: players=%+v err=%v", saved.Players, err)
	}
	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-room-occurrence", lease, "따라 Bob 2")
	if err != nil || !replay.Replayed || store.commits != 1 || !reflect.DeepEqual(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineResolvesNamedExpelOccurrenceFromFollowerOrder(t *testing.T) {
	state := followCommandFixture()
	state = addFollowCommandPlayer(t, state, "c", "Bob")
	s, err := world.DecodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	// Keep room order opposite to first_fol order. FollowPlayer head-inserts,
	// so c is first and b is the second matching follower.
	s, err = s.FollowPlayer("b", "a")
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.FollowPlayer("c", "a")
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "c", "b"}
	s.Rooms[1] = room
	state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	expel, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-follower-occurrence", lease, "내보내 Bob 2")
	if err != nil || expel.Replayed || store.commits != 1 {
		t.Fatalf("expel=%+v err=%v commits=%d", expel, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["b"].FollowingID != "" || saved.Players["c"].FollowingID != "a" || !reflect.DeepEqual(saved.Players["a"].FollowerIDs, []string{"c"}) {
		t.Fatalf("follower occurrence selected wrong target: players=%+v err=%v", saved.Players, err)
	}
	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-follower-occurrence", lease, "내보내 Bob 2")
	if err != nil || !replay.Replayed || store.commits != 1 || !reflect.DeepEqual(replay.Response, expel.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineSelfAliasLeavesLeaderOrFailsClosed(t *testing.T) {
	t.Run("existing leader", func(t *testing.T) {
		store := &departureStore{state: followCommandFixture()}
		var owners Ownership
		lease, _ := owners.Acquire("a")
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := owners.ExecuteFollowLine(context.Background(), store, "w", "self-follow", lease, "따라 Bob"); err != nil {
			t.Fatal(err)
		}
		beforeSelfAlias := append([]byte(nil), store.state...)
		first, err := owners.ExecuteFollowLine(context.Background(), store, "w", "self-alias", lease, "따라 나")
		if err != nil || first.Replayed || store.commits != 2 || strings.Contains(string(store.state), `"FollowingID":"a"`) {
			t.Fatalf("self alias receipt=%+v err=%v commits=%d", first, err, store.commits)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil || saved.Players["a"].FollowingID != "" || len(saved.Players["b"].FollowerIDs) != 0 {
			t.Fatalf("self alias created/kept edge: state=%+v err=%v", saved.Players, err)
		}
		if bytes.Equal(beforeSelfAlias, store.state) {
			t.Fatal("self alias did not remove existing leader")
		}
		replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "self-alias", lease, "따라 나")
		if err != nil || !replay.Replayed || store.commits != 2 || !reflect.DeepEqual(replay.Response, first.Response) {
			t.Fatalf("self alias replay=%+v err=%v commits=%d", replay, err, store.commits)
		}
	})

	t.Run("no leader", func(t *testing.T) {
		store := &departureStore{state: followCommandFixture()}
		before := append([]byte(nil), store.state...)
		var owners Ownership
		lease, _ := owners.Acquire("a")
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := owners.ExecuteFollowLine(context.Background(), store, "w", "self-alias-no-leader", lease, "따라 나"); err == nil || store.commits != 0 || !bytes.Equal(before, store.state) {
			t.Fatalf("self alias accepted without leader: err=%v commits=%d changed=%v", err, store.commits, !bytes.Equal(before, store.state))
		}
	})
}

func TestExecuteFollowLineInvalidOccurrenceAndNoMatchDoNotCommit(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "invalid occurrence", line: "따라 Bob 0"},
		{name: "room occurrence absent", line: "따라 Bob 2"},
		{name: "named actor self", line: "따라 Alice"},
		{name: "follower occurrence absent", line: "내보내 Bob 2"},
		{name: "follower name absent", line: "내보내 Nobody"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := followCommandFixture()
			if strings.HasPrefix(tc.line, "내보내 Bob") {
				s, err := world.DecodeState(state)
				if err != nil {
					t.Fatal(err)
				}
				s, err = s.FollowPlayer("b", "a")
				if err != nil {
					t.Fatal(err)
				}
				state, err = json.Marshal(s)
				if err != nil {
					t.Fatal(err)
				}
			}
			store := &departureStore{state: state}
			before := append([]byte(nil), store.state...)
			var owners Ownership
			lease, _ := owners.Acquire("a")
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			_, err := owners.ExecuteFollowLine(context.Background(), store, "w", "invalid-follow-"+tc.name, lease, tc.line)
			if err == nil || store.commits != 0 || !bytes.Equal(before, store.state) {
				t.Fatalf("line=%q err=%v commits=%d changed=%v", tc.line, err, store.commits, !bytes.Equal(before, store.state))
			}
		})
	}
}
