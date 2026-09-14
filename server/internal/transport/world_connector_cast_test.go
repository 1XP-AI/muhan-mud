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

func TestWorldConnectorSubmitTargetedRecallFailsClosed(t *testing.T) {
	store := connectorRecallStore(t, false)
	_, actor, observer, dest := connectorThreeWorldConnections(t, store, "recall-targeted")
	text, err := actor.Submit(context.Background(), "주문 귀환 Bob")
	if err != nil || text != "아직 구현되지 않은 명령입니다.\r\n" {
		t.Fatalf("targeted recall=%q err=%v", text, err)
	}
	select {
	case event := <-observer.events:
		t.Fatalf("targeted source event=%q", event)
	default:
	}
	select {
	case event := <-dest.events:
		t.Fatalf("targeted dest event=%q", event)
	default:
	}
	if store.commits != 0 {
		t.Fatalf("commits=%d", store.commits)
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
