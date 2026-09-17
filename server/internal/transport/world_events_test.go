package transport

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type followerProjectionAckStore struct {
	*connectorCommandStore
	mu   sync.Mutex
	acks []string
}

func (s *followerProjectionAckStore) MarkWorldReceiptProjectionDelivered(_ context.Context, _ string, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks = append(s.acks, commandID)
	return nil
}

func (s *followerProjectionAckStore) ackCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.acks)
}

var _ storage.ReceiptProjectionStore = (*followerProjectionAckStore)(nil)

func directionalChaseMovementStates() (world.State, world.State) {
	before := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지"}},
				PlayerIDs: []string{"a", "observer", "follower"},
				NPCIDs:    []string{"zeta", "managed", "alpha"},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}},
				PlayerIDs: []string{"destination"},
			},
		},
		Players: map[string]world.PlayerState{
			"a":           {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true, FollowerIDs: []string{"follower"}},
			"observer":    {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
			"follower":    {Body: world.LegacyMonster{Name: "Follower", Type: 0, RoomID: 1}, Online: true, FollowingID: "a"},
			"destination": {Body: world.LegacyMonster{Name: "Destination", Type: 0, RoomID: 2}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"zeta": {
				Body:    world.LegacyMonster{Name: "Zulu", Type: 1, RoomID: 1},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
			},
			"managed": {
				Body:              world.LegacyMonster{Name: "Managed", Type: 1, RoomID: 1, Flags: [8]byte{1 << (46 % 8)}},
				Enemies:           []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
				FollowingPlayerID: "a",
			},
			"alpha": {
				Body:    world.LegacyMonster{Name: "Alpha", Type: 1, RoomID: 1, Flags: [8]byte{1 << (9 % 8)}},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
			},
		},
	}
	after := before
	after.Rooms = map[int16]world.RoomState{
		1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "출발지"}},
			PlayerIDs: []string{"observer"},
		},
		2: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}},
			PlayerIDs: []string{"destination", "a", "follower"},
			NPCIDs:    []string{"alpha", "managed", "zeta"},
		},
	}
	after.Players = map[string]world.PlayerState{
		"a":           {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 2}, Online: true, FollowerIDs: []string{"follower"}},
		"observer":    {Body: world.LegacyMonster{Name: "Observer", Type: 0, RoomID: 1}, Online: true},
		"follower":    {Body: world.LegacyMonster{Name: "Follower", Type: 0, RoomID: 2}, Online: true, FollowingID: "a"},
		"destination": {Body: world.LegacyMonster{Name: "Destination", Type: 0, RoomID: 2}, Online: true},
	}
	after.NPCs = map[string]world.NPCState{
		"zeta": {
			Body:    world.LegacyMonster{Name: "Zulu", Type: 1, RoomID: 2},
			Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
		},
		"managed": {
			Body:              world.LegacyMonster{Name: "Managed", Type: 1, RoomID: 2, Flags: [8]byte{1 << (46 % 8)}},
			Enemies:           []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
			FollowingPlayerID: "a",
		},
		"alpha": {
			Body:    world.LegacyMonster{Name: "Alpha", Type: 1, RoomID: 2, Flags: [8]byte{1 << (9 % 8)}},
			Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
		},
	}
	return before, after
}

