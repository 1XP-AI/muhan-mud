package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func connectorFamilyMutationState(active, pending bool) world.State {
	var applicantFlags [8]byte
	if active {
		applicantFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	}
	if pending {
		applicantFlags[world.FamilyPendingFlag/8] |= 1 << (world.FamilyPendingFlag % 8)
	}
	applicant := world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Flags: applicantFlags}
	if active || pending {
		applicant.Daily[world.FamilyDailySlot].Max = 2
	}
	bossFlags := [8]byte{}
	bossFlags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	bossFlags[world.FamilyBossFlag/8] |= 1 << (world.FamilyBossFlag % 8)
	boss := world.LegacyMonster{Name: "Boss", Type: 0, RoomID: 1, Flags: bossFlags}
	boss.Daily[world.FamilyDailySlot].Max = 2
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"applicant", "boss"},
		}},
		Players: map[string]world.PlayerState{
			"applicant": {Body: applicant, Online: true},
			"boss":      {Body: boss, Online: true},
		},
	}
}

func connectorFamilyMutationCatalog() world.FamilyCatalog {
	return world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{
		2: {ID: 2, Name: "청룡", Boss: "Boss"},
	}}
}

func TestWorldConnectorSubmitDispatchesFamilyMutation(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyMutationState(false, false), "applicant", "boss")
	connector.config.FamilyCatalog = connectorFamilyMutationCatalog()

	output, err := connections[0].Submit(context.Background(), "패거리가입 청룡")
	if err != nil || output != world.FamilyApplicationResponse || store.commits != 1 {
		t.Fatalf("join output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	applicant := saved.Players["applicant"].Body
	if applicant.Daily[world.FamilyDailySlot].Max != 2 || applicant.Flags[world.FamilyPendingFlag/8]&(1<<(world.FamilyPendingFlag%8)) == 0 || applicant.Flags[world.FamilyMemberFlag/8]&(1<<(world.FamilyMemberFlag%8)) != 0 {
		t.Fatalf("family application not persisted: %+v", applicant)
	}

	approval, err := connections[1].Submit(context.Background(), "가입허가 Alice")
	if err != nil || !strings.Contains(approval, "아직 구현되지 않은 명령") || store.commits != 1 {
		t.Fatalf("approval output=%q err=%v commits=%d", approval, err, store.commits)
	}
}

func TestWorldConnectorFamilyMutationFailsClosedForActiveLeave(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyMutationState(true, false), "applicant", "boss")
	connector.config.FamilyCatalog = connectorFamilyMutationCatalog()

	output, err := connections[0].Submit(context.Background(), "패거리탈퇴")
	if err != nil || !strings.Contains(output, "아직 구현되지 않은 명령") || store.commits != 0 {
		t.Fatalf("active leave output=%q err=%v commits=%d", output, err, store.commits)
	}
}
