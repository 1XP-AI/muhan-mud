package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorGoState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "출발지",
					Exits: []world.LegacyExit{{Name: "동굴", Destination: 2}},
				}},
				PlayerIDs: []string{"actor", "observer"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}},
				PlayerIDs: []string{"away"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"away": {
				Body:   world.LegacyMonster{Name: "Carol", Type: 0, RoomID: 2, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
}

func connectorDirectionalCardinalState() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "서편",
					Exits: []world.LegacyExit{
						{Name: "북", Destination: 2},
						{Name: "동", Destination: 3},
					},
				}},
				PlayerIDs: []string{"actor", "observer"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "북쪽방"}},
				PlayerIDs: []string{"north"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			3: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "동쪽방"}},
				PlayerIDs: []string{"east"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"observer": {
				Body:   world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"north": {
				Body:   world.LegacyMonster{Name: "Nora", Type: 0, RoomID: 2, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
			"east": {
				Body:   world.LegacyMonster{Name: "Ed", Type: 0, RoomID: 3, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
}

func admitConnectorPlayers(t *testing.T, connector *WorldConnector, ids []string) map[string]*worldConnection {
	t.Helper()
	connections := map[string]*worldConnection{}
	for _, id := range ids {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = conn
		connector.connections[conn] = struct{}{}
	}
	return connections
}

func TestWorldConnectorSubmitDispatchesGoAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorGoState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "go", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "away"})

	output, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || !strings.Contains(output, "도착지") || store.commits != 1 {
		t.Fatalf("go=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections["observer"].events:
		if !strings.Contains(event, "떠났습니다") {
			t.Fatalf("source event=%q", event)
		}
	default:
		t.Fatal("source observer missing leave event")
	}
	select {
	case event := <-connections["away"].events:
		if !strings.Contains(event, "도착했습니다") {
			t.Fatalf("dest event=%q", event)
		}
	default:
		t.Fatal("destination observer missing arrival event")
	}
	select {
	case event := <-connections["actor"].events:
		t.Fatalf("actor received own room event: %q", event)
	default:
	}

	replay, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-connections["observer"].events:
		t.Fatalf("replay fanned out to source: %q", event)
	default:
	}
	select {
	case event := <-connections["away"].events:
		t.Fatalf("replay fanned out to dest: %q", event)
	default:
	}

	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 2 {
		t.Fatalf("saved actor=%+v err=%v", saved.Players["actor"], err)
	}
}

func TestWorldConnectorSubmitGoClosedFlyTimeSexGatesAndReplay(t *testing.T) {
	for _, tc := range []struct {
		id   string
		line string
		hour int
		want string
		flag uint
		male bool
	}{
		{id: "closed", line: "가 동굴", hour: 12, want: world.GoClosedResponse, flag: 3},
		{id: "fly", line: "들어가 동굴", hour: 12, want: world.GoFlyResponse, flag: 11},
		{id: "night", line: "동굴 가", hour: 12, want: world.GoNightResponse, flag: 16},
		{id: "day", line: "가 동굴", hour: 21, want: world.GoDayResponse, flag: 17},
		{id: "female", line: "가 동굴", hour: 12, want: world.GoFemaleResponse, flag: 12, male: true},
		{id: "male", line: "가 동굴", hour: 12, want: world.GoMaleResponse, flag: 13},
	} {
		t.Run(tc.id, func(t *testing.T) {
			state := connectorGoState()
			room := state.Rooms[1]
			room.Resource.Exits[0].Flags[tc.flag/8] |= 1 << (tc.flag % 8)
			state.Rooms[1] = room
			if tc.male {
				actor := state.Players["actor"]
				actor.Body.Flags[12/8] |= 1 << (12 % 8)
				state.Players["actor"] = actor
			}
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
			store.connectorCommandStore.state = raw
			connector, err := NewWorldConnector(WorldConnectorConfig{
				Store: store, WorldID: "go-gate-" + tc.id, Clock: func() (int32, int) { return 100, tc.hour }, MaxSessions: 3,
			})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "away"})
			output, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || output != tc.want || store.commits != 1 {
				t.Fatalf("go=%q err=%v commits=%d", output, err, store.commits)
			}
			select {
			case event := <-connections["observer"].events:
				t.Fatalf("denied go fanned out: %q", event)
			default:
			}
			replay, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || replay != output {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["actor"].Body.RoomID != 1 {
				t.Fatalf("saved actor=%+v err=%v", saved.Players["actor"], err)
			}
		})
	}
}

func TestWorldConnectorSubmitDirectionalMissingDestinationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		line string
		name string
	}{
		{line: "북", name: "북"},
		{line: "동", name: "동"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			state := connectorDirectionalCardinalState()
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
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
			store.connectorCommandStore.state = raw
			connector, err := NewWorldConnector(WorldConnectorConfig{
				Store: store, WorldID: "dir-missing-" + tc.line, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "north", "east"})
			output, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || output != session.DirectionalMapMissingResponse || output == world.GoMapMissingResponse || store.commits != 0 {
				t.Fatalf("missing dest output=%q err=%v commits=%d", output, err, store.commits)
			}
			if !connections["actor"].ready {
				t.Fatal("missing dest sealed the socket")
			}
			select {
			case event := <-connections["observer"].events:
				t.Fatalf("source observer event=%q", event)
			default:
			}
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["actor"].Body.RoomID != 1 || saved.Rooms[1].Resource.Track != "" {
				t.Fatalf("fail-closed mutated snapshot actor=%+v track=%q err=%v", saved.Players["actor"], saved.Rooms[1].Resource.Track, err)
			}
			replay, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || replay != session.DirectionalMapMissingResponse || store.commits != 0 {
				t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
			}
			if !connections["actor"].ready {
				t.Fatal("replay sealed the socket")
			}
		})
	}
}