func TestMovementEventsDirectionalNPCChaseUsesSourceOrderAndSkipsManagedFollower(t *testing.T) {
	before, after := directionalChaseMovementStates()
	chases := world.NPCFollowerChaseFanoutEvents(before, after, "a")
	if len(chases) != 2 || !strings.Contains(chases[0].Text, "Zulu") || !strings.Contains(chases[1].Text, "Alpha") {
		t.Fatalf("chase fan-out=%+v", chases)
	}
	if strings.Contains(chases[0].Text, "Managed") || strings.Contains(chases[1].Text, "Managed") {
		t.Fatalf("MDMFOL was mistaken for chase=%+v", chases)
	}

	events := movementEvents(before, after, "a", []string{"zeta", "alpha"})
	var sourceChases []string
	managedArrivals, playerFollowerArrivals, committedChaseArrivals := 0, 0, 0
	for _, event := range events {
		if event.RoomID == 1 && strings.Contains(event.Text, "따라갑니다") {
			sourceChases = append(sourceChases, event.Text)
		}
		if event.RoomID == 2 && strings.Contains(event.Text, "Managed") && strings.Contains(event.Text, "따라왔습니다") {
			managedArrivals++
		}
		if event.RoomID == 2 && strings.Contains(event.Text, "Follower") && strings.Contains(event.Text, "따라왔습니다") {
			playerFollowerArrivals++
		}
		if event.RoomID == 2 && (strings.Contains(event.Text, "Zulu") || strings.Contains(event.Text, "Alpha")) && strings.Contains(event.Text, "따라왔습니다") {
			committedChaseArrivals++
		}
	}
	if len(sourceChases) != 2 || !strings.Contains(sourceChases[0], "Zulu") || !strings.Contains(sourceChases[1], "Alpha") {
		t.Fatalf("source chase order=%v events=%+v", sourceChases, events)
	}
	if managedArrivals != 1 || playerFollowerArrivals != 1 {
		t.Fatalf("generic follower arrivals managed=%d player=%d events=%+v", managedArrivals, playerFollowerArrivals, events)
	}
	if committedChaseArrivals != 0 {
		t.Fatalf("committed chase was duplicated as destination arrival=%d events=%+v", committedChaseArrivals, events)
	}

	sourceEvents := make(chan string, 8)
	destinationEvents := make(chan string, 8)
	sourceConnection := &worldConnection{lease: session.SessionLease{ActorID: "observer"}, events: sourceEvents}
	destinationConnection := &worldConnection{lease: session.SessionLease{ActorID: "destination"}, events: destinationEvents}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{sourceConnection: {}, destinationConnection: {}}}
	g.publishMovement(before, after, "a", []string{"zeta", "alpha"})
	for _, want := range []string{"떠났습니다", "Zulu", "Alpha"} {
		select {
		case got := <-sourceEvents:
			if !strings.Contains(got, want) {
				t.Fatalf("source event=%q want %q", got, want)
			}
		default:
			t.Fatalf("source observer missing event %q", want)
		}
	}
	select {
	case extra := <-sourceEvents:
		t.Fatalf("unexpected extra source event=%q", extra)
	default:
	}
}

func TestMovementEventsWithoutCommittedNPCChaseIDsDoesNotInferChase(t *testing.T) {
	before, after := directionalChaseMovementStates()
	alarmBefore := world.NPCState{
		Body:    world.LegacyMonster{Name: "Alarm Guard", Type: 1, RoomID: 1},
		Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
	}
	before.NPCs["alarm"] = alarmBefore
	source := before.Rooms[1]
	source.NPCIDs = append(source.NPCIDs, "alarm")
	before.Rooms[1] = source
	alarmAfter := alarmBefore
	alarmAfter.Body.RoomID = 2
	alarmAfter.Enemies = nil
	after.NPCs["alarm"] = alarmAfter
	destination := after.Rooms[2]
	destination.NPCIDs = append(destination.NPCIDs, "alarm")
	after.Rooms[2] = destination
	for _, event := range movementEvents(before, after, "a") {
		if strings.Contains(event.Text, "Alarm Guard") {
			t.Fatalf("alarm relocation was presented as NPC follower movement: %+v", event)
		}
	}
}

