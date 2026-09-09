package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func fleeCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, NPCIDs: []string{"wolf-id"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 10, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf-id": {Body: world.LegacyMonster{Name: "늑대", RoomID: 1, Type: 1, Class: 4, Level: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}}},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseFleeLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"도망", " 도 "} {
		if _, ok := ParseFleeLine(line); !ok {
			t.Fatalf("flee alias rejected: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandFlee || len(parsed.Tokens) != 1 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
	}
	for _, line := range []string{"도망 북", "도망\n", "도망\x00", "flee"} {
		if _, ok := ParseFleeLine(line); ok {
			t.Fatalf("unsupported flee accepted: %q", line)
		}
	}
	if parsed, err := ParseCommand("도망 북"); err != nil || parsed.Kind != CommandUnknown {
		t.Fatalf("targeted flee classification parsed=%+v err=%v", parsed, err)
	}
}

func TestExecuteFleeLinePersistsMovementAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: fleeCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteFleeLine(context.Background(), store, "w", "flee-1", lease, "도망", 100, 12, func(_, _ int) int { return 1 }, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.FleeResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || !result.Moved || !strings.Contains(result.Response, "줄행랑") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 2 || saved.Rooms[1].Resource.Track != "북" {
		t.Fatalf("saved=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteFleeLine(context.Background(), store, "w", "flee-1", lease, "도망", 100, 12, func(int, int) int { t.Fatal("flee RNG replayed"); return 0 }, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFleeLineRejectsUnsupportedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: fleeCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteFleeLine(context.Background(), store, "w", "flee-bad", lease, "도망 북", 100, 12, func(int, int) int { return 1 }, nil, nil); err != ErrUnsupportedFleeLine || store.commits != 0 {
		t.Fatalf("unsupported flee reached receipt: err=%v commits=%d", err, store.commits)
	}
}
