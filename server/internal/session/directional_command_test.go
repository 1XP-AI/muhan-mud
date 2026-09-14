package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func directionalCommandFixture() []byte {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteDirectionalLineCommitsCanonicalMovementAndReplays(t *testing.T) {
	store := &departureStore{state: directionalCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-1", lease, "  8  북문", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.Response), "도착지") || store.commits != 1 {
		t.Fatalf("response=%q commits=%d", first.Response, store.commits)
	}
	state, err := world.DecodeState(store.state)
	if err != nil || state.Players["a"].Body.RoomID != 2 || len(state.Rooms[1].PlayerIDs) != 0 || len(state.Rooms[2].PlayerIDs) != 1 {
		t.Fatalf("movement state %+v %v", state, err)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-1", lease, "  8  북문", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectionalLineClosedFlyTimeSexGatesAndReplay(t *testing.T) {
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		line string
		hour int
		want string
		flag uint
		male bool
	}{
		{id: "dir-closed", line: "북", hour: 12, want: "문이 닫혀 있습니다.", flag: 3},
		{id: "dir-fly", line: "북", hour: 12, want: "그 쪽으로는 날아서 가야 될것 같군요.", flag: 11},
		{id: "dir-night", line: "북", hour: 12, want: "그 출구는 밤에만 열려 있습니다.", flag: 16},
		{id: "dir-day", line: "북", hour: 21, want: "그 출구는 밤에는 닫혀 있습니다.", flag: 17},
		{id: "dir-female", line: "북", hour: 12, want: "여성만 들어갈수 있습니다. 여탕인가~~", flag: 12, male: true},
		{id: "dir-male", line: "북", hour: 12, want: "남성만 들어갈수 있습니다.", flag: 13},
	} {
		t.Run(tc.id, func(t *testing.T) {
			state, err := world.DecodeState(directionalCommandFixture())
			if err != nil {
				t.Fatal(err)
			}
			room := state.Rooms[1]
			room.Resource.Exits[0].Flags[tc.flag/8] |= 1 << (tc.flag % 8)
			state.Rooms[1] = room
			if tc.male {
				actor := state.Players["a"]
				actor.Body.Flags[12/8] |= 1 << (12 % 8)
				state.Players["a"] = actor
			}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, tc.hour, world.SceneOptions{}, nil, nil, nil)
			if err != nil || first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
			}
			var text string
			if err := json.Unmarshal(first.Response, &text); err != nil || text != tc.want {
				t.Fatalf("response=%q err=%v", text, err)
			}
			saved, err := world.DecodeState(store.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("gate moved actor=%+v err=%v", saved.Players["a"], err)
			}
			replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, tc.hour, world.SceneOptions{}, nil, nil, nil)
			if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
			}
		})
	}
}

func TestExecuteDirectionalLineMissingDestinationFailsClosed(t *testing.T) {
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		line string
		name string
	}{
		{id: "dir-missing-north", line: "북", name: "북"},
		{id: "dir-missing-east", line: "동", name: "동"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			state, err := world.DecodeState(directionalCardinalCommandFixture())
			if err != nil {
				t.Fatal(err)
			}
			room := state.Rooms[1]
			for i, exit := range room.Resource.Exits {
				if exit.Name == tc.name {
					room.Resource.Exits[i].Destination = 99
				}
			}
			state.Rooms[1] = room
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, 12, world.SceneOptions{}, nil, nil, nil)
			if first.Replayed || len(first.Response) != 0 {
				t.Fatalf("receipt=%+v", first)
			}
			if !errors.Is(err, ErrDirectionalDestinationUnresolved) || errors.Is(err, world.ErrGoDestinationUnresolved) {
				t.Fatalf("err=%v", err)
			}
			if err.Error() != DirectionalMapMissingResponse || err.Error() == world.GoMapMissingResponse {
				t.Fatalf("copy=%q", err.Error())
			}
			if store.commits != 0 {
				t.Fatalf("commits=%d", store.commits)
			}
			saved, err := world.DecodeState(store.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Rooms[1].Resource.Track != "" {
				t.Fatalf("fail-closed mutated snapshot actor=%+v track=%q err=%v", saved.Players["a"], saved.Rooms[1].Resource.Track, err)
			}
			replay, replayErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, 12, world.SceneOptions{}, nil, nil, nil)
			if replay.Replayed || !errors.Is(replayErr, ErrDirectionalDestinationUnresolved) || store.commits != 0 {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, replayErr, store.commits)
			}
			if replayErr.Error() != DirectionalMapMissingResponse {
				t.Fatalf("replay copy=%q", replayErr.Error())
			}
		})
	}
}

func TestExecuteDirectionalLineRejectsNonMovementWithoutCommit(t *testing.T) {
	store := &departureStore{state: directionalCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "bad", lease, "봐", 100, 12, world.SceneOptions{}, nil, nil, nil); err == nil || store.commits != 0 {
		t.Fatalf("unsupported direction committed: %v commits=%d", err, store.commits)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 봐", "look-suffix-east"},
		{"북 보다", "look-suffix-north"},
		{"동 junk 봐", "look-extra-east"},
		{"북 foo 보다", "look-extra-north"},
	} {
		if _, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); err == nil || store.commits != 0 {
			t.Fatalf("look suffix %q moved: %v commits=%d", tt.line, err, store.commits)
		}
	}
}