func TestMovementEventsCommittedChaseSurvivesActorReallocationAndUsesOldNPCName(t *testing.T) {
	before, after := directionalChaseMovementStates()
	alarmBefore := world.NPCState{
		Body:    world.LegacyMonster{Name: "Alarm Guard", Type: 1, RoomID: 1},
		Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}}},
	}
	before.NPCs["alarm"] = alarmBefore
	source := before.Rooms[1]
	source.NPCIDs = append(source.NPCIDs, "alarm")
	before.Rooms[1] = source
	alarmAfter := alarmBefore
	alarmAfter.Body.RoomID = 2
	alarmAfter.Enemies = nil
	after.NPCs["alarm"] = alarmAfter
	destination := after.Rooms[2]
	destination.NPCIDs = append(destination.NPCIDs, "alarm")
	after.Rooms[2] = destination
	afterActor := after.Players["a"]
	afterActor.Body.RoomID = 99
	afterActor.Body.Name = "Reallocated"
	after.Players["a"] = afterActor
	after.Rooms[2] = world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}},
		PlayerIDs: []string{"destination", "follower"},
		NPCIDs:    []string{"alpha", "alarm", "managed", "zeta"},
	}
	after.Rooms[99] = world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 99, Name: "부활지"}},
		PlayerIDs: []string{"a"},
	}
	replaced := after.NPCs["zeta"]
	replaced.Body.Name = "Replacement"
	after.NPCs["zeta"] = replaced
	delete(after.NPCs, "alpha")

	events := movementEvents(before, after, "a", []string{"zeta", "alpha"})
	var chaseTexts []string
	for _, event := range events {
		if event.RoomID == 1 && strings.Contains(event.Text, "따라갑니다") {
			chaseTexts = append(chaseTexts, event.Text)
		}
	}
	if len(chaseTexts) != 2 || chaseTexts[0] != world.NPCFollowerChaseRoomText("Zulu", "Alice") || chaseTexts[1] != world.NPCFollowerChaseRoomText("Alpha", "Alice") {
		t.Fatalf("reallocated actor chase events=%v all=%+v", chaseTexts, events)
	}
	if strings.Contains(strings.Join(chaseTexts, ""), "Replacement") {
		t.Fatalf("post-transition NPC name replaced old notice=%v", chaseTexts)
	}
}

func TestMovementEventsPreserveCommittedActorFollowerAndNPCOrder(t *testing.T) {
	before := world.State{
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}},
		},
		NPCs: map[string]world.NPCState{
			"guard": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 1}, FollowingPlayerID: "a"},
		},
	}
	after := world.State{
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 2}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, FollowingID: "a"},
		},
		NPCs: map[string]world.NPCState{
			"guard": {Body: world.LegacyMonster{Name: "Guard", Type: 1, RoomID: 2}, FollowingPlayerID: "a"},
		},
	}
	events := movementEvents(before, after, "a")
	if len(events) != 4 {
		t.Fatalf("events=%+v", events)
	}
	wantRooms := []int16{1, 2, 2, 2}
	wantExcludes := []string{"a", "a", "b", ""}
	for i, event := range events {
		if event.RoomID != wantRooms[i] || event.ExcludeActorID != wantExcludes[i] {
			t.Fatalf("event[%d]=%+v", i, event)
		}
	}
	if events[0].Text == events[1].Text || events[2].Text == "" || events[3].Text == "" {
		t.Fatalf("event text=%+v", events)
	}
}

func TestWorldConnectorPublishesEventsWithoutBlockingCommand(t *testing.T) {
	events := make(chan string, 4)
	connection := &worldConnection{lease: session.SessionLease{ActorID: "b"}, events: events}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{connection: {}}}
	before := world.State{Players: map[string]world.PlayerState{
		"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}},
		"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: true},
	}}
	after := world.State{Players: map[string]world.PlayerState{
		"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 2}, Online: true},
		"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: true},
	}}
	g.publishMovement(before, after, "a")
	select {
	case text := <-events:
		if text == "" {
			t.Fatal("empty room event")
		}
	default:
		t.Fatal("room event was not queued")
	}
}

