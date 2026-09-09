package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

// npcTalkReplayStore keeps the connector test focused on transport replay
// behavior. After the first command is committed it returns that receipt for
// the next command ID as a replay, which models the durable receipt lookup
// without making the test depend on crypto/rand output from Submit.
type npcTalkReplayStore struct {
	*connectorCommandStore
}

func (s *npcTalkReplayStore) ReadWorldReceipt(ctx context.Context, worldID, command string, request json.RawMessage) (storage.WorldReceipt, error) {
	receipt, err := s.connectorCommandStore.ReadWorldReceipt(ctx, worldID, command, request)
	if err == nil {
		return receipt, nil
	}
	s.connectorCommandStore.mu.Lock()
	defer s.connectorCommandStore.mu.Unlock()
	if s.connectorCommandStore.receipt == nil {
		return storage.WorldReceipt{}, err
	}
	receipt = *s.connectorCommandStore.receipt
	receipt.Replayed = true
	return receipt, nil
}

func npcTalkConnectorStore(t *testing.T) *npcTalkReplayStore {
	t.Helper()
	actor := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: [8]byte{2}}
	observer := world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}
	npc := world.LegacyMonster{Name: "Guide", Type: 1, RoomID: 1, Talk: "어서오세요"}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b"},
				NPCIDs:    []string{"guide"},
			},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: actor, Online: true},
			"b": {Body: observer, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"guide": {Body: npc, Enemies: []world.NPCEnemy{}},
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &npcTalkReplayStore{connectorCommandStore: &connectorCommandStore{state: raw}}
}

func npcTalkConnectorConnections(t *testing.T, store *npcTalkReplayStore) (*WorldConnector, *worldConnection, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-talk-world",
		Clock:       func() (int32, int) { return 100, 12 },
		MaxSessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]*worldConnection, 0, 2)
	for _, id := range []string{"a", "b"} {
		lease, err := connector.owners.Acquire(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := connector.owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		connections = append(connections, &worldConnection{
			game:   connector,
			lease:  lease,
			ready:  true,
			events: make(chan string, 8),
		})
	}
	connector.mu.Lock()
	for _, connection := range connections {
		connector.connections[connection] = struct{}{}
	}
	connector.mu.Unlock()
	return connector, connections[0], connections[1]
}

func TestWorldConnectorSubmitDispatchesNPCTalkAndDoesNotFanOutReplay(t *testing.T) {
	store := npcTalkConnectorStore(t)
	_, actor, observer := npcTalkConnectorConnections(t, store)

	output, err := actor.Submit(context.Background(), "대화 Guide")
	if err != nil || output != "\nGuide가 당신에게 \"어서오세요\"라고 이야기합니다.\r\n" {
		t.Fatalf("actor output=%q err=%v", output, err)
	}
	if store.commits != 1 {
		t.Fatalf("commits after first submit=%d", store.commits)
	}

	roomEvents := []string{
		"\nAlice님이 Guide와 이야기를 합니다.\r\n",
		"\nGuide가 Alice님에게 \"어서오세요\"라고 이야기합니다.\r\n",
	}
	for i, want := range roomEvents {
		select {
		case got := <-observer.events:
			if got != want {
				t.Fatalf("observer event[%d]=%q want=%q", i, got, want)
			}
		default:
			t.Fatalf("observer event[%d] missing", i)
		}
	}
	select {
	case got := <-actor.events:
		t.Fatalf("actor received own NPC talk event=%q", got)
	default:
	}

	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	savedActor := saved.Players["a"]
	if savedActor.Body.Flags[0]&(1<<1) != 0 {
		t.Fatal("NPC talk did not persist PHIDDN release")
	}
	if len(saved.NPCs["guide"].Enemies) != 0 {
		t.Fatalf("non-aggressive NPC gained an enemy: %+v", saved.NPCs["guide"].Enemies)
	}

	// The replay store returns the durable first receipt even though Submit
	// creates a fresh command ID. WorldConnector must still return the actor
	// output, leave the commit count unchanged, and skip publishNPCTalk.
	replayedOutput, err := actor.Submit(context.Background(), "대화 Guide")
	if err != nil || replayedOutput != output {
		t.Fatalf("replayed actor output=%q err=%v want=%q", replayedOutput, err, output)
	}
	if store.commits != 1 {
		t.Fatalf("replay committed again: commits=%d", store.commits)
	}
	select {
	case got := <-observer.events:
		t.Fatalf("replay fanned out observer event=%q", got)
	default:
	}
	select {
	case got := <-actor.events:
		t.Fatalf("replay fanned out actor event=%q", got)
	default:
	}
}
