package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func settingsCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[26/8] &^= 1 << (26 % 8)
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseSettingsLineAndCommandClassification(t *testing.T) {
	cases := []struct {
		line, action, key string
		hasValue          bool
		value             int32
	}{
		{"설정", "set", "", false, 0},
		{"설정 색", "set", "색", false, 0},
		{"설정 도망수치 1", "set", "도망수치", true, 1},
		{"해제 방이름", "clear", "방이름", false, 0},
	}
	for _, tc := range cases {
		got, ok := ParseSettingsLine(tc.line)
		if !ok || got.Action != tc.action || got.Key != tc.key || got.HasValue != tc.hasValue || got.Value != tc.value {
			t.Fatalf("ParseSettingsLine(%q)=%+v ok=%v want=%+v", tc.line, got, ok, tc)
		}
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != CommandSettings {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", tc.line, parsed, err)
		}
	}
	for _, line := range []string{"설정 도망수치 nope", "해제 도망수치 10", "설정 색 extra", "설정\n", "설정 색\x00"} {
		if _, ok := ParseSettingsLine(line); ok {
			t.Fatalf("unsupported settings accepted: %q", line)
		}
	}
}

func TestExecuteSettingsLinePersistsAndReplays(t *testing.T) {
	store := &departureStore{state: settingsCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteSettingsLine(context.Background(), store, "w", "settings-1", lease, "설정 색")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.SettingsResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !strings.Contains(result.Response, "색        :  사용 ") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteSettingsLine(context.Background(), store, "w", "settings-1", lease, "설정 색")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Flags[26/8]&(1<<(26%8)) == 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteSettingsLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: settingsCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteSettingsLine(context.Background(), store, "w", "settings-bad", lease, "해제 도망수치 10"); err == nil || store.commits != 0 {
		t.Fatalf("malformed settings reached receipt: err=%v commits=%d", err, store.commits)
	}
}
