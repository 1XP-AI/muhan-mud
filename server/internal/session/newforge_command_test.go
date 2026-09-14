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

type sessionNewForgeCatalog map[int16]world.LegacyObject

func (sessionNewForgeCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{}, errors.New("monster lookup not used")
}

func (c sessionNewForgeCatalog) Object(id int16) (world.LegacyObject, error) {
	object, ok := c[id]
	if !ok {
		return world.LegacyObject{}, fmt.Errorf("unmigrated object %d", id)
	}
	return object, nil
}

func sessionNewForgeWeaponCatalog() sessionNewForgeCatalog {
	return sessionNewForgeCatalog{
		900: {Name: "무명도", Weight: 1},
		901: {Name: "무명검", Weight: 1},
		902: {Name: "무명봉", Weight: 1},
		903: {Name: "무명창", Weight: 1},
		904: {Name: "무명궁", Weight: 1},
	}
}

func sessionNewForgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[world.NewForgeRoomFlag/8] |= 1 << (world.NewForgeRoomFlag % 8)
	}
	return flags
}

func sessionNewForgeFixture(t *testing.T, roomID int16, rforge bool) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			roomID: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: roomID, Name: "대장간", Flags: sessionNewForgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: roomID}, Online: true},
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

func admitNewForgeOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseNewForgeLineAdmitsBareAliasOnly(t *testing.T) {
	command, ok := ParseNewForgeLine("  무기만들기  ")
	if !ok || command.Alias != "무기만들기" || !IsNewForgeLine("무기만들기") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{
		"무기만들기 검", "검 무기만들기", "제련", "newforge", "무기만들기\n", "무기만들기\x00", string([]byte{0xff}),
	} {
		if _, ok := ParseNewForgeLine(line); ok || IsNewForgeLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesNewForge(t *testing.T) {
	parsed, err := ParseCommand("무기만들기")
	if err != nil || parsed.Kind != CommandNewForge {
		t.Fatalf("ParseCommand(무기만들기)=%+v err=%v", parsed, err)
	}
	for _, line := range []string{"무기만들기 검", "제련", "검 무기만들기"} {
		parsed, err := ParseCommand(line)
		if err != nil {
			t.Fatalf("ParseCommand(%q) err=%v", line, err)
		}
		if line == "제련" {
			if parsed.Kind != CommandForge {
				t.Fatalf("ParseCommand(제련)=%+v want CommandForge", parsed)
			}
			continue
		}
		if parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteNewForgeLineStartsContinuationAndReplaysWithoutReprompt(t *testing.T) {
	initial := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-1", lease, "무기만들기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgePrompt || !result.Changed || result.Continuation != world.NewForgeSelectArmPhase ||
		result.Response != world.NewForgePromptResponse || result.NoBroadcast || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatalf("saved flags=%+v want PREADI without PNOBRD", saved.Players["actor"].Body.Flags)
	}
	replay, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-1", lease, "무기만들기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
	if world.PlayerFlagSet(replayed.Players["actor"].Body, world.NewForgeNoBroadcastFlag) {
		t.Fatal("replay re-set PNOBRD")
	}
}

func TestExecuteNewForgeLineClearsExistingPNOBRDAndReplayDoesNotReset(t *testing.T) {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			world.NewForgeRoomID: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: world.NewForgeRoomID, Name: "대장간", Flags: sessionNewForgeRoomFlags(true),
				}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: world.NewForgeRoomID}, Online: true},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.NewForgeNoBroadcastFlag/8] |= 1 << (world.NewForgeNoBroadcastFlag % 8)
	s.Players["actor"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-mute", lease, "무기만들기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if result.NoBroadcast || !result.Reading || world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeNoBroadcastFlag) ||
		!world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatalf("result=%+v flags=%+v", result, saved.Players["actor"].Body.Flags)
	}
	replay, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-mute", lease, "무기만들기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Flags != saved.Players["actor"].Body.Flags {
		t.Fatalf("replay mutated flags err=%v", err)
	}
	if world.PlayerFlagSet(replayed.Players["actor"].Body, world.NewForgeNoBroadcastFlag) {
		t.Fatal("replay re-set PNOBRD")
	}
}

