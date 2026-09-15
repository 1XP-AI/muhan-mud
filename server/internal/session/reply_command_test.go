package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseReplyLineKeepsSourceAliasesAndMessageBoundaries(t *testing.T) {
	tests := []struct {
		line, text string
		ok         bool
	}{
		{line: "대답 안녕하세요", text: "안녕하세요", ok: true},
		{line: "/ hello   world", text: "hello   world", ok: true},
		{line: "대답", text: "", ok: true},
		{line: "/", text: "", ok: true},
		{line: "대답hello", ok: false},
		{line: "//path", ok: false},
		{line: "말 hello", ok: false},
		{line: "대답 hello\nworld", ok: false},
	}
	for _, tc := range tests {
		got, ok := ParseReplyLine(tc.line)
		if ok != tc.ok || got.Text != tc.text {
			t.Fatalf("line=%q got=%+v ok=%v want text=%q ok=%v", tc.line, got, ok, tc.text, tc.ok)
		}
	}
	for _, line := range []string{"대답 안녕", "/ 안녕"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandReply {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want reply", line, parsed, err)
		}
	}
}

func TestExecuteReplyLineBindsExactIncomingSenderAndReplays(t *testing.T) {
	initialRaw := followCommandFixture()
	initial, err := world.DecodeState(initialRaw)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: initialRaw}
	var owners Ownership
	lease, err := owners.Acquire("b")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteReplyLine(context.Background(), store, "w", "reply-1", lease, "/ 안녕하세요", "a", "Alice")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DirectMessageResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Response != "Alice님에게 말을 전달하였습니다.\r\n" || !result.Delivered || result.Event == nil || result.Event.TargetID != "a" || !strings.Contains(result.Event.Text, "Bob님이 당신에게") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !reflect.DeepEqual(saved, initial) {
		t.Fatalf("reply mutated state: saved=%+v initial=%+v err=%v", saved, initial, err)
	}

	replay, err := owners.ExecuteReplyLine(context.Background(), store, "w", "reply-1", lease, "/ 안녕하세요", "a", "Alice")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteReplyLineRejectsStaleTargetBeforeReceipt(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("b")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteReplyLine(context.Background(), store, "w", "reply-stale", lease, "/ hi", "missing", "Alice"); !errors.Is(err, ErrReplyTargetUnavailable) || store.commits != 0 {
		t.Fatalf("stale target err=%v commits=%d", err, store.commits)
	}
}
