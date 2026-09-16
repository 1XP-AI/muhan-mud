package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorLocateFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	var spells [16]byte
	spells[46/8] |= 1 << (46 % 8)
	targetRoom := world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "광장"}}
	targetRoom.Flags[1] |= 1 << 1 // RF 9 / RDARKN
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"a", "c"}},
			2: {Resource: targetRoom, PlayerIDs: []string{"b"}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 5, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 30,
					Spells: spells,
				},
				Online: true,
			},
			"b": {
				Body: world.LegacyMonster{
					Name: "Bob", Type: 0, Class: 4, Level: 8, RoomID: 2,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 80, HPCurrent: 80,
				},
				Online: true,
			},
			"c": {Body: world.LegacyMonster{Name: "Carol", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitCastsLocateWithClockHourAndTargetText(t *testing.T) {
	store := connectorLocateFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "locate-world")
	connector.config.Clock = func() (int32, int) { return 100, 12 }
	connector.config.Roll = func(int, int) int { return 1 }

	text, err := actor.Submit(context.Background(), "주문 천리안 Bob")
	if err != nil || !strings.Contains(text, "마음을 Bob에게 집중") || !strings.Contains(text, "광장") || strings.Contains(text, "너무 어두워서") {
		t.Fatalf("locate response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		if !strings.Contains(event, "Alice이 당신의 눈으로 주위를 보고 있습니다") {
			t.Fatalf("target locate event=%q", event)
		}
	default:
		t.Fatal("target locate event missing")
	}
	select {
	case event := <-observer.events:
		if !strings.Contains(event, "Alice이 천리안 주문을 외웠습니다") {
			t.Fatalf("observer locate event=%q", event)
		}
	default:
		t.Fatal("observer locate event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own locate event=%q", event)
	default:
	}
	select {
	case event := <-target.events:
		t.Fatalf("target received extra event=%q", event)
	default:
	}
	if store.commits != 1 {
		t.Fatalf("commits=%d", store.commits)
	}
}

func TestWorldConnectorSubmitLocateNightHourFailsClosedWithoutItems(t *testing.T) {
	store := connectorLocateFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "locate-night")
	connector.config.Clock = func() (int32, int) { return 100, 0 }
	connector.config.Roll = func(int, int) int { return 1 }

	text, err := actor.Submit(context.Background(), "주문 천리안 Bob")
	if err != nil || text != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("night locate response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		t.Fatalf("night target event=%q", event)
	default:
	}
	select {
	case event := <-observer.events:
		t.Fatalf("night observer event=%q", event)
	default:
	}
	if store.commits != 0 {
		t.Fatalf("commits=%d", store.commits)
	}
}

func connectorKnowAlignmentFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	var spells [16]byte
	spells[41/8] |= 1 << uint(41%8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a", "b", "c"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: world.ClericClass, Level: 8, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18}, MPMax: 50, MPCurrent: 30, Spells: spells,
			}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Bobby", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitKnowAlignmentFansOutTargetAndSuppressesReplay(t *testing.T) {
	baseStore := connectorKnowAlignmentFixture(t)
	store := &familyMutationReplayStore{connectorCommandStore: baseStore}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "know-alignment-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
	actor, target, observer := connections["a"], connections["b"], connections["c"]
	connector.config.Clock = func() (int32, int) { return 100, 12 }
	connector.config.Roll = func(int, int) int {
		t.Fatal("know-alignment target consumed RNG")
		return 0
	}

	output, err := actor.Submit(context.Background(), "주문 선악 Bob 2")
	if err != nil || output != "당신은 Bobby에게 선악감지 주문을 외웁니다.\r\n" || store.commits != 1 {
		t.Fatalf("know-alignment output=%q err=%v commits=%d", output, err, store.commits)
	}
	roomWant := "\nAlice이 Bobby에게 선악감지 주문을 외웁니다.\r\n그는 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n"
	targetWant := "\nAlice이 당신에게 선악감지 주문을 외웁니다.\r\n당신은 선악을 감지할 수 있는 식별력이 높아졌습니다.\r\n"
	select {
	case event := <-target.events:
		if event != roomWant {
			t.Fatalf("target room event=%q want=%q", event, roomWant)
		}
	default:
		t.Fatal("target room event missing")
	}
	select {
	case event := <-observer.events:
		if event != roomWant {
			t.Fatalf("target room event=%q want=%q", event, roomWant)
		}
	default:
		t.Fatal("target room event missing")
	}
	select {
	case event := <-observer.events:
		if event != targetWant {
			t.Fatalf("target private event=%q want=%q", event, targetWant)
		}
	default:
		t.Fatalf("target private event missing (queued=%d)", len(observer.events))
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own event=%q", event)
	default:
	}

	replay, err := actor.Submit(context.Background(), "주문 선악 Bob 2")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	select {
	case event := <-target.events:
		t.Fatalf("target replay event=%q", event)
	default:
	}
	select {
	case event := <-observer.events:
		t.Fatalf("observer replay event=%q", event)
	default:
	}
}

