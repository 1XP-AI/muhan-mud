package transport

import (
	"context"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/session"
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

func connectorFamilyApprovalState() world.State {
	state := connectorFamilyMutationState(false, true)
	state.Players["applicant"] = world.PlayerState{Body: func() world.LegacyMonster {
		body := state.Players["applicant"].Body
		body.Gold = 100000
		return body
	}(), Online: true}
	boss := state.Players["boss"]
	boss.Body.Gold = 100000
	state.Players["boss"] = boss
	state.Family = &world.FamilyState{Members: map[int16][]world.FamilyMember{2: {{ID: "boss", Name: "Boss", Class: 0}}}}
	return state
}

func connectorFamilyExpulsionState() world.State {
	state := connectorFamilyApprovalState()
	applicant := state.Players["applicant"]
	applicant.Body.Flags[world.FamilyPendingFlag/8] &^= 1 << (world.FamilyPendingFlag % 8)
	applicant.Body.Flags[world.FamilyMemberFlag/8] |= 1 << (world.FamilyMemberFlag % 8)
	state.Players["applicant"] = applicant
	state.Family.Members[2] = append(state.Family.Members[2], world.FamilyMember{ID: "applicant", Name: "Alice", Class: 0})
	return state
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

func TestWorldConnectorBareFamilyApplicationUsesLocalSelectionBeforeOneReceipt(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyMutationState(false, false), "applicant", "boss")
	connector.config.FamilyCatalog = connectorFamilyMutationCatalog()

	start, err := connections[0].Submit(context.Background(), "패거리가입")
	if err != nil || !strings.Contains(start, "청룡") || !strings.Contains(start, "패거리의 이름을 입력") || store.commits != 0 {
		t.Fatalf("start=%q err=%v commits=%d", start, err, store.commits)
	}
	choice, err := connections[0].Submit(context.Background(), "청룡")
	if err != nil || choice != "청룡에 가입을 하시겠습니까? (예/아니오) " || store.commits != 0 {
		t.Fatalf("choice=%q err=%v commits=%d", choice, err, store.commits)
	}
	confirmed, err := connections[0].Submit(context.Background(), "예")
	if err != nil || confirmed != world.FamilyApplicationResponse || store.commits != 1 {
		t.Fatalf("confirmed=%q err=%v commits=%d", confirmed, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	applicant := saved.Players["applicant"].Body
	if !world.PlayerFlagSet(applicant, world.FamilyPendingFlag) || applicant.Daily[world.FamilyDailySlot].Max != 2 {
		t.Fatalf("application was not persisted: %+v", applicant)
	}
}

func TestWorldConnectorBareFamilyApplicationCancelAndInvalidChoiceAreReceiptFree(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyMutationState(false, false), "applicant", "boss")
	connector.config.FamilyCatalog = connectorFamilyMutationCatalog()
	if _, err := connections[0].Submit(context.Background(), "패거리가입"); err != nil {
		t.Fatal(err)
	}
	if output, err := connections[0].Submit(context.Background(), "없는패거리"); err != nil || output != session.FamilyApplicationInvalidChoice || store.commits != 0 {
		t.Fatalf("invalid choice=%q err=%v commits=%d", output, err, store.commits)
	}
	if _, err := connections[0].Submit(context.Background(), "패거리가입"); err != nil {
		t.Fatal(err)
	}
	if output, err := connections[0].Submit(context.Background(), "청룡"); err != nil || store.commits != 0 {
		t.Fatalf("selection=%q err=%v commits=%d", output, err, store.commits)
	}
	if output, err := connections[0].Submit(context.Background(), "아니오"); err != nil || output != session.FamilyApplicationCancelResponse || store.commits != 0 {
		t.Fatalf("cancel=%q err=%v commits=%d", output, err, store.commits)
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

func TestWorldConnectorSubmitDispatchesFamilyApprovalAndActiveLeave(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyApprovalState(), "applicant", "boss")
	connector.config.FamilyCatalog = world.FamilyCatalog{Families: map[int16]world.FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss", Fee: 3}}}
	output, err := connections[1].Submit(context.Background(), "가입허가 Alice")
	if err != nil || !strings.Contains(output, "가입을 허가") || store.commits != 1 {
		t.Fatalf("approval output=%q err=%v commits=%d", output, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || !saved.Players["applicant"].Online || saved.Players["applicant"].Body.Gold != 130000 || saved.Players["boss"].Body.Gold != 70000 {
		t.Fatalf("saved=%+v err=%v", saved.Players, err)
	}
}

func TestWorldConnectorSubmitDispatchesFamilyExpulsionAndTargetNotification(t *testing.T) {
	store := &connectorCommandStore{}
	connector, connections := boundedLaneConnection(t, store, connectorFamilyExpulsionState(), "boss", "applicant")
	connector.config.FamilyCatalog = connectorFamilyMutationCatalog()
	output, err := connections[0].Submit(context.Background(), "패거리추방 Alice")
	if err != nil || !strings.Contains(output, "Alice님을 패거리에서 추방") || store.commits != 1 {
		t.Fatalf("output=%q err=%v commits=%d", output, err, store.commits)
	}
	select {
	case event := <-connections[1].events:
		if event != world.FamilyExpulsionNotification {
			t.Fatalf("target event=%q", event)
		}
	default:
		t.Fatal("target notification missing")
	}
	select {
	case event := <-connections[0].events:
		t.Fatalf("boss unexpectedly notified=%q", event)
	default:
	}
	if duplicate, err := connections[0].Submit(context.Background(), "패거리추방 Alice"); err != nil || !strings.Contains(duplicate, "아직 구현되지 않은 명령") || store.commits != 1 {
		// Each transport Submit receives a fresh command ID; this is a new
		// command and must fail after the member has been removed.
		t.Fatalf("duplicate command output=%q err=%v commits=%d", duplicate, err, store.commits)
	}
}
