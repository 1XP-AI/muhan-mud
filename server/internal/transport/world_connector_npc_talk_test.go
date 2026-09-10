package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

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

func npcTalkTopicConnectorStore(t *testing.T) *npcTalkReplayStore {
	t.Helper()
	store := npcTalkConnectorStore(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := state.NPCs["guide"]
	npc.Body.Flags[23/8] |= 1 << (23 % 8) // MTALKS
	state.NPCs["guide"] = npc
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func npcTalkCastConnectorStore(t *testing.T) *npcTalkReplayStore {
	t.Helper()
	store := npcTalkTopicConnectorStore(t)
	state, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["a"]
	actor.Body.Level = 1
	actor.Body.Class = 4
	actor.Body.Stats = [5]byte{10, 10, 10, 10, 10}
	actor.Body.Flags = [8]byte{}
	actor.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	stats, err := actor.Items.CombatStats(actor.Body)
	if err != nil {
		t.Fatal(err)
	}
	actor.Body.Armor, actor.Body.Thaco = byte(stats.Armor), byte(stats.Thaco)
	state.Players["a"] = actor
	npc := state.NPCs["guide"]
	npc.Body.Level = 5
	npc.Body.Class = 3
	npc.Body.Stats[3] = 10
	npc.Body.MPMax, npc.Body.MPCurrent = 30, 30
	npc.Body.Spells[4/8] |= 1 << (4 % 8)
	state.NPCs["guide"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store.state, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func npcTalkTransportCatalog(t *testing.T, body string) world.TalkCatalog {
	t.Helper()
	catalog, err := world.LoadTalkCatalog(fstest.MapFS{
		"Guide-0": &fstest.MapFile{Data: []byte(body)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func npcTalkTransportCatalogAtLevel(t *testing.T, level int, body string) world.TalkCatalog {
	t.Helper()
	catalog, err := world.LoadTalkCatalog(fstest.MapFS{
		fmt.Sprintf("Guide-%d", level): &fstest.MapFile{Data: []byte(body)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func npcTalkConnectorConnections(t *testing.T, store *npcTalkReplayStore) (*WorldConnector, *worldConnection, *worldConnection) {
	return npcTalkConnectorConnectionsWithCatalog(t, store, nil)
}

func npcTalkConnectorConnectionsWithCatalog(t *testing.T, store *npcTalkReplayStore, catalog *world.TalkCatalog) (*WorldConnector, *worldConnection, *worldConnection) {
	t.Helper()
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-talk-world",
		Clock:       func() (int32, int) { return 100, 12 },
		MaxSessions: 2,
		TalkCatalog: catalog,
		Roll:        func(int, int) int { return 1 },
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

func TestWorldConnectorSubmitDispatchesNPCTalkTopicFromInjectedCatalog(t *testing.T) {
	store := npcTalkTopicConnectorStore(t)
	catalog := npcTalkTransportCatalog(t, "quest\ncanonical answer\n")
	connector, actor, observer := npcTalkConnectorConnectionsWithCatalog(t, store, &catalog)

	// NewWorldConnector takes an immutable value copy. Replacing the caller's
	// variable after construction must not change the live command dependency.
	catalog = npcTalkTransportCatalog(t, "quest\nreplacement answer\n")
	file, ok, err := connector.config.TalkCatalog.Lookup("Guide", 0)
	if err != nil || !ok || len(file.Topics) != 1 || file.Topics[0].Response != "canonical answer" {
		t.Fatalf("connector catalog changed through caller replacement: file=%+v ok=%v err=%v", file, ok, err)
	}

	output, err := actor.Submit(context.Background(), "대화 Guide quest")
	if err != nil || output != "\nGuide가 당신에게 \"canonical answer\"라고 이야기합니다.\r\n" {
		t.Fatalf("actor output=%q err=%v", output, err)
	}
	if store.commits != 1 {
		t.Fatalf("commits after topic submit=%d", store.commits)
	}
	roomEvents := []string{
		"\nAlice님이 Guide에게 \"quest\"에 관해 물어봅니다.\r\n",
		"\nGuide가 Alice님에게 \"canonical answer\"라고 이야기합니다.\r\n",
	}
	for i, want := range roomEvents {
		select {
		case got := <-observer.events:
			if got != want {
				t.Fatalf("observer topic event[%d]=%q want=%q", i, got, want)
			}
		default:
			t.Fatalf("observer topic event[%d] missing", i)
		}
	}
}

func TestWorldConnectorSubmitDispatchesNPCTalkAttackAction(t *testing.T) {
	store := npcTalkTopicConnectorStore(t)
	catalog := npcTalkTransportCatalog(t, "quest ATTACK\ncanonical answer\n")
	_, actor, observer := npcTalkConnectorConnectionsWithCatalog(t, store, &catalog)

	output, err := actor.Submit(context.Background(), "대화 Guide quest")
	wantOutput := "\nGuide가 당신에게 \"canonical answer\"라고 이야기합니다.\r\n\nGuide가 당신을 공격합니다.\n"
	if err != nil || output != wantOutput || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	wantEvents := []string{
		"\nAlice님이 Guide에게 \"quest\"에 관해 물어봅니다.\r\n",
		"\nGuide가 Alice님에게 \"canonical answer\"라고 이야기합니다.\r\n",
		"\nGuide가 Alice님을 공격합니다.\n",
	}
	for i, want := range wantEvents {
		select {
		case got := <-observer.events:
			if got != want {
				t.Fatalf("observer action event[%d]=%q want=%q", i, got, want)
			}
		default:
			t.Fatalf("observer action event[%d] missing", i)
		}
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.NPCs["guide"].Enemies) != 1 || saved.NPCs["guide"].Enemies[0].Target != (world.EntityRef{Kind: "player", ID: "a"}) {
		t.Fatalf("saved NPC=%+v err=%v", saved.NPCs["guide"], err)
	}
}

func TestWorldConnectorSubmitDispatchesNPCTalkCastAndPersistsEffect(t *testing.T) {
	store := npcTalkCastConnectorStore(t)
	catalog := npcTalkTransportCatalogAtLevel(t, 5, "quest CAST 성현진 PLAYER\ncanonical answer\n")
	_, actor, observer := npcTalkConnectorConnectionsWithCatalog(t, store, &catalog)

	output, err := actor.Submit(context.Background(), "대화 Guide quest")
	wantOutput := "\nGuide가 당신에게 \"canonical answer\"라고 이야기합니다.\r\n\nGuide가 당신의 머리에 한쪽손을 얹으며 성현진을 외웁니다.\n당신의 머리에서 삼매광이 뿜어져 나와 성스러운 기운이 몸을\n휘감습니다.\n"
	if err != nil || output != wantOutput || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	wantEvents := []string{
		"\nAlice님이 Guide에게 \"quest\"에 관해 물어봅니다.\r\n",
		"\nGuide가 Alice님에게 \"canonical answer\"라고 이야기합니다.\r\n",
		"\nGuide가 Alice의 머리에 한쪽손을 얹으며 성현진을 \n외웁니다.\n그의 머리에서 삼매광이 뿜어져 나와 성스러운 기운이 몸을\n휘감습니다.\n",
	}
	for i, want := range wantEvents {
		select {
		case got := <-observer.events:
			if got != want {
				t.Fatalf("observer cast event[%d]=%q want=%q", i, got, want)
			}
		default:
			t.Fatalf("observer cast event[%d] missing", i)
		}
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["guide"].Body.MPCurrent != 20 || saved.Players["a"].Body.Flags[0]&1 == 0 || saved.Players["a"].Body.Timers[2].Interval != 1320 {
		t.Fatalf("saved cast state npc=%+v actor=%+v", saved.NPCs["guide"].Body, saved.Players["a"].Body)
	}
}

func TestWorldConnectorSubmitNPCTalkTopicFailsClosedWithoutCatalog(t *testing.T) {
	store := npcTalkTopicConnectorStore(t)
	_, actor, _ := npcTalkConnectorConnections(t, store)
	if _, err := actor.Submit(context.Background(), "대화 Guide quest"); err == nil {
		t.Fatal("topic without injected catalog unexpectedly succeeded")
	} else if !errors.Is(err, world.ErrNPCTalkTopicsUnavailable) {
		t.Fatalf("topic without injected catalog err=%v", err)
	}
	if store.commits != 0 {
		t.Fatalf("topic without catalog committed=%d", store.commits)
	}
}
