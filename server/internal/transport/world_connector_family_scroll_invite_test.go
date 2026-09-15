package transport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorReadScrollState() world.State {
	room := world.RoomState{
		Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
		Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		PlayerIDs: []string{"actor", "observer"},
	}
	actor := world.PlayerState{
		Body:   world.LegacyMonster{Name: "Alice", Type: 0, Class: 5, Level: 10, RoomID: 1, HPMax: 30, HPCurrent: 10, MPMax: 20, MPCurrent: 5, Stats: [5]byte{10, 10, 10, 10, 10}},
		Online: true,
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"scroll-1": {Object: world.LegacyObject{Name: "회복부", Type: world.ReadScrollType, MagicPower: 1, ShotsCurrent: 1, DiceCount: 1}},
		}, Inventory: []string{"scroll-1"}},
	}
	return world.State{Version: 1, Rooms: map[int16]world.RoomState{1: room}, Players: map[string]world.PlayerState{
		"actor": actor, "observer": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
	}}
}

func connectorPropertyInviteState() world.State {
	var flags [8]byte
	flags[world.PropertyInviteRoomFlag/8] |= 1 << (world.PropertyInviteRoomFlag % 8)
	room := world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "집", Special: 7, Flags: flags}}, PlayerIDs: []string{"actor", "target"}}
	actor := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}
	actor.Daily[8].Max = 7
	return world.State{Version: 1, Rooms: map[int16]world.RoomState{1: room}, Players: map[string]world.PlayerState{
		"actor":  {Body: actor, Online: true},
		"target": {Body: world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
	}, Invitations: map[int16][]string{7: nil}}
}

func TestWorldConnectorSubmitDispatchesReadScrollAndPublishesRoomEvent(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorReadScrollState(), "actor", "observer")
	connector.config.Roll = func(low, _ int) int { return low }

	output, err := connections[0].Submit(context.Background(), "읽어 회복부")
	if err != nil || output == "" || store.commits != 1 {
		t.Fatalf("scroll output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if !strings.Contains(event, "Alice") || !strings.Contains(event, "회복부") {
			t.Fatalf("scroll observer event=%q", event)
		}
	default:
		t.Fatal("scroll observer event missing")
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Players["actor"].Items.Items) != 0 || saved.Players["actor"].Body.HPCurrent <= 10 {
		t.Fatalf("scroll state=%+v err=%v", saved.Players["actor"], err)
	}
}

func TestWorldConnectorSubmitDispatchesPropertyInviteToggleAndList(t *testing.T) {
	store := &connectorCommandStore{}
	_, connections := boundedLaneConnection(t, store, connectorPropertyInviteState(), "actor", "target")
	added, err := connections[0].Submit(context.Background(), "초대 Bob")
	if err != nil || !strings.Contains(added, "추가") || store.commits != 1 {
		t.Fatalf("invite add=%q err=%v commits=%d", added, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Invitations[7]) != 1 || saved.Invitations[7][0] != "target" {
		t.Fatalf("invite state=%+v err=%v", saved.Invitations, err)
	}
	listed, err := connections[0].Submit(context.Background(), "초대")
	if err != nil || !strings.Contains(listed, "Bob") || store.commits != 2 {
		t.Fatalf("invite list=%q err=%v commits=%d", listed, err, store.commits)
	}
}

func TestWorldConnectorSubmitDispatchesFamilyStatusWithCatalog(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyStatusState(), "actor", "member")
	connector.config.FamilyCatalog = world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}}}
	output, err := connections[0].Submit(context.Background(), "패거리누구")
	if err != nil || !strings.Contains(output, "청룡") || !strings.Contains(output, "Member") || store.commits != 1 {
		t.Fatalf("family output=%q err=%v commits=%d", output, err, store.commits)
	}
	if _, err := json.Marshal(output); err != nil {
		t.Fatal(err)
	}
}

func connectorFamilyStatusState() world.State {
	var actorFlags [8]byte
	actorFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	var memberFlags [8]byte
	memberFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	room := world.RoomState{Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}}, PlayerIDs: []string{"actor", "member"}}
	return world.State{Version: 1, Rooms: map[int16]world.RoomState{1: room}, Players: map[string]world.PlayerState{
		"actor":  {Body: world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: actorFlags, Daily: [10]world.LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {}, {Max: 2}}}, Online: true},
		"member": {Body: world.LegacyMonster{Name: "Member", Type: 0, RoomID: 1, Flags: memberFlags, Daily: [10]world.LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {}, {Max: 2}}}, Online: true},
	}}
}
