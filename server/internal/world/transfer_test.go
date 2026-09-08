package world

import (
	"reflect"
	"testing"
)

func transferFixture() TransferInput {
	destination := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "광장"}}
	return TransferInput{
		Movement: MovementInput{
			Source:      LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Exits: []LegacyExit{{Name: "북", Destination: 2}}}},
			Destination: &destination, Prefix: "북", Occurrence: 1,
			Traversal: TraversalInput{HP: 30}, Now: 1,
		},
		ActorID: "a", SourcePlayers: []RoomPlayerView{{ID: "a", Name: "Alice"}},
	}
}

func TestTransferComposesBothRooms(t *testing.T) {
	in := transferFixture()
	got, err := PlanTransfer(in, nil, nil)
	if err != nil || !got.Movement.Moved || got.Movement.RoomID != 2 || got.Movement.HP != 30 || got.Source.Track != "북" || len(got.SourcePlayers) != 0 || !got.DeactivateSourceMonsters || got.Entry == nil || got.Entry.Room.BeenHere != 1 || got.Entry.Players[0].ID != "a" {
		t.Fatalf("%+v %v", got, err)
	}
	got.Source.Exits[0].Name = "changed"
	got.Entry.Room.Name = "changed"
	if in.Movement.Source.Exits[0].Name != "북" || in.Movement.Destination.Name != "광장" || len(in.SourcePlayers) != 1 {
		t.Fatal("proposal aliases input")
	}
}

func TestTransferEntryFailureReturnsNoPartialDeparture(t *testing.T) {
	in := transferFixture()
	in.Movement.Destination.PermanentObjects[0] = LegacyTimer{Misc: 1}
	got, err := PlanTransfer(in, refreshCatalog{failObject: true}, nil)
	if err == nil || !reflect.DeepEqual(got, TransferProposal{}) || in.Movement.Source.Track != "" || in.SourcePlayers[0].ID != "a" || in.Movement.Destination.BeenHere != 0 {
		t.Fatalf("partial transfer: %+v %v", got, err)
	}
}

func TestTransferDeniedAdmissionKeepsPremovementEffects(t *testing.T) {
	in := transferFixture()
	in.Movement.Hidden = true
	in.SourcePlayers[0].Flags[0] = 2
	in.Movement.Destination.LowLevel = 10
	got, err := PlanTransfer(in, nil, nil)
	if err != nil || got.Movement.Moved || got.Entry != nil || got.DeactivateSourceMonsters || got.Source.Track != "북" || len(got.SourcePlayers) != 1 || flag(got.SourcePlayers[0].Flags[:], 1) || got.Movement.Hidden {
		t.Fatalf("denied effects lost: %+v %v", got, err)
	}
	if !flag(in.SourcePlayers[0].Flags[:], 1) {
		t.Fatal("mutated input visibility")
	}
}

func TestTransferUpdatesEntrantVisibilityBeforeAnnouncement(t *testing.T) {
	in := transferFixture()
	in.Movement.Hidden = true
	in.SourcePlayers[0].Flags[0] = 2
	got, err := PlanTransfer(in, nil, nil)
	if err != nil || got.Entry == nil || !got.Entry.AnnounceArrival || flag(got.Entry.Players[0].Flags[:], 1) {
		t.Fatalf("stale hidden flag: %+v %v", got, err)
	}
}

func TestTransferFallDamageWithoutDeparture(t *testing.T) {
	in := transferFixture()
	in.Movement.Source.Exits[0].Flags[1] = 1 // climb
	got, err := PlanTransfer(in, nil, func(lo, hi int) int { return lo })
	if err != nil || got.Movement.Moved || got.Movement.HP != 25 || got.Entry != nil || len(got.SourcePlayers) != 1 || got.Source.Track != "" {
		t.Fatalf("fall did not preserve actor: %+v %v", got, err)
	}
}

func TestTransferRejectsInvalidMembershipBeforeRandom(t *testing.T) {
	for _, mutation := range []func(*TransferInput){
		func(in *TransferInput) { in.ActorID = "missing" },
		func(in *TransferInput) { in.Movement.Occupants = append(in.Movement.Occupants, in.SourcePlayers[0]) },
		func(in *TransferInput) { in.Movement.Hidden = true },
		func(in *TransferInput) {
			in.SourcePlayers = append(in.SourcePlayers, RoomPlayerView{ID: "b"})
			in.Movement.Occupants = []RoomPlayerView{{ID: "b"}}
		},
		func(in *TransferInput) { in.Movement.Now = 2147483648 },
	} {
		in := transferFixture()
		mutation(&in)
		got, err := PlanTransfer(in, nil, func(int, int) int { t.Fatal("random consumed for invalid snapshot"); return 1 })
		if err == nil || !reflect.DeepEqual(got, TransferProposal{}) {
			t.Fatalf("invalid snapshot accepted: %+v %v", got, err)
		}
	}
}
