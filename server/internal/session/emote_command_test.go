package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func emoteCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	p.Body.Flags[1/8] |= 1 << (1 % 8) // PHIDDN
	s.Players["a"] = p
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseEmoteLineRecognizesClosedAliasesAndOneTarget(t *testing.T) {
	for _, alias := range world.EmoteAliases() {
		command, ok := ParseEmoteLine("  " + alias + "  ")
		if !ok || command.Alias != alias || command.Target != "" {
			t.Fatalf("alias=%q command=%+v ok=%v", alias, command, ok)
		}
	}
	command, ok := ParseEmoteLine("미소 Bob")
	if !ok || command.Alias != "미소" || command.Target != "Bob" {
		t.Fatalf("target command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{"", "보아", "미소 Bob extra", "미소\tBob\tmore", "감정표현 Bob" /* reducer rejects this targetless action */} {
		if line == "감정표현 Bob" {
			if command, ok := ParseEmoteLine(line); !ok || command.Target != "Bob" {
				t.Fatalf("parser should preserve explicit target for reducer rejection: %+v %v", command, ok)
			}
			continue
		}
		if _, ok := ParseEmoteLine(line); ok {
			t.Fatalf("unsupported line accepted: %q", line)
		}
	}
}

func TestExecuteEmoteLinePersistsRevealAndReplays(t *testing.T) {
	store := &departureStore{state: emoteCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteEmoteLine(context.Background(), store, "w", "emote-1", lease, "미소 Bob")
	if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "Bob님에게 미소") {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["a"]
	if actor.Body.Flags[0]&(1<<1) != 0 {
		t.Fatal("emote did not clear PHIDDN")
	}
	event, ok, err := saved.RoomEmoteEvent("a", "미소", "b")
	if err != nil || !ok || !strings.Contains(event.Text, "Alice님이 Bob님에게 미소") || !strings.Contains(event.TargetText, "Alice님이 당신에게") {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}

	replay, err := owners.ExecuteEmoteLine(context.Background(), store, "w", "emote-1", lease, "미소 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteEmoteLineFailsClosedWithoutReceiptForUnsupportedTarget(t *testing.T) {
	for _, line := range []string{"보아", "감정표현 Bob", "미소 Nobody", "미소 Bob extra"} {
		store := &departureStore{state: emoteCommandFixture(t)}
		var owners Ownership
		lease, err := owners.Acquire("a")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := owners.ExecuteEmoteLine(context.Background(), store, "w", "emote-bad", lease, line); err == nil || store.commits != 0 {
			t.Fatalf("line=%q err=%v commits=%d", line, err, store.commits)
		}
	}
}