func TestExecuteNewForgeLineMissingRForgeDoesNotMutate(t *testing.T) {
	initial := sessionNewForgeFixture(t, world.NewForgeRoomID, false)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-not", lease, "무기만들기")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != world.NewForgeNotForge || result.Response != world.NewForgeNotForgeResponse {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-not", lease, "무기만들기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNewForgeLineWrongRoomDoesNotMutate(t *testing.T) {
	initial := sessionNewForgeFixture(t, 1, true)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-room", lease, "무기만들기")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != world.NewForgeWrongRoom || result.Response != world.NewForgeWrongRoomResponse {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-room", lease, "무기만들기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNewForgeLineRejectsUnsupportedAndUnmigratedBeforeReceipt(t *testing.T) {
	initial := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	if _, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-bad", lease, "제련"); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("forge err=%v commits=%d", err, store.commits)
	}
	unmigrated := world.State{
		Version: 1,
		Rooms:   map[int16]world.RoomState{},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: world.NewForgeRoomID}, Online: true},
		},
	}
	raw, err := json.Marshal(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	if _, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-unmigrated", lease, "무기만들기"); err == nil || store.commits != 0 {
		t.Fatalf("unmigrated err=%v commits=%d", err, store.commits)
	}
}

func sessionNewForgeSelectArmFixture(t *testing.T, gold int32) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			world.NewForgeRoomID: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: world.NewForgeRoomID, Name: "대장간", Flags: sessionNewForgeRoomFlags(true)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: world.NewForgeRoomID, Gold: gold}, Online: true},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.NewForgeReadingFlag/8] |= 1 << (world.NewForgeReadingFlag % 8)
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

func TestParseNewForgeSelectArmLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseNewForgeSelectArmLine("2")
	if !ok || command.Input != "2" || command.Choice != 2 || !IsNewForgeSelectArmLine("2") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseNewForgeSelectArmLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "6", "x", " 1", "도", "제련"} {
		command, ok := ParseNewForgeSelectArmLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseNewForgeSelectArmLine("무기만들기"); ok || IsNewForgeSelectArmLine("무기만들기") {
		t.Fatal("select-arm parser claimed the start alias")
	}
	if _, ok := ParseNewForgeSelectArmLine("  무기만들기  "); ok {
		t.Fatal("padded start alias admitted as select-arm")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseNewForgeSelectArmLine(line); ok || IsNewForgeSelectArmLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteNewForgeSelectArmLineLoadsTemplatesAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
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
		initial := sessionNewForgeSelectArmFixture(t, 50000)
		store := &departureStore{state: initial}
		owners, lease := admitNewForgeOwner(t)
		first, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-"+line, lease, line, catalog)
		if err != nil || first.Replayed || store.commits != 1 {
			t.Fatalf("line %q first=%+v err=%v commits=%d", line, first, err, store.commits)
		}
		var result world.NewForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.NewForgeMaterial || result.Changed || result.Continuation != world.NewForgeMaterialPhase ||
			result.Response != world.NewForgeMaterialResponse || result.WeaponChoice != want.choice ||
			result.ObjectID != want.id || result.ObjectName != want.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("line %q result=%+v", line, result)
		}
		if result.Response == world.ForgeMaterialResponse {
			t.Fatalf("line %q reused 제련 material prompt", line)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("line %q gold=%d", line, saved.Players["actor"].Body.Gold)
		}
		if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeNoBroadcastFlag) ||
			!world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
			t.Fatalf("line %q flags=%+v", line, saved.Players["actor"].Body.Flags)
		}
		replay, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-"+line, lease, line, catalog)
		if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("line %q replay=%+v err=%v commits=%d", line, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("line %q replay gold err=%v gold=%d", line, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteNewForgeSelectArmLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-bad", lease, "6", catalog)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeReprompt || result.Changed || result.Continuation != world.NewForgeSelectArmPhase ||
		result.Response != world.NewForgeRepromptResponse || result.ObjectID != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-bad", lease, "6", catalog)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("replay mutated state")
	}
}

