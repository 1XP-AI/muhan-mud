package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func sessionNotepadFixture(imported, actorOnline bool, class byte) []byte {
	playerIDs := []string(nil)
	if actorOnline {
		playerIDs = []string{"actor"}
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: playerIDs},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "Caretaker", RoomID: 1, Class: class}, Online: actorOnline},
		},
	}
	if imported {
		state.Notepad = []string{}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	return raw
}

func admitNotepadOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseNotepadLineRecognizesOnlyExactAliasesAndLegacyBranches(t *testing.T) {
	cases := []struct {
		line   string
		verb   string
		action world.NotepadAction
		ok     bool
	}{
		{line: "*notepad", verb: "*notepad", action: world.NotepadView, ok: true},
		{line: " *메모 ", verb: "*메모", action: world.NotepadView, ok: true},
		{line: "*notepad a", verb: "*notepad", action: world.NotepadAppend, ok: true},
		{line: "*메모 D", verb: "*메모", action: world.NotepadClear, ok: true},
		{line: "*notepad x", verb: "*notepad", action: world.NotepadInvalid, ok: true},
		// post.c checks cmnd->num == 2 for the option branches; extra
		// tokens therefore retain the bare view behavior.
		{line: "*notepad a extra", verb: "*notepad", action: world.NotepadView, ok: true},
	}
	for _, tc := range cases {
		command, ok := ParseNotepadLine(tc.line)
		if !ok || !tc.ok || command.Verb != tc.verb || command.Action != tc.action {
			t.Errorf("ParseNotepadLine(%q)=%+v,%v want verb=%q action=%q", tc.line, command, ok, tc.verb, tc.action)
		}
	}
	for _, line := range []string{"*NOTEPAD", "*notepad' a", "*notepad a\nbody", "*other", "*notepad a b c d e f g h"} {
		if _, ok := ParseNotepadLine(line); ok {
			t.Errorf("malformed/non-exact notepad line accepted: %q", line)
		}
	}
	for _, line := range []string{"*notepad", "*메모 d", "*notepad x"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandNotepad {
			t.Errorf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	if parsed, err := ParseCommand("메모 Bob body"); err != nil || parsed.Kind != CommandMemo {
		t.Fatalf("regular memo was reclassified: %+v err=%v", parsed, err)
	}
}

func TestExecuteNotepadViewClearAppendAndReplay(t *testing.T) {
	state := sessionNotepadFixture(true, true, 10)
	decoded, err := world.DecodeState(state)
	if err != nil {
		t.Fatal(err)
	}
	decoded.Notepad = []string{world.NotepadHeaderLine, "", "old"}
	state, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	owners, lease := admitNotepadOwner(t)

	view, err := owners.ExecuteNotepadLine(context.Background(), store, "world", "notepad-view", lease, "*notepad")
	if err != nil || view.Replayed || store.commits != 1 {
		t.Fatalf("view=%+v err=%v commits=%d", view, err, store.commits)
	}
	var viewResult world.NotepadResult
	if err := json.Unmarshal(view.Response, &viewResult); err != nil || viewResult.Action != world.NotepadView || viewResult.Response != world.NotepadHeaderLine+"\n\nold\n" {
		t.Fatalf("view result=%+v err=%v", viewResult, err)
	}

	clear, err := owners.ExecuteNotepadLine(context.Background(), store, "world", "notepad-clear", lease, "*메모 d")
	if err != nil || store.commits != 2 {
		t.Fatalf("clear=%+v err=%v commits=%d", clear, err, store.commits)
	}
	var clearResult world.NotepadResult
	if err := json.Unmarshal(clear.Response, &clearResult); err != nil || clearResult.Action != world.NotepadClear || clearResult.Response != world.NotepadClearResponse || !clearResult.Changed {
		t.Fatalf("clear result=%+v err=%v", clearResult, err)
	}

	appendReceipt, err := owners.ExecuteNotepadAppendWithVerb(context.Background(), store, "world", "notepad-append", lease, "*메모", []string{"one", "two"})
	if err != nil || appendReceipt.Replayed || store.commits != 3 {
		t.Fatalf("append=%+v err=%v commits=%d", appendReceipt, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !strings.EqualFold(saved.Notepad[2], "one") || saved.Notepad[3] != "two" {
		t.Fatalf("saved=%+v err=%v", saved.Notepad, err)
	}
	replay, err := owners.ExecuteNotepadAppendWithVerb(context.Background(), store, "world", "notepad-append", lease, "*메모", []string{"one", "two"})
	if err != nil || !replay.Replayed || store.commits != 3 || string(replay.Response) != string(appendReceipt.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteNotepadFailsClosedBeforeReceiptAndKeepsAppendInteractive(t *testing.T) {
	owners, lease := admitNotepadOwner(t)
	for _, tc := range []struct {
		name string
		raw  []byte
		want error
	}{
		{name: "unresolved", raw: sessionNotepadFixture(false, true, 10), want: world.ErrNotepadStateUnresolved},
		{name: "offline", raw: sessionNotepadFixture(true, false, 10), want: world.ErrNotepadActorAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &departureStore{state: tc.raw}
			_, err := owners.ExecuteNotepadLine(context.Background(), store, "world", "notepad-fail", lease, "*notepad")
			if !errors.Is(err, tc.want) || store.commits != 0 {
				t.Fatalf("err=%v commits=%d want=%v", err, store.commits, tc.want)
			}
		})
	}
	store := &departureStore{state: sessionNotepadFixture(true, true, 10)}
	if _, err := owners.ExecuteNotepadLine(context.Background(), store, "world", "notepad-start", lease, "*notepad a"); !errors.Is(err, ErrNotepadAppendContinuationRequired) || store.commits != 0 {
		t.Fatalf("append start err=%v commits=%d", err, store.commits)
	}
	if _, err := owners.ExecuteNotepadAppendWithVerb(context.Background(), store, "world", "notepad-bad", lease, "*notepad", []string{"bad\nline"}); !errors.Is(err, world.ErrNotepadLineInvalid) || store.commits != 0 {
		t.Fatalf("malformed append err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteNotepadRejectsNonExactAppendAliasAndTruncatesUTF8Safely(t *testing.T) {
	store := &departureStore{state: sessionNotepadFixture(true, true, 10)}
	owners, lease := admitNotepadOwner(t)
	if _, err := owners.ExecuteNotepadAppendWithVerb(context.Background(), store, "world", "notepad-alias", lease, "*NOTEPAD", []string{"line"}); !errors.Is(err, ErrUnsupportedNotepadLine) || store.commits != 0 {
		t.Fatalf("non-exact alias err=%v commits=%d", err, store.commits)
	}
	line := strings.Repeat("가", 40)
	receipt, err := owners.ExecuteNotepadAppendWithVerb(context.Background(), store, "world", "notepad-utf8", lease, "*notepad", []string{line})
	if err != nil || store.commits != 1 {
		t.Fatalf("UTF-8 append=%+v err=%v commits=%d", receipt, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Notepad) != 3 || len(saved.Notepad[2]) > world.MaxNotepadLineBytes || !utf8Prefix(line, saved.Notepad[2]) {
		t.Fatalf("saved=%+v err=%v", saved.Notepad, err)
	}
}

func utf8Prefix(full, prefix string) bool {
	return strings.HasPrefix(full, prefix)
}