func TestWorldConnectorPublishesArrivalTrapFromCommittedRoomWhenActorDiesAndRespawns(t *testing.T) {
	events := make(chan string, 1)
	connection := &worldConnection{lease: session.SessionLease{ActorID: "observer"}, events: events}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{connection: {}}}
	after := world.State{Players: map[string]world.PlayerState{
		"actor":    {Body: world.LegacyMonster{Name: "Alice", RoomID: 1008}, Online: true},
		"observer": {Body: world.LegacyMonster{Name: "Bob", RoomID: 2}, Online: true},
	}}
	event := world.ArrivalTrapEvent{
		ActorID:   "actor",
		ActorName: "Alice",
		RoomID:    2,
		Trap:      world.TrapPit,
		ActorText: "당신은 구덩이에 빠졌습니다!\n당신은 1점의 피해를 입었습니다.\n",
		RoomText:  "\nAlice이 구덩이에 빠졌습니다.\r\n",
	}
	g.publishArrivalTrap(after, event)
	select {
	case got := <-events:
		if got != event.RoomText {
			t.Fatalf("trap room text=%q want=%q", got, event.RoomText)
		}
	default:
		t.Fatal("trap room event missing after actor relocation")
	}
}

func TestWorldConnectorFollowerTrapUsesCheckTimeRecipients(t *testing.T) {
	observerEvents := make(chan string, 1)
	lateEvents := make(chan string, 1)
	observer := &worldConnection{lease: session.SessionLease{ActorID: "observer"}, events: observerEvents}
	late := &worldConnection{lease: session.SessionLease{ActorID: "late"}, events: lateEvents}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{observer: {}, late: {}}}
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	// The observer left after check_traps, while late arrived after it. The
	// durable event must still follow the captured descriptor membership.
	after := world.State{Players: map[string]world.PlayerState{
		"observer": {Body: world.LegacyMonster{RoomID: 3}, Online: true},
		"late":     {Body: world.LegacyMonster{RoomID: 2}, Online: true},
	}}
	if !g.publishFollowerArrivalTraps(after, []world.ArrivalTrapEvent{event}) {
		t.Fatal("captured recipient was not accepted")
	}
	select {
	case got := <-observerEvents:
		if got != event.RoomText {
			t.Fatalf("observer event=%q", got)
		}
	default:
		t.Fatal("check-time observer missed trap event")
	}
	select {
	case got := <-lateEvents:
		t.Fatalf("final-state-only recipient received event=%q", got)
	default:
	}
}

func TestWorldConnectorFollowerTrapBackpressureStaysPending(t *testing.T) {
	actorEvents := make(chan followerProjectionDelivery, 1)
	observerEvents := make(chan followerProjectionDelivery, 1)
	actor := &worldConnection{lease: session.SessionLease{ActorID: "follower"}, projectionEvents: actorEvents}
	observer := &worldConnection{lease: session.SessionLease{ActorID: "observer"}, projectionEvents: observerEvents}
	observerEvents <- followerProjectionDelivery{text: "busy"}
	g := &WorldConnector{
		config:      WorldConnectorConfig{Store: &followerProjectionAckStore{connectorCommandStore: &connectorCommandStore{}}, WorldID: "follower-backpressure"},
		connections: map[*worldConnection]struct{}{actor: {}, observer: {}},
	}
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		ActorText:        "당신은 숨겨진 독화살에 맞았습니다!\n",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-backpressure-1") {
		t.Fatal("backpressured follower projection was treated as delivered")
	}
	if len(observer.pendingEvents) != 0 {
		t.Fatalf("backpressured follower event entered retry-unsafe queue=%q", observer.pendingEvents)
	}
	if got := (<-observerEvents).text; got != "busy" {
		t.Fatalf("backpressure sentinel=%q", got)
	}
	select {
	case got := <-observerEvents:
		t.Fatalf("backpressured trap event was delivered unexpectedly=%+v", got)
	default:
	}
}

