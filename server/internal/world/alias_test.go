package world

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func aliasStateFixture(aliases []PlayerAlias) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}},
		},
		Players: map[string]PlayerState{
			"a": {
				Body:    LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online:  true,
				Aliases: aliases,
			},
		},
	}
}

func TestValidatePlayerAliasUsesByteLimitsAndFailsClosedSubstitution(t *testing.T) {
	if err := ValidatePlayerAliasName("가가가가"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"가가가가가", // 15 UTF-8 bytes
		"bad alias",
		" bad",
		"bad ",
		"~!",
		"bad\n",
		string([]byte{0xff}),
	} {
		if err := ValidatePlayerAliasName(name); err == nil {
			t.Fatalf("invalid alias accepted: %q", name)
		}
	}
	if err := ValidatePlayerAliasProcess(strings.Repeat("a", MaxAliasProcessBytes)); err != nil {
		t.Fatal(err)
	}
	for _, process := range []string{
		"",
		strings.Repeat("a", MaxAliasProcessBytes+1),
		" leading",
		"trailing ",
		"bad\nprocess",
		"검 $1",
		"~!",
		string([]byte{0xff}),
	} {
		if err := ValidatePlayerAliasProcess(process); err == nil {
			t.Fatalf("invalid process accepted: %q", process)
		}
	}
	if !errors.Is(ValidatePlayerAliasProcess("검 $1"), ErrAliasCommandSubstitution) {
		t.Fatal("substitution did not fail closed")
	}
}

func TestPlanApplyAliasPreservesOrderAndRejectsStaleProposal(t *testing.T) {
	s := aliasStateFixture([]PlayerAlias{{Alias: "z", Process: "마지막"}, {Alias: "a", Process: "처음"}})
	p, err := s.PlanAddAlias("a", "m", "중간")
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyAlias(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []PlayerAlias{{Alias: "z", Process: "마지막"}, {Alias: "a", Process: "처음"}, {Alias: "m", Process: "중간"}}
	if !reflect.DeepEqual(next.Players["a"].Aliases, want) || !reflect.DeepEqual(result.Aliases, want) || !result.Changed {
		t.Fatalf("next=%+v result=%+v", next.Players["a"].Aliases, result)
	}
	if len(s.Players["a"].Aliases) != 2 {
		t.Fatal("planning/apply mutated source snapshot")
	}

	deleteProposal, err := next.PlanDeleteAlias("a", "a")
	if err != nil {
		t.Fatal(err)
	}
	deleted, deleteResult, err := next.ApplyAlias(deleteProposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deleted.Players["a"].Aliases, []PlayerAlias{{Alias: "z", Process: "마지막"}, {Alias: "m", Process: "중간"}}) || deleteResult.PreviousProcess != "처음" {
		t.Fatalf("deleted=%+v result=%+v", deleted.Players["a"].Aliases, deleteResult)
	}

	changed := s
	changedPlayer := changed.Players["a"]
	changedPlayer.Body.Description = "changed"
	changed.Players["a"] = changedPlayer
	if _, _, err := changed.ApplyAlias(p); !errors.Is(err, ErrAliasStaleProposal) {
		t.Fatalf("stale alias proposal accepted: %v", err)
	}
}

func TestAliasRejectsDuplicateAndLimit(t *testing.T) {
	s := aliasStateFixture([]PlayerAlias{{Alias: "foo", Process: "bar"}})
	if _, err := s.PlanAddAlias("a", "foo", "again"); !errors.Is(err, ErrAliasDuplicate) {
		t.Fatalf("duplicate err=%v", err)
	}
	full := make([]PlayerAlias, MaxPlayerAliases)
	for i := range full {
		full[i] = PlayerAlias{Alias: "a" + strings.Repeat("x", i%10), Process: "do"}
		if i > 0 {
			full[i].Alias = full[i].Alias + string(rune('0'+i/10))
		}
	}
	// Ensure the generated bounded fixture is unique before testing the limit.
	seen := map[string]bool{}
	for i := range full {
		for seen[full[i].Alias] {
			full[i].Alias += "y"
		}
		seen[full[i].Alias] = true
	}
	fullState := aliasStateFixture(full)
	if _, err := fullState.PlanAddAlias("a", "new", "process"); !errors.Is(err, ErrAliasLimit) {
		t.Fatalf("limit err=%v", err)
	}
}

func TestAliasJSONRecoveryPreservesInsertionOrder(t *testing.T) {
	aliases := []PlayerAlias{{Alias: "one", Process: "첫째"}, {Alias: "two", Process: "둘째"}, {Alias: "three", Process: "셋째"}}
	s := aliasStateFixture(aliases)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := DecodeState(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.Players["a"].Aliases, aliases) {
		t.Fatalf("recovered aliases=%+v want=%+v", recovered.Players["a"].Aliases, aliases)
	}
}
