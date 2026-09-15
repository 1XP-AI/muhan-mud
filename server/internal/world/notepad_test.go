package world

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func notepadWorldFixture(imported bool, class byte) State {
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Caretaker", RoomID: 1, Class: class},
				Online: true,
			},
		},
	}
	if imported {
		state.Notepad = []string{}
	}
	return state
}

func TestNotepadPlanMatchesLegacyBranchesAndAliases(t *testing.T) {
	state := notepadWorldFixture(true, playerCaretakerClass)
	state.Notepad = []string{NotepadHeaderLine, "", "existing"}

	view, err := state.PlanNotepad("actor", "*notepad")
	if err != nil || view.Action != NotepadView || view.Body != NotepadHeaderLine+"\n\nexisting\n" || view.Response != view.Body {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	alias, err := state.PlanNotepad("actor", "*메모")
	if err != nil || alias.Action != NotepadView || alias.Response != view.Response {
		t.Fatalf("alias=%+v err=%v", alias, err)
	}

	appendStart, err := state.PlanNotepad("actor", "*notepad", "a")
	if err != nil || appendStart.Action != NotepadAppend || appendStart.Response != NotepadAppendPrompt || appendStart.Changed {
		t.Fatalf("append start=%+v err=%v", appendStart, err)
	}
	if _, _, err := state.ApplyNotepad(appendStart); !errors.Is(err, ErrNotepadInvalidProposal) {
		t.Fatalf("append prompt apply err=%v", err)
	}

	clear, err := state.PlanNotepad("actor", "*notepad", "d")
	if err != nil || clear.Action != NotepadClear || clear.Response != NotepadClearResponse || !clear.Changed {
		t.Fatalf("clear=%+v err=%v", clear, err)
	}
	cleared, result, err := state.ApplyNotepad(clear)
	if err != nil || result.Response != NotepadClearResponse || !result.Changed || cleared.Notepad == nil || len(cleared.Notepad) != 0 {
		t.Fatalf("cleared=%+v result=%+v err=%v", cleared, result, err)
	}

	invalid, err := state.PlanNotepad("actor", "*notepad", "x")
	if err != nil || invalid.Action != NotepadInvalid || invalid.Response != NotepadInvalidOptionResponse {
		t.Fatalf("invalid=%+v err=%v", invalid, err)
	}
	unchanged, result, err := state.ApplyNotepad(invalid)
	if err != nil || result.Response != NotepadInvalidOptionResponse || result.Changed || !reflect.DeepEqual(unchanged.Notepad, state.Notepad) {
		t.Fatalf("invalid apply state=%+v result=%+v err=%v", unchanged, result, err)
	}
}

func TestNotepadAppendAddsLegacyHeaderAndTruncatesAt79Bytes(t *testing.T) {
	state := notepadWorldFixture(true, playerCaretakerClass)
	line := strings.Repeat("a", MaxNotepadLineBytes+10)
	next, result, err := state.AppendNotepad("actor", []string{line})
	if err != nil {
		t.Fatal(err)
	}
	wantLine := strings.Repeat("a", MaxNotepadLineBytes)
	want := []string{NotepadHeaderLine, "", wantLine}
	if result.Action != NotepadAppend || result.Response != NotepadAppendResponse || !result.Changed || !reflect.DeepEqual(next.Notepad, want) {
		t.Fatalf("next=%+v result=%+v", next.Notepad, result)
	}
	if state.Notepad == nil || len(state.Notepad) != 0 {
		t.Fatalf("append mutated source state: %#v", state.Notepad)
	}

	utf8Line := strings.Repeat("가", 40)
	if got := TruncateNotepadLine(utf8Line); len(got) > MaxNotepadLineBytes || !strings.HasPrefix(utf8Line, got) {
		t.Fatalf("UTF-8 truncation got bytes=%d value=%q", len(got), got)
	}
	if err := ValidateNotepadLine("bad\nline"); !errors.Is(err, ErrNotepadLineInvalid) {
		t.Fatalf("newline validation err=%v", err)
	}
}

func TestNotepadAuthorizationUsesCanonicalOnlineActorAndRoom(t *testing.T) {
	unauthorized := notepadWorldFixture(true, playerCaretakerClass-1)
	proposal, err := unauthorized.PlanNotepad("actor", "*notepad")
	if err != nil || proposal.Action != NotepadUnknown || proposal.Response != NotepadUnknownResponse("*notepad") {
		t.Fatalf("unauthorized proposal=%+v err=%v", proposal, err)
	}
	if err := unauthorized.AuthorizeNotepad("actor"); !errors.Is(err, ErrNotepadUnauthorized) {
		t.Fatalf("AuthorizeNotepad err=%v", err)
	}

	missingMembership := notepadWorldFixture(true, playerCaretakerClass)
	missingMembership.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}
	if _, err := missingMembership.PlanNotepad("actor", "*notepad"); !errors.Is(err, ErrNotepadStateInvalid) {
		t.Fatalf("missing membership err=%v", err)
	}

	offline := notepadWorldFixture(true, playerCaretakerClass)
	actor := offline.Players["actor"]
	actor.Online = false
	offline.Players["actor"] = actor
	offline.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}
	if _, err := offline.PlanNotepad("actor", "*notepad"); !errors.Is(err, ErrNotepadActorAbsent) {
		t.Fatalf("offline actor err=%v", err)
	}
}

func TestNotepadStateCloneAndJSONPreserveExplicitEmptyProjection(t *testing.T) {
	state := notepadWorldFixture(true, playerCaretakerClass)
	clone := state.clone()
	if clone.Notepad == nil {
		t.Fatal("clone lost explicit empty notepad projection")
	}
	clone.Notepad = append(clone.Notepad, "local")
	if len(state.Notepad) != 0 {
		t.Fatalf("clone mutated source=%#v", state.Notepad)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeState(raw)
	if err != nil || decoded.Notepad == nil || len(decoded.Notepad) != 0 {
		t.Fatalf("decoded=%#v err=%v raw=%s", decoded.Notepad, err, raw)
	}

	unresolved := notepadWorldFixture(false, playerCaretakerClass)
	raw, err = json.Marshal(unresolved)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeState(raw)
	if err != nil || decoded.Notepad != nil {
		t.Fatalf("nil projection decoded=%#v err=%v", decoded.Notepad, err)
	}
}

func TestNotepadValidationRejectsUnsafeCanonicalProjection(t *testing.T) {
	state := notepadWorldFixture(true, playerCaretakerClass)
	state.Notepad = []string{strings.Repeat("x", MaxNotepadLineBytes+1)}
	if err := state.Validate(); !errors.Is(err, ErrNotepadStateInvalid) {
		t.Fatalf("oversized state err=%v", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeState(raw); !errors.Is(err, ErrNotepadStateInvalid) {
		t.Fatalf("oversized decode err=%v", err)
	}
}

func TestNotepadApplyRejectsTamperedAppendWithoutPanic(t *testing.T) {
	state := notepadWorldFixture(true, playerCaretakerClass)
	proposal, err := state.PlanNotepadAppendWithVerb("actor", "*메모", []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	proposal.afterLines = nil
	if _, _, err := state.ApplyNotepad(proposal); !errors.Is(err, ErrNotepadStaleProposal) {
		t.Fatalf("tampered after lines err=%v", err)
	}

	proposal, err = state.PlanNotepadAppendWithVerb("actor", "*메모", []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	proposal.inputLines = nil
	if _, _, err := state.ApplyNotepad(proposal); !errors.Is(err, ErrNotepadInvalidProposal) {
		t.Fatalf("tampered input lines err=%v", err)
	}
}