func TestWorldConnectorFollowerTrapBackpressureRemainsReplayableAfterClose(t *testing.T) {
	store := &followerProjectionAckStore{connectorCommandStore: &connectorCommandStore{}}
	actorEvents := make(chan followerProjectionDelivery, 1)
	observerEvents := make(chan followerProjectionDelivery, 1)
	actor := &worldConnection{lease: session.SessionLease{ActorID: "follower"}, projectionEvents: actorEvents}
	observer := &worldConnection{lease: session.SessionLease{ActorID: "observer"}, projectionEvents: observerEvents}
	g := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-close"},
		connections:          map[*worldConnection]struct{}{actor: {}, observer: {}},
		publishedProjections: map[string]struct{}{},
		followerProjections:  map[string]*followerProjectionState{},
	}
	actor.game, observer.game = g, g
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		ActorText:        "당신은 숨겨진 독화살에 맞았습니다!\n",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	observerEvents <- followerProjectionDelivery{text: "busy"}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-close-1") {
		t.Fatal("backpressured projection was treated as delivered before connection close")
	}
	if store.ackCount() != 0 {
		t.Fatal("projection acknowledged while still in the backpressure queue")
	}
	if len(observer.pendingEvents) != 0 {
		t.Fatalf("backpressured follower event entered pending queue=%q", observer.pendingEvents)
	}
	g.unregister(observer)
	if store.ackCount() != 0 {
		t.Fatal("closing connection acknowledged a dropped pending projection")
	}

	// A fresh connector models process restart. The durable projection is
	// replayed to both recipients because the previous attempt never reached
	// the client-write boundary for observer.
	restarted := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-close"},
		connections:          map[*worldConnection]struct{}{},
		publishedProjections: map[string]struct{}{},
	}
	actorRestarted := &worldConnection{game: restarted, lease: actor.lease, projectionEvents: make(chan followerProjectionDelivery, 1)}
	observerRestarted := &worldConnection{game: restarted, lease: observer.lease, projectionEvents: make(chan followerProjectionDelivery, 1)}
	restarted.connections[actorRestarted] = struct{}{}
	restarted.connections[observerRestarted] = struct{}{}
	if restarted.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-close-1") {
		t.Fatal("replayed enqueue was reported as a wire delivery")
	}
	if store.ackCount() != 0 {
		t.Fatalf("replayed projection acknowledged before client writes=%d", store.ackCount())
	}
	actorDelivery := <-actorRestarted.projectionEvents
	if actorDelivery.text != event.ActorText {
		t.Fatalf("replayed actor event=%+v", actorDelivery)
	}
	observerDelivery := <-observerRestarted.projectionEvents
	if observerDelivery.text != event.RoomText {
		t.Fatalf("replayed room event=%+v", observerDelivery)
	}
	actorRestarted.followerProjectionWrite(actorDelivery)
	if store.ackCount() != 0 {
		t.Fatal("one replacement client write acknowledged the aggregate projection")
	}
	observerRestarted.followerProjectionWrite(observerDelivery)
	if store.ackCount() != 1 {
		t.Fatalf("replayed projection acknowledgements=%d want=1", store.ackCount())
	}
}

