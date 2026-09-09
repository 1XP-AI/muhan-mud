package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorDivorceState() world.State {
	s := connectorMarriageState()
	for id, spouse := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		player := s.Players[id]
		player.Body.Flags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
		player.Body.Keys[world.MarriageSpouseKeyIndex] = spouse
		s.Players[id] = player
	}
	// Carol opted out of the ordinary broadcast() channel (PNOBRD).  Spouse
	// notifications remain targeted, while the acceptance announcement must
	// skip this observer.
	observer := s.Players["observer"]
	observer.Body.Flags[world.MarriageDivorceNoBroadcastFlag/8] |= 1 << (world.MarriageDivorceNoBroadcastFlag % 8)
	s.Players["observer"] = observer
	return s
}

func TestWorldConnectorSubmitDivorceRequestAcceptPublishesDurableProjections(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorDivorceState(), "alice", "bob", "observer")

	request, err := connections[0].Submit(context.Background(), "이혼")
	if err != nil || !strings.Contains(request, "이혼신청") || store.commits != 1 {
		t.Fatalf("request output=%q err=%v commits=%d", request, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice님이 당신에게 이혼신청") {
			t.Fatalf("request target event=%q", event)
		}
	default:
		t.Fatal("request target event missing")
	}

	accept, err := connections[1].Submit(context.Background(), "이혼")
	if err != nil || !strings.Contains(accept, "Alice님의 이혼신청을 받아들") || store.commits != 2 {
		t.Fatalf("accept output=%q err=%v commits=%d", accept, err, store.commits)
	}
	select {
	case event := <-connections[0].events:
		if !strings.Contains(event, "Bob님이 당신의 이혼신청을 받아들") {
			t.Fatalf("accept proposer event=%q", event)
		}
	default:
		t.Fatal("accept proposer event missing")
	}
	for i := 0; i < 2; i++ {
		select {
		case event := <-connections[i].events:
			if !strings.Contains(event, "Bob님과 Alice님이 이혼을 하였습니다") {
				t.Fatalf("broadcast connection=%d event=%q", i, event)
			}
		default:
			t.Fatalf("broadcast connection=%d missing", i)
		}
	}
	select {
	case event := <-connections[2].events:
		t.Fatalf("PNOBRD observer received broadcast=%q", event)
	default:
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	for id, key := range map[string]string{"alice": "mBob", "bob": "mAlice"} {
		body := saved.Players[id].Body
		if world.PlayerFlagSet(body, world.MarriageActiveFlag) || world.PlayerFlagSet(body, world.MarriagePendingFlag) || world.PlayerFlagSet(body, world.MarriageDivorcePendingFlag) || body.Keys[world.MarriageSpouseKeyIndex] != key {
			t.Fatalf("saved divorce state id=%s body=%+v", id, body)
		}
	}
}

func TestWorldConnectorMarriageFollowupFailClosedWithoutCommit(t *testing.T) {
	for _, test := range []struct {
		name string
		line string
	}{
		{name: "malformed divorce", line: "이혼 extra"},
		{name: "descriptor-bound spouse message", line: "사랑말 hello"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &connectorCommandStore{}
			_, connections := boundedLaneConnection(t, store, connectorDivorceState(), "alice")
			output, err := connections[0].Submit(context.Background(), test.line)
			if err != nil || output != "아직 구현되지 않은 명령입니다.\r\n" || store.commits != 0 {
				t.Fatalf("line=%q output=%q err=%v commits=%d", test.line, output, err, store.commits)
			}
		})
	}
}