func TestExecuteNewForgeSelectArmLineStartThenChoiceDoesNotChargeGold(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	s, err := world.DecodeState(sessionNewForgeFixture(t, world.NewForgeRoomID, true))
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
	owners, lease := admitNewForgeOwner(t)
	start, err := owners.ExecuteNewForgeLine(context.Background(), store, "w", "newforge-start", lease, "무기만들기")
	if err != nil || start.Replayed || store.commits != 1 {
		t.Fatalf("start=%+v err=%v commits=%d", start, err, store.commits)
	}
	before, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	gold := before.Players["actor"].Body.Gold
	choice, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-choice-1", lease, "1", catalog)
	if err != nil || choice.Replayed || store.commits != 2 {
		t.Fatalf("choice=%+v err=%v commits=%d", choice, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(choice.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeMaterial || result.ObjectID != 900 || result.ObjectName != "무명도" ||
		result.Continuation != world.NewForgeMaterialPhase || result.Changed {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != gold {
		t.Fatalf("gold before=%d after=%d", gold, saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-choice-1", lease, "1", catalog)
	if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, choice.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNewForgeSelectArmLineFailClosedUnmigratedCatalogAndIdleActor(t *testing.T) {
	owners, lease := admitNewForgeOwner(t)
	started := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-nil", lease, "1", nil); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	partial := sessionNewForgeWeaponCatalog()
	delete(partial, 901)
	if _, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-missing", lease, "2", partial); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("missing 901 err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed catalog mutated state")
	}
	idle := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-idle", lease, "1", sessionNewForgeWeaponCatalog()); !errors.Is(err, world.ErrNewForgeNotReading) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-start", lease, "무기만들기", sessionNewForgeWeaponCatalog()); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-ctrl", lease, "1\n", sessionNewForgeWeaponCatalog()); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}

func TestParseNewForgeSelectMaterialLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseNewForgeSelectMaterialLine("2")
	if !ok || command.Input != "2" || command.Choice != 2 || !IsNewForgeSelectMaterialLine("2") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseNewForgeSelectMaterialLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "4", "5", "6", "x", " 1", "도", "제련"} {
		command, ok := ParseNewForgeSelectMaterialLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseNewForgeSelectMaterialLine("무기만들기"); ok || IsNewForgeSelectMaterialLine("무기만들기") {
		t.Fatal("material parser claimed the start alias")
	}
	if _, ok := ParseNewForgeSelectMaterialLine("  무기만들기  "); ok {
		t.Fatal("padded start alias admitted as material")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseNewForgeSelectMaterialLine(line); ok || IsNewForgeSelectMaterialLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteNewForgeSelectMaterialLineSetsDiceAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	cases := []struct {
		arm      string
		material string
		choice   int
		id       int16
		name     string
		dice     int16
		sum      int32
	}{
		{"1", "1", 1, 900, "무명도", world.NewForgeEmeraldDiceCount, world.NewForgeEmeraldCost},
		{"2", "2 extra", 2, 901, "무명검", world.NewForgeTitaniumDiceCount, world.NewForgeTitaniumCost},
		{"3", "3", 3, 902, "무명봉", world.NewForgeIllusionDiceCount, world.NewForgeIllusionCost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionNewForgeSelectArmFixture(t, 50000))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitNewForgeOwner(t)
		arm, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.NewForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		first, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || first.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, first, err, store.commits)
		}
		var result world.NewForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.NewForgeQuench || result.Changed || result.Continuation != world.NewForgeQuenchPhase ||
			result.Response != world.NewForgeQuenchResponse || result.MaterialChoice != tt.choice ||
			result.DiceCount != tt.dice || result.DiceSides != world.NewForgeMaterialDiceSides ||
			result.DicePlus != world.NewForgeMaterialDicePlus || result.Sum != tt.sum ||
			result.ObjectID != tt.id || result.ObjectName != tt.name || !result.Enchanted ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("material %q result=%+v", tt.material, result)
		}
		if result.Response == world.ForgeQuenchResponse {
			t.Fatalf("material %q reused 제련 quench prompt", tt.material)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("material %q gold=%d", tt.material, saved.Players["actor"].Body.Gold)
		}
		if len(saved.Players["actor"].Body.Inventory) != 0 || saved.Players["actor"].Items != nil {
			t.Fatalf("material %q invented inventory", tt.material)
		}
		replay, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || !replay.Replayed || store.commits != 2 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("material %q replay=%+v err=%v commits=%d", tt.material, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("material %q replay gold err=%v gold=%d", tt.material, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteNewForgeSelectMaterialLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-bad", lease, "6", catalog, 900)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeReprompt || result.Changed || result.Continuation != world.NewForgeMaterialPhase ||
		result.Response != world.NewForgeRepromptResponse || result.Sum != 0 || result.ObjectID != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-bad", lease, "6", catalog, 900)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("replay mutated state")
	}
}

func TestExecuteNewForgeSelectMaterialLineFailClosedUnmigratedCatalogAndIdleActor(t *testing.T) {
	owners, lease := admitNewForgeOwner(t)
	started := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-nil", lease, "1", nil, 900); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	partial := sessionNewForgeWeaponCatalog()
	delete(partial, 901)
	if _, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-missing", lease, "2", partial, 901); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("missing 901 err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed catalog mutated state")
	}
	idle := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-idle", lease, "1", sessionNewForgeWeaponCatalog(), 900); !errors.Is(err, world.ErrNewForgeNotReading) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-start", lease, "무기만들기", sessionNewForgeWeaponCatalog(), 900); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-ctrl", lease, "1\n", sessionNewForgeWeaponCatalog(), 900); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}

func TestParseNewForgeSelectQuenchLineAdmitsDigitsAndInvalid(t *testing.T) {
	command, ok := ParseNewForgeSelectQuenchLine("4")
	if !ok || command.Input != "4" || command.Choice != 4 || !IsNewForgeSelectQuenchLine("4") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseNewForgeSelectQuenchLine("2 extra")
	if !ok || command.Choice != 2 {
		t.Fatalf("suffix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "0", "6", "x", " 1", "도", "제련"} {
		command, ok := ParseNewForgeSelectQuenchLine(line)
		if !ok || command.Choice != 0 || command.Input != line {
			t.Fatalf("invalid %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseNewForgeSelectQuenchLine("무기만들기"); ok || IsNewForgeSelectQuenchLine("무기만들기") {
		t.Fatal("quench parser claimed the start alias")
	}
	if _, ok := ParseNewForgeSelectQuenchLine("  무기만들기  "); ok {
		t.Fatal("padded start alias admitted as quench")
	}
	for _, line := range []string{"1\n", "1\x00", string([]byte{0xff})} {
		if _, ok := ParseNewForgeSelectQuenchLine(line); ok || IsNewForgeSelectQuenchLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteNewForgeSelectQuenchLineSetsShotsAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	cases := []struct {
		arm      string
		material string
		quench   string
		choice   int
		id       int16
		name     string
		shots    int16
		matSum   int32
		cost     int32
	}{
		{"1", "1", "1", 1, 900, "무명도", world.NewForgeQuench100Shots, world.NewForgeEmeraldCost, world.NewForgeQuench100Cost},
		{"2", "2 extra", "2 extra", 2, 901, "무명검", world.NewForgeQuench200Shots, world.NewForgeTitaniumCost, world.NewForgeQuench200Cost},
		{"3", "3", "5", 5, 902, "무명봉", world.NewForgeQuench500Shots, world.NewForgeIllusionCost, world.NewForgeQuench500Cost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionNewForgeSelectArmFixture(t, 50000))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitNewForgeOwner(t)
		arm, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.NewForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		mat, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || mat.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, mat, err, store.commits)
		}
		var material world.NewForgeResult
		if err := json.Unmarshal(mat.Response, &material); err != nil {
			t.Fatal(err)
		}
		if material.Sum != tt.matSum {
			t.Fatalf("material %q sum=%d want %d", tt.material, material.Sum, tt.matSum)
		}
		first, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, material.Sum)
		if err != nil || first.Replayed || store.commits != 3 {
			t.Fatalf("quench %q first=%+v err=%v commits=%d", tt.quench, first, err, store.commits)
		}
		var result world.NewForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.NewForgeName || result.Changed || result.Continuation != world.NewForgeNamePhase ||
			result.Response != world.NewForgeNameResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.MaterialSum != tt.matSum || result.Sum != tt.matSum+tt.cost ||
			result.ObjectID != tt.id || result.ObjectName != tt.name || !result.Enchanted ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("quench %q result=%+v", tt.quench, result)
		}
		if result.QuenchChoice == 1 && result.Sum == tt.matSum+world.ForgeQuench100Cost {
			t.Fatalf("quench %q reused 제련 quench cost", tt.quench)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("quench %q gold=%d", tt.quench, saved.Players["actor"].Body.Gold)
		}
		if len(saved.Players["actor"].Body.Inventory) != 0 || saved.Players["actor"].Items != nil {
			t.Fatalf("quench %q invented inventory", tt.quench)
		}
		replay, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, material.Sum)
		if err != nil || !replay.Replayed || store.commits != 3 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("quench %q replay=%+v err=%v commits=%d", tt.quench, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("quench %q replay gold err=%v gold=%d", tt.quench, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteNewForgeSelectQuenchLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-bad", lease, "6", catalog, 900, world.NewForgeEmeraldCost)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeReprompt || result.Changed || result.Continuation != world.NewForgeQuenchPhase ||
		result.Response != world.NewForgeRepromptResponse || result.Sum != 0 || result.ObjectID != 0 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid digit mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-bad", lease, "6", catalog, 900, world.NewForgeEmeraldCost)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("replay mutated state")
	}
}

func TestExecuteNewForgeSelectQuenchLineFailClosedUnmigratedCatalogAndIdleActor(t *testing.T) {
	owners, lease := admitNewForgeOwner(t)
	started := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-nil", lease, "1", nil, 900, world.NewForgeEmeraldCost); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-steel", lease, "1", sessionNewForgeWeaponCatalog(), 900, world.ForgeSteelCost); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("제련 sum err=%v commits=%d", err, store.commits)
	}
	partial := sessionNewForgeWeaponCatalog()
	delete(partial, 901)
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-missing", lease, "2", partial, 901, world.NewForgeTitaniumCost); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("missing 901 err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed catalog mutated state")
	}
	idle := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-idle", lease, "1", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost); !errors.Is(err, world.ErrNewForgeNotReading) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-start", lease, "무기만들기", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-ctrl", lease, "1\n", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}

func TestParseNewForgeSelectNameLineAdmitsNamesAndInvalid(t *testing.T) {
	command, ok := ParseNewForgeSelectNameLine("불의검")
	if !ok || command.Input != "불의검" || !IsNewForgeSelectNameLine("불의검") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	command, ok = ParseNewForgeSelectNameLine("abc")
	if !ok || command.Input != "abc" {
		t.Fatalf("ascii command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"", "ab", "ab(c", "123456789012345678901", "제련", "예", "도"} {
		command, ok := ParseNewForgeSelectNameLine(line)
		if !ok || command.Input != line {
			t.Fatalf("admitted %q command=%+v ok=%t", line, command, ok)
		}
	}
	if _, ok := ParseNewForgeSelectNameLine("무기만들기"); ok || IsNewForgeSelectNameLine("무기만들기") {
		t.Fatal("name parser claimed the start alias")
	}
	if _, ok := ParseNewForgeSelectNameLine("  무기만들기  "); ok {
		t.Fatal("padded start alias admitted as name")
	}
	for _, line := range []string{"abc\n", "abc\x00", string([]byte{0xff})} {
		if _, ok := ParseNewForgeSelectNameLine(line); ok || IsNewForgeSelectNameLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteNewForgeSelectNameLineSetsNameAndReplaysWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	cases := []struct {
		arm, material, quench, name string
		choice                      int
		id                          int16
		shots                       int16
		materialSum                 int32
		quenchCost                  int32
	}{
		{"1", "1", "1", "abc", 1, 900, world.NewForgeQuench100Shots, world.NewForgeEmeraldCost, world.NewForgeQuench100Cost},
		{"2", "2 extra", "2 extra", "불의검", 2, 901, world.NewForgeQuench200Shots, world.NewForgeTitaniumCost, world.NewForgeQuench200Cost},
		{"3", "3", "5", "12345678901234567890", 5, 902, world.NewForgeQuench500Shots, world.NewForgeIllusionCost, world.NewForgeQuench500Cost},
	}
	for _, tt := range cases {
		s, err := world.DecodeState(sessionNewForgeSelectArmFixture(t, 50000))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		store := &departureStore{state: raw}
		owners, lease := admitNewForgeOwner(t)
		arm, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-"+tt.arm, lease, tt.arm, catalog)
		if err != nil || arm.Replayed || store.commits != 1 {
			t.Fatalf("arm %q first=%+v err=%v commits=%d", tt.arm, arm, err, store.commits)
		}
		var loaded world.NewForgeResult
		if err := json.Unmarshal(arm.Response, &loaded); err != nil {
			t.Fatal(err)
		}
		if loaded.ObjectID != tt.id {
			t.Fatalf("arm %q object=%d want %d", tt.arm, loaded.ObjectID, tt.id)
		}
		mat, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-"+tt.arm, lease, tt.material, catalog, loaded.ObjectID)
		if err != nil || mat.Replayed || store.commits != 2 {
			t.Fatalf("material %q first=%+v err=%v commits=%d", tt.material, mat, err, store.commits)
		}
		var material world.NewForgeResult
		if err := json.Unmarshal(mat.Response, &material); err != nil {
			t.Fatal(err)
		}
		if material.Sum != tt.materialSum {
			t.Fatalf("material %q sum=%d want %d", tt.material, material.Sum, tt.materialSum)
		}
		quench, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-"+tt.arm, lease, tt.quench, catalog, loaded.ObjectID, material.Sum)
		if err != nil || quench.Replayed || store.commits != 3 {
			t.Fatalf("quench %q first=%+v err=%v commits=%d", tt.quench, quench, err, store.commits)
		}
		var quenched world.NewForgeResult
		if err := json.Unmarshal(quench.Response, &quenched); err != nil {
			t.Fatal(err)
		}
		first, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-"+tt.arm, lease, tt.name, catalog, loaded.ObjectID, material.Sum, quenched.QuenchChoice)
		if err != nil || first.Replayed || store.commits != 4 {
			t.Fatalf("name %q first=%+v err=%v commits=%d", tt.name, first, err, store.commits)
		}
		var result world.NewForgeResult
		if err := json.Unmarshal(first.Response, &result); err != nil {
			t.Fatal(err)
		}
		if result.Action != world.NewForgeConfirm || result.Changed || result.Continuation != world.NewForgeConfirmPhase ||
			result.Response != world.NewForgeConfirmResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.MaterialSum != tt.materialSum || result.Sum != tt.materialSum+tt.quenchCost ||
			result.ObjectID != tt.id || result.ObjectName != tt.name || !result.Enchanted ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("name %q result=%+v", tt.name, result)
		}
		if result.QuenchChoice == 1 && result.Sum == tt.materialSum+world.ForgeQuench100Cost {
			t.Fatalf("name %q reused 제련 quench cost", tt.name)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("name %q gold=%d", tt.name, saved.Players["actor"].Body.Gold)
		}
		if len(saved.Players["actor"].Body.Inventory) != 0 || saved.Players["actor"].Items != nil {
			t.Fatalf("name %q invented inventory", tt.name)
		}
		replay, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-"+tt.arm, lease, tt.name, catalog, loaded.ObjectID, material.Sum, quenched.QuenchChoice)
		if err != nil || !replay.Replayed || store.commits != 4 || !bytes.Equal(replay.Response, first.Response) {
			t.Fatalf("name %q replay=%+v err=%v commits=%d", tt.name, replay, err, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || replayed.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("name %q replay gold err=%v gold=%d", tt.name, err, replayed.Players["actor"].Body.Gold)
		}
	}
}

func TestExecuteNewForgeSelectNameLineInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-bad", lease, "ab", catalog, 900, world.NewForgeEmeraldCost, 1)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeReprompt || result.Changed || result.Continuation != world.NewForgeNamePhase ||
		result.Response != world.NewForgeNameShortResponse || result.Sum != 0 || result.ObjectName != "" {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || !bytes.Equal(store.state, initial) {
		t.Fatalf("invalid name mutated state gold=%d", saved.Players["actor"].Body.Gold)
	}
	replay, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-bad", lease, "ab", catalog, 900, world.NewForgeEmeraldCost, 1)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("replay mutated state")
	}
}

func TestExecuteNewForgeSelectNameLineFailClosedUnmigratedCatalogAndIdleActor(t *testing.T) {
	owners, lease := admitNewForgeOwner(t)
	started := sessionNewForgeSelectArmFixture(t, 50000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-nil", lease, "abc", nil, 900, world.NewForgeEmeraldCost, 1); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-steel", lease, "abc", sessionNewForgeWeaponCatalog(), 900, world.ForgeSteelCost, 1); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("제련 sum err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-zero-quench", lease, "abc", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost, 0); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("zero quench err=%v commits=%d", err, store.commits)
	}
	partial := sessionNewForgeWeaponCatalog()
	delete(partial, 901)
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-missing", lease, "abc", partial, 901, world.NewForgeTitaniumCost, 2); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("missing 901 err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed catalog mutated state")
	}
	idle := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-idle", lease, "abc", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost, 1); !errors.Is(err, world.ErrNewForgeNotReading) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-start", lease, "무기만들기", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost, 1); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("start alias err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-ctrl", lease, "abc\n", sessionNewForgeWeaponCatalog(), 900, world.NewForgeEmeraldCost, 1); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}

func sessionNewForgeConfirmFixture(t *testing.T, gold int32) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			world.NewForgeRoomID: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: world.NewForgeRoomID, Name: "대장간", Flags: sessionNewForgeRoomFlags(true)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: world.NewForgeRoomID, Gold: gold},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	actor := s.Players["actor"]
	actor.Body.Flags[world.NewForgeReadingFlag/8] |= 1 << (world.NewForgeReadingFlag % 8)
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

func TestParseNewForgeSelectConfirmLineAdmitsYesNoAndCancel(t *testing.T) {
	command, ok := ParseNewForgeSelectConfirmLine("예")
	if !ok || command.Input != "예" || !command.Yes || !IsNewForgeSelectConfirmLine("예") {
		t.Fatalf("yes command=%+v ok=%t", command, ok)
	}
	command, ok = ParseNewForgeSelectConfirmLine("예스")
	if !ok || !command.Yes {
		t.Fatalf("prefix command=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"아니오", "", "무기만들기", "제련", "no", "abc"} {
		command, ok := ParseNewForgeSelectConfirmLine(line)
		if !ok || command.Yes || command.Input != line || !IsNewForgeSelectConfirmLine(line) {
			t.Fatalf("cancel %q command=%+v ok=%t", line, command, ok)
		}
	}
	for _, line := range []string{"예\n", "예\x00", string([]byte{0xff})} {
		if _, ok := ParseNewForgeSelectConfirmLine(line); ok || IsNewForgeSelectConfirmLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestExecuteNewForgeSelectConfirmLineChargesGoldAddsWeaponAndReplaysWithoutRecharge(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	s, err := world.DecodeState(sessionNewForgeConfirmFixture(t, 2000000))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners, lease := admitNewForgeOwner(t)
	arm, err := owners.ExecuteNewForgeSelectArmLine(context.Background(), store, "w", "newforge-arm-confirm", lease, "1", catalog)
	if err != nil || arm.Replayed || store.commits != 1 {
		t.Fatalf("arm first=%+v err=%v commits=%d", arm, err, store.commits)
	}
	material, err := owners.ExecuteNewForgeSelectMaterialLine(context.Background(), store, "w", "newforge-mat-confirm", lease, "1", catalog, 900)
	if err != nil || material.Replayed || store.commits != 2 {
		t.Fatalf("material first=%+v err=%v commits=%d", material, err, store.commits)
	}
	quench, err := owners.ExecuteNewForgeSelectQuenchLine(context.Background(), store, "w", "newforge-quench-confirm", lease, "1", catalog, 900, world.NewForgeEmeraldCost)
	if err != nil || quench.Replayed || store.commits != 3 {
		t.Fatalf("quench first=%+v err=%v commits=%d", quench, err, store.commits)
	}
	name, err := owners.ExecuteNewForgeSelectNameLine(context.Background(), store, "w", "newforge-name-confirm", lease, "불의검", catalog, 900, world.NewForgeEmeraldCost, 1)
	if err != nil || name.Replayed || store.commits != 4 {
		t.Fatalf("name first=%+v err=%v commits=%d", name, err, store.commits)
	}
	first, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-1", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 5 {
		t.Fatalf("confirm first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	wantSum := world.NewForgeEmeraldCost + world.NewForgeQuench100Cost
	if result.Action != world.NewForgeGive || !result.Changed || result.Continuation != 0 ||
		result.Response != world.NewForgeGiveResponse || result.ObjectName != "불의검" ||
		result.Sum != wantSum || result.GoldAfter != 2000000-wantSum || result.ItemID == "" ||
		result.Reading {
		t.Fatalf("result=%+v", result)
	}
	if result.Sum == world.ForgeSteelCost+world.ForgeQuench100Cost {
		t.Fatal("newforge reused 제련 confirm cost")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000-wantSum {
		t.Fatalf("gold=%d", saved.Players["actor"].Body.Gold)
	}
	if world.PlayerFlagSet(saved.Players["actor"].Body, world.NewForgeReadingFlag) {
		t.Fatal("confirm left PREADI set")
	}
	item, ok := saved.Players["actor"].Items.Items[result.ItemID]
	if !ok || item.Object.Name != "불의검" || len(saved.Players["actor"].Items.Inventory) != 1 {
		t.Fatalf("items=%+v itemID=%q", saved.Players["actor"].Items, result.ItemID)
	}
	replay, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-1", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 5 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || replayed.Players["actor"].Body.Gold != 2000000-wantSum {
		t.Fatalf("replay gold err=%v gold=%d", err, replayed.Players["actor"].Body.Gold)
	}
	if len(replayed.Players["actor"].Items.Items) != 1 {
		t.Fatalf("replay invented items=%+v", replayed.Players["actor"].Items)
	}
}

func TestExecuteNewForgeSelectConfirmLineInsufficientGoldDoesNotCharge(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeConfirmFixture(t, 50000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-poor", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeTooPoor || result.ItemID != "" || result.GoldAfter != 50000 ||
		result.Response != world.NewForgeTooPoorResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 50000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("poor mutated gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	replay, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-poor", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNewForgeSelectConfirmLineCancelDoesNotCharge(t *testing.T) {
	catalog := sessionNewForgeWeaponCatalog()
	initial := sessionNewForgeConfirmFixture(t, 2000000)
	store := &departureStore{state: initial}
	owners, lease := admitNewForgeOwner(t)
	first, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-cancel", lease, "아니오", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.NewForgeResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.NewForgeCancel || result.ItemID != "" || result.GoldAfter != 2000000 ||
		result.Response != world.NewForgeCancelResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Gold != 2000000 || len(saved.Players["actor"].Items.Items) != 0 {
		t.Fatalf("cancel mutated gold=%d items=%+v", saved.Players["actor"].Body.Gold, saved.Players["actor"].Items)
	}
	replay, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-cancel", lease, "아니오", catalog, 900, world.NewForgeEmeraldCost, 1, "불의검")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNewForgeSelectConfirmLineFailClosedUnmigratedGoldGraphAndIdle(t *testing.T) {
	owners, lease := admitNewForgeOwner(t)
	catalog := sessionNewForgeWeaponCatalog()
	started := sessionNewForgeSelectArmFixture(t, 2000000)
	store := &departureStore{state: started}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-nil-items", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "abc"); !errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("nil items err=%v commits=%d", err, store.commits)
	}
	if !bytes.Equal(store.state, started) {
		t.Fatal("fail-closed graph mutated state")
	}
	ready := sessionNewForgeConfirmFixture(t, 2000000)
	store = &departureStore{state: ready}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-nil-cat", lease, "예", nil, 900, world.NewForgeEmeraldCost, 1, "abc"); !errors.Is(err, world.ErrNewForgeCatalogUnmigrated) || store.commits != 0 {
		t.Fatalf("nil catalog err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-zero-sum", lease, "예", catalog, 900, 0, 1, "abc"); !errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero sum err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-steel", lease, "예", catalog, 900, world.ForgeSteelCost, 1, "abc"); !errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("제련 sum err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-zero-quench", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 0, "abc"); !errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("zero quench err=%v commits=%d", err, store.commits)
	}
	idle := sessionNewForgeFixture(t, world.NewForgeRoomID, true)
	store = &departureStore{state: idle}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-idle", lease, "예", catalog, 900, world.NewForgeEmeraldCost, 1, "abc"); !errors.Is(err, world.ErrNewForgeNotReading) && !errors.Is(err, world.ErrNewForgeGoldObjectUnmigrated) || store.commits != 0 {
		t.Fatalf("idle err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNewForgeSelectConfirmLine(context.Background(), store, "w", "newforge-confirm-control", lease, "예\n", catalog, 900, world.NewForgeEmeraldCost, 1, "abc"); !errors.Is(err, ErrUnsupportedNewForgeLine) || store.commits != 0 {
		t.Fatalf("control err=%v commits=%d", err, store.commits)
	}
}