func directionalCardinalCommandFixture() []byte {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지", Exits: []world.LegacyExit{{Name: "북", Destination: 2}, {Name: "동", Destination: 3}}}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "북쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			3: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "동쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}},
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestParseCommandLookVerbInMiddleDoesNotExecuteDirectionalOrMove(t *testing.T) {
	store := &departureStore{state: directionalCardinalCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 봐 extra", "look-mid-east"},
		{"북 보다 junk", "look-mid-north"},
	} {
		parsed, err := ParseCommand(tt.line)
		if err != nil || parsed.Kind == CommandDirectional {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want fail-closed unknown or look", tt.line, parsed, err)
		}
		if parsed.Kind != CommandUnknown && parsed.Kind != CommandLook {
			t.Fatalf("ParseCommand(%q)=%+v want CommandUnknown or CommandLook", tt.line, parsed)
		}
		switch parsed.Kind {
		case CommandDirectional:
			if _, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); execErr != nil {
				t.Fatalf("Submit ExecuteDirectionalLine(%q) err=%v", tt.line, execErr)
			}
			t.Fatalf("Submit dispatched CommandDirectional for %q", tt.line)
		case CommandLook:
			if _, execErr := owners.ExecuteLookLine(context.Background(), store, "w", tt.id+"-look", lease, tt.line, 12); execErr == nil {
				saved, decodeErr := world.DecodeState(store.state)
				if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
					t.Fatalf("look mid-verb %q moved actor: %+v err=%v", tt.line, saved.Players["a"], decodeErr)
				}
			}
		}
		if _, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); err == nil || store.commits != 0 {
			t.Fatalf("look mid-verb %q moved: %v commits=%d", tt.line, err, store.commits)
		}
		saved, err := world.DecodeState(store.state)
		if err != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("look mid-verb %q changed RoomID: %+v err=%v", tt.line, saved.Players["a"], err)
		}
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-mid-look", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("move after reject=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-mid-look", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectionalLineRejectsLastTokenItemMutationWithoutCommit(t *testing.T) {
	store := &departureStore{state: directionalCardinalCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 주워", "item-dir-east-take"},
		{"동 버려", "item-dir-east-drop"},
		{"동 꺼내", "item-dir-east-get"},
		{"동 넣어", "item-dir-east-put"},
		{"동 junk extra 버려", "item-dir-east-extra-drop"},
		{"북 foo extra 주워", "item-dir-north-extra-take"},
	} {
		if _, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); !errors.Is(execErr, ErrUnsupportedDirectionalLine) || store.commits != 0 {
			t.Fatalf("last-token item mutation %q moved: %v commits=%d", tt.line, execErr, store.commits)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("last-token item mutation %q changed RoomID: %+v err=%v", tt.line, saved.Players["a"], decodeErr)
		}
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-item-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("move after item reject=%+v err=%v commits=%d", first, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 2 {
		t.Fatalf("move after item reject RoomID=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-item-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectionalLineRejectsLastTokenItemsWithoutCommit(t *testing.T) {
	store := &departureStore{state: directionalCardinalCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 소지품", "items-dir-east-inv"},
		{"동 장비", "items-dir-east-eq"},
		{"동 장", "items-dir-east-eq-alias"},
		{"북 소지품", "items-dir-north-inv"},
		{"동 junk extra 소지품", "items-dir-east-extra-inv"},
		{"북 foo extra 장비", "items-dir-north-extra-eq"},
		{"북 foo extra 장", "items-dir-north-extra-eq-alias"},
	} {
		parsed, parseErr := ParseCommand(tt.line)
		if parseErr != nil || parsed.Kind != CommandItems {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandItems, not directional", tt.line, parsed, parseErr)
		}
		if _, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); !errors.Is(execErr, ErrUnsupportedDirectionalLine) || store.commits != 0 {
			t.Fatalf("last-token items %q moved: %v commits=%d", tt.line, execErr, store.commits)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("last-token items %q changed RoomID: %+v err=%v", tt.line, saved.Players["a"], decodeErr)
		}
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-items-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("move after items reject=%+v err=%v commits=%d", first, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 2 {
		t.Fatalf("move after items reject RoomID=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-items-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDirectionalLineRejectsMidVerbItemMutationWithoutCommit(t *testing.T) {
	store := &departureStore{state: directionalCardinalCommandFixture()}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 버려 extra", "item-mid-east"},
		{"북 주워 junk", "item-mid-north"},
	} {
		parsed, parseErr := ParseCommand(tt.line)
		if parseErr != nil || parsed.Kind == CommandDirectional {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandUnknown, not CommandDirectional", tt.line, parsed, parseErr)
		}
		if parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%+v want CommandUnknown", tt.line, parsed)
		}
		if _, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", tt.id, lease, tt.line, 100, 12, world.SceneOptions{}, nil, nil, nil); !errors.Is(execErr, ErrUnsupportedDirectionalLine) || store.commits != 0 {
			t.Fatalf("mid-verb item mutation %q moved: %v commits=%d", tt.line, execErr, store.commits)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("mid-verb item mutation %q changed RoomID: %+v err=%v", tt.line, saved.Players["a"], decodeErr)
		}
	}
	first, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-item-mid-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("move after item mid reject=%+v err=%v commits=%d", first, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 2 {
		t.Fatalf("move after item mid reject RoomID=%+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteDirectionalLine(context.Background(), store, "w", "move-after-item-mid-reject", lease, "북", 100, 12, world.SceneOptions{}, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
