package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func npcScavengeTransportState(t *testing.T, lastTime int32) json.RawMessage {
	t.Helper()
	npcFlags := [8]byte{}
	npcFlags[11/8] |= 1 << (11 % 8)
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "scavenge"}},
				PlayerIDs: []string{"target", "observer"},
				NPCIDs:    []string{"scavenger"},
				Items: &world.ItemCollection{
					Items:     map[string]world.Item{"gem": {Object: world.LegacyObject{Name: "gem"}}},
					Inventory: []string{"gem"},
				},
			},
			2: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "away"}},
				PlayerIDs: []string{"away"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"target":   {Body: world.LegacyMonster{Name: "대상", Type: 0, RoomID: 1}, Online: true},
			"observer": {Body: world.LegacyMonster{Name: "관전자", Type: 0, RoomID: 1}, Online: true},
			"away":     {Body: world.LegacyMonster{Name: "먼사람", Type: 0, RoomID: 2}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"scavenger": {
				Body: world.LegacyMonster{
					Name: "수집자", Type: 1, RoomID: 1, Flags: npcFlags,
					Timers: [45]world.LegacyTimer{{}},
				},
				Items:   &world.ItemCollection{Items: map[string]world.Item{}},
				Enemies: []world.NPCEnemy{},
			},
		},
		ActiveNPCIDs: []string{"scavenger"},
	}
	npc := state.NPCs["scavenger"]
	npc.Body.Timers[4] = world.LegacyTimer{LastTime: lastTime}
	state.NPCs["scavenger"] = npc
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeNPCScavengeTransportSummary(t *testing.T, receiptResponse json.RawMessage) world.NPCScavengeTickSummary {
	t.Helper()
	var summary world.NPCScavengeTickSummary
	if err := json.Unmarshal(receiptResponse, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func TestRunNPCScavengeTickPersistsSummaryAndFansOutOnlyCommittedRoomEvent(t *testing.T) {
	store := &connectorCommandStore{state: npcScavengeTransportState(t, 100)}
	rollCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-scavenge-transport", MaxSessions: 3,
		Clock: func() (int32, int) { return 203, 12 },
		Roll: func(low, high int) int {
			rollCalls++
			if low != 1 || high != 100 {
				t.Fatalf("unexpected MSCAVE range %d..%d", low, high)
			}
			return 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := installNPCAITickConnections(t, connector, "target", "observer", "away")
	receipt, ran, err := connector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || !ran || receipt.Replayed || store.commits != 1 || rollCalls != 1 {
		t.Fatalf("receipt=%+v ran=%v commits=%d rolls=%d err=%v", receipt, ran, store.commits, rollCalls, err)
	}
	if store.command != "npc-scavenge-10" {
		t.Fatalf("command=%q", store.command)
	}
	summary := decodeNPCScavengeTransportSummary(t, receipt.Response)
	if summary.Now != 200 || len(summary.Actions) != 1 || summary.Actions[0].SelectedItemID != "gem" || len(summary.Events) != 1 || summary.Events[0].RoomID != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if got := <-connections["target"].events; got != summary.Events[0].Text {
		t.Fatalf("target event=%q want=%q", got, summary.Events[0].Text)
	}
	if got := <-connections["observer"].events; got != summary.Events[0].Text {
		t.Fatalf("observer event=%q want=%q", got, summary.Events[0].Text)
	}
	select {
	case got := <-connections["away"].events:
		t.Fatalf("away received room event=%q", got)
	default:
	}

	duplicate, ran, err := connector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || ran || duplicate.Response != nil || store.commits != 1 || rollCalls != 1 {
		t.Fatalf("duplicate=%+v ran=%v commits=%d rolls=%d err=%v", duplicate, ran, store.commits, rollCalls, err)
	}
	stateRaw, _ := store.snapshot()
	saved, err := world.DecodeState(stateRaw)
	if err != nil || saved.Rooms[1].Items.Inventory != nil || !reflect.DeepEqual(saved.NPCs["scavenger"].Items.Inventory, []string{"gem"}) {
		t.Fatalf("saved state=%+v err=%v", saved, err)
	}
}

func TestRunNPCScavengeTickReplaysAcrossConnectorWithoutRerollOrFanout(t *testing.T) {
	store := &connectorCommandStore{state: npcScavengeTransportState(t, 100)}
	rollCalls := 0
	roll := func(int, int) int {
		rollCalls++
		return 1
	}
	config := WorldConnectorConfig{
		Store: store, WorldID: "npc-scavenge-replay", MaxSessions: 3,
		Clock: func() (int32, int) { return 203, 12 }, Roll: roll,
	}
	firstConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	firstConnections := installNPCAITickConnections(t, firstConnector, "target", "observer", "away")
	first, ran, err := firstConnector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || !ran || first.Replayed || rollCalls != 1 {
		t.Fatalf("first=%+v ran=%v rolls=%d err=%v", first, ran, rollCalls, err)
	}
	firstSummary := decodeNPCScavengeTransportSummary(t, first.Response)
	if got := <-firstConnections["target"].events; got != firstSummary.Events[0].Text {
		t.Fatalf("first target event=%q", got)
	}
	if got := <-firstConnections["observer"].events; got != firstSummary.Events[0].Text {
		t.Fatalf("first observer event=%q", got)
	}

	secondConnector, err := NewWorldConnector(config)
	if err != nil {
		t.Fatal(err)
	}
	secondConnections := installNPCAITickConnections(t, secondConnector, "target", "observer", "away")
	replay, ran, err := secondConnector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || !ran || !replay.Replayed || store.commits != 1 || rollCalls != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v ran=%v commits=%d rolls=%d err=%v", replay, ran, store.commits, rollCalls, err)
	}
	for id, connection := range secondConnections {
		select {
		case got := <-connection.events:
			t.Fatalf("replay delivered duplicate to %s: %q", id, got)
		default:
		}
	}
}

func TestRunNPCScavengeTickRetainsExactPendingRequestAcrossRetry(t *testing.T) {
	base := &connectorCommandStore{state: npcScavengeTransportState(t, 100)}
	store := &retryTickStore{base: base, failOnce: true}
	now := int32(203)
	clockCalls := 0
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-scavenge-retry", MaxSessions: 3,
		Clock: func() (int32, int) {
			clockCalls++
			return now, 12
		},
		Roll: func(int, int) int { return 1 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ran, err := connector.RunNPCScavengeTick(context.Background(), 20*time.Second); err == nil || !ran {
		t.Fatalf("uncertain first ran=%v err=%v", ran, err)
	}
	now = 219
	retry, ran, err := connector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || !ran || retry.Replayed || clockCalls != 1 {
		t.Fatalf("retry=%+v ran=%v clock-calls=%d err=%v", retry, ran, clockCalls, err)
	}
	if len(store.ids) != 2 || store.ids[0] != "npc-scavenge-10" || store.ids[1] != store.ids[0] || len(store.requests) != 2 || !bytes.Equal(store.requests[0], store.requests[1]) {
		t.Fatalf("retry IDs=%v requests=%q", store.ids, store.requests)
	}
	var request npcScavengeTickRequest
	if err := json.Unmarshal(store.requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.Kind != "npc-scavenge-phase" || request.Slot != 10 || request.Now != 200 {
		t.Fatalf("request=%+v", request)
	}
}

func TestRunNPCScavengeTickNonDueAllowsNilRollAndRejectsTamperedFanout(t *testing.T) {
	store := &connectorCommandStore{state: npcScavengeTransportState(t, 190)}
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "npc-scavenge-nondue", MaxSessions: 1,
		Clock: func() (int32, int) { return 203, 12 },
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, ran, err := connector.RunNPCScavengeTick(context.Background(), 20*time.Second)
	if err != nil || !ran {
		t.Fatalf("non-due receipt=%+v ran=%v err=%v", receipt, ran, err)
	}
	summary := decodeNPCScavengeTransportSummary(t, receipt.Response)
	if len(summary.Actions) != 1 || summary.Actions[0].Attempted || summary.Changed || !summary.NoOp || len(summary.Events) != 0 {
		t.Fatalf("non-due summary=%+v", summary)
	}

	connections := installNPCAITickConnections(t, connector, "target")
	event := world.NPCScavengeEvent{
		RoomID: 1, NPCID: "scavenger", NPCName: "수집자", ItemID: "gem", ItemName: "gem",
		Text: "위조된 MSCAVE 알림",
	}
	connector.publishNPCScavenge(func() world.State {
		stateRaw, _ := store.snapshot()
		state, decodeErr := world.DecodeState(stateRaw)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return state
	}(), event)
	select {
	case got := <-connections["target"].events:
		t.Fatalf("tampered fanout delivered=%q", got)
	default:
	}
}
