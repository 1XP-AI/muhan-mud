package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseSaveLineAdmitsOnlyBoundedAliases(t *testing.T) {
	tests := []struct {
		line  string
		alias string
		ok    bool
	}{
		{line: "저장", alias: "저장", ok: true},
		{line: "save", alias: "save", ok: true},
		{line: "  저장  ", alias: "저장", ok: true},
		{line: "savegame", ok: false}, // savegame is the C function, not a cmdlist alias.
		{line: "SAVE", ok: false},
		{line: "저장 지금", ok: false},
		{line: "save now", ok: false},
		{line: "저장\n", ok: false},
		{line: "save\x00", ok: false},
		{line: "저장\u2028", ok: false},
		{line: "\xff", ok: false},
	}
	for _, tt := range tests {
		got, ok := ParseSaveLine(tt.line)
		if ok != tt.ok || (ok && got.Alias != tt.alias) {
			t.Fatalf("ParseSaveLine(%q)=(%+v,%v), want alias=%q ok=%v", tt.line, got, ok, tt.alias, tt.ok)
		}
		if IsSaveLine(tt.line) != tt.ok {
			t.Fatalf("IsSaveLine(%q)=%v, want %v", tt.line, IsSaveLine(tt.line), tt.ok)
		}
	}
}

func TestExecuteSaveLinePersistsNoStateReceiptAndReplaysExactly(t *testing.T) {
	for _, alias := range []string{"저장", "save"} {
		t.Run(alias, func(t *testing.T) {
			initial := attackCommandFixture()
			store := &departureStore{state: initial}
			var owners Ownership
			lease, err := owners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}

			first, err := owners.ExecuteSaveLine(context.Background(), store, "w", "save-"+alias, lease, alias)
			if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
				t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
			}
			var response SaveResponse
			if err := json.Unmarshal(first.Response, &response); err != nil || response != SaveResponse(SaveResponseText) {
				t.Fatalf("response=%q err=%v", response, err)
			}
			if !bytes.Equal(store.state, initial) {
				t.Fatal("save command changed canonical state")
			}

			replay, err := owners.ExecuteSaveLine(context.Background(), store, "w", "save-"+alias, lease, alias)
			if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
			}
			if !bytes.Equal(store.state, initial) {
				t.Fatal("save replay changed canonical state")
			}
		})
	}
}

func TestExecuteSaveLineRequiresOnlineActor(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	player := state.Players["a"]
	player.Online = false
	state.Players["a"] = player
	offline, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: offline}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteSaveLine(context.Background(), store, "w", "save-offline", lease, "저장"); err == nil {
		t.Fatal("offline actor accepted")
	}
	if store.commits != 0 {
		t.Fatalf("offline actor created receipt commits=%d", store.commits)
	}
}

func TestExecuteSaveLineRejectsMalformedInputBeforeReceipt(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"savegame", "저장 인자", "save\n", "\xff"} {
		if _, err := owners.ExecuteSaveLine(context.Background(), store, "w", "save-invalid-"+line, lease, line); !errors.Is(err, ErrUnsupportedSaveLine) {
			t.Fatalf("line %q err=%v", line, err)
		}
		if store.commits != 0 {
			t.Fatalf("malformed line %q created receipt commits=%d", line, store.commits)
		}
	}
}