func connectorRecallState(male bool) world.State {
	var spells [16]byte
	spells[16/8] |= 1 << (16 % 8)
	var flags [8]byte
	if male {
		flags[12/8] |= 1 << (12 % 8)
	}
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"a", "b"}},
			1001: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001, Name: "광장"}}, PlayerIDs: []string{"c"}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 3, Level: 8, RoomID: 1, Flags: flags,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 80, MPCurrent: 40,
					Spells: spells,
				},
				Online: true,
			},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Carol", Type: 0, Class: 4, Level: 4, RoomID: 1001}, Online: true},
		},
	}
}

func connectorTargetedRecallState(targetFlags [8]byte) world.State {
	state := connectorRecallState(false)
	actor := state.Players["a"]
	actor.Body.RoomID = 2
	state.Players["a"] = actor
	target := state.Players["b"]
	target.Body.RoomID = 2
	target.Body.Flags = targetFlags
	state.Players["b"] = target
	observer := state.Players["c"]
	observer.Body.RoomID = 2
	state.Players["c"] = observer
	state.Players["d"] = world.PlayerState{
		Body:   world.LegacyMonster{Name: "Dora", Type: 0, Class: 4, Level: 4, RoomID: 1},
		Online: true,
	}
	state.Rooms[1] = world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"d"}}
	state.Rooms[2] = world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "마을"}}, PlayerIDs: []string{"a", "b", "c"}}
	state.Rooms[1001] = world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001, Name: "광장"}}}
	return state
}

func TestWorldConnectorSubmitRecallFansOutSourceBroadcastAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorRecallState(false))
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-fanout", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀환")
	if err != nil || !strings.HasPrefix(output, "귀환 주문을 외웠습니다.\r\n") || !strings.Contains(output, "광장") || store.commits != 1 {
		t.Fatalf("recall=%q err=%v commits=%d", output, err, store.commits)
	}
	want := "\nAlice이 그녀 자신에게 귀환 주문을 외웠습니다.\r\n"
	select {
	case event := <-connections["b"].events:
		if event != want {
			t.Fatalf("source observer event=%q want=%q", event, want)
		}
	default:
		t.Fatal("source observer missing recall broadcast")
	}
	select {
	case event := <-connections["a"].events:
		t.Fatalf("caster received own recall event=%q", event)
	default:
	}
	destWant := world.RecallDestArrivalText("Alice")
	select {
	case event := <-connections["c"].events:
		if event != destWant {
			t.Fatalf("dest observer event=%q want=%q", event, destWant)
		}
	default:
		t.Fatal("dest observer missing add_ply_rom arrival")
	}

	replay, err := connections["a"].Submit(context.Background(), "주문 귀환")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-connections["b"].events:
		t.Fatalf("replay fanned out to source: %q", event)
	default:
	}
	select {
	case event := <-connections["c"].events:
		t.Fatalf("replay fanned out to dest: %q", event)
	default:
	}

	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1001 || saved.Players["a"].Body.MPCurrent != 10 {
		t.Fatalf("saved actor=%+v err=%v", saved.Players["a"], err)
	}
	if containsStringIDs(saved.Rooms[1].PlayerIDs, "a") || !containsStringIDs(saved.Rooms[1001].PlayerIDs, "a") {
		t.Fatalf("occupancy source=%v dest=%v", saved.Rooms[1].PlayerIDs, saved.Rooms[1001].PlayerIDs)
	}
}

func TestWorldConnectorSubmitRecallMaleSourceBroadcast(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorRecallState(true))
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-male", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀")
	if err != nil || !strings.HasPrefix(output, "귀환 주문을 외웠습니다.\r\n") || store.commits != 1 {
		t.Fatalf("recall=%q err=%v commits=%d", output, err, store.commits)
	}
	want := "\nAlice이 그 자신에게 귀환 주문을 외웠습니다.\r\n"
	select {
	case event := <-connections["b"].events:
		if event != want {
			t.Fatalf("source observer event=%q want=%q", event, want)
		}
	default:
		t.Fatal("source observer missing male recall broadcast")
	}
	select {
	case event := <-connections["a"].events:
		t.Fatalf("caster received own recall event=%q", event)
	default:
	}
	destWant := world.RecallDestArrivalText("Alice")
	select {
	case event := <-connections["c"].events:
		if event != destWant {
			t.Fatalf("dest observer event=%q want=%q", event, destWant)
		}
	default:
		t.Fatal("dest observer missing male add_ply_rom arrival")
	}
}

