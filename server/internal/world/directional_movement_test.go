package world

import "testing"

func TestDirectionalMovementSkipsGoCooldownAndUsesExactExit(t *testing.T) {
	source := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Name: "북문", Destination: 3}, {Name: "북", Destination: 2}}}}
	dest := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}
	in := MovementInput{Source: source, Destination: &dest, Prefix: "8", Occurrence: 1, Now: 10, AttackReadyAt: 99, Traversal: TraversalInput{Class: 4, HP: 30}, Visitor: DestinationVisitor{Class: 4, Level: 1}}
	got, err := PlanDirectionalMovement(in, nil)
	if err != nil || !got.Moved || got.RoomID != 2 || got.WaitUntil != 0 || got.Track != "북" {
		t.Fatalf("%+v %v", got, err)
	}
	in.Prefix = "북"
	normal, err := PlanMovement(in, nil)
	if err != nil || normal.WaitUntil != 99 || normal.Moved {
		t.Fatalf("go changed %+v %v", normal, err)
	}
	in.Prefix = "서"
	got, err = PlanDirectionalMovement(in, nil)
	if err != nil || len(got.Messages) != 1 || got.Messages[0] != "길이 막혀 있습니다." {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestDirectionalDestinationUsesExactHighLevelMessage(t *testing.T) {
	source := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Name: "북", Destination: 2}}}}
	dest := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, HighLevel: 5}}
	in := MovementInput{Source: source, Destination: &dest, Prefix: "북", Occurrence: 1, Traversal: TraversalInput{Class: 4, HP: 30}, Visitor: DestinationVisitor{Class: 4, Level: 6}}
	got, err := PlanDirectionalMovement(in, nil)
	if err != nil || got.Moved || got.Track != "북" || len(got.Messages) != 1 || got.Messages[0] != "그곳으로 갈려면 레벨 5이하여야만 합니다." {
		t.Fatalf("%+v %v", got, err)
	}
	in.Destination = nil
	got, err = PlanDirectionalMovement(in, nil)
	if err != nil || got.Messages[0] != "그쪽으로 지도가 없습니다. 신에게 연락해 주세요." {
		t.Fatalf("%+v %v", got, err)
	}
}