func TestWorldConnectorFollowerTrapEnqueueBeforeWireWriteReplaysAfterClose(t *testing.T) {
	store := &followerProjectionAckStore{connectorCommandStore: &connectorCommandStore{}}
	old := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-wire-close"},
		connections:          map[*worldConnection]struct{}{},
		publishedProjections: map[string]struct{}{},
		followerProjections:  map[string]*followerProjectionState{},
	}
	oldActor := &worldConnection{
		game:             old,
		lease:            session.SessionLease{ActorID: "follower"},
		projectionEvents: make(chan followerProjectionDelivery, 1),
	}
	oldObserver := &worldConnection{
		game:             old,
		lease:            session.SessionLease{ActorID: "observer"},
		projectionEvents: make(chan followerProjectionDelivery, 1),
	}
	old.connections[oldActor] = struct{}{}
	old.connections[oldObserver] = struct{}{}
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		ActorText:        "당신은 숨겨진 독화살에 맞았습니다!\n",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	if old.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-wire-close-1") {
		t.Fatal("enqueue was reported as a wire delivery")
	}
	if store.ackCount() != 0 {
		t.Fatal("projection acknowledged before the writer consumed either envelope")
	}
	if len(oldActor.projectionEvents) != 1 || len(oldObserver.projectionEvents) != 1 {
		t.Fatalf("enqueued projection counts actor=%d observer=%d", len(oldActor.projectionEvents), len(oldObserver.projectionEvents))
	}

	// Neither envelope is consumed: this is the connection-close window after
	// enqueue but before the WebSocket writer reaches its wire boundary.
	old.unregister(oldActor)
	old.unregister(oldObserver)
	if store.ackCount() != 0 {
		t.Fatal("connection close acknowledged an unwritten projection")
	}

	restarted := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-wire-close"},
		connections:          map[*worldConnection]struct{}{},
		publishedProjections: map[string]struct{}{},
	}
	newActor := &worldConnection{
		game:             restarted,
		lease:            oldActor.lease,
		projectionEvents: make(chan followerProjectionDelivery, 1),
	}
	newObserver := &worldConnection{
		game:             restarted,
		lease:            oldObserver.lease,
		projectionEvents: make(chan followerProjectionDelivery, 1),
	}
	restarted.connections[newActor] = struct{}{}
	restarted.connections[newObserver] = struct{}{}
	if restarted.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-wire-close-1") {
		t.Fatal("replayed enqueue was reported as a wire delivery")
	}
	if store.ackCount() != 0 {
		t.Fatal("replay acknowledged before either replacement writer consumed its envelope")
	}
	actorDelivery := <-newActor.projectionEvents
	if actorDelivery.text != event.ActorText || actorDelivery.key != "0:actor:follower" {
		t.Fatalf("actor replay delivery=%+v", actorDelivery)
	}
	newActor.followerProjectionWrite(actorDelivery)
	if store.ackCount() != 0 {
		t.Fatal("partial wire write acknowledged the aggregate projection")
	}
	observerDelivery := <-newObserver.projectionEvents
	if observerDelivery.text != event.RoomText || observerDelivery.key != "0:room:observer" {
		t.Fatalf("observer replay delivery=%+v", observerDelivery)
	}
	newObserver.followerProjectionWrite(observerDelivery)
	if store.ackCount() != 1 {
		t.Fatalf("replacement wire writes acknowledgements=%d want=1", store.ackCount())
	}
}

func TestWorldConnectorFollowerTrapPartialRetryWaitsForWireWritesAndIsIdempotent(t *testing.T) {
	store := &followerProjectionAckStore{connectorCommandStore: &connectorCommandStore{}}
	actor := &worldConnection{
		lease:            session.SessionLease{ActorID: "follower"},
		projectionEvents: make(chan followerProjectionDelivery, 2),
	}
	g := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-wire-partial"},
		connections:          map[*worldConnection]struct{}{actor: {}},
		publishedProjections: map[string]struct{}{},
		followerProjections:  map[string]*followerProjectionState{},
	}
	actor.game = g
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		ActorText:        "당신은 숨겨진 독화살에 맞았습니다!\n",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-wire-partial-1") {
		t.Fatal("partial enqueue was reported as a wire delivery")
	}
	observer := &worldConnection{
		game:             g,
		lease:            session.SessionLease{ActorID: "observer"},
		projectionEvents: make(chan followerProjectionDelivery, 2),
	}
	g.connections[observer] = struct{}{}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-wire-partial-1") {
		t.Fatal("partial retry was reported as a wire delivery")
	}
	if len(actor.projectionEvents) != 1 || len(observer.projectionEvents) != 1 {
		t.Fatalf("partial retry duplicated actor=%d observer=%d", len(actor.projectionEvents), len(observer.projectionEvents))
	}
	if store.ackCount() != 0 {
		t.Fatal("partial retry acknowledged before wire writes")
	}
	actor.followerProjectionWrite(<-actor.projectionEvents)
	if store.ackCount() != 0 {
		t.Fatal("actor wire write acknowledged before observer wire write")
	}
	observer.followerProjectionWrite(<-observer.projectionEvents)
	if store.ackCount() != 1 {
		t.Fatalf("partial retry acknowledgements=%d want=1", store.ackCount())
	}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-wire-partial-1") != true {
		t.Fatal("published projection did not remain idempotently complete")
	}
	select {
	case duplicate := <-actor.projectionEvents:
		t.Fatalf("actor projection duplicated after aggregate ack=%+v", duplicate)
	default:
	}
	select {
	case duplicate := <-observer.projectionEvents:
		t.Fatalf("observer projection duplicated after aggregate ack=%+v", duplicate)
	default:
	}
}