func TestWorldConnectorSubmitTargetedRecallMovesTargetAndSuppressesReplay(t *testing.T) {
	state := connectorTargetedRecallState([8]byte{})
	target := state.Players["b"]
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-targeted", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c", "d"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob")
	if err != nil || output != "귀환 주문을 Bob에게 외웠습니다.\r\n" || store.commits != 1 {
		t.Fatalf("targeted recall=%q err=%v commits=%d", output, err, store.commits)
	}
	assertConnectorEvent(t, connections["b"], "Alice이 당신에게 귀환 주문을 외웠습니다.\r\n")
	assertConnectorEvent(t, connections["c"], "Alice이 Bob에게 귀환 주문을 외웠습니다.")
	assertConnectorEvent(t, connections["d"], world.RecallDestArrivalText("Bob"))
	assertConnectorNoEvent(t, connections["a"])
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.RoomID != 2 || saved.Players["b"].Body.RoomID != 1 || saved.Players["b"].Body.HPCurrent != target.Body.HPCurrent || saved.Rooms[2].Resource.BeenHere != 0 || !equalStringIDs(saved.Rooms[2].PlayerIDs, []string{"a", "c"}) || !equalStringIDs(saved.Rooms[1].PlayerIDs, []string{"b", "d"}) || saved.Rooms[1].Resource.BeenHere != 1 {
		t.Fatalf("targeted occupancy actor=%+v target=%+v source=%v dest=%v", saved.Players["a"].Body, saved.Players["b"].Body, saved.Rooms[2].PlayerIDs, saved.Rooms[1].PlayerIDs)
	}

	replay, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob")
	if err != nil || replay != output {
		t.Fatalf("targeted replay=%q err=%v", replay, err)
	}
	if store.commits != 1 {
		t.Fatalf("replay committed again: commits=%d", store.commits)
	}
	assertConnectorNoEvent(t, connections["a"])
	assertConnectorNoEvent(t, connections["b"])
	assertConnectorNoEvent(t, connections["c"])
	assertConnectorNoEvent(t, connections["d"])
}

func TestWorldConnectorSubmitTargetedRecallOccurrenceSelectsSecondAndReplays(t *testing.T) {
	state := connectorTargetedRecallState([8]byte{})
	state.Players["e"] = world.PlayerState{
		Body:   world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 2, HPMax: 80, HPCurrent: 61},
		Online: true,
	}
	room := state.Rooms[2]
	room.PlayerIDs = []string{"a", "b", "e", "c"}
	state.Rooms[2] = room
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-targeted-occurrence", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c", "d", "e"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob 2")
	if err != nil || output != "귀환 주문을 Bob에게 외웠습니다.\r\n" || store.commits != 1 {
		t.Fatalf("targeted occurrence=%q err=%v commits=%d", output, err, store.commits)
	}
	assertConnectorEvent(t, connections["e"], "Alice이 당신에게 귀환 주문을 외웠습니다.\r\n")
	assertConnectorEvent(t, connections["b"], "Alice이 Bob에게 귀환 주문을 외웠습니다.")
	assertConnectorEvent(t, connections["c"], "Alice이 Bob에게 귀환 주문을 외웠습니다.")
	assertConnectorEvent(t, connections["d"], world.RecallDestArrivalText("Bob"))
	assertConnectorNoEvent(t, connections["a"])
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["b"].Body.RoomID != 2 || saved.Players["e"].Body.RoomID != 1 || saved.Players["e"].Body.HPCurrent != 61 || !equalStringIDs(saved.Rooms[2].PlayerIDs, []string{"a", "b", "c"}) || !equalStringIDs(saved.Rooms[1].PlayerIDs, []string{"e", "d"}) {
		t.Fatalf("targeted occurrence occupancy b=%+v e=%+v source=%v dest=%v", saved.Players["b"].Body, saved.Players["e"].Body, saved.Rooms[2].PlayerIDs, saved.Rooms[1].PlayerIDs)
	}

	replay, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob 2")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("targeted occurrence replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		assertConnectorNoEvent(t, connections[id])
	}
}

