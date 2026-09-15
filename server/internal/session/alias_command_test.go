package session

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseAliasLineAdmitsListAddDeleteAndCSourceSuffix(t *testing.T) {
	tests := []struct {
		line    string
		action  world.AliasAction
		alias   string
		process string
	}{
		{line: "줄임말", action: world.AliasView},
		{line: " 줄임말 foo 북쪽으로 이동 ", action: world.AliasAdd, alias: "foo", process: "북쪽으로 이동"},
		{line: "줄임말 숫자foo", action: world.AliasDelete, alias: "foo"},
		{line: "foo 북쪽으로 이동 줄임말", action: world.AliasAdd, alias: "foo", process: "북쪽으로 이동"},
		{line: "숫자foo 줄임말", action: world.AliasDelete, alias: "foo"},
	}
	for _, tc := range tests {
		got, ok := ParseAliasLine(tc.line)
		if !ok || got.Action != tc.action || got.Alias != tc.alias || got.Process != tc.process {
			t.Fatalf("ParseAliasLine(%q)=%+v,%v want=%+v", tc.line, got, ok, tc)
		}
	}
	for _, line := range []string{
		"줄임말 foo  process",
		"줄임말 foo bad\nprocess",
		"줄임말 가가가가가 process",
		"줄임말 foo $0",
		"줄임말 foo $17",
		"줄임말 foo $x",
		"줄임말 foo ; 시간",
		"줄임말 foo ~!",
		"줄임말 foo " + strings.Repeat("a", world.MaxAliasProcessBytes+1),
		"줄임말 foo\tprocess",
	} {
		if _, ok := ParseAliasLine(line); ok {
			t.Fatalf("invalid alias line accepted: %q", line)
		}
	}
	if command, ok := ParseAliasLine("줄임말 foo"); !ok || command.Action != world.AliasDelete || command.Alias != "foo" {
		t.Fatalf("delete command=%+v ok=%v", command, ok)
	}
}

func TestExpandAliasLineMatchesOnceAndSubstitutesArguments(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Aliases: []world.PlayerAlias{{Alias: "t", Process: "말 $2"}}},
		},
	}
	expanded, matched, err := ExpandAliasLine(state, "a", "t 안녕")
	if err != nil || !matched || expanded != "말 안녕" {
		t.Fatalf("expanded=%q matched=%v err=%v", expanded, matched, err)
	}
	ordinary, matched, err := ExpandAliasLine(state, "a", "시간")
	if err != nil || matched || ordinary != "시간" {
		t.Fatalf("ordinary=%q matched=%v err=%v", ordinary, matched, err)
	}
}

func aliasCommandState(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body:    world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online:  true,
				Aliases: []world.PlayerAlias{{Alias: "one", Process: "첫째"}},
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteAliasLinePersistsOrderAndReplaysReceipt(t *testing.T) {
	store := &departureStore{state: aliasCommandState(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteAliasLine(context.Background(), store, "w", "alias-1", lease, "줄임말 two 둘째")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.AliasResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Changed || result.Alias != "two" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	want := []world.PlayerAlias{{Alias: "one", Process: "첫째"}, {Alias: "two", Process: "둘째"}}
	if !reflect.DeepEqual(saved.Players["a"].Aliases, want) {
		t.Fatalf("saved aliases=%+v want=%+v", saved.Players["a"].Aliases, want)
	}

	replay, err := owners.ExecuteAliasLine(context.Background(), store, "w", "alias-1", lease, "줄임말 two 다른 처리")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	list, err := owners.ExecuteAliasLine(context.Background(), store, "w", "alias-list", lease, "줄임말")
	if err != nil {
		t.Fatal(err)
	}
	var listed world.AliasResult
	if err := json.Unmarshal(list.Response, &listed); err != nil || len(listed.Aliases) != 2 || listed.Aliases[0].Alias != "one" || listed.Aliases[1].Alias != "two" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}
