package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestExecuteHelpLineSupportsAdditionalSourceBackedAliases(t *testing.T) {
	state, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	state.Players["a"] = world.PlayerState{Body: state.Players["a"].Body, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	// These document names are the exact files checked into help/ for the
	// corresponding aliases in src/global.c. The marker is deliberately the
	// source bytes supplied by the test, not guessed player-facing output.
	source := fstest.MapFS{
		"help.21":  &fstest.MapFile{Data: []byte("source:help.21\n")},
		"help.22":  &fstest.MapFile{Data: []byte("source:help.22\n")},
		"help.24":  &fstest.MapFile{Data: []byte("source:help.24\n")},
		"help.25":  &fstest.MapFile{Data: []byte("source:help.25\n")},
		"help.26":  &fstest.MapFile{Data: []byte("source:help.26\n")},
		"help.27":  &fstest.MapFile{Data: []byte("source:help.27\n")},
		"help.28":  &fstest.MapFile{Data: []byte("source:help.28\n")},
		"help.29":  &fstest.MapFile{Data: []byte("source:help.29\n")},
		"help.32":  &fstest.MapFile{Data: []byte("source:help.32\n")},
		"help.33":  &fstest.MapFile{Data: []byte("source:help.33\n")},
		"help.34":  &fstest.MapFile{Data: []byte("source:help.34\n")},
		"help.35":  &fstest.MapFile{Data: []byte("source:help.35\n")},
		"help.61":  &fstest.MapFile{Data: []byte("source:help.61\n")},
		"help.100": &fstest.MapFile{Data: []byte("source:help.100\n")},
	}
	tests := []struct {
		topic string
		doc   string
	}{
		{topic: "추적", doc: "help.21"},
		{topic: "엿봐", doc: "help.22"},
		{topic: "검색", doc: "help.24"},
		{topic: "찾아", doc: "help.24"},
		{topic: "표현", doc: "help.25"},
		{topic: "숨겨", doc: "help.26"},
		{topic: "숨어", doc: "help.26"},
		{topic: "설정", doc: "help.27"},
		{topic: "해제", doc: "help.28"},
		{topic: "외쳐", doc: "help.29"},
		{topic: "닫아", doc: "help.32"},
		{topic: "풀어", doc: "help.33"},
		{topic: "잠궈", doc: "help.34"},
		{topic: "따", doc: "help.35"},
		{topic: "환영", doc: "help.61"},
		{topic: "보아", doc: "help.100"},
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
	for i, tc := range tests {
		receipt, err := owners.ExecuteHelpLine(context.Background(), store, "w", fmt.Sprintf("help-alias-%d", i), lease, "도움말 "+tc.topic, source)
		if err != nil {
			t.Fatalf("topic=%q err=%v", tc.topic, err)
		}
		var text string
		if err := json.Unmarshal(receipt.Response, &text); err != nil {
			t.Fatal(err)
		}
		want := string(source[tc.doc].Data)
		if text != want {
			t.Fatalf("topic=%q response=%q want source %q", tc.topic, text, want)
		}
	}
	if store.commits != len(tests) {
		t.Fatalf("commits=%d want %d", store.commits, len(tests))
	}
}

func TestExecuteHelpLineAdditionalAliasStillFailsClosedAndReplays(t *testing.T) {
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
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	invalid := fstest.MapFS{"help.21": &fstest.MapFile{Data: []byte{0xff, 0xfe, 'x'}}}
	if _, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-alias-invalid", lease, "도움말 추적", invalid); err == nil {
		t.Fatal("invalid UTF-8 document unexpectedly succeeded")
	}
	if store.commits != 0 {
		t.Fatalf("invalid document created receipt: commits=%d", store.commits)
	}
	if _, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-alias-missing", lease, "도움말 추적", fstest.MapFS{}); err == nil {
		t.Fatal("missing document unexpectedly succeeded")
	}
	if store.commits != 0 {
		t.Fatalf("missing document created receipt: commits=%d", store.commits)
	}

	source := fstest.MapFS{"help.21": &fstest.MapFile{Data: []byte("source:help.21\n")}}
	first, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-alias-replay", lease, "도움말 추적", source)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteHelpLine(context.Background(), store, "w", "help-alias-replay", lease, "도움말 추적", fstest.MapFS{})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