func TestWorldConnectorSubmitTargetedRecallOccurrenceOutOfRangeIsReceiptNoOp(t *testing.T) {
	state := connectorTargetedRecallState([8]byte{})
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-targeted-occurrence-missing", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c", "d"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob 2")
	if err != nil || output != "그런 사람이 존재하지 않습니다.\r\n" || store.commits != 1 {
		t.Fatalf("out-of-range recall=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.RoomID != 2 || saved.Players["a"].Body.MPCurrent != 40 || saved.Players["b"].Body.RoomID != 2 || !equalStringIDs(saved.Rooms[2].PlayerIDs, []string{"a", "b", "c"}) || !equalStringIDs(saved.Rooms[1].PlayerIDs, []string{"d"}) || saved.Rooms[1].Resource.BeenHere != 0 {
		t.Fatalf("out-of-range mutated actor=%+v target=%+v source=%v dest=%v", saved.Players["a"].Body, saved.Players["b"].Body, saved.Rooms[2].PlayerIDs, saved.Rooms[1].PlayerIDs)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		assertConnectorNoEvent(t, connections[id])
	}

	replay, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob 2")
	if err != nil || replay != output || store.commits != 1 {
		t.Fatalf("out-of-range replay=%q err=%v commits=%d", replay, err, store.commits)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		assertConnectorNoEvent(t, connections[id])
	}
}

func TestWorldConnectorSubmitTargetedRecallInvalidOccurrenceHasNoReceipt(t *testing.T) {
	for _, line := range []string{"주문 귀환 Bob 0", "주문 귀환 Bob -1", "주문 귀환 Bob nope", "주문 귀환 Bob 2147483648", "주문 귀환 2", "주문 귀환 Bob 2 extra"} {
		t.Run(line, func(t *testing.T) {
			store := connectorRecallStore(t, false)
			_, actor, observer, destination := connectorThreeWorldConnections(t, store, "recall-invalid-occurrence")
			text, err := actor.Submit(context.Background(), line)
			if err != nil || text != "아직 구현되지 않은 명령입니다.\r\n" {
				t.Fatalf("invalid occurrence line=%q text=%q err=%v", line, text, err)
			}
			select {
			case event := <-observer.events:
				t.Fatalf("invalid occurrence source event=%q", event)
			default:
			}
			select {
			case event := <-destination.events:
				t.Fatalf("invalid occurrence destination event=%q", event)
			default:
			}
			if store.commits != 0 {
				t.Fatalf("invalid occurrence committed: %d", store.commits)
			}
		})
	}
}

func TestWorldConnectorSubmitTargetedRecallSuppressesInvisibleDestinationArrival(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag int
	}{
		{name: "hidden", flag: 1},
		{name: "dm-invisible", flag: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var targetFlags [8]byte
			targetFlags[tc.flag/8] |= 1 << (tc.flag % 8)
			state := connectorTargetedRecallState(targetFlags)
			raw, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
			connector, err := NewWorldConnector(WorldConnectorConfig{
				Store: store, WorldID: "recall-targeted-" + tc.name, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c", "d"})

			output, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob")
			if err != nil || output != "귀환 주문을 Bob에게 외웠습니다.\r\n" || store.commits != 1 {
				t.Fatalf("targeted %s=%q err=%v commits=%d", tc.name, output, err, store.commits)
			}
			assertConnectorEvent(t, connections["b"], "Alice이 당신에게 귀환 주문을 외웠습니다.\r\n")
			assertConnectorEvent(t, connections["c"], "Alice이 Bob에게 귀환 주문을 외웠습니다.")
			assertConnectorNoEvent(t, connections["d"])
			assertConnectorNoEvent(t, connections["a"])

			replay, err := connections["a"].Submit(context.Background(), "주문 귀환 Bob")
			if err != nil || replay != output || store.commits != 1 {
				t.Fatalf("%s replay=%q err=%v commits=%d", tc.name, replay, err, store.commits)
			}
			assertConnectorNoEvent(t, connections["a"])
			assertConnectorNoEvent(t, connections["b"])
			assertConnectorNoEvent(t, connections["c"])
			assertConnectorNoEvent(t, connections["d"])
		})
	}
}

func TestWorldConnectorSubmitRecallActivatesEmptyDestMonstersAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	state := connectorRecallState(false)
	delete(state.Players, "c")
	dest := state.Rooms[1001]
	dest.PlayerIDs = nil
	dest.NPCIDs = []string{"guard", "bear"}
	state.Rooms[1001] = dest
	state.NPCs = map[string]world.NPCState{
		"guard": {Body: world.LegacyMonster{Name: "경비", Type: 1, RoomID: 1001}},
		"bear":  {Body: world.LegacyMonster{Name: "곰", Type: 1, RoomID: 1001}},
	}
	state.ActiveNPCIDs = []string{}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-dest-active", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b"})

	output, err := connections["a"].Submit(context.Background(), "주문 귀환")
	if err != nil || !strings.HasPrefix(output, "귀환 주문을 외웠습니다.\r\n") || store.commits != 1 {
		t.Fatalf("recall=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections["b"].events:
		if event != "\nAlice이 그녀 자신에게 귀환 주문을 외웠습니다.\r\n" {
			t.Fatalf("source observer event=%q", event)
		}
	default:
		t.Fatal("source observer missing recall broadcast")
	}
	select {
	case event := <-connections["a"].events:
		t.Fatalf("caster received dest event=%q", event)
	default:
	}
	saved, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || len(saved.ActiveNPCIDs) != 2 || saved.ActiveNPCIDs[0] != "bear" || saved.ActiveNPCIDs[1] != "guard" {
		t.Fatalf("dest add_active=%v err=%v", saved.ActiveNPCIDs, err)
	}

	replay, err := connections["a"].Submit(context.Background(), "주문 귀환")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	select {
	case event := <-connections["b"].events:
		t.Fatalf("replay fanned out to source: %q", event)
	default:
	}
	again, err := world.DecodeState(store.connectorCommandStore.state)
	if err != nil || !equalStringIDs(again.ActiveNPCIDs, saved.ActiveNPCIDs) {
		t.Fatalf("replay re-activated dest: first=%v again=%v err=%v", saved.ActiveNPCIDs, again.ActiveNPCIDs, err)
	}
}

