package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteHelpLineReadsDocumentAndReplaysWithoutWorldMutation(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	state.Players["a"] = world.PlayerState{Body: state.Players["a"].Body, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	source := fstest.MapFS{
		"help.16": &fstest.MapFile{Data: []byte("도움말:\n\t정보\n\n상태 문서\n")},
	}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-1", lease, "도움말 정보", source)
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "상태 문서") {
		t.Fatalf("help response=%q", text)
	}
	if string(store.state) != string(raw) {
		t.Fatal("help command mutated world state")
	}

	replay, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-1", lease, "도움말 정보", fstest.MapFS{})
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteHelpLineSupportsLegacyTopicsAndFailsClosed(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	state.Players["a"] = world.PlayerState{Body: state.Players["a"].Body, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{
		"helpfile":  &fstest.MapFile{Data: []byte("명령어 도움말")},
		"spellfile": &fstest.MapFile{Data: []byte("주문 목록")},
		"policy":    &fstest.MapFile{Data: []byte("정책")},
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
	for _, tc := range []struct {
		line string
		want string
	}{
		{line: "도움말", want: "명령어 도움말"},
		{line: "? 주술", want: "주문 목록"},
		{line: "도움말 정책", want: "정책"},
		{line: "도움말 아직없음", want: "그 명령어에 대한 도움말은 없습니다."},
	} {
		receipt, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-"+tc.line, lease, tc.line, source)
		if err != nil {
			t.Fatalf("line=%q err=%v", tc.line, err)
		}
		var text string
		if err := json.Unmarshal(receipt.Response, &text); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, tc.want) {
			t.Fatalf("line=%q text=%q want=%q", tc.line, text, tc.want)
		}
	}
	if store.commits != 4 {
		t.Fatalf("commits=%d want 4", store.commits)
	}

	if _, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-bad", lease, "도움말 정보 extra", source); !errors.Is(err, ErrUnsupportedHelpLine) {
		t.Fatalf("too many help tokens err=%v", err)
	}
	if store.commits != 4 {
		t.Fatalf("unsupported help created receipt: commits=%d", store.commits)
	}
	if _, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-missing", lease, "도움말 정보", fstest.MapFS{}); err == nil {
		t.Fatal("missing document unexpectedly succeeded")
	}
	if store.commits != 4 {
		t.Fatalf("missing document committed receipt: commits=%d", store.commits)
	}
}
