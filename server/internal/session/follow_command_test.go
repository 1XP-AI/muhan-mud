package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func followCommandFixture() []byte {
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Players["b"] = world.PlayerState{Body: world.LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1, HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteFollowLinePersistsRelationAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-1", lease, "따라 Bob")
	if err != nil || !strings.Contains(string(first.Response), "Bob") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].FollowingID != "b" || len(saved.Players["b"].FollowerIDs) != 1 || saved.Players["b"].FollowerIDs[0] != "a" {
		t.Fatalf("follow state=%+v err=%v", saved.Players, err)
	}
	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-1", lease, "따라 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineStopsFollowingAndReplays(t *testing.T) {
	store := &departureStore{state: followCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	follow, err := owners.ExecuteFollowLine(context.Background(), store, "w", "follow-2", lease, "따라 Bob")
	if err != nil || !strings.Contains(string(follow.Response), "Bob") || store.commits != 1 {
		t.Fatalf("follow=%q err=%v commits=%d", follow.Response, err, store.commits)
	}

	expel, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-1", lease, "내보내")
	if err != nil || !strings.Contains(string(expel.Response), "그만 따라다니기로") || store.commits != 2 {
		t.Fatalf("expel=%q err=%v commits=%d", expel.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].FollowingID != "" || len(saved.Players["b"].FollowerIDs) != 0 {
		t.Fatalf("expelled state=%+v err=%v", saved.Players, err)
	}

	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-1", lease, "내보내")
	if err != nil || !replay.Replayed || store.commits != 2 || string(replay.Response) != string(expel.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteFollowLineExpelsFollowerAndReplays(t *testing.T) {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	s, err = s.FollowPlayer("b", "a")
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	expel, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-2", lease, "내보내 Bob")
	if err != nil || !strings.Contains(string(expel.Response), "못 따라오도록") || store.commits != 1 {
		t.Fatalf("expel=%q err=%v commits=%d", expel.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["b"].FollowingID != "" || len(saved.Players["a"].FollowerIDs) != 0 {
		t.Fatalf("expelled state=%+v err=%v", saved.Players, err)
	}

	replay, err := owners.ExecuteFollowLine(context.Background(), store, "w", "expel-2", lease, "내보내 Bob")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(expel.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
