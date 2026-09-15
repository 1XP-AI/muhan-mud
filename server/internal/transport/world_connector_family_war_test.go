package transport

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorFamilyWarState() world.State {
	dragonFlags := [8]byte{}
	dragonFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	dragonFlags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	dragon := world.LegacyMonster{Name: "Boss", Type: 0, RoomID: 1, Flags: dragonFlags}
	dragon.Daily[world.FamilyDailySlot].Max = 2
	tigerFlags := [8]byte{}
	tigerFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	tigerFlags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	tiger := world.LegacyMonster{Name: "Tiger", Type: 0, RoomID: 1, Flags: tigerFlags}
	tiger.Daily[world.FamilyDailySlot].Max = 3
	quietFlags := tigerFlags
	quietFlags[world.FamilyMutationNoBroadcastFlag/8] |= 1 << (world.FamilyMutationNoBroadcastFlag % 8)
	quiet := world.LegacyMonster{Name: "Quiet", Type: 0, RoomID: 1, Flags: quietFlags}
	quiet.Daily[world.FamilyDailySlot].Max = 3
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"dragon-boss", "tiger-boss", "quiet"},
		}},
		Players: map[string]world.PlayerState{
			"dragon-boss": {Body: dragon, Online: true},
			"tiger-boss":  {Body: tiger, Online: true},
			"quiet":       {Body: quiet, Online: true},
		},
		War: &world.FamilyWar{},
	}
}

func connectorFamilyWarCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
		3: {ID: 3, Name: "백호", Boss: "Tiger"},
	}}
}

func TestWorldConnectorSubmitDispatchesFamilyWarDeclareAndSuppressesReplay(t *testing.T) {
	store := &familyMutationReplayStore{connectorCommandStore: &connectorCommandStore{}}
	raw, err := json.Marshal(connectorFamilyWarState())
	if err != nil {
		t.Fatal(err)
	}
	store.connectorCommandStore.state = raw
	connector, err := NewWorldConnector(WorldConnectorConfig{
		Store: store, WorldID: "family-war", Clock: func() (int32, int) { return 8, 12 }, MaxSessions: 3,
		FamilyCatalog: connectorFamilyWarCatalog(),
	})
	if err != nil {
		t.Fatal(err)
	}
	connections := map[string]*worldConnection{}
	for _, id := range []string{"dragon-boss", "tiger-boss", "quiet"} {
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

	output, err := connections["dragon-boss"].Submit(context.Background(), "선전포고 백호")
	want := world.FamilyWarDeclareText("청룡", "백호")
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("declare=%q err=%v commits=%d", output, err, store.commits)
	}
	for _, id := range []string{"tiger-boss", "quiet"} {
		select {
		case event := <-connections[id].events:
			if event != want {
				t.Fatalf("%s event=%q", id, event)
			}
		default:
			t.Fatalf("%s missing declare broadcast", id)
		}
	}

	replay, err := connections["dragon-boss"].Submit(context.Background(), "선전포고 백호")
	if err != nil || replay != output {
		t.Fatalf("replay=%q err=%v", replay, err)
	}
	if _, commits := store.snapshot(); commits != 1 {
		t.Fatalf("replay committed again: commits=%d", commits)
	}
	for _, id := range []string{"tiger-boss", "quiet"} {
		select {
		case event := <-connections[id].events:
			t.Fatalf("replay fanned out to %s: %q", id, event)
		default:
		}
	}
}

func TestWorldConnectorFamilyWarAcceptHonorsNoBroadcast(t *testing.T) {
	store := &connectorCommandStore{}
	state := connectorFamilyWarState()
	state.War = &world.FamilyWar{CalledBy: 2, CalledAgainst: 3}
	connector, connections := boundedLaneConnection(t, store, state, "tiger-boss", "dragon-boss", "quiet")
	connector.config.FamilyCatalog = connectorFamilyWarCatalog()

	output, err := connections[0].Submit(context.Background(), "선전포고 청룡")
	want := world.FamilyWarAcceptText("백호")
	if err != nil || output != want || store.commits != 1 {
		t.Fatalf("accept=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if event != want {
			t.Fatalf("declarer event=%q", event)
		}
	default:
		t.Fatal("declarer missing accept broadcast")
	}
	select {
	case event := <-connections[2].events:
		t.Fatalf("PNOBRD player received broadcast: %q", event)
	default:
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.War == nil || saved.War.Active != 3*16+2 {
		t.Fatalf("saved war=%+v", saved.War)
	}
}
