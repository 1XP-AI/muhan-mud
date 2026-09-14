package transport

import (
	"context"
	"reflect"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func npcCombatFamilyDefeatCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Bob"},
		3: {ID: 3, Name: "백호", Boss: "Tiger"},
	}}
}

func npcCombatFamilyDefeatState(t *testing.T) world.State {
	t.Helper()
	state := npcCombatLethalTickFixture(t)
	state.War = &world.FamilyWar{Active: 2*16 + 3, CalledBy: 2, CalledAgainst: 3}
	victim := state.Players["player-b"]
	victim.Body.Daily[world.FamilyDailySlot].Max = 2
	victim.Body.Flags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	state.Players["player-b"] = victim
	observer := state.Players["player-a"]
	observer.Body.Daily[world.FamilyDailySlot].Max = 3
	observer.Body.Flags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	state.Players["player-a"] = observer
	quietFlags := observer.Body.Flags
	quietFlags[world.FamilyMutationNoBroadcastFlag/8] |= 1 << (world.FamilyMutationNoBroadcastFlag % 8)
	quiet := world.PlayerState{
		Body: world.LegacyMonster{
			Name: "Quiet", RoomID: 1, HPMax: 30, HPCurrent: 30, Armor: 70, Flags: quietFlags,
		},
		Online: true,
	}
	quiet.Body.Daily[world.FamilyDailySlot].Max = 3
	state.Players["quiet"] = quiet
	room := state.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "quiet")
	state.Rooms[1] = room
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func drainFamilyDefeatEvents(t *testing.T, conn *worldConnection, n int) []string {
	t.Helper()
	got := make([]string, 0, n)
	for i := 0; i < n; i++ {
		select {
		case event := <-conn.events:
			got = append(got, event)
		default:
			t.Fatalf("missing family-defeat event %d", i)
		}
	}
	return got
}

func TestRunNPCCombatPhasePublishesFamilyDefeatAndSuppressesReplay(t *testing.T) {
	state := npcCombatFamilyDefeatState(t)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "npc-combat-family-defeat",
		MaxSessions:   3,
		Clock:         func() (int32, int) { return now, 12 },
		Roll:          func(_, high int) int { return high },
		FamilyCatalog: npcCombatFamilyDefeatCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"player-b", "player-a", "quiet"} {
		lease, acquireErr := connector.owners.Acquire(id)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		if admitErr := connector.owners.Admit(lease, func() error { return nil }); admitErr != nil {
			t.Fatal(admitErr)
		}
		conn := &worldConnection{game: connector, lease: lease, ready: true, events: make(chan string, 4)}
		connections[id] = conn
		connector.connections[conn] = struct{}{}
	}

	receipt, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-family-defeat", 5, 100)
	if err != nil || receipt.Replayed || store.commits != 1 {
		t.Fatalf("receipt=%+v commits=%d err=%v", receipt, store.commits, err)
	}
	summary := decodeNPCCombatTickSummary(t, receipt.Response)
	if len(summary.Deaths) != 1 || !summary.Deaths[0].FamilyDefeated {
		t.Fatalf("summary=%+v", summary)
	}
	want := []string{world.FamilyWarBossDiedText("청룡"), world.FamilyWarDefeatedText("청룡")}
	if !reflect.DeepEqual(eventTexts(summary.Deaths[0].Events), want) {
		t.Fatalf("death events=%+v", summary.Deaths[0].Events)
	}
	saved, err := world.DecodeState(store.snapshot())
	if err != nil || saved.War == nil || *saved.War != (world.FamilyWar{}) {
		t.Fatalf("saved war=%+v err=%v", saved.War, err)
	}
	for _, id := range []string{"player-b", "player-a", "quiet"} {
		if got := drainFamilyDefeatEvents(t, connections[id], 2); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s events=%q want=%q", id, got, want)
		}
	}

	replay, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-family-defeat", 5, 100)
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v commits=%d err=%v", replay, store.commits, err)
	}
	for _, id := range []string{"player-b", "player-a", "quiet"} {
		select {
		case event := <-connections[id].events:
			t.Fatalf("replay fanned out to %s: %q", id, event)
		default:
		}
	}
}

func TestRunNPCCombatPhaseFamilyDefeatRejectsUnmigratedWar(t *testing.T) {
	state := npcCombatFamilyDefeatState(t)
	state.War = nil
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:         store,
		WorldID:       "npc-combat-family-defeat-unmigrated",
		MaxSessions:   1,
		Clock:         func() (int32, int) { return now, 12 },
		Roll:          func(_, high int) int { return high },
		FamilyCatalog: npcCombatFamilyDefeatCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-unmigrated-war", 5, 100); err == nil {
		t.Fatal("accepted unmigrated war")
	}
	if store.commits != 0 {
		t.Fatalf("partial commit=%d", store.commits)
	}
}

func TestRunNPCCombatPhaseFamilyDefeatRejectsMissingCatalog(t *testing.T) {
	state := npcCombatFamilyDefeatState(t)
	store := &npcCombatTickStore{state: encodeNPCCombatTickState(t, state)}
	now := int32(100)
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store:       store,
		WorldID:     "npc-combat-family-defeat-catalog",
		MaxSessions: 1,
		Clock:       func() (int32, int) { return now, 12 },
		Roll:        func(_, high int) int { return high },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connector.RunNPCCombatPhase(context.Background(), "npc-combat-missing-catalog", 5, 100); err == nil {
		t.Fatal("accepted missing catalog")
	}
	if store.commits != 0 {
		t.Fatalf("partial commit=%d", store.commits)
	}
}

func eventTexts(events []world.FamilyWarEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Text)
	}
	return out
}
