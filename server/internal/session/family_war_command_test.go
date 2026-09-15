package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyWarSessionFlag(body *world.LegacyMonster, bit uint, enabled bool) {
	if enabled {
		body.Flags[bit/8] |= 1 << (bit % 8)
		return
	}
	body.Flags[bit/8] &^= 1 << (bit % 8)
}

func familyWarSessionFixture(t *testing.T) []byte {
	t.Helper()
	dragon := world.LegacyMonster{Name: "Boss", Type: 0, Class: 4, RoomID: 1}
	dragon.Daily[world.FamilyDailySlot].Max = 2
	familyWarSessionFlag(&dragon, world.FamilyMemberFlag, true)
	familyWarSessionFlag(&dragon, world.FamilyBossFlag, true)
	tiger := world.LegacyMonster{Name: "Tiger", Type: 0, Class: 4, RoomID: 1}
	tiger.Daily[world.FamilyDailySlot].Max = 3
	familyWarSessionFlag(&tiger, world.FamilyMemberFlag, true)
	familyWarSessionFlag(&tiger, world.FamilyBossFlag, true)
	state := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"dragon-boss", "tiger-boss"}}},
		Players: map[string]world.PlayerState{
			"dragon-boss": {Body: dragon, Online: true},
			"tiger-boss":  {Body: tiger, Online: true},
		},
		War: &world.FamilyWar{},
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

func familyWarSessionCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
		3: {ID: 3, Name: "백호", Boss: "Tiger"},
	}}
}

func TestParseFamilyWarLineAdmitsSourceForms(t *testing.T) {
	command, ok := ParseFamilyWarLine("선전포고 백호")
	if !ok || command.Target != "백호" || !IsFamilyWarLine("선전포고 백호") {
		t.Fatalf("named=%+v ok=%t", command, ok)
	}
	bare, ok := ParseFamilyWarLine("  선전포고  ")
	if !ok || bare.Target != "" {
		t.Fatalf("bare=%+v ok=%t", bare, ok)
	}
	extra, ok := ParseFamilyWarLine("선전포고 백호 extra")
	if !ok || extra.Target != "" {
		t.Fatalf("extra=%+v ok=%t", extra, ok)
	}
	for _, line := range []string{"선전포고\n백호", "패거리공지", string([]byte{0xff})} {
		if _, ok := ParseFamilyWarLine(line); ok || IsFamilyWarLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesFamilyWar(t *testing.T) {
	for _, line := range []string{"선전포고", "선전포고 백호"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandFamilyWar {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteFamilyWarLineDeclaresAndReplaysWithoutDuplicateCommit(t *testing.T) {
	store := &departureStore{state: familyWarSessionFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("dragon-boss")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	catalog := familyWarSessionCatalog()
	first, err := owners.ExecuteFamilyWarLine(context.Background(), store, "w", "family-war-1", lease, "선전포고 백호", catalog)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FamilyWarResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.FamilyWarDeclare || result.After.CalledBy != 2 || result.After.CalledAgainst != 3 || !result.Changed {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.War == nil || saved.War.CalledBy != 2 || saved.War.CalledAgainst != 3 || saved.War.Active != 0 {
		t.Fatalf("saved war=%+v", saved.War)
	}
	replay, err := owners.ExecuteFamilyWarLine(context.Background(), store, "w", "family-war-1", lease, "선전포고 백호", catalog)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteFamilyWarLine(context.Background(), store, "w", "family-war-bad", lease, "패거리공지", catalog); !errors.Is(err, ErrUnsupportedFamilyWarLine) {
		t.Fatalf("unsupported err=%v", err)
	}
}
