package world

import (
	"errors"
	"strings"
	"testing"
)

func familyStatusState() State {
	room := RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}
	actor := LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Daily: [10]LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {}, {Max: 2}}}
	actor.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
	bob := LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 3, Daily: [10]LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {}, {Max: 2}}}
	bob.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
	carol := LegacyMonster{Name: "Carol", Type: 0, RoomID: 1, Class: 4, Daily: [10]LegacyDaily{{}, {}, {}, {}, {}, {}, {}, {}, {}, {Max: 2}}}
	carol.Flags[FamilyPendingFlag/8] |= 1 << (FamilyPendingFlag % 8)
	room.PlayerIDs = []string{"a", "b", "c"}
	return State{Version: 1, Rooms: map[int16]RoomState{1: room}, Players: map[string]PlayerState{
		"a": {Body: actor, Online: true}, "b": {Body: bob, Online: true}, "c": {Body: carol, Online: true},
	}}
}

func familyStatusCatalog() FamilyCatalog {
	return FamilyCatalog{Families: map[int16]FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "Boss"}, 1: {ID: 1, Name: "백호", Boss: "Tiger"}}}
}

func TestProjectFamilyWhoRendersDeterministicRosterAndPendingMarker(t *testing.T) {
	projection, err := familyStatusState().ProjectFamilyWho("a", "", familyStatusCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if projection.FamilyID != 2 || projection.FamilyName != "청룡" || len(projection.Members) != 3 {
		t.Fatalf("projection=%+v", projection)
	}
	if !projection.Members[2].Pending || !strings.Contains(projection.Response, "(-)Carol") || !strings.Contains(projection.Response, "총 3명의") {
		t.Fatalf("response=%q", projection.Response)
	}
}

func TestProjectFamilyWhoTargetHonorsVisibilityAndNoMembership(t *testing.T) {
	s := familyStatusState()
	got, err := s.FamilyWho("a", "Bob", familyStatusCatalog())
	if err != nil || !strings.Contains(got, "[청룡]") {
		t.Fatalf("Bob got=%q err=%v", got, err)
	}
	plain := s
	p := plain.Players["b"]
	p.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	plain.Players["b"] = p
	if _, err := plain.ProjectFamilyWho("a", "Bob", familyStatusCatalog()); !errors.Is(err, ErrFamilyTargetUnavailable) {
		t.Fatalf("invisible err=%v", err)
	}
	viewer := plain.Players["a"]
	viewer.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	plain.Players["a"] = viewer
	if _, err := plain.ProjectFamilyWho("a", "Bob", familyStatusCatalog()); err != nil {
		t.Fatalf("detected err=%v", err)
	}
}

func TestFamilyStatusFailsClosedWithoutCatalogAndBlindDoesNotLeakTarget(t *testing.T) {
	s := familyStatusState()
	if _, err := s.ProjectFamilyWho("a", "", FamilyCatalog{}); !errors.Is(err, ErrFamilyCatalogUnavailable) {
		t.Fatalf("catalog err=%v", err)
	}
	p := s.Players["a"]
	p.Body.Flags[FamilyBlindFlag/8] |= 1 << (FamilyBlindFlag % 8)
	s.Players["a"] = p
	projection, err := s.ProjectFamilyWho("a", "missing", FamilyCatalog{})
	if err != nil || !strings.Contains(projection.Response, "눈이 멀") {
		t.Fatalf("blind projection=%+v err=%v", projection, err)
	}
}

func TestListFamilySortsByCanonicalID(t *testing.T) {
	got, err := familyStatusState().ListFamily(familyStatusCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(got, "백호") > strings.Index(got, "청룡") {
		t.Fatalf("not sorted=%q", got)
	}
}

func TestFamilyMemberRejectsPendingApplicationAsActiveMembership(t *testing.T) {
	s := familyStatusState()
	actor := s.Players["a"]
	actor.Body.Flags[FamilyMemberFlag/8] &^= 1 << (FamilyMemberFlag % 8)
	actor.Body.Flags[FamilyPendingFlag/8] |= 1 << (FamilyPendingFlag % 8)
	s.Players["a"] = actor
	got, err := s.FamilyMember("a", familyStatusCatalog())
	if err != nil || got != "당신은 패거리에 가입되어 있지 않습니다.\r\n" {
		t.Fatalf("pending member response=%q err=%v", got, err)
	}
}
