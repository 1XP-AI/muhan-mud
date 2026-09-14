package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type sessionForgeCatalog map[int16]world.LegacyObject

func (sessionForgeCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c sessionForgeCatalog) Object(id int16) (world.LegacyObject, error) {
	object, ok := c[id]
	if !ok {
		return world.LegacyObject{}, fmt.Errorf("unmigrated object %d", id)
	}
	return object, nil
}

func sessionForgeWeaponCatalog() sessionForgeCatalog {
	return sessionForgeCatalog{
		900: {Name: "무명도", Weight: 1},
		901: {Name: "무명검", Weight: 1},
		902: {Name: "무명봉", Weight: 1},
		903: {Name: "무명창", Weight: 1},
		904: {Name: "무명궁", Weight: 1},
	}
}

func sessionForgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[world.ForgeRoomFlag/8] |= 1 << (world.ForgeRoomFlag % 8)
	}
	return flags
}

func sessionForgeFixture(t *testing.T, rforge bool) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "대장간", Flags: sessionForgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
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

func sessionForgeMaterialFixture(t *testing.T, gold int32, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "대장간", Flags: sessionForgeRoomFlags(true)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: gold, Class: class},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.ForgeReadingFlag/8] |= 1 << (world.ForgeReadingFlag % 8)
	s.Players["actor"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sessionForgeSelectArmFixture(t *testing.T, gold int32) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "대장간", Flags: sessionForgeRoomFlags(true)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: gold}, Online: true},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.ForgeReadingFlag/8] |= 1 << (world.ForgeReadingFlag % 8)
	s.Players["actor"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitForgeOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseForgeLineAdmitsBareAliasOnly(t *testing.T) {
	command, ok := ParseForgeLine("  제련  ")
	if !ok || command.Alias != "제련" || !IsForgeLine("제련") || !IsForgeStartLine("제련") || !ParseForgeStartLine("제련") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{
		"제련 검", "검 제련", "무기만들기", "forge", "제련\n", "제련\x00", string([]byte{0xff}),
	} {
		if _, ok := ParseForgeLine(line); ok || IsForgeLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesForge(t *testing.T) {
	parsed, err := ParseCommand("제련")
	if err != nil || parsed.Kind != CommandForge {
		t.Fatalf("ParseCommand(제련)=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"제련 검", "검 제련"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	parsed, err = ParseCommand("무기만들기")
	if err != nil || parsed.Kind != CommandNewForge {
		t.Fatalf("ParseCommand(무기만들기)=%+v err=%v want CommandNewForge", parsed, err)
	}
}

func TestExecuteForgeLineStartsContinuationAndReplaysWithoutReprompt(t *testing.T) {
	initial := sessionForgeFixture(t, true)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-1", lease, "제련")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgePrompt || !result.Changed || result.Continuation != world.ForgeSelectArmPhase ||
		result.Response != world.ForgePromptResponse || result.NoBroadcast || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI without PNOBRD", saved.Players["actor"].Body.Flags)
	}
	replay, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-1", lease, "제련")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
	if world.PlayerFlagSet(replayed.Players["actor"].Body, world.ForgeNoBroadcastFlag) {
		t.Fatal("replay re-set PNOBRD")
	}
}

func TestExecuteForgeLineClearsExistingPNOBRDAndReplayDoesNotReset(t *testing.T) {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "대장간", Flags: sessionForgeRoomFlags(true)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.ForgeNoBroadcastFlag/8] |= 1 << (world.ForgeNoBroadcastFlag % 8)
	s.Players["actor"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-mute", lease, "제련")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoBroadcast || !result.Reading || world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatalf("result=%+v flags=%+v", result, saved.Players["actor"].Body.Flags)
	}
	replay, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-mute", lease, "제련")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
	if world.PlayerFlagSet(replayed.Players["actor"].Body, world.ForgeNoBroadcastFlag) {
		t.Fatal("replay re-set PNOBRD")
	}
}

func TestExecuteForgeLineMissingRForgeDoesNotMutate(t *testing.T) {
	initial := sessionForgeFixture(t, false)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-not", lease, "제련")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != world.ForgeNotForge || result.Response != world.ForgeNotForgeResponse {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-not", lease, "제련")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeLineRejectsUnsupportedAndUnmigratedBeforeReceipt(t *testing.T) {
	initial := sessionForgeFixture(t, true)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	if _, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-bad", lease, "무기만들기"); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("newforge err=%v commits=%d", err, store.commits)
	}
	unmigrated := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	if _, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-unmigrated", lease, "제련"); err == nil || store.commits != 0 {
		t.Fatalf("unmigrated err=%v commits=%d", err, store.commits)
	}
}

func TestParseForgeSelectArmLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseForgeSelectArmLine("2")
	if !ok || command.Input != "2" || command.Choice != 2 || !IsForgeSelectArmLine("2") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseForgeSelectArmLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "6", "x", " 1", "도", "무기만들기"} {
		command, ok := ParseForgeSelectArmLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseForgeSelectArmLine("제련"); ok || IsForgeSelectArmLine("제련") {
		t.Fatal("select-arm parser claimed the start alias")
	}
	if _, ok := ParseForgeSelectArmLine("  제련  "); ok {
		t.Fatal("padded start alias admitted as select-arm")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseForgeSelectArmLine(line); ok || IsForgeSelectArmLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteForgeSelectArmLineLoadsTemplatesAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	names := map[string]struct {
		choice int
		id     int16
		name   string
	}{
		"1": {1, 900, "무명도"},
		"2": {2, 901, "무명검"},
		"3": {3, 902, "무명봉"},
		"4": {4, 903, "무명창"},
		"5": {5, 904, "무명궁"},
	}
	for line, want := range names {
		initial := sessionForgeSelectArmFixture(t, 50000)
		store := &departureStore{state: initial}
		owners, lease := admitForgeOwner(t)
		first, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-"+line, lease, line, catalog)
		if err != nil || first.Replayed || store.commits != 1 {
			t.Fatalf("line %q first=%+v err=%v commits=%d", line, first, err, store.commits)
		}
		var result world.ForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.ForgeMaterial || result.Changed || result.Continuation != world.ForgeMaterialPhase ||
			result.Response != world.ForgeMaterialResponse || result.WeaponChoice != want.choice ||
			result.ObjectID != want.id || result.ObjectName != want.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("line %q result=%+v", line, result)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("line %q gold=%d", line, saved.Players["actor"].Body.Gold)
		}
		if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeNoBroadcastFlag) ||
			!world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
			t.Fatalf("line %q flags=%+v", line, saved.Players["actor"].Body.Flags)
		}
		replay, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-"+line, lease, line, catalog)
		if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("line %q replay=%+v err=%v commits=%d", line, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("line %q replay gold err=%v gold=%d", line, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteForgeSelectArmLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-bad", lease, "6", catalog)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeReprompt || result.Changed || result.Continuation != world.ForgeSelectArmPhase ||
		result.Response != world.ForgeRepromptResponse || result.ObjectID != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-bad", lease, "6", catalog)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("replay mutated state")
	}
}