func TestWorldConnectorSubmitRecallDarkDestFailsClosed(t *testing.T) {
	store := connectorRecallStore(t, false)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	dest := state.Rooms[1001]
	dest.Resource.Flags[1] |= 1 << 0
	state.Rooms[1001] = dest
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	_, actor, observer, destConn := connectorThreeWorldConnections(t, store, "recall-dark-dest")
	text, err := actor.Submit(context.Background(), "주문 귀환")
	if err != nil || text != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("dark dest recall=%q err=%v", text, err)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("dark source event=%q", event)
	default:
	}
	select {
	case event := <-destConn.events:
		t.Fatalf("dark dest event=%q", event)
	default:
	}
	if store.commits != 0 {
		t.Fatalf("commits=%d", store.commits)
	}
}

func TestWorldConnectorSubmitRecallUnmigratedDestNPCFailsClosed(t *testing.T) {
	store := connectorRecallStore(t, false)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	delete(state.Players, "c")
	dest := state.Rooms[1001]
	dest.PlayerIDs = nil
	dest.NPCIDs = []string{"guard"}
	state.Rooms[1001] = dest
	state.NPCs = map[string]world.NPCState{"guard": {Body: world.LegacyMonster{Name: "경비", Type: 1, RoomID: 1001}}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "recall-unmigrated-dest", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := admitConnectorPlayers(t, connector, []string{"a", "b"})
	text, err := connections["a"].Submit(context.Background(), "주문 귀환")
	if err != nil || text != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("unmigrated dest=%q err=%v", text, err)
	}
	select {
	case event := <-connections["b"].events:
		t.Fatalf("unmigrated dest source event=%q", event)
	default:
	}
	if store.commits != 0 {
		t.Fatalf("commits=%d", store.commits)
	}
}

func connectorRecallStore(t *testing.T, male bool) *connectorCommandStore {
	t.Helper()
	raw, err := json.Marshal(connectorRecallState(male))
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func containsStringIDs(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func equalStringIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func assertConnectorEvent(t *testing.T, connection *worldConnection, want string) {
	t.Helper()
	select {
	case event := <-connection.events:
		if event != want {
			t.Fatalf("%s event=%q want=%q", connection.lease.ActorID, event, want)
		}
	default:
		t.Fatalf("%s event missing", connection.lease.ActorID)
	}
	assertConnectorNoEvent(t, connection)
}

func assertConnectorNoEvent(t *testing.T, connection *worldConnection) {
	t.Helper()
	select {
	case event := <-connection.events:
		t.Fatalf("%s received unexpected event=%q", connection.lease.ActorID, event)
	default:
	}
}
