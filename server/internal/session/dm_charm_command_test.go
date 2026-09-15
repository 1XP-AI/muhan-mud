package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func dmCharmSessionFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "먼방"}},
				PlayerIDs: []string{"target"},
			},
			3: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "NPC방"}},
				PlayerIDs: []string{"player-charmer"},
				NPCIDs:    []string{"npc-charmer"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor":          {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"target":         {Body: world.LegacyMonster{Name: "대상자", Type: 0, Class: 4, RoomID: 2}, Online: true, CharmRefs: []world.EntityRef{{Kind: "player", ID: "player-charmer"}, {Kind: "npc", ID: "npc-charmer"}}},
			"player-charmer": {Body: world.LegacyMonster{Name: "플레이어최면", Type: 0, Class: 4, RoomID: 3}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"npc-charmer": {Body: world.LegacyMonster{Name: "NPC최면", Type: 1, RoomID: 3}},
		},
		ActiveNPCIDs: []string{},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitDMCharmSessionOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseDMCharmLineAdmitsExactAliasesAndGlobalTargetSuffix(t *testing.T) {
	for _, test := range []struct {
		line, verb, target string
	}{
		{line: "*charm", verb: "*charm"},
		{line: "*최면", verb: "*최면"},
		{line: "  대상자  *charm  ", verb: "*charm", target: "대상자"},
		{line: "대상자 *최면", verb: "*최면", target: "대상자"},
	} {
		command, ok := ParseDMCharmLine(test.line)
		if !ok || command.Verb != test.verb || command.Target != test.target || !IsDMCharmLine(test.line) {
			t.Fatalf("ParseDMCharmLine(%q)=%+v ok=%t", test.line, command, ok)
		}
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != CommandDMCharm {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", test.line, parsed, err)
		}
	}
	for _, line := range []string{
		"*char", "대상자", "*charm 대상자", "대상자 *charm extra", "대상자 1 *charm", "대상자 ' *charm", "\"대상자\" *charm", "대상자 *charm\n", "대상자 *charm\u00a0", string([]byte{0xff}),
	} {
		if _, ok := ParseDMCharmLine(line); ok || IsDMCharmLine(line) {
			t.Fatalf("accepted malformed DM charm line %q", line)
		}
		parsed, err := ParseCommand(line)
		if err == nil && parsed.Kind == CommandDMCharm {
			t.Fatalf("ParseCommand(%q) routed malformed line: %+v", line, parsed)
		}
	}
}

func TestExecuteDMCharmLineIsReadOnlyAndReplaysWithUnchangedSnapshot(t *testing.T) {
	store := &departureStore{state: dmCharmSessionFixture(t, 12)}
	initial := append([]byte(nil), store.state...)
	owners, lease := admitDMCharmSessionOwner(t)

	first, err := owners.ExecuteDMCharmLine(context.Background(), store, "w", "dm-charm-1", lease, "대상자 *charm")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d stateChanged=%t", first, err, store.commits, !bytes.Equal(store.state, initial))
	}
	var result world.DMCharmResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	want := "대상자의 피최면자:\n플레이어최면.\nNPC최면.\n"
	if result.Action != world.DMCharmList || result.TargetID != "target" || result.TargetName != "대상자" || !reflect.DeepEqual(result.CharmerIDs, []string{"player-charmer", "npc-charmer"}) || result.Response != want || result.Changed {
		t.Fatalf("result=%+v", result)
	}

	replay, err := owners.ExecuteDMCharmLine(context.Background(), store, "w", "dm-charm-1", lease, "대상자 *최면")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || !bytes.Equal(store.state, initial) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDMCharmLineGateAndFailClosedBoundaries(t *testing.T) {
	owners, lease := admitDMCharmSessionOwner(t)
	store := &departureStore{state: dmCharmSessionFixture(t, 4)}
	initial := append([]byte(nil), store.state...)
	first, err := owners.ExecuteDMCharmLine(context.Background(), store, "w", "dm-charm-mortal", lease, "없는대상 *최면")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("unauthorized=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMCharmResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMCharmUnknown || result.Response != world.DMCharmUnknownResponse("*최면") || result.TargetID != "" || len(result.CharmerNames) != 0 {
		t.Fatalf("unauthorized result=%+v", result)
	}

	for _, test := range []struct {
		name   string
		line   string
		mutate func(world.State) world.State
		want   error
	}{
		{name: "missing target", line: "없는대상 *charm", want: world.ErrDMCharmTargetAbsent},
		{name: "missing argument", line: "*charm", want: world.ErrDMCharmTargetRequired},
		{name: "nil relation", line: "대상자 *charm", mutate: func(s world.State) world.State {
			target := s.Players["target"]
			target.CharmRefs = nil
			s.Players["target"] = target
			return s
		}, want: world.ErrDMCharmRelationsUnresolved},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := world.State{}
			raw := dmCharmSessionFixture(t, 12)
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				state = test.mutate(state)
				raw, err = json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
			}
			failed := &departureStore{state: raw}
			_, err := owners.ExecuteDMCharmLine(context.Background(), failed, "w", "dm-charm-fail-"+test.name, lease, test.line)
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want=%v", err, test.want)
			}
			if failed.commits != 0 || failed.receipt != nil {
				t.Fatalf("failed command left receipt commits=%d receipt=%+v", failed.commits, failed.receipt)
			}
		})
	}

	if _, ok := ParseDMCharmLine(strings.TrimSpace("대상자 1 *charm")); ok {
		t.Fatal("occurrence form accepted")
	}
}
