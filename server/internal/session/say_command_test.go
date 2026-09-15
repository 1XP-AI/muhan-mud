package session

import (
	"context"
	"strings"
	"testing"
)

func TestExecuteSayLinePersistsRevealAndReplays(t *testing.T) {
	state := followCommandFixture()
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSayLine(context.Background(), store, "w", "say-1", lease, "말 안녕하세요")
	if err != nil || !strings.Contains(string(first.Response), "좋습니다") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteSayLine(context.Background(), store, "w", "say-1", lease, "말 안녕하세요")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestSayLineTextRecognizesOnlySupportedAliases(t *testing.T) {
	for _, tc := range []struct {
		line, want string
	}{
		{"말 hello", "hello"},
		{"\"hello", "hello"},
		{"'hello", "hello"},
		{"말", ""},
	} {
		got, ok := SayLineText(tc.line)
		if !ok || got != tc.want {
			t.Fatalf("line=%q got=%q ok=%v", tc.line, got, ok)
		}
	}
	if _, ok := SayLineText("말투"); ok {
		t.Fatal("unsupported prefix recognized")
	}
}
