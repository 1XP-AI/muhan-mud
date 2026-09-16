package transport

import (
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

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