func TestWorldConnectorSubmitGoMissingDestinationFailsClosed(t *testing.T) {
	state := connectorGoState()
	room := state.Rooms[1]
	room.Resource.Exits[0].Destination = 99
	state.Rooms[1] = room
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "go-missing", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "away"})
	output, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || output != world.GoMapMissingResponse || store.commits != 0 {
		t.Fatalf("missing dest output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 1 || saved.Rooms[1].Resource.Track != "" {
		t.Fatalf("fail-closed mutated snapshot actor=%+v track=%q err=%v", saved.Players["actor"], saved.Rooms[1].Resource.Track, err)
	}
}

func TestWorldConnectorSubmitGoChaseFanOutSuppressesReplay(t *testing.T) {
	state := connectorGoState()
	src := state.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	state.Rooms[1] = src
	wolf := world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	wolf.Flags[9/8] |= 1 << (9 % 8)
	state.NPCs = map[string]world.NPCState{
		"wolf": {Body: wolf, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}},
	}
	state.ActiveNPCIDs = []string{}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "go-chase", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
		Roll: func(int, int) int { return 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "away"})

	output, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || !strings.Contains(output, world.NPCGoChaseActorText("늑대")) || store.commits != 1 {
		t.Fatalf("go=%q err=%v commits=%d", output, err, store.commits)
	}

	gotLeave, gotChase := false, false
	for i := 0; i < 2; i++ {
		select {
		case event := <-connections["observer"].events:
			switch {
			case strings.Contains(event, "떠났습니다"):
				gotLeave = true
			case strings.Contains(event, world.NPCGoChaseRoomText("늑대", "Alice")):
				gotChase = true
			default:
				t.Fatalf("unexpected source event=%q", event)
			}
		default:
			t.Fatal("source observer missing chase fan-out")
		}
	}
	if !gotLeave || !gotChase {
		t.Fatalf("source leave=%t chase=%t", gotLeave, gotChase)
	}

	replay, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-connections["observer"].events:
		t.Fatalf("replay fanned out chase: %q", event)
	default:
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 2 || saved.NPCs["wolf"].Body.RoomID != 2 {
		t.Fatalf("saved actor=%+v npc=%+v err=%v", saved.Players["actor"], saved.NPCs, err)
	}
}

