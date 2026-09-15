package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorExpressionLookFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func connectorThreeWorldConnections(t *testing.T, store *connectorCommandStore, worldID string) (*WorldConnector, *worldConnection, *worldConnection, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: worldID, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 3)
	for _, id := range []string{"a", "b", "c"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		connections = append(connections, &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	return connector, connections[0], connections[1], connections[2]
}

func TestWorldConnectorSubmitDispatchesExpressAndFanOutsCommittedRoomEvent(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "express-world")
	text, err := actor.Submit(context.Background(), "표현 hello")
	if err != nil || text != "예. 좋습니다.\r\n" {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if event != "\n:Alice님이 hello.\r\n" {
				t.Fatalf("%s event=%q", name, event)
			}
		default:
			t.Fatalf("%s expression event missing", name)
		}
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own expression event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesLookAtTargetWithRecipientProjection(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-at-world")
	text, err := actor.Submit(context.Background(), "보아 Bob")
	if err != nil || !strings.Contains(text, "Bob님을 봅니다") {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		if event != "\nAlice님이 당신을 봅니다.\r\n" {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target look-at event missing")
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice님이 Bob님을 봅니다.\r\n" {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("observer look-at event missing")
	}
	select {
	case event := <-actor.events:
		t.Fatalf("actor received own look-at event=%q", event)
	default:
	}
}

func TestWorldConnectorSubmitDispatchesLookAtTargetPrefixOccurrenceWithRecipientProjection(t *testing.T) {
	store := connectorExpressionLookFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-at-occurrence-world")
	text, err := actor.Submit(context.Background(), "보아 B 1")
	if err != nil || !strings.Contains(text, "Bob님을 봅니다") {
		t.Fatalf("actor response=%q err=%v", text, err)
	}
	select {
	case event := <-target.events:
		if event != "\nAlice님이 당신을 봅니다.\r\n" {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target occurrence look-at event missing")
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice님이 Bob님을 봅니다.\r\n" {
			t.Fatalf("observer event=%q", event)
		}
	default:
		t.Fatal("observer occurrence look-at event missing")
	}
}

func TestWorldConnectorSubmitDispatchesLookThroughExitWithoutMoving(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "광장",
					Exits: []world.LegacyExit{{Name: "동", Destination: 2}},
				}},
				PlayerIDs: []string{"a"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "동쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-exit-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()

	text, err := actor.Submit(context.Background(), "봐 동")
	if err != nil || !strings.Contains(text, "동쪽방") || strings.Contains(text, "== 광장 ==") {
		t.Fatalf("look-through=%q err=%v", text, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("look-through moved actor: %+v err=%v", saved.Players["a"], err)
	}
	unsupported, err := actor.Submit(context.Background(), "봐 늑대")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") {
		t.Fatalf("unmigrated look=%q err=%v", unsupported, err)
	}
}

func TestWorldConnectorSubmitLookIncludesDisplayRomCombatNotice(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				NPCIDs:    []string{"npc-wolf"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-combat-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()

	text, err := actor.Submit(context.Background(), "봐")
	if err != nil || !strings.Contains(text, "늑대가 당신과 싸우고 있습니다.") || store.commits != 1 {
		t.Fatalf("look combat=%q err=%v commits=%d", text, err, store.commits)
	}

	unmigrated := state
	wolf := unmigrated.NPCs["npc-wolf"]
	wolf.Enemies = nil
	unmigrated.NPCs["npc-wolf"] = wolf
	raw, err = json.Marshal(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	closed := &connectorCommandStore{state: raw}
	closedConnector, err := NewWorldConnector(WorldConnectorConfig{Store: closed, WorldID: "look-combat-nil", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	closedLease, err := closedConnector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := closedConnector.owners.Admit(closedLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	closedActor := &worldConnection{game: closedConnector, lease: closedLease, ready: true, events: make(chan string, 8)}
	closedConnector.mu.Lock()
	closedConnector.connections[closedActor] = struct{}{}
	closedConnector.mu.Unlock()
	unsupported, err := closedActor.Submit(context.Background(), "봐")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || closed.commits != 0 {
		t.Fatalf("unmigrated combat=%q err=%v commits=%d", unsupported, err, closed.commits)
	}
}

func lookInspectBroadcastWant(line string) string {
	switch {
	case strings.Contains(line, "나"):
		return "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"
	case strings.Contains(line, "늑대"):
		return "\nAlice님이 늑대를 봅니다.\r\n"
	case strings.Contains(line, "Bob"):
		return "\nAlice님이 Bob님을 봅니다.\r\n"
	default:
		return ""
	}
}

func expectLookInspectFanOut(t *testing.T, connections map[string]*worldConnection, line string) {
	t.Helper()
	want := lookInspectBroadcastWant(line)
	if want == "" {
		t.Fatalf("unknown look inspect line %q", line)
	}
	for _, id := range []string{"b", "c"} {
		select {
		case event := <-connections[id].events:
			if event != want {
				t.Fatalf("%s event=%q want %q", id, event, want)
			}
		default:
			t.Fatalf("%s look inspect event missing", id)
		}
	}
	select {
	case event := <-connections["a"].events:
		t.Fatalf("actor received own look inspect event=%q", event)
	default:
	}
}

func connectorLookInspectFixture(t *testing.T) *connectorCommandStore {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "광장",
					Exits: []world.LegacyExit{{Name: "동", Destination: 2}},
				}},
				PlayerIDs: []string{"a", "b", "c"},
				NPCIDs:    []string{"npc-wolf"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "동쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Enemies: []world.NPCEnemy{}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &connectorCommandStore{state: raw}
}

func TestWorldConnectorSubmitLookInspectFansOutCRoomBroadcasts(t *testing.T) {
	for _, tt := range []struct {
		line, actor, room string
	}{
		{"봐 나", "당신은 거울을 들고 자신을 봅니다.", "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"},
		{"봐 늑대", "당신은 늑대를 봅니다.", "\nAlice님이 늑대를 봅니다.\r\n"},
		{"봐 Bob", "당신은 Bob님을 봅니다.", "\nAlice님이 Bob님을 봅니다.\r\n"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := connectorLookInspectFixture(t)
			_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-inspect-"+tt.line)
			text, err := actor.Submit(context.Background(), tt.line)
			if err != nil || !strings.Contains(text, tt.actor) || strings.Contains(text, "바라 봅니다") {
				t.Fatalf("actor response=%q err=%v", text, err)
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d", store.commits)
			}
			for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
				select {
				case event := <-connection.events:
					if event != tt.room {
						t.Fatalf("%s event=%q want %q", name, event, tt.room)
					}
				default:
					t.Fatalf("%s look inspect event missing", name)
				}
			}
			select {
			case event := <-actor.events:
				t.Fatalf("actor received own look inspect event=%q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookInspectReplayDoesNotRefanOut(t *testing.T) {
	store := connectorLookInspectFixture(t)
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "look-inspect-replay")
	first, err := connector.owners.ExecuteLookLine(context.Background(), store, "look-inspect-replay", "look-self-1", actor.lease, "봐 나", 12)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, "자신을 봅니다") {
		t.Fatalf("actor text=%q err=%v", text, unmarshalErr)
	}
	if after, ok := connector.snapshot(context.Background()); ok {
		connector.publishLookInspect(after, actor.lease.ActorID, "나", 1, 12)
	}
	want := "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			if event != want {
				t.Fatalf("%s event=%q", name, event)
			}
		default:
			t.Fatalf("%s first inspect event missing", name)
		}
	}
	replay, err := connector.owners.ExecuteLookLine(context.Background(), store, "look-inspect-replay", "look-self-1", actor.lease, "봐 나", 12)
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !replay.Replayed {
		if after, ok := connector.snapshot(context.Background()); ok {
			connector.publishLookInspect(after, actor.lease.ActorID, "나", 1, 12)
		}
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer, "actor": actor} {
		select {
		case event := <-connection.events:
			t.Fatalf("%s replay event=%q", name, event)
		default:
		}
	}
}

func TestWorldConnectorSubmitLookInspectPINVISFansOutPerViewerNames(t *testing.T) {
	store := connectorLookInspectFixture(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	alice := state.Players["a"]
	alice.Body.Flags[2/8] |= 1 << (2 % 8) // PINVIS
	state.Players["a"] = alice
	carol := state.Players["c"]
	carol.Body.Flags[21/8] |= 1 << (21 % 8) // PDINVI
	state.Players["c"] = carol
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	for _, tt := range []struct {
		line, actor, hidden, detected string
	}{
		{"봐 나", "당신은 거울을 들고 자신을 봅니다.", "\n누군가가 거울을 들고 자신을 바라 봅니다.\r\n", "\nAlice(*)님이 거울을 들고 자신을 바라 봅니다.\r\n"},
		{"봐 늑대", "당신은 늑대를 봅니다.", "\n누군가가 늑대를 봅니다.\r\n", "\nAlice(*)님이 늑대를 봅니다.\r\n"},
		{"봐 Bob", "당신은 Bob님을 봅니다.", "\n누군가가 Bob님을 봅니다.\r\n", "\nAlice(*)님이 Bob님을 봅니다.\r\n"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			runStore := &connectorCommandStore{state: append([]byte(nil), store.state...)}
			_, actor, target, observer := connectorThreeWorldConnections(t, runStore, "look-inspect-pinvis-"+tt.line)
			text, submitErr := actor.Submit(context.Background(), tt.line)
			if submitErr != nil || !strings.Contains(text, tt.actor) || strings.Contains(text, "바라 봅니다") {
				t.Fatalf("actor response=%q err=%v", text, submitErr)
			}
			if runStore.commits != 1 {
				t.Fatalf("commits=%d", runStore.commits)
			}
			select {
			case event := <-target.events:
				if event != tt.hidden || strings.Contains(event, "Alice") {
					t.Fatalf("bob event=%q want %q", event, tt.hidden)
				}
			default:
				t.Fatal("bob look inspect event missing")
			}
			select {
			case event := <-observer.events:
				if event != tt.detected {
					t.Fatalf("carol event=%q want %q", event, tt.detected)
				}
			default:
				t.Fatal("carol look inspect event missing")
			}
			select {
			case event := <-actor.events:
				t.Fatalf("actor received own look inspect event=%q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookInspectPINVISReplayDoesNotRefanOut(t *testing.T) {
	store := connectorLookInspectFixture(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	alice := state.Players["a"]
	alice.Body.Flags[2/8] |= 1 << (2 % 8) // PINVIS
	state.Players["a"] = alice
	carol := state.Players["c"]
	carol.Body.Flags[21/8] |= 1 << (21 % 8) // PDINVI
	state.Players["c"] = carol
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	connector, actor, target, observer := connectorThreeWorldConnections(t, store, "look-inspect-pinvis-replay")
	first, err := connector.owners.ExecuteLookLine(context.Background(), store, "look-inspect-pinvis-replay", "look-self-pinvis-1", actor.lease, "봐 나", 12)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	if after, ok := connector.snapshot(context.Background()); ok {
		connector.publishLookInspect(after, actor.lease.ActorID, "나", 1, 12)
	}
	select {
	case event := <-target.events:
		if event != "\n누군가가 거울을 들고 자신을 바라 봅니다.\r\n" {
			t.Fatalf("bob event=%q", event)
		}
	default:
		t.Fatal("bob first inspect event missing")
	}
	select {
	case event := <-observer.events:
		if event != "\nAlice(*)님이 거울을 들고 자신을 바라 봅니다.\r\n" {
			t.Fatalf("carol event=%q", event)
		}
	default:
		t.Fatal("carol first inspect event missing")
	}
	replay, err := connector.owners.ExecuteLookLine(context.Background(), store, "look-inspect-pinvis-replay", "look-self-pinvis-1", actor.lease, "봐 나", 12)
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if !replay.Replayed {
		if after, ok := connector.snapshot(context.Background()); ok {
			connector.publishLookInspect(after, actor.lease.ActorID, "나", 1, 12)
		}
	}
	for name, connection := range map[string]*worldConnection{"target": target, "observer": observer, "actor": actor} {
		select {
		case event := <-connection.events:
			t.Fatalf("%s replay event=%q", name, event)
		default:
		}
	}
}

func TestWorldConnectorSubmitLookInspectFailsClosedForUnmigratedOccupant(t *testing.T) {
	store := connectorLookInspectFixture(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	carol := state.Players["c"]
	carol.Body.Name = "Carol "
	state.Players["c"] = carol
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.state = raw
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-inspect-ghost")
	text, err := actor.Submit(context.Background(), "봐 나")
	if err != nil || !strings.Contains(text, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("unmigrated occupant=%q err=%v commits=%d", text, err, store.commits)
	}
	for name, connection := range map[string]*worldConnection{"actor": actor, "target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			t.Fatalf("%s event=%q", name, event)
		default:
		}
	}
}

func TestWorldConnectorSubmitLookPeekAndBareDoNotFanOutInspectBroadcast(t *testing.T) {
	store := connectorLookInspectFixture(t)
	_, actor, target, observer := connectorThreeWorldConnections(t, store, "look-inspect-none")
	text, err := actor.Submit(context.Background(), "봐 동")
	if err != nil || !strings.Contains(text, "동쪽방") {
		t.Fatalf("peek=%q err=%v", text, err)
	}
	bare, err := actor.Submit(context.Background(), "봐")
	if err != nil || !strings.Contains(bare, "광장") {
		t.Fatalf("bare=%q err=%v", bare, err)
	}
	for name, connection := range map[string]*worldConnection{"actor": actor, "target": target, "observer": observer} {
		select {
		case event := <-connection.events:
			t.Fatalf("%s unexpected look event=%q", name, event)
		default:
		}
	}
}

func TestWorldConnectorSubmitLastTokenLookWithExtraTokensDoesNotMove(t *testing.T) {
	for _, tt := range []struct {
		line     string
		want     string
		watcher  string
		destID   int16
		destName string
	}{
		{"동 junk 봐", "동쪽방", "east", 3, "동쪽방"},
		{"북 foo 보다", "북쪽방", "north", 2, "북쪽방"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			raw, err := json.Marshal(connectorDirectionalCardinalState())
			if err != nil {
				t.Fatal(err)
			}
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
			connector, err := NewWorldConnector(WorldConnectorConfig{
				Store: store, WorldID: "look-extra-" + tt.line, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 4,
			})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"actor", "observer", "north", "east"})

			text, err := connections["actor"].Submit(context.Background(), tt.line)
			if err != nil || !strings.Contains(text, tt.want) || strings.Contains(text, "== 서편 ==") {
				t.Fatalf("look extra=%q err=%v", text, err)
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			select {
			case event := <-connections["observer"].events:
				t.Fatalf("source observer fanned out: %q", event)
			default:
			}
			select {
			case event := <-connections[tt.watcher].events:
				t.Fatalf("destination observer fanned out: %q", event)
			default:
			}
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["actor"].Body.RoomID != 1 {
				t.Fatalf("extra-token look moved actor: room=%d err=%v", saved.Players["actor"].Body.RoomID, err)
			}

			replay, err := connections["actor"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
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
			saved, err = world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["actor"].Body.RoomID != 1 || saved.Players["actor"].Body.RoomID == tt.destID {
				t.Fatalf("replay moved actor to %s: %+v err=%v", tt.destName, saved.Players["actor"], err)
			}
		})
	}
}

func TestWorldConnectorSubmitLookSelfAndFirstPlyWithoutMoving(t *testing.T) {
	for _, tt := range []struct {
		line, want, id string
	}{
		{"봐 나", world.LookSelfMirrorResponse, "look-self"},
		{"나 봐", world.LookSelfMirrorResponse, "look-self-suffix"},
		{"봐 Bob", "당신은 Bob님을 봅니다.", "look-bob"},
		{"Bob 봐", "당신은 Bob님을 봅니다.", "look-bob-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			raw, err := json.Marshal(world.State{
				Version: 1,
				Rooms: map[int16]world.RoomState{
					1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
				},
				Players: map[string]world.PlayerState{
					"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
					"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
					"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-ply-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || !strings.Contains(text, strings.TrimRight(tt.want, "\r\n")) || strings.Contains(text, "== ") {
				t.Fatalf("look=%q err=%v want %q", text, err, tt.want)
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("self/first_ply look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookAppendsConsiderAndEquipListWithoutMoving(t *testing.T) {
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, NPCIDs: []string{"npc-wolf"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100},
				Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
				Enemies: []world.NPCEnemy{},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-consider", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-consider", []string{"당신은 늑대를 봅니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-equip", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"꼭 맞는", "한방에"}},
		{"Bob 봐", "look-bob-equip-suffix", []string{"그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"꼭 맞는"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-equip-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("consider/equip look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookAppendsHPBandsWithoutMoving(t *testing.T) {
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, NPCIDs: []string{"npc-wolf"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, Level: 4, HPCurrent: 89, HPMax: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPCurrent: 10, HPMax: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPCurrent: 50, HPMax: 100},
				Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
				Enemies: []world.NPCEnemy{},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-hp", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-hp", []string{"당신은 늑대를 봅니다.", "그녀는 많은 상처를 입었습니다.", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-hp-suffix", []string{"그녀는 많은 상처를 입었습니다."}, nil},
		{"봐 Bob", "look-bob-hp", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "[  몸  ]  갑옷"}, []string{"여러군데", "많은 상처", "심각한", "죽기 직전"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-hp-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("hp-band look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookUnmigratedHPMaxFailsClosed(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPCurrent: 10, HPMax: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-hpmax-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 나")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("unmigrated hpmax=%q err=%v commits=%d", unsupported, err, store.commits)
	}

	state = world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPCurrent: 10, HPMax: 0}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store = &connectorCommandStore{state: raw}
	connector, err = NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-ply-hpmax-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err = connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor = &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err = actor.Submit(context.Background(), "봐 Bob")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("unmigrated first_ply hpmax=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookUnmigratedPlayerFailsClosed(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob ", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-ghost-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 Bob")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("unmigrated first_ply=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookAppendsFirstPlyMarriageWithoutMoving(t *testing.T) {
	bobFlags := [8]byte{}
	bobFlags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	bobFlags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	var bobKeys [3]string
	bobKeys[world.MarriageSpouseKeyIndex] = world.MarriageSpouseKeyPrefix + "Carol"
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPCurrent: 100, HPMax: 100, Flags: bobFlags, Keys: bobKeys}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 Bob", "look-bob-marriage"},
		{"Bob 봐", "look-bob-marriage-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-marry-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range []string{"당신은 Bob님을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "[  몸  ]  갑옷"} {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range []string{"상처", "죽기 직전", "꼭 맞는", "광채"} {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("marriage look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookUnmigratedFirstPlySpouseFailsClosed(t *testing.T) {
	var bobFlags [8]byte
	bobFlags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, Flags: bobFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-married-ghost-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 Bob")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("unmigrated first_ply spouse=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookAppendsPKNOWAGlowWithoutMoving(t *testing.T) {
	var actorFlags, wolfFlags [8]byte
	actorFlags[33/8] |= 1 << (33 % 8)
	wolfFlags[12/8] |= 1 << (12 % 8)
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, NPCIDs: []string{"npc-wolf"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, Level: 4, HPCurrent: 89, HPMax: 100, Alignment: -50, Flags: actorFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPCurrent: 10, HPMax: 100, Alignment: -50}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPCurrent: 50, HPMax: 100, Alignment: 40, Flags: wolfFlags},
				Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
				Enemies: []world.NPCEnemy{},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-pknowa", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"나 봐", "look-self-pknowa-suffix", []string{"그녀는 바르게 서 있습니다", "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-pknowa", []string{"당신은 늑대를 봅니다.", "그에게서 푸른 광채가 뻗어 나오고 있습니다.", "그는 많은 상처를 입었습니다.", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-pknowa-suffix", []string{"회색 늑대다.", "그에게서 푸른 광채가 뻗어 나오고 있습니다.", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-pknowa", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "그녀에게서 푸른 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다.", "[  몸  ]  갑옷"}, []string{"붉은", "꼭 맞는", "죽기 직전"}},
		{"Bob 봐", "look-bob-pknowa-suffix", []string{"그녀에게서 푸른 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다."}, []string{"붉은"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-pknowa-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("pknowa look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookAppendsFirstPlyStandingDescriptionWithoutMoving(t *testing.T) {
	bobFlags := [8]byte{}
	bobFlags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	bobFlags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	var bobKeys [3]string
	bobKeys[world.MarriageSpouseKeyIndex] = world.MarriageSpouseKeyPrefix + "Carol"
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100, Flags: bobFlags, Keys: bobKeys}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 Bob", "look-bob-standing"},
		{"Bob 봐", "look-bob-standing-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-stand-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range []string{"당신은 Bob님을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "[  몸  ]  갑옷"} {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range []string{"특별한 것은 보이지 않습니다", "상처", "죽기 직전", "꼭 맞는", "광채"} {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("standing look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookAppendsSelfStandingDescriptionWithoutMoving(t *testing.T) {
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 나", "look-self-standing"},
		{"나 봐", "look-self-standing-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-self-stand-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"} {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range []string{"특별한 것은 보이지 않습니다", "그는 서 있습니다"} {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("self standing look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookUnmigratedSelfDescriptionFailsClosed(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-self-desc-empty-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 나")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("empty self description=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookAppendsSelfMarriageWithoutMoving(t *testing.T) {
	aliceFlags := [8]byte{}
	aliceFlags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	aliceFlags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	var aliceKeys [3]string
	aliceKeys[world.MarriageSpouseKeyIndex] = world.MarriageSpouseKeyPrefix + "Carol"
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100, Flags: aliceFlags, Keys: aliceKeys}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 나", "look-self-marriage"},
		{"나 봐", "look-self-marriage-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-self-marry-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range []string{"당신은 거울을 들고 자신을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "그는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"} {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("self marriage look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookUnmigratedSelfSpouseFailsClosed(t *testing.T) {
	var aliceFlags [8]byte
	aliceFlags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100, Flags: aliceFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-self-married-ghost-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 나")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("self PMARRI without spouse=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookUnmigratedFirstPlyDescriptionFailsClosed(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-desc-empty-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 Bob")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("empty first_ply description=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}

func TestWorldConnectorSubmitLookAppendsEnemyLinesWithoutMoving(t *testing.T) {
	raw, err := json.Marshal(world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a", "b", "c"}, NPCIDs: []string{"npc-wolf"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", Description: "바르게 ", RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100}, Online: true, PlayerEnemies: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}}, Ready: [20]string{6: "helm"}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, PlayerEnemies: []string{"a"}, Items: &world.ItemCollection{Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}}, Ready: [20]string{0: "armor"}}},
			"c": {Body: world.LegacyMonster{Name: "Carol", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {
				Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100},
				Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-enm", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신에게 매우 화가 난것 같습니다.", "그녀는 당신과 싸우고 있습니다.", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-enm", []string{"당신은 늑대를 봅니다.", "그녀는 당신에게 매우 화가 난것 같습니다.", "그녀는 당신과 싸우고 있습니다.", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-enm-suffix", []string{"그녀는 당신과 싸우고 있습니다."}, nil},
		{"봐 Bob", "look-bob-enm", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"화가", "싸우고"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{state: append([]byte(nil), raw...)}}
			connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-enm-" + tt.id, Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3})
			if err != nil {
				t.Fatal(err)
			}
			connections := admitConnectorPlayers(t, connector, []string{"a", "b", "c"})
			text, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil {
				t.Fatalf("look err=%v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== ") {
					t.Fatalf("look=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("look=%q has forbidden %q", text, not)
				}
			}
			if store.commits != 1 {
				t.Fatalf("commits=%d want 1", store.commits)
			}
			expectLookInspectFanOut(t, connections, tt.line)
			saved, err := world.DecodeState(store.connectorCommandStore.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("enm look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay, err := connections["a"].Submit(context.Background(), tt.line)
			if err != nil || replay != text {
				t.Fatalf("replay=%q err=%v", replay, err)
			}
			if _, commits := store.snapshot(); commits != 1 {
				t.Fatalf("replay committed again: commits=%d", commits)
			}
			select {
			case event := <-connections["c"].events:
				t.Fatalf("replay fanned out: %q", event)
			default:
			}
		})
	}
}

func TestWorldConnectorSubmitLookNilEnemiesFailsClosed(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"a"}, NPCIDs: []string{"npc-wolf"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &connectorCommandStore{state: raw}
	connector, err := NewWorldConnector(WorldConnectorConfig{Store: store, WorldID: "look-nil-enm-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connector.owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	actor := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 8)}
	connector.mu.Lock()
	connector.connections[actor] = struct{}{}
	connector.mu.Unlock()
	unsupported, err := actor.Submit(context.Background(), "봐 늑대")
	if err != nil || !strings.Contains(unsupported, "아직 구현되지 않은 명령입니다") || store.commits != 0 {
		t.Fatalf("nil Enemies=%q err=%v commits=%d", unsupported, err, store.commits)
	}
}
