package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func trackCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	p.Body.Class = 7
	p.Body.Stats[1] = 15
	p.Body.Flags[1/8] |= 1 << (1 % 8)
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Resource.Track = "동"
	s.Rooms[1] = r
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseTrackLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"추적", " 추적 "} {
		if _, ok := ParseTrackLine(line); !ok {
			t.Fatalf("track alias rejected: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandTrack || len(parsed.Tokens) != 1 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
	}
	for _, line := range []string{"추적 동", "추적\n", "추적\x00", "track"} {
		if _, ok := ParseTrackLine(line); ok {
			t.Fatalf("unsupported track accepted: %q", line)
		}
	}
}

func TestExecuteTrackLinePersistsResponseAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: trackCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteTrackLine(context.Background(), store, "w", "track-1", lease, "추적", 100, func(_, _ int) int { return 1 })
	if err != nil || first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "동쪽") {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Flags[0]&(1<<1) != 0 || saved.Players["a"].Body.Timers[4].LastTime != 100 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteTrackLine(context.Background(), store, "w", "track-1", lease, "추적", 100, func(int, int) int { t.Fatal("track RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%q err=%v commits=%d", replay.Response, err, store.commits)
	}
}
