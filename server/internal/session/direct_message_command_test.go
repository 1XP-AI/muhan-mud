package session

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestParseDirectMessageLineRecognizesAliasesAndRetainsMessage(t *testing.T) {
	tests := []struct {
		line, target, text string
		ok                 bool
	}{
		{line: "얘기 Bob 안녕하세요", target: "Bob", text: "안녕하세요", ok: true},
		{line: "이야기 Bob hello   world", target: "Bob", text: "hello   world", ok: true},
		{line: "얘기 Bob", target: "Bob", text: "", ok: true},
		{line: "얘기", target: "", text: "", ok: true},
		{line: "얘기Bob hello", ok: false},
		{line: "말 Bob hello", ok: false},
		{line: "얘기 Bob hello\nworld", ok: false},
	}
	for _, tc := range tests {
		got, ok := ParseDirectMessageLine(tc.line)
		if ok != tc.ok || got.Target != tc.target || got.Text != tc.text {
			t.Fatalf("line=%q got=%+v ok=%v want target=%q text=%q ok=%v", tc.line, got, ok, tc.target, tc.text, tc.ok)
		}
	}
}

func TestExecuteDirectMessageLinePersistsEventAndReplaysWithoutStateMutation(t *testing.T) {
	initialRaw := followCommandFixture()
	initial, err := world.DecodeState(initialRaw)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: initialRaw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteDirectMessageLine(context.Background(), store, "w", "direct-1", lease, "얘기 Bob 안녕하세요")
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DirectMessageResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Delivered || result.Response != "Bob님에게 말을 전달하였습니다.\r\n" || result.Event == nil || result.Event.TargetID != "b" || !strings.Contains(result.Event.Text, "Alice님이 당신에게") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !reflect.DeepEqual(saved, initial) {
		t.Fatalf("direct message mutated state: saved=%+v initial=%+v err=%v", saved, initial, err)
	}

	replay, err := owners.ExecuteDirectMessageLine(context.Background(), store, "w", "direct-1", lease, "얘기 Bob 안녕하세요")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectMessageLinePersistsListenRejectionWithoutMutation(t *testing.T) {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	target := s.Players["b"]
	target.Body.Flags[35/8] |= 1 << (35 % 8) // PIGNOR
	s.Players["b"] = target
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteDirectMessageLine(context.Background(), store, "w", "direct-reject-1", lease, "얘기 Bob hello")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DirectMessageResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Delivered || result.Event != nil || result.Response != "Bob님은 이야기 듣기 거부 상태입니다.\r\n" {
		t.Fatalf("listen rejection result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !reflect.DeepEqual(saved, s) {
		t.Fatalf("listen rejection mutated state: saved=%+v initial=%+v err=%v", saved, s, err)
	}
}
