package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func meditateCommandFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{
				Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: 4,
				Stats: [5]byte{10, 10, 10, 10, 15},
			}, Online: true},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseMeditateLineAdmitsOnlyExactBareAlias(t *testing.T) {
	for _, line := range []string{"참선", " 참선 "} {
		command, ok := ParseMeditateLine(line)
		if !ok || command.Alias != "참선" {
			t.Fatalf("line=%q command=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"참선 추가", "참 선", "참선\n", "참선\x00", "meditate", "명상", ""} {
		if IsMeditateLine(line) {
			t.Fatalf("malformed meditate line accepted: %q", line)
		}
	}
}

func TestExecuteMeditateLinePersistsTypedSuccessAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: meditateCommandFixture(t, world.ClericClass)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteMeditateLine(context.Background(), store, "w", "meditate-1", lease, "참선", 1000, func(int, int) int {
		calls++
		return 1
	})
	if err != nil || first.Replayed || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.MeditateResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Succeeded || !result.Broadcast || result.Action != "meditate" || result.Event == nil || !strings.Contains(result.Response, "새롭게") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["a"].Body
	if actor.Stats[3] != 13 || actor.Timers[world.MeditateTimerIndex].LastTime != 1000 || actor.Timers[world.MeditateTimerIndex].Interval != 150 || actor.Flags[world.MeditateFlag/8]&(1<<(world.MeditateFlag%8)) == 0 {
		t.Fatalf("saved actor=%+v", actor)
	}
	replay, err := owners.ExecuteMeditateLine(context.Background(), store, "w", "meditate-1", lease, "참선", 1000, func(int, int) int {
		t.Fatal("meditate RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || calls != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
}

func TestExecuteMeditateLinePersistsFailureCooldownAndRejectsExtraToken(t *testing.T) {
	store := &departureStore{state: meditateCommandFixture(t, world.PaladinClass)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteMeditateLine(context.Background(), store, "w", "meditate-fail", lease, "참선", 1000, func(int, int) int { return 100 })
	if err != nil || first.Replayed {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var result world.MeditateResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Succeeded || !result.Attempted || !result.Broadcast || !strings.Contains(result.Response, "주화입마") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := saved.Players["a"].Body
	if actor.Stats[3] != 10 || actor.Flags[world.MeditateFlag/8]&(1<<(world.MeditateFlag%8)) != 0 || actor.Timers[world.MeditateTimerIndex].LastTime != 410 {
		t.Fatalf("saved actor=%+v", actor)
	}
	if _, err := owners.ExecuteMeditateLine(context.Background(), store, "w", "meditate-bad", lease, "참선 추가", 1000, func(int, int) int { t.Fatal("invalid line consumed RNG"); return 1 }); err == nil || store.commits != 1 {
		t.Fatalf("extra token reached receipt: err=%v commits=%d", err, store.commits)
	}
}
