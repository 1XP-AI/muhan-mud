package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func sessionChangeClassRoomFlags(class byte, training bool) (flags [8]byte) {
	if training {
		flags[world.ChangeClassRoomFlag/8] |= 1 << (world.ChangeClassRoomFlag % 8)
	}
	if class > 0 && class <= world.ChangeClassMaxClass {
		bits := class - 1
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				at := world.ChangeClassRoomFlag + 3 - i
				flags[at/8] |= 1 << (at % 8)
			}
		}
	}
	return flags
}

func sessionChangeClassFixture(t *testing.T, body world.LegacyMonster, destination byte) []byte {
	t.Helper()
	body.Type = 0
	body.RoomID = 1
	if body.Name == "" {
		body.Name = "Alice"
	}
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: sessionChangeClassRoomFlags(destination, true)}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{"actor": {Body: body, Online: true}},
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

func admitChangeClassOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseChangeClassLineAdmitsBarePromptAndOneLineYes(t *testing.T) {
	for _, tc := range []struct {
		line      string
		confirmed bool
	}{
		{line: "직업전환", confirmed: false},
		{line: "  직업전환  ", confirmed: false},
		{line: "직업전환 예", confirmed: true},
		{line: "  직업전환 예  ", confirmed: true},
	} {
		command, ok := ParseChangeClassLine(tc.line)
		if !ok || command.Alias != "직업전환" || command.Confirmed != tc.confirmed || !IsChangeClassLine(tc.line) || !IsClassChangeLine(tc.line) {
			t.Fatalf("line=%q command=%+v ok=%t", tc.line, command, ok)
		}
	}
	for _, line := range []string{"직업전환 아니오", "직업전환 예 더", "change_class", "직업전환\n", "직업전환\x00", "직업전환\u2028", string([]byte{0xff})} {
		if _, ok := ParseChangeClassLine(line); ok || IsChangeClassLine(line) {
			t.Fatalf("unsupported change-class line accepted: %q", line)
		}
	}
	for _, line := range []string{"직업전환", "  직업전환  "} {
		if !ParseChangeClassStartLine(line) || !IsChangeClassStartLine(line) {
			t.Fatalf("bare change-class start rejected: %q", line)
		}
	}
	if ParseChangeClassStartLine("직업전환 예") || IsChangeClassStartLine("직업전환 예") {
		t.Fatal("confirmed change-class line entered the local start flow")
	}
	if ChangeClassCancelResponse != "직업전환이 되지 않았습니다" || ChangeClassRetryResponse == "" {
		t.Fatalf("unexpected continuation responses: cancel=%q retry=%q", ChangeClassCancelResponse, ChangeClassRetryResponse)
	}
}

func TestExecuteChangeClassYesCommitsTypedReceiptAndReplays(t *testing.T) {
	body := world.LegacyMonster{Class: 4, Level: 5, Experience: 100000, Stats: [5]byte{10, 11, 12, 13, 14}, HPMax: 200, HPCurrent: 77, MPMax: 200, MPCurrent: 66}
	body.Timers[7] = world.LegacyTimer{Interval: 31, LastTime: 47, Misc: 59}
	initial := sessionChangeClassFixture(t, body, 5)
	store := &departureStore{state: initial}
	owners, lease := admitChangeClassOwner(t)
	first, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-1", lease, "직업전환 예")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ChangeClassResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "change_class" || !result.Confirmed || !result.Changed || result.Class != 5 || result.Experience != 0 || result.ExperienceSpent != 100000 || result.Level != 1 || result.LevelsDown != 4 || result.Broadcast || result.BroadcastPending || result.BeforeTimers != result.Timers {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["actor"].Body
	if actor.Class != 5 || actor.Level != 1 || actor.Experience != 0 || actor.HPMax != 192 || actor.HPCurrent != 192 || actor.MPMax != 194 || actor.MPCurrent != 194 || actor.Stats != [5]byte{10, 11, 12, 13, 13} || actor.Timers != body.Timers {
		t.Fatalf("saved actor=%+v", actor)
	}

	replay, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-1", lease, "직업전환 예")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteChangeClassBareIsTypedPromptNoOpAndReplays(t *testing.T) {
	body := world.LegacyMonster{Class: 4, Level: 2, Experience: 100000}
	initial := sessionChangeClassFixture(t, body, 5)
	store := &departureStore{state: initial}
	owners, lease := admitChangeClassOwner(t)
	first, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-prompt", lease, "직업전환")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.ChangeClassResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.PendingConfirmation || !result.NoOp || result.Changed || result.Confirmed || result.Response != world.ChangeClassPromptResponse {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-prompt", lease, "직업전환")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || !bytes.Equal(store.state, initial) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteChangeClassRejectsUnsupportedAndWorldGatesBeforeReceipt(t *testing.T) {
	initial := sessionChangeClassFixture(t, world.LegacyMonster{Class: 4, Level: 2, Experience: 100000}, 5)
	store := &departureStore{state: initial}
	owners, lease := admitChangeClassOwner(t)
	if _, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-invalid-line", lease, "직업전환 아니오"); !errors.Is(err, ErrUnsupportedChangeClassLine) || store.commits != 0 {
		t.Fatalf("invalid line err=%v commits=%d", err, store.commits)
	}
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["actor"]
	actor.Body.Experience = 99999
	state.Players["actor"] = actor
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteChangeClassLine(context.Background(), store, "w", "class-gate", lease, "직업전환 예"); !errors.Is(err, world.ErrChangeClassExperience) || store.commits != 0 {
		t.Fatalf("gate err=%v commits=%d", err, store.commits)
	}
}
