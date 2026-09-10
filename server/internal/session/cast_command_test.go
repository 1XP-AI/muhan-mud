package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func castSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[0] = 1 << 0 // SVIGOR / 회복
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{"a": {
			Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: world.ClericClass, Level: 8, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18},
				HPMax: 100, HPCurrent: 10, MPMax: 50, MPCurrent: 30,
				Spells: spells,
			},
			Online: true,
		}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseCastLineKeepsSelfTargetBoundary(t *testing.T) {
	tests := []struct {
		line      string
		kind      CommandKind
		spellName string
	}{
		{line: "주문", kind: CommandCast},
		{line: "주문 회복", kind: CommandCast, spellName: "회복"},
		{line: "주문 원기회복", kind: CommandCast, spellName: "원기회복"},
	}
	for _, tc := range tests {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", tc.line, parsed, err)
		}
		command, ok := ParseCastLine(tc.line)
		if !ok || command.SpellName != tc.spellName {
			t.Fatalf("ParseCastLine(%q)=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{"주문 회복 Alice", "주문\n회복", "주문\x00"} {
		if IsCastLine(line) {
			t.Fatalf("unsupported cast form accepted: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("malformed cast form=%+v err=%v", parsed, err)
		}
	}
}

func TestExecuteCastLinePersistsResponseAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: castSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-1", lease, "주문 회복", 100, func(low, high int) int {
		calls++
		if low != 1 || high < low {
			t.Fatalf("unexpected cast random bounds %d..%d", low, high)
		}
		return high
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "회복" || result.HPDelta <= 0 || !strings.Contains(result.Response, "회복 주문") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.MPCurrent != 28 || saved.Players["a"].Body.HPCurrent != 22 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex].LastTime != 100 {
		t.Fatalf("saved body=%+v", saved.Players["a"].Body)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-1", lease, "주문 회복", 200, func(int, int) int {
		t.Fatal("cast RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteCastLineRejectsTargetFormBeforeReceipt(t *testing.T) {
	store := &departureStore{state: castSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-target", lease, "주문 회복 Alice", 100, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("target form reached receipt: err=%v commits=%d", err, store.commits)
	}
}