func TestWorldConnectorSubmitDispatchesKoreanDirectionalNorthAndEastAndSuppressesReplay(t *testing.T) {
	for _, tc := range []struct {
		line     string
		destID   int16
		destName string
		watcher  string
	}{
		{line: "북", destID: 2, destName: "북쪽방", watcher: "north"},
		{line: "동", destID: 3, destName: "동쪽방", watcher: "east"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
			raw, err := json.Marshal(connectorDirectionalCardinalState())
			if err != nil {
				t.Fatal(err)
			}
			store.connectorCommandStore.state = raw
			connector, err := NewWorldConnector(WorldConnectorConfig{
				Store: store, WorldID: "cardinal-" + tc.line, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "north", "east"})

			output, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || !strings.Contains(output, tc.destName) || store.commits != 1 {
				t.Fatalf("move %q output=%q err=%v commits=%d", tc.line, output, err, store.commits)
			}
			select {
			case event := <-connections["observer"].events:
				if !strings.Contains(event, "떠났습니다") {
					t.Fatalf("source event=%q", event)
				}
			default:
				t.Fatal("source observer missing leave event")
			}
			select {
			case event := <-connections[tc.watcher].events:
				if !strings.Contains(event, "도착했습니다") {
					t.Fatalf("dest event=%q", event)
				}
			default:
				t.Fatal("destination observer missing arrival event")
			}

			replay, err := connections["actor"].Submit(context.Background(), tc.line)
			if err != nil || replay != output {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["observer"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["actor"].Body.RoomID != tc.destID {
				t.Fatalf("saved room=%d err=%v", saved.Players["actor"].Body.RoomID, err)
			}
		})
	}
}

func TestWorldConnectorSubmitLoggedInNorthThenGoChangesRooms(t *testing.T) {
	initial := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "출발지",
					Exits: []world.LegacyExit{
						{Name: "북", Destination: 2},
						{Name: "동굴", Destination: 3},
					},
				}},
				PlayerIDs: []string{"actor"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 2, Name: "북쪽방",
					Exits: []world.LegacyExit{{Name: "남", Destination: 1}},
				}},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
			3: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 3, Name: "동굴방",
					Exits: []world.LegacyExit{{Name: "광장", Destination: 1}},
				}},
				Items: &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "live-move-submit", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"actor"})
	north, err := connections["actor"].Submit(context.Background(), "북")
	if err != nil || !strings.Contains(north, "북쪽방") {
		t.Fatalf("북=%q err=%v", north, err)
	}
	south, err := connections["actor"].Submit(context.Background(), "남")
	if err != nil || !strings.Contains(south, "출발지") {
		t.Fatalf("남=%q err=%v", south, err)
	}
	goLine, err := connections["actor"].Submit(context.Background(), "가 동굴")
	if err != nil || !strings.Contains(goLine, "동굴방") {
		t.Fatalf("가 동굴=%q err=%v", goLine, err)
	}
	stateRaw, commits := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || commits != 3 || saved.Players["actor"].Body.RoomID != 3 {
		t.Fatalf("submit 북/가 did not persist cave commits=%d err=%v player=%+v", commits, err, saved.Players["actor"])
	}
}

func TestWorldConnectorSubmitGoMissingExitDoesNotMove(t *testing.T) {
	store := &connectorCommandStore{}
	raw, err := json.Marshal(connectorGoState())
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "go-missing", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 2)}
	connector.connections[conn] = struct{}{}
	output, err := conn.Submit(context.Background(), "가 없는문")
	if err != nil || output != world.GoMissingResponse {
		t.Fatalf("missing=%q err=%v", output, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 1 || store.commits != 1 {
		t.Fatalf("missing moved actor commits=%d err=%v", store.commits, err)
	}
}