func TestWorldConnectorFollowerTrapPartialRecipientRetryIsIdempotent(t *testing.T) {
	store := &followerProjectionAckStore{connectorCommandStore: &connectorCommandStore{}}
	actor := &worldConnection{lease: session.SessionLease{ActorID: "follower"}, projectionEvents: make(chan followerProjectionDelivery, 2)}
	g := &WorldConnector{
		config:               WorldConnectorConfig{Store: store, WorldID: "follower-partial"},
		connections:          map[*worldConnection]struct{}{actor: {}},
		publishedProjections: map[string]struct{}{},
		followerProjections:  map[string]*followerProjectionState{},
	}
	actor.game = g
	event := world.ArrivalTrapEvent{
		ActorID:          "follower",
		ActorText:        "당신은 숨겨진 독화살에 맞았습니다!\n",
		RoomID:           2,
		RoomText:         "\nBob이 숨겨진 독화살에 맞았습니다.\r\n",
		RoomRecipientIDs: []string{"observer"},
	}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-partial-1") {
		t.Fatal("partial recipient enqueue reported as wire delivery")
	}
	observer := &worldConnection{game: g, lease: session.SessionLease{ActorID: "observer"}, projectionEvents: make(chan followerProjectionDelivery, 2)}
	g.connections[observer] = struct{}{}
	if g.publishFollowerArrivalTraps(world.State{}, []world.ArrivalTrapEvent{event}, "follower-partial-1") {
		t.Fatal("retry enqueue was reported as wire delivery")
	}
	if store.ackCount() != 0 {
		t.Fatalf("partial retry acknowledged before wire writes=%d", store.ackCount())
	}
	actorDelivery := <-actor.projectionEvents
	if actorDelivery.text != event.ActorText {
		t.Fatalf("actor event=%+v", actorDelivery)
	}
	observerDelivery := <-observer.projectionEvents
	if observerDelivery.text != event.RoomText {
		t.Fatalf("observer event=%+v", observerDelivery)
	}
	actor.followerProjectionWrite(actorDelivery)
	if store.ackCount() != 0 {
		t.Fatal("actor write acknowledged before observer write")
	}
	observer.followerProjectionWrite(observerDelivery)
	if store.ackCount() != 1 {
		t.Fatalf("partial retry acknowledgements=%d want=1", store.ackCount())
	}
	select {
	case got := <-actor.projectionEvents:
		t.Fatalf("actor recipient duplicated on retry: %+v", got)
	default:
	}
	select {
	case got := <-observer.projectionEvents:
		t.Fatalf("room recipient duplicated on retry: %+v", got)
	default:
	}
}

func TestWorldConnectorPublishesSayToOtherPlayersInRoom(t *testing.T) {
	events := make(chan string, 1)
	connection := &worldConnection{lease: session.SessionLease{ActorID: "b"}, events: events}
	g := &WorldConnector{connections: map[*worldConnection]struct{}{connection: {}}}
	after := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a", "b"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			"b": {Body: world.LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
	g.publishSay(after, "a", "안녕하세요")
	select {
	case text := <-events:
		if text != "\nAlice님이 \"안녕하세요\"라고 말합니다.\r\n" {
			t.Fatalf("say event=%q", text)
		}
	default:
		t.Fatal("say event was not queued")
	}
}
