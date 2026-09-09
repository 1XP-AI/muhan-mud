package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func familyTalkConnectorState(t *testing.T) []byte {
	t.Helper()
	member := func(name string, room int16) world.PlayerState {
		body := world.LegacyMonster{Name: name, Type: 0, RoomID: room}
		body.Flags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
		body.Daily[world.FamilyDailySlot].Max = 2
		return world.PlayerState{Body: body, Online: true}
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"actor", "member"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "외곽"}}, PlayerIDs: []string{"remote"}},
		},
		Players: map[string]world.PlayerState{
			"actor":  member("Actor", 1),
			"member": member("Member", 1),
			"remote": member("Remote", 2),
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWorldConnectorSubmitDispatchesFamilyTalkAndSuppressesReplayFanout(t *testing.T) {
	initial := familyTalkConnectorState(t)
	store := &groupTalkConnectorStore{state: append(json.RawMessage(nil), initial...)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "family-talk-world", Clock: func() (int32, int) { return 100, 12 }, MaxSessions: 3,
		FamilyCatalog: world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "두목"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	leases := make(map[string]session.SessionLease)
	for _, id := range []string{"actor", "member", "remote"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		leases[id] = lease
	}
	connections := make(map[string]*worldConnection, len(leases))
	connector.mu.Lock()
	for id, lease := range leases {
		connection := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = connection
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()

	output, err := connections["actor"].Submit(context.Background(), "패거리말 hello")
	if err != nil || output != "예. 좋습니다.\r\n" {
		t.Fatalf("output=%q err=%v", output, err)
	}
	state, receipt, commits := store.snapshot()
	if commits != 1 || string(state) != string(initial) || receipt == nil {
		t.Fatalf("state/receipt/commits changed unexpectedly: state=%d/%d receipt=%v commits=%d", len(state), len(initial), receipt, commits)
	}
	var result world.FamilyTalkResult
	if err := json.Unmarshal(receipt.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || len(result.Events) != 3 || result.Events[0].RecipientID != "actor" || result.Events[1].RecipientID != "member" || result.Events[2].RecipientID != "remote" {
		t.Fatalf("family result=%+v", result)
	}
	for _, id := range []string{"actor", "member", "remote"} {
		select {
		case event := <-connections[id].events:
			if event != "\nActor>>> hello\r\n" {
				t.Fatalf("%s event=%q", id, event)
			}
		default:
			t.Fatalf("%s family event missing", id)
		}
	}

	replayOutput, err := connections["actor"].Submit(context.Background(), "패거리말 hello")
	if err != nil || replayOutput != output {
		t.Fatalf("replay output=%q err=%v", replayOutput, err)
	}
	if _, _, commits = store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	for _, id := range []string{"actor", "member", "remote"} {
		select {
		case event := <-connections[id].events:
			t.Fatalf("replay fanned out to %s: %q", id, event)
		default:
		}
	}
}
