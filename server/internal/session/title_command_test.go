package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func titleCommandState(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(world.State{Version: 1, Rooms: map[int16]world.RoomState{1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}}}, Players: map[string]world.PlayerState{"actor": {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseTitleLine(t *testing.T) {
	for _, line := range []string{"칭호", "칭호 무명의 수호자", "칭호삭제"} {
		if command, ok := ParseTitleLine(line); !ok || command.Action == "" {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	for _, line := range []string{"칭호 ", "칭호 leading ", "칭호\n나쁨", "칭호삭제 extra", "title test"} {
		if _, ok := ParseTitleLine(line); ok {
			t.Fatalf("accepted invalid line %q", line)
		}
	}
}

func TestExecuteTitleLinePersistsAndReplays(t *testing.T) {
	store := &departureStore{state: titleCommandState(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	set, err := owners.ExecuteTitleLine(context.Background(), store, "w", "title-set", lease, "칭호 무명의 수호자")
	if err != nil || set.Replayed || store.commits != 1 {
		t.Fatalf("set=%+v err=%v commits=%d", set, err, store.commits)
	}
	var setResult world.TitleResult
	if err := json.Unmarshal(set.Response, &setResult); err != nil || setResult.Title != "무명의 수호자" || !setResult.Changed {
		t.Fatalf("set result=%+v err=%v", setResult, err)
	}
	replay, err := owners.ExecuteTitleLine(context.Background(), store, "w", "title-set", lease, "칭호 무명의 수호자")
	if err != nil || !replay.Replayed || string(replay.Response) != string(set.Response) || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	view, err := owners.ExecuteTitleLine(context.Background(), store, "w", "title-view", lease, "칭호")
	if err != nil || view.Replayed || store.commits != 2 {
		t.Fatalf("view=%+v err=%v commits=%d", view, err, store.commits)
	}
	var viewResult world.TitleResult
	if err := json.Unmarshal(view.Response, &viewResult); err != nil || !strings.Contains(viewResult.Response, "무명의 수호자") {
		t.Fatalf("view result=%+v err=%v", viewResult, err)
	}
	clear, err := owners.ExecuteTitleLine(context.Background(), store, "w", "title-clear", lease, "칭호삭제")
	if err != nil || clear.Replayed || store.commits != 3 {
		t.Fatalf("clear=%+v err=%v commits=%d", clear, err, store.commits)
	}
}
