package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseTimeLineAdmitsOnlyBareLegacyAlias(t *testing.T) {
	command, ok := ParseTimeLine("  시간  ")
	if !ok || command.Alias != "시간" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{
		"", "시간 내일", "현재시간", "시간\n", "시간\x00", "시간\u2028", string([]byte{'\xc0', '\xaf'}),
	} {
		if _, ok := ParseTimeLine(line); ok {
			t.Fatalf("invalid time line accepted: %q", line)
		}
	}
}

func TestExecuteTimeLinePersistsNoStateReceiptAndReplays(t *testing.T) {
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
	options := TimeCommandOptions{
		Now:       25,
		WallClock: time.Date(2026, time.January, 2, 23, 4, 5, 0, time.UTC),
	}
	first, err := owners.ExecuteTimeLine(context.Background(), store, "w", "time-v2-1", lease, "시간", options)
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	want := "현재 시간: 오전 1시.\n실제 시간: Fri Jan  2 15:04:05 2026 (PST).\n"
	if text != want {
		t.Fatalf("response=%q want=%q", text, want)
	}
	if string(store.state) != string(initial) {
		t.Fatal("time command changed world state")
	}

	replay, err := owners.ExecuteTimeLine(context.Background(), store, "w", "time-v2-1", lease, "시간", options)
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteTimeLineRejectsInvalidLineOrClockBeforeReceipt(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	validWallClock := time.Unix(0, 0)
	for _, tc := range []struct {
		name    string
		line    string
		options TimeCommandOptions
	}{
		{name: "argument", line: "시간 내일", options: TimeCommandOptions{Now: 1, WallClock: validWallClock}},
		{name: "control", line: "시간\n", options: TimeCommandOptions{Now: 1, WallClock: validWallClock}},
		{name: "negative game clock", line: "시간", options: TimeCommandOptions{Now: -1, WallClock: validWallClock}},
		{name: "missing wall clock", line: "시간", options: TimeCommandOptions{Now: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := owners.ExecuteTimeLine(context.Background(), store, "w", "time-v2-invalid-"+tc.name, lease, tc.line, tc.options); err == nil {
				t.Fatalf("invalid time command accepted: %+v", tc)
			}
			if store.commits != 0 {
				t.Fatalf("invalid time command created receipt: %+v", tc)
			}
		})
	}
}

func TestExecuteTimeLineRequiresOnlineActorBeforeReceipt(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Online = false
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
	_, err = owners.ExecuteTimeLine(context.Background(), store, "w", "time-v2-offline", lease, "시간", TimeCommandOptions{Now: 1, WallClock: time.Unix(0, 0)})
	if err == nil || store.commits != 0 {
		t.Fatalf("offline actor err=%v commits=%d", err, store.commits)
	}
}
