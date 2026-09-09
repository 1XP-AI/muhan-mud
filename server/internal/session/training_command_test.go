package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func trainingCommandRoomFlags(class byte) (flags [8]byte) {
	flags[world.TrainingRoomFlag/8] |= 1 << (world.TrainingRoomFlag % 8)
	if class > 0 && class <= 8 {
		bits := class - 1
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				roomFlag := world.TrainingRoomFlag + 3 - i
				flags[roomFlag/8] |= 1 << (roomFlag % 8)
			}
		}
	}
	return flags
}

func trainingCommandFixture(t *testing.T) []byte {
	t.Helper()
	class := byte(4)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Flags: trainingCommandRoomFlags(class)}},
			PlayerIDs: []string{"actor"},
		}},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: class, Level: 1, Experience: 128, Gold: 100},
				Online: true,
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitTrainingOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseTrainingLineAdmitsOnlyBareGlobalAlias(t *testing.T) {
	for _, line := range []string{"수련", "  수련  "} {
		command, ok := ParseTrainingLine(line)
		if !ok || command.Alias != "수련" || !IsTrainingLine(line) || !IsTrainLine(line) {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	for _, line := range []string{"train", "훈련", "수련 더", `"수련"`, "수련\n", string([]byte{0xff})} {
		if _, ok := ParseTrainingLine(line); ok || IsTrainingLine(line) {
			t.Fatalf("unsupported training line accepted: %q", line)
		}
	}
}

func TestExecuteTrainingLineCommitsTypedReceiptAndReplays(t *testing.T) {
	store := &departureStore{state: trainingCommandFixture(t)}
	owners, lease := admitTrainingOwner(t)
	first, err := owners.ExecuteTrainingLine(context.Background(), store, "w", "train-1", lease, "수련")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.TrainingResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "train" || result.Level != 2 || result.Class != 4 || result.Gold != 94 || result.GoldSpent != 6 || result.Broadcast || !result.BroadcastPending || result.Event != nil {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.Level != 2 || saved.Players["actor"].Body.Gold != 94 {
		t.Fatalf("saved actor=%+v", saved.Players["actor"].Body)
	}
	replay, err := owners.ExecuteTrainingLine(context.Background(), store, "w", "train-1", lease, "수련")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteTrainingLineFailsBeforeReceiptForUnsupportedOrWorldGate(t *testing.T) {
	store := &departureStore{state: trainingCommandFixture(t)}
	owners, lease := admitTrainingOwner(t)
	if _, err := owners.ExecuteTrainingLine(context.Background(), store, "w", "train-bad", lease, "수련 더"); !errors.Is(err, ErrUnsupportedTrainingLine) || store.commits != 0 {
		t.Fatalf("unsupported err=%v commits=%d", err, store.commits)
	}
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["actor"]
	actor.Body.Level = 100
	actor.Body.Experience = 7984959
	actor.Body.Gold = 500000
	actor.Body.Flags[world.TrainingFamilyFlag/8] |= 1 << (world.TrainingFamilyFlag % 8)
	state.Players["actor"] = actor
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteTrainingLineWithOptions(context.Background(), store, "w", "train-family", lease, "수련", TrainingOptions{}); !errors.Is(err, world.ErrTrainingFamilyPending) || store.commits != 0 {
		t.Fatalf("family err=%v commits=%d", err, store.commits)
	}
}
