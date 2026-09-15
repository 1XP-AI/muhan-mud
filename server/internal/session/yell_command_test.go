package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func TestExecuteYellLinePersistsRevealAndReplays(t *testing.T) {
	fixture, err := decodeYellFixture()
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: fixture}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteYellLine(context.Background(), store, "w", "yell-1", lease, "외쳐 안녕하세요")
	if err != nil || first.Replayed || first.Revision != 1 || !strings.Contains(string(first.Response), "좋습니다") || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteYellLine(context.Background(), store, "w", "yell-1", lease, "외쳐 안녕하세요")
	if err != nil || !replay.Replayed || replay.Revision != first.Revision || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func decodeYellFixture() ([]byte, error) {
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		return nil, err
	}
	room := s.Rooms[1]
	room.Resource.Exits = []world.LegacyExit{{Destination: 2}}
	s.Rooms[1] = room
	s.Rooms[2] = world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	p := s.Players["a"]
	p.Body.Flags[0] |= 1 << 1
	s.Players["a"] = p
	return json.Marshal(s)
}
