package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseSocialLineAdmitsBareAliasesAndExplicitLongMode(t *testing.T) {
	tests := []struct {
		line string
		kind string
		long bool
	}{
		{line: "누구", kind: "who"},
		{line: " 누구 ", kind: "who"},
		{line: "누구 l", kind: "who", long: true},
		{line: "그룹", kind: "group"},
		{line: "무리", kind: "group"},
	}
	for _, tt := range tests {
		got, ok := ParseSocialLine(tt.line)
		if !ok || got.Kind != tt.kind || got.Long != tt.long {
			t.Fatalf("ParseSocialLine(%q)=%+v,%v want kind=%q long=%t", tt.line, got, ok, tt.kind, tt.long)
		}
	}
}

func TestParseSocialLineRejectsMalformedExtraArguments(t *testing.T) {
	for _, line := range []string{
		"누구 x", "누구 L", "누구 l l", "누구 l extra", "누구\n", "누구\x00", "누구 그룹", "그룹 l", "무리 l", "",
	} {
		if got, ok := ParseSocialLine(line); ok || got != (SocialCommand{}) {
			t.Fatalf("ParseSocialLine(%q)=%+v,%v want fail-closed", line, got, ok)
		}
	}
}

func TestExecuteSocialLinePersistsReadOnlyReceiptAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-1", lease, "누구")
	if err != nil || !strings.Contains(string(first.Response), "Alice") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-1", lease, "누구")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSocialLineSupportsLegacyGroupAlias(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	group, err := owners.ExecuteSocialLine(context.Background(), store, "w", "group-1", lease, "그룹")
	if err != nil {
		t.Fatalf("그룹 err=%v", err)
	}
	party, err := owners.ExecuteSocialLine(context.Background(), store, "w", "group-2", lease, "무리")
	if err != nil {
		t.Fatalf("무리 err=%v", err)
	}
	if group.Replayed || party.Replayed || store.commits != 2 || string(group.Response) != string(party.Response) {
		t.Fatalf("group=%+v party=%+v commits=%d", group, party, store.commits)
	}

	replay, err := owners.ExecuteSocialLine(context.Background(), store, "w", "group-2", lease, "무리")
	if err != nil || !replay.Replayed || store.commits != 2 || string(replay.Response) != string(party.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSocialLineSupportsExplicitWhoLongAndReplaysReadOnly(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-long-1", lease, "누구 l")
	if err != nil || store.commits != 1 || !strings.Contains(string(first.Response), "종족") {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-long-1", lease, "누구 l")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteSocialLineUsesProvidedFamilyCatalogWithoutMutatingState(t *testing.T) {
	state, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Body.Flags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	actor.Body.Daily[world.FamilyDailySlot].Max = 2
	state.Players["a"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	catalog := world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "문주"}}}
	first, err := owners.ExecuteSocialLine(context.Background(), store, "w", "who-family-1", lease, "누구 l", catalog)
	if err != nil || !strings.Contains(string(first.Response), "청룡") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Daily[world.FamilyDailySlot].Max != 2 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteSocialLineRejectsMalformedInputWithoutReceipt(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"누구 x", "누구 l l", "누구\n"} {
		if _, err := owners.ExecuteSocialLine(context.Background(), store, "w", "bad-"+strings.ReplaceAll(line, " ", "-"), lease, line); !errors.Is(err, ErrUnsupportedSocialLine) {
			t.Fatalf("line=%q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("malformed line committed: %q commits=%d", line, store.commits)
		}
	}
}