func TestExecuteForgeSelectArmLineStartThenChoiceDoesNotChargeGold(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	s, err := world.DecodeState(sessionForgeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	s.Players["actor"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitForgeOwner(t)
	start, err := owners.ExecuteForgeLine(context.Background(), store, "w", "forge-start", lease, "제련")
	if err != nil || start.Replayed || store.commits != 1 {
		t.Fatalf("start=%+v err=%v commits=%d", start, err, store.commits)
	}
	before, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	gold := before.Players["actor"].Body.Gold
	choice, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-choice-1", lease, "1", catalog)
	if err != nil || choice.Replayed || store.commits != 2 {
		t.Fatalf("choice=%+v err=%v commits=%d", choice, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(choice.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeMaterial || result.ObjectID != 900 || result.ObjectName != "무명도" ||
		result.Continuation != world.ForgeMaterialPhase || result.Changed {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != gold {
		t.Fatalf("gold before=%d after=%d", gold, saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-choice-1", lease, "1", catalog)
	if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, choice.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectArmLineFailClosedUnmigratedCatalogAndIdleActor(t *testing.T) {
	owners, lease := admitForgeOwner(t)
	started := sessionForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-nil", lease, "1", nil); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	partial := sessionForgeWeaponCatalog()
	delete(partial, 901)
	if _, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-missing", lease, "2", partial); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("missing 901 err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed catalog mutated state")
	}
	idle := sessionForgeFixture(t, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-idle", lease, "1", sessionForgeWeaponCatalog()); !errors.Is(err, world.ErrForgeNotReading) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-start", lease, "제련", sessionForgeWeaponCatalog()); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-ctrl", lease, "1\n", sessionForgeWeaponCatalog()); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}

func TestParseForgeSelectMaterialLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseForgeSelectMaterialLine("2")
	if !ok || command.Input != "2" || command.Choice != 2 || !IsForgeSelectMaterialLine("2") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseForgeSelectMaterialLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "4", "5", "6", "x", " 1", "도", "무기만들기"} {
		command, ok := ParseForgeSelectMaterialLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseForgeSelectMaterialLine("제련"); ok || IsForgeSelectMaterialLine("제련") {
		t.Fatal("material parser claimed the start alias")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseForgeSelectMaterialLine(line); ok || IsForgeSelectMaterialLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteForgeSelectMaterialLineSetsSdiceAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	cases := []struct {
		arm      string
		material string
		choice   int
		id       int16
		name     string
		dice     int16
		sum      int32
	}{
		{"1", "1", 1, 900, "무명도", world.ForgeSteelDice, world.ForgeSteelCost},
		{"2", "2 extra", 2, 901, "무명검", world.ForgePreciousDice, world.ForgePreciousCost},
		{"3", "3", 3, 902, "무명봉", world.ForgeDiamondDice, world.ForgeDiamondCost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionForgeMaterialFixture(t, 50000, 4))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitForgeOwner(t)
		arm, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.ForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		first, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || first.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, first, err, store.commits)
		}
		var result world.ForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.ForgeQuench || result.Changed || result.Continuation != world.ForgeQuenchPhase ||
			result.Response != world.ForgeQuenchResponse || result.MaterialChoice != tt.choice ||
			result.DiceSides != tt.dice || result.Sum != tt.sum || result.ObjectID != tt.id ||
			result.ObjectName != tt.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("material %q result=%+v", tt.material, result)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("material %q gold=%d", tt.material, saved.Players["actor"].Body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("material %q invented items", tt.material)
		}
		replay, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("material %q replay=%+v err=%v commits=%d", tt.material, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("material %q replay gold err=%v gold=%d", tt.material, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteForgeSelectMaterialLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 50000, 4)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-bad", lease, "6", catalog, 900)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeReprompt || result.Changed || result.Continuation != world.ForgeMaterialPhase ||
		result.Response != world.ForgeRepromptResponse || result.Sum != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-bad", lease, "6", catalog, 900)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectMaterialLineDeniesRestrictedClassWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 300000, world.ForgeClericClass)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-cleric", lease, "3", catalog, 900)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeMaterialDenied || result.Changed || result.Continuation != world.ForgeMaterialPhase ||
		result.Response != world.ForgeMaterialDeniedResponse || result.Sum != 0 || result.DiceSides != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 300000 {
		t.Fatalf("denied gold=%d", saved.Players["actor"].Body.Gold)
	}
}

func TestExecuteForgeSelectMaterialLineFailClosedUnmigratedGoldGraphAndIdle(t *testing.T) {
	owners, lease := admitForgeOwner(t)
	catalog := sessionForgeWeaponCatalog()
	started := sessionForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-nil-items", lease, "1", catalog, 900); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("nil items err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed graph mutated state")
	}
	ready := sessionForgeMaterialFixture(t, 50000, 4)
	store = &departureStore{state: ready}
	if _, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-nil-cat", lease, "1", nil, 900); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	idle := sessionForgeFixture(t, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-idle", lease, "1", catalog, 900); !errors.Is(err, world.ErrForgeNotReading) && !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-start", lease, "제련", catalog, 900); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
}

func TestParseForgeSelectQuenchLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseForgeSelectQuenchLine("4")
	if !ok || command.Input != "4" || command.Choice != 4 || !IsForgeSelectQuenchLine("4") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseForgeSelectQuenchLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "6", "x", " 1", "도", "무기만들기"} {
		command, ok := ParseForgeSelectQuenchLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseForgeSelectQuenchLine("제련"); ok || IsForgeSelectQuenchLine("제련") {
		t.Fatal("quench parser claimed the start alias")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseForgeSelectQuenchLine(line); ok || IsForgeSelectQuenchLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteForgeSelectQuenchLineSetsShotsAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	cases := []struct {
		arm, material, quench string
		choice                int
		id                    int16
		name                  string
		shots                 int16
		materialSum           int32
		quenchCost            int32
	}{
		{"1", "1", "1", 1, 900, "무명도", world.ForgeQuench100Shots, world.ForgeSteelCost, world.ForgeQuench100Cost},
		{"2", "2 extra", "2 extra", 2, 901, "무명검", world.ForgeQuench200Shots, world.ForgePreciousCost, world.ForgeQuench200Cost},
		{"3", "3", "3", 3, 902, "무명봉", world.ForgeQuench300Shots, world.ForgeDiamondCost, world.ForgeQuench300Cost},
		{"4", "1", "4", 4, 903, "무명창", world.ForgeQuench400Shots, world.ForgeSteelCost, world.ForgeQuench400Cost},
		{"5", "1", "5", 5, 904, "무명궁", world.ForgeQuench500Shots, world.ForgeSteelCost, world.ForgeQuench500Cost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionForgeMaterialFixture(t, 50000, 4))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitForgeOwner(t)
		arm, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.ForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		material, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || material.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, material, err, store.commits)
		}
		var mat world.ForgeResult
		if err := json.Unmarshal(material.Response, &mat); err != nil {
			t.Fatal(err)
		}
		if mat.Sum != tt.materialSum {
			t.Fatalf("material %q sum=%d want %d", tt.material, mat.Sum, tt.materialSum)
		}
		first, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, mat.Sum)
		if err != nil || first.Replayed || store.commits != 3 {
			t.Fatalf("quench %q first=%+v err=%v commits=%d", tt.quench, first, err, store.commits)
		}
		var result world.ForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.ForgeName || result.Changed || result.Continuation != world.ForgeNamePhase ||
			result.Response != world.ForgeNameResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != tt.materialSum+tt.quenchCost || result.ObjectID != tt.id ||
			result.ObjectName != tt.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("quench %q result=%+v", tt.quench, result)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("quench %q gold=%d", tt.quench, saved.Players["actor"].Body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("quench %q invented items", tt.quench)
		}
		replay, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, mat.Sum)
		if err != nil || !replay.Replayed || store.commits != 3 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("quench %q replay=%+v err=%v commits=%d", tt.quench, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("quench %q replay gold err=%v gold=%d", tt.quench, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteForgeSelectQuenchLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 50000, 4)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-bad", lease, "6", catalog, 900, world.ForgeSteelCost)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeReprompt || result.Changed || result.Continuation != world.ForgeQuenchPhase ||
		result.Response != world.ForgeRepromptResponse || result.Sum != 0 || result.ShotsMax != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-bad", lease, "6", catalog, 900, world.ForgeSteelCost)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectQuenchLineFailClosedUnmigratedGoldGraphAndIdle(t *testing.T) {
	owners, lease := admitForgeOwner(t)
	catalog := sessionForgeWeaponCatalog()
	started := sessionForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-nil-items", lease, "1", catalog, 900, world.ForgeSteelCost); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("nil items err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed graph mutated state")
	}
	ready := sessionForgeMaterialFixture(t, 50000, 4)
	store = &departureStore{state: ready}
	if _, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-nil-cat", lease, "1", nil, 900, world.ForgeSteelCost); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-zero-sum", lease, "1", catalog, 900, 0); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero sum err=%v commits=%d", err, store.commits)
	}
	idle := sessionForgeFixture(t, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-idle", lease, "1", catalog, 900, world.ForgeSteelCost); !errors.Is(err, world.ErrForgeNotReading) && !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-start", lease, "제련", catalog, 900, world.ForgeSteelCost); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
}

func TestParseForgeSelectNameLineAdmitsNamesAndInvalid(t *testing.T) {
	command, ok := ParseForgeSelectNameLine("불의검")
	if !ok || command.Input != "불의검" || !IsForgeSelectNameLine("불의검") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseForgeSelectNameLine("abc")
	if !ok || command.Input != "abc" {
		t.Fatalf("ascii command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "ab", "ab(c", "123456789012345678901", "무기만들기", "예"} {
		command, ok := ParseForgeSelectNameLine(line)
		if !ok || command.Input != line {
			t.Fatalf("admitted %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseForgeSelectNameLine("제련"); ok || IsForgeSelectNameLine("제련") {
		t.Fatal("name parser claimed the start alias")
	}
	for _, line := range []string{"abc\n", "abc\x00", string([]byte{0xff})} {
		if _, ok := ParseForgeSelectNameLine(line); ok || IsForgeSelectNameLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteForgeSelectNameLineSetsNameAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	cases := []struct {
		arm, material, quench, name string
		choice                      int
		id                          int16
		shots                       int16
		materialSum                 int32
		quenchCost                  int32
	}{
		{"1", "1", "1", "abc", 1, 900, world.ForgeQuench100Shots, world.ForgeSteelCost, world.ForgeQuench100Cost},
		{"2", "2 extra", "2 extra", "불의검", 2, 901, world.ForgeQuench200Shots, world.ForgePreciousCost, world.ForgeQuench200Cost},
		{"3", "3", "3", "12345678901234567890", 3, 902, world.ForgeQuench300Shots, world.ForgeDiamondCost, world.ForgeQuench300Cost},
		{"4", "1", "4", "무명창이름", 4, 903, world.ForgeQuench400Shots, world.ForgeSteelCost, world.ForgeQuench400Cost},
		{"5", "1", "5", "xyz", 5, 904, world.ForgeQuench500Shots, world.ForgeSteelCost, world.ForgeQuench500Cost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionForgeMaterialFixture(t, 50000, 4))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitForgeOwner(t)
		arm, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.ForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		material, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || material.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, material, err, store.commits)
		}
		var mat world.ForgeResult
		if err := json.Unmarshal(material.Response, &mat); err != nil {
			t.Fatal(err)
		}
		if mat.Sum != tt.materialSum {
			t.Fatalf("material %q sum=%d want %d", tt.material, mat.Sum, tt.materialSum)
		}
		quench, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, mat.Sum)
		if err != nil || quench.Replayed || store.commits != 3 {
			t.Fatalf("quench %q first=%+v err=%v commits=%d", tt.quench, quench, err, store.commits)
		}
		var quenched world.ForgeResult
		if err := json.Unmarshal(quench.Response, &quenched); err != nil {
			t.Fatal(err)
		}
		first, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-"+tt.arm, lease, tt.name, catalog, loaded.ObjectID, mat.Sum, quenched.QuenchChoice)
		if err != nil || first.Replayed || store.commits != 4 {
			t.Fatalf("name %q first=%+v err=%v commits=%d", tt.name, first, err, store.commits)
		}
		var result world.ForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.ForgeConfirm || result.Changed || result.Continuation != world.ForgeConfirmPhase ||
			result.Response != world.ForgeConfirmResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != tt.materialSum+tt.quenchCost || result.ObjectID != tt.id ||
			result.ObjectName != tt.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("name %q result=%+v", tt.name, result)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("name %q gold=%d", tt.name, saved.Players["actor"].Body.Gold)
		}
		if saved.Players["actor"].Items == nil || len(saved.Players["actor"].Items.Items) != 0 {
			t.Fatalf("name %q invented items", tt.name)
		}
		replay, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-"+tt.arm, lease, tt.name, catalog, loaded.ObjectID, mat.Sum, quenched.QuenchChoice)
		if err != nil || !replay.Replayed || store.commits != 4 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("name %q replay=%+v err=%v commits=%d", tt.name, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("name %q replay gold err=%v gold=%d", tt.name, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteForgeSelectNameLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 50000, 4)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-bad", lease, "ab", catalog, 900, world.ForgeSteelCost, 1)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeReprompt || result.Changed || result.Continuation != world.ForgeNamePhase ||
		result.Response != world.ForgeNameShortResponse || result.Sum != 0 || result.ObjectName != "" {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid name mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-bad", lease, "ab", catalog, 900, world.ForgeSteelCost, 1)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectNameLineFailClosedUnmigratedGoldGraphAndIdle(t *testing.T) {
	owners, lease := admitForgeOwner(t)
	catalog := sessionForgeWeaponCatalog()
	started := sessionForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-nil-items", lease, "abc", catalog, 900, world.ForgeSteelCost, 1); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("nil items err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed graph mutated state")
	}
	ready := sessionForgeMaterialFixture(t, 50000, 4)
	store = &departureStore{state: ready}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-nil-cat", lease, "abc", nil, 900, world.ForgeSteelCost, 1); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-zero-sum", lease, "abc", catalog, 900, 0, 1); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero sum err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-zero-quench", lease, "abc", catalog, 900, world.ForgeSteelCost, 0); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero quench err=%v commits=%d", err, store.commits)
	}
	idle := sessionForgeFixture(t, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-idle", lease, "abc", catalog, 900, world.ForgeSteelCost, 1); !errors.Is(err, world.ErrForgeNotReading) && !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-start", lease, "제련", catalog, 900, world.ForgeSteelCost, 1); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
}

func TestParseForgeSelectConfirmLineAdmitsYesNoAndCancel(t *testing.T) {
	command, ok := ParseForgeSelectConfirmLine("예")
	if !ok || command.Input != "예" || !command.Yes || !IsForgeSelectConfirmLine("예") {
		t.Fatalf("yes command=%+v ok=%t", command, ok)
	}
	command, ok = ParseForgeSelectConfirmLine("예스")
	if !ok || !command.Yes {
		t.Fatalf("prefix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"아니오", "", "무기만들기", "제련", "no", "abc"} {
		command, ok := ParseForgeSelectConfirmLine(line)
		if !ok || command.Yes || command.Input != line || !IsForgeSelectConfirmLine(line) {
			t.Fatalf("cancel %q command=%+v ok=%t", line, command, ok)
		}
	}
	for _, line := range []string{"예\n", "예\x00", string([]byte{0xff})} {
		if _, ok := ParseForgeSelectConfirmLine(line); ok || IsForgeSelectConfirmLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteForgeSelectConfirmLineChargesGoldAddsWeaponAndReplaysWithoutRecharge(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	s, err := world.DecodeState(sessionForgeMaterialFixture(t, 100000, 4))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitForgeOwner(t)
	arm, err := owners.ExecuteForgeSelectArmLine(context.Background(), store, "w", "forge-arm-confirm", lease, "1", catalog)
	if err != nil || arm.Replayed || store.commits != 1 {
		t.Fatalf("arm first=%+v err=%v commits=%d", arm, err, store.commits)
	}
	material, err := owners.ExecuteForgeSelectMaterialLine(context.Background(), store, "w", "forge-mat-confirm", lease, "1", catalog, 900)
	if err != nil || material.Replayed || store.commits != 2 {
		t.Fatalf("material first=%+v err=%v commits=%d", material, err, store.commits)
	}
	quench, err := owners.ExecuteForgeSelectQuenchLine(context.Background(), store, "w", "forge-quench-confirm", lease, "1", catalog, 900, world.ForgeSteelCost)
	if err != nil || quench.Replayed || store.commits != 3 {
		t.Fatalf("quench first=%+v err=%v commits=%d", quench, err, store.commits)
	}
	name, err := owners.ExecuteForgeSelectNameLine(context.Background(), store, "w", "forge-name-confirm", lease, "불의검", catalog, 900, world.ForgeSteelCost, 1)
	if err != nil || name.Replayed || store.commits != 4 {
		t.Fatalf("name first=%+v err=%v commits=%d", name, err, store.commits)
	}
	first, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-1", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 5 {
		t.Fatalf("confirm first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	wantSum := world.ForgeSteelCost + world.ForgeQuench100Cost
	if result.Action != world.ForgeGive || !result.Changed || result.Continuation != 0 ||
		result.Response != world.ForgeGiveResponse || result.ObjectName != "불의검" ||
		result.Sum != wantSum || result.GoldAfter != 100000-wantSum || result.ItemID == "" ||
		result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 100000-wantSum {
		t.Fatalf("gold=%d", saved.Players["actor"].Body.Gold)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.ForgeReadingFlag) {
		t.Fatal("confirm left PREADI set")
	}
	item, ok := saved.Players["actor"].Items.Items[result.ItemID]
	if !ok || item.Object.Name != "불의검" || len(saved.Players["actor"].Items.Inventory) != 1 {
		t.Fatalf("items=%+v itemID=%q", saved.Players["actor"].Items, result.ItemID)
	}
	replay, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-1", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 5 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Gold != 100000-wantSum {
		t.Fatalf("replay gold err=%v gold=%d", err, replayed.Players["actor"].Body.Gold)
	}
	if len(replayed.Players["actor"].Items.Items) != 1 {
		t.Fatalf("replay invented items=%+v", replayed.Players["actor"].Items)
	}
}

func TestExecuteForgeSelectConfirmLineInsufficientGoldDoesNotCharge(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 50000, 4)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-poor", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeTooPoor || result.ItemID != "" || result.GoldAfter != 50000 ||
		result.Response != world.ForgeTooPoorResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("poor mutated gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	replay, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-poor", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectConfirmLineCancelDoesNotCharge(t *testing.T) {
	catalog := sessionForgeWeaponCatalog()
	initial := sessionForgeMaterialFixture(t, 100000, 4)
	store := &departureStore{state: initial}
	owners, lease := admitForgeOwner(t)
	first, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-cancel", lease, "아니오", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.ForgeCancel || result.ItemID != "" || result.GoldAfter != 100000 ||
		result.Response != world.ForgeCancelResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 100000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("cancel mutated gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	replay, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-cancel", lease, "아니오", catalog, 900, world.ForgeSteelCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteForgeSelectConfirmLineFailClosedUnmigratedGoldGraphAndIdle(t *testing.T) {
	owners, lease := admitForgeOwner(t)
	catalog := sessionForgeWeaponCatalog()
	started := sessionForgeSelectArmFixture(t, 100000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-nil-items", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "abc"); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("nil items err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed graph mutated state")
	}
	ready := sessionForgeMaterialFixture(t, 100000, 4)
	store = &departureStore{state: ready}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-nil-cat", lease, "예", nil, 900, world.ForgeSteelCost, 1, "abc"); !errors.Is(err, world.ErrForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-zero-sum", lease, "예", catalog, 900, 0, 1, "abc"); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero sum err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-zero-quench", lease, "예", catalog, 900, world.ForgeSteelCost, 0, "abc"); !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero quench err=%v commits=%d", err, store.commits)
	}
	idle := sessionForgeFixture(t, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-idle", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "abc"); !errors.Is(err, world.ErrForgeNotReading) && !errors.Is(err, world.ErrForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteForgeSelectConfirmLine(context.Background(), store, "w", "forge-confirm-control", lease, "예\n", catalog, 900, world.ForgeSteelCost, 1, "abc"); !errors.Is(err, ErrUnsupportedForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}
