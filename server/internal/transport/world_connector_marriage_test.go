package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorMarriageState() world.State {
	var roomFlags [8]byte
	roomFlags[world.MarriageRoomFlag/8] |= 1 << (world.MarriageRoomFlag % 8)
	var aliceFlags [8]byte
	aliceFlags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	alice := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: aliceFlags}
	alice.Timers[world.MarriageHoursTimerIndex].Interval = 7 * 86400
	bob := world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}
	bob.Timers[world.MarriageHoursTimerIndex].Interval = 7 * 86400
	observer := world.LegacyMonster{Name: "Carol", Type: 0, RoomID: 1}
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "결혼식장", Flags: roomFlags}},
			PlayerIDs: []string{"alice", "bob", "observer"},
		}},
		Players: map[string]world.PlayerState{
			"alice":    {Body: alice, Online: true},
			"bob":      {Body: bob, Online: true},
			"observer": {Body: observer, Online: true},
		},
	}
}

func TestWorldConnectorSubmitMarriageRequestAndAcceptPublishesDurableProjections(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorMarriageState(), "alice", "bob", "observer")

	request, err := connections[0].Submit(context.Background(), "결혼 Bob")
	if err != nil || !strings.Contains(request, "Bob님에게 결혼을 신청") || store.commits != 1 {
		t.Fatalf("request output=%q err=%v commits=%d", request, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice님이 당신에게 결혼을 신청") {
			t.Fatalf("request target event=%q", event)
		}
	default:
		t.Fatal("request target event missing")
	}

	accept, err := connections[1].Submit(context.Background(), "결혼 Alice")
	if err != nil || !strings.Contains(accept, "Alice님의 결혼신청을 받아들") || store.commits != 2 {
		t.Fatalf("accept output=%q err=%v commits=%d", accept, err, store.commits)
	}
	select {
	case event := <-connections[0].events:
		if !strings.Contains(event, "Bob님이 당신의 결혼신청을 받아들") {
			t.Fatalf("accept proposer event=%q", event)
		}
	default:
		t.Fatal("accept proposer event missing")
	}
	for i, connection := range connections {
		select {
		case event := <-connection.events:
			if !strings.Contains(event, "Alice님과 Bob님이 결혼을 하였습니다") {
				t.Fatalf("broadcast connection=%d event=%q", i, event)
			}
		default:
			t.Fatalf("broadcast connection=%d missing", i)
		}
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if !world.PlayerFlagSet(saved.Players["alice"].Body, world.MarriageActiveFlag) || !world.PlayerFlagSet(saved.Players["bob"].Body, world.MarriageActiveFlag) || world.PlayerFlagSet(saved.Players["alice"].Body, world.MarriagePendingFlag) || world.PlayerFlagSet(saved.Players["bob"].Body, world.MarriagePendingFlag) {
		t.Fatalf("marriage flags alice=%v bob=%v", saved.Players["alice"].Body.Flags, saved.Players["bob"].Body.Flags)
	}
	if saved.Players["alice"].Body.Keys[world.MarriageSpouseKeyIndex] != "mBob" || saved.Players["bob"].Body.Keys[world.MarriageSpouseKeyIndex] != "mAlice" {
		t.Fatalf("spouse keys alice=%q bob=%q", saved.Players["alice"].Body.Keys[world.MarriageSpouseKeyIndex], saved.Players["bob"].Body.Keys[world.MarriageSpouseKeyIndex])
	}
}

func TestWorldConnectorMarriageRejectsMalformedLineWithoutCommit(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorMarriageState(), "alice")
	output, err := connections[0].Submit(context.Background(), "결혼 Bob extra")
	if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
		t.Fatalf("malformed output=%q err=%v commits=%d", output, err, store.commits)
	}
}
