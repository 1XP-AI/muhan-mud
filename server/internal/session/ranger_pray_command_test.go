package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func rangerPrayCommandFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	actor := s.Players["a"]
	actor.Body.Name = "Alice"
	actor.Body.Type = 0
	actor.Body.RoomID = 1
	actor.Body.Class = class
	actor.Body.Level = 4
	actor.Body.Stats = [5]byte{10, 15, 10, 10, 15}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseRangerPrayLinesAdmitsOnlyExactBareAliases(t *testing.T) {
	for _, tc := range []struct {
		line string
		kind world.RangerPrayKind
	}{
		{line: "활보법", kind: world.RangerHaste},
		{line: " 신원법 ", kind: world.RangerPray},
	} {
		command, ok := ParseRangerPrayLine(tc.line)
		if !ok || command.Kind != tc.kind {
			t.Fatalf("line=%q command=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{"활보법 추가", "신원법 추가", "활 보법", "활보법\n", "신원법\x00", "haste", "pray"} {
		if IsRangerPrayLine(line) {
			t.Fatalf("malformed ranger/pray line accepted: %q", line)
		}
	}
}

func TestExecuteHasteLinePersistsTypedResultAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: rangerPrayCommandFixture(t, world.RangerClass)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteHasteLine(context.Background(), store, "w", "haste-1", lease, "활보법", 1000, func(int, int) int { return 1 })
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.HasteResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Succeeded || !result.Broadcast || result.Kind != world.RangerHaste || !strings.Contains(result.Response, "민첩") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Stats[1] != 30 || saved.Players["a"].Body.Timers[world.HasteTimerIndex].LastTime != 1000 || saved.Players["a"].Body.Flags[world.HasteFlag/8]&(1<<(world.HasteFlag%8)) == 0 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteHasteLine(context.Background(), store, "w", "haste-1", lease, "활보법", 1000, func(int, int) int { t.Fatal("haste RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecutePrayFailureRecordsOnlyCooldownAndRejectsExtraToken(t *testing.T) {
	store := &departureStore{state: rangerPrayCommandFixture(t, world.ClericClass)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecutePrayLine(context.Background(), store, "w", "pray-1", lease, "신원법", 1000, func(int, int) int { return 100 })
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.PrayResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Succeeded || !result.Attempted || !result.Broadcast {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.Stats[4] != 15 || saved.Players["a"].Body.Flags[world.PrayFlag/8]&(1<<(world.PrayFlag%8)) != 0 || saved.Players["a"].Body.Timers[world.PrayTimerIndex].LastTime != 410 {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	if _, err := owners.ExecutePrayLine(context.Background(), store, "w", "pray-bad", lease, "신원법 추가", 1000, func(int, int) int { t.Fatal("invalid line consumed RNG"); return 1 }); err == nil || store.commits != 1 {
		t.Fatalf("extra token reached receipt: err=%v commits=%d", err, store.commits)
	}
}
