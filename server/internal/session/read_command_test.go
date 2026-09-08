package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExecuteReadLinePersistsDeterministicTimeReceiptAndReplays(t *testing.T) {
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
	options := ReadLineOptions{
		GameHour:  0,
		WallClock: time.Date(2026, time.January, 2, 15, 4, 5, 0, time.FixedZone("PST", -8*60*60)),
	}
	first, err := owners.ExecuteReadLine(context.Background(), store, "w", "time-1", lease, "시간", options)
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := unmarshalReceiptText(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "현재 시간: 오전 12시.") || !strings.Contains(text, "실제 시간: Fri Jan  2 15:04:05 2026 (PST).") {
		t.Fatalf("unexpected time response %q", text)
	}
	if string(store.state) != string(initial) {
		t.Fatal("read command mutated world state")
	}

	replay, err := owners.ExecuteReadLine(context.Background(), store, "w", "time-1", lease, "시간", ReadLineOptions{
		GameHour:  23,
		WallClock: time.Date(2030, time.January, 1, 1, 2, 3, 0, time.UTC),
	})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteReadLineRejectsUnsupportedOrUnboundClockWithoutReceipt(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		line    string
		options ReadLineOptions
	}{
		{line: "도움말"},
		{line: "시간 내일", options: ReadLineOptions{GameHour: 12, WallClock: time.Unix(0, 0)}},
		{line: "시간", options: ReadLineOptions{GameHour: 24, WallClock: time.Unix(0, 0)}},
		{line: "시간", options: ReadLineOptions{GameHour: 12}},
	} {
		if _, err := owners.ExecuteReadLine(context.Background(), store, "w", "unsupported-"+tc.line, lease, tc.line, tc.options); err == nil {
			t.Fatalf("unsupported input accepted: %q", tc.line)
		}
		if store.commits != 0 {
			t.Fatalf("unsupported input created receipt: %q", tc.line)
		}
	}
}

func unmarshalReceiptText(raw []byte, out *string) error {
	// Keep this helper local to the new test file: command receipts encode the
	// terminal response as a JSON string, matching the other session slices.
	return json.Unmarshal(raw, out)
}
