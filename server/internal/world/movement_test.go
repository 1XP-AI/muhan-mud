package world

import "testing"

func TestMovementPermanentTrackAndSnapshotMismatch(t *testing.T) {
	in := movementFixture()
	in.Source.Flags[2] = 4
	in.Source.Track = "고정"
	r, err := PlanMovement(in, nil)
	if err != nil || !r.Moved || r.Track != "고정" {
		t.Fatalf("%+v %v", r, err)
	}
	in.Destination.ID = 3
	if _, err := PlanMovement(in, nil); err == nil {
		t.Fatal("wrong destination snapshot accepted")
	}
	in.Destination = nil
	r, err = PlanMovement(in, nil)
	if err != nil || r.Moved || r.RoomID != 1 {
		t.Fatalf("missing resource: %+v %v", r, err)
	}
}

func movementFixture() MovementInput {
	return MovementInput{Source: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Name: "북", Destination: 2}}}}, Destination: &LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}, Prefix: "북", Occurrence: 1, Traversal: TraversalInput{HP: 30}, Visitor: DestinationVisitor{Level: 1}}
}

func TestMovementProposalAndRejectedArrivalTrack(t *testing.T) {
	in := movementFixture()
	in.Hidden = true
	r, err := PlanMovement(in, nil)
	if err != nil || !r.Moved || r.RoomID != 2 || r.Track != "북" || r.Hidden {
		t.Fatalf("%+v %v", r, err)
	}
	in.Destination.LowLevel = 10
	r, err = PlanMovement(in, nil)
	if err != nil || r.Moved || r.RoomID != 1 || r.Track != "북" || r.Hidden {
		t.Fatalf("rejected destination changes: %+v %v", r, err)
	}
	if in.Source.Track != "" || !in.Hidden {
		t.Fatal("mutated input")
	}
}

func TestMovementFallBeforeCooldown(t *testing.T) {
	in := movementFixture()
	in.Source.Exits[0].Flags = [4]byte{0, 2}
	in.Now = 10
	in.AttackReadyAt = 12
	in.Hidden = true
	calls := 0
	r, err := PlanMovement(in, func(int, int) int {
		calls++
		if calls == 1 {
			return 1
		}
		return 7
	})
	if err != nil || r.HP != 23 || r.Moved || !r.Hidden || r.Track != "" || r.WaitUntil != 12 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestMovementStealthBlockAndBoundary(t *testing.T) {
	in := movementFixture()
	in.Hidden = true
	in.Traversal.Class = 8
	in.Traversal.Invisible = true
	in.Visitor.Class = 8
	in.Source.Monsters = []LegacyMonster{{Flags: [8]byte{0, 1}}}
	in.EnemyOfPlayer = []bool{true}
	// Level 1, bonus 0 gives chance 11. Equality succeeds, 12 fails.
	r, err := PlanMovement(in, func(int, int) int { return 11 })
	if err != nil || !r.Moved || !r.Hidden {
		t.Fatalf("boundary: %+v %v", r, err)
	}
	r, err = PlanMovement(in, func(int, int) int { return 12 })
	if err != nil || r.Moved || r.Hidden || r.BlockerIndex != 0 || r.Track != "" {
		t.Fatalf("blocker: %+v %v", r, err)
	}
}
