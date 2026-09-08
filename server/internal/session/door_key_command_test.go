package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func doorKeyCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(doorCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	room.Resource.Exits = []world.LegacyExit{{Name: "북", Flags: [4]byte{0x3c}, Key: 7}}
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Class = 8
	actor.Body.Level = 4
	actor.Body.Stats[1] = 20
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{
		"key-1": {Object: world.LegacyObject{Name: "열쇠", Type: 11, DiceCount: 7, ShotsCurrent: 2}},
	}, Inventory: []string{"key-1"}}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseDoorKeyLineAndCommandClassification(t *testing.T) {
	for _, line := range []string{"풀어 북 열쇠", "잠궈 북 열쇠", "따 북", "풀어", "잠궈", "따"} {
		command, ok := ParseDoorKeyLine(line)
		if !ok || command.Action == "" {
			t.Fatalf("ParseDoorKeyLine(%q)=%+v ok=%v", line, command, ok)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDoorKey {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	command, ok := ParseDoorKeyLine("풀어 북 열쇠")
	if !ok || command.Action != world.DoorUnlock || command.Target != "북" || command.Key != "열쇠" {
		t.Fatalf("command=%+v ok=%v", command, ok)
	}
	for _, line := range []string{"풀어 북 열쇠 여분", "따 북 열쇠", "풀어\n", "잠궈 북\x00"} {
		if _, ok := ParseDoorKeyLine(line); ok {
			t.Fatalf("unsupported door-key line accepted: %q", line)
		}
	}
}

func TestExecuteDoorKeyLinePersistsAndReplaysWithoutRandom(t *testing.T) {
	store := &departureStore{state: doorKeyCommandFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteDoorKeyLineWithOptions(context.Background(), store, "w", "door-key-1", lease, "풀어 북 열쇠", DoorKeyOptions{Now: 10})
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DoorKeyCommandResult
	if err := json.Unmarshal(first.Response, &result); err != nil || !result.Broadcast || result.Action != world.DoorUnlock || !strings.Contains(result.Response, "찰칵") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	replay, err := owners.ExecuteDoorKeyLineWithOptions(context.Background(), store, "w", "door-key-1", lease, "풀어 북 열쇠", DoorKeyOptions{Now: 10, Roll: func(int, int) int { t.Fatal("unlock replay consumed RNG"); return 1 }})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Rooms[1].Resource.Exits[0].Flags[0]&4 != 0 || saved.Players["a"].Items.Items["key-1"].Object.ShotsCurrent != 1 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if _, err := owners.ExecuteDoorKeyLineWithOptions(context.Background(), store, "w", "door-key-lock", lease, "잠궈 북 열쇠", DoorKeyOptions{Now: 11}); err != nil {
		t.Fatalf("lock=%v", err)
	}

	// Picklock records its random result in the response. Replaying the same
	// command must not invoke the random source a second time.
	pick, err := owners.ExecuteDoorKeyLineWithOptions(context.Background(), store, "w", "door-key-pick", lease, "따 북", DoorKeyOptions{Now: 20, Roll: func(_, _ int) int { return 1 }})
	if err != nil || pick.Replayed {
		t.Fatalf("pick=%+v err=%v", pick, err)
	}
	pickReplay, err := owners.ExecuteDoorKeyLineWithOptions(context.Background(), store, "w", "door-key-pick", lease, "따 북", DoorKeyOptions{Now: 20, Roll: func(int, int) int { t.Fatal("pick replay consumed RNG"); return 100 }})
	if err != nil || !pickReplay.Replayed || store.commits != 3 {
		t.Fatalf("pick replay=%+v err=%v commits=%d", pickReplay, err, store.commits)
	}
}

func TestExecuteDoorKeyLineRejectsMalformedBeforeReceipt(t *testing.T) {
	store := &departureStore{state: doorKeyCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	_ = owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteDoorKeyLine(context.Background(), store, "w", "door-key-bad", lease, "따 북 열쇠", 10, func(_, _ int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("malformed door-key reached receipt: err=%v commits=%d", err, store.commits)
	}
}
