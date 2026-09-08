package world

import "testing"

func TestDestinationSignedRoomLimits(t *testing.T) {
	if got := DestinationRestriction(LegacyRoomHeader{LowLevel: 255}, nil, DestinationVisitor{Level: 1}); got != "" {
		t.Fatal(got)
	}
	if got := DestinationRestriction(LegacyRoomHeader{HighLevel: 255}, nil, DestinationVisitor{Level: 1}); got != "그곳으로 갈려면 레벨 0이하여야만 합니다." {
		t.Fatal(got)
	}
}

func TestDestinationLevelsAndPrecedence(t *testing.T) {
	r := LegacyRoomHeader{LowLevel: 10, HighLevel: 20, Flags: [8]byte{0, 64}}
	for _, tc := range []struct {
		v    DestinationVisitor
		want string
	}{
		{DestinationVisitor{Level: 9}, "레벨 10 이상이어야 그 곳으로 갈 수 있습니다."},
		{DestinationVisitor{Level: 21}, "그곳으로 갈려면 레벨 21이하여야만 합니다."},
		{DestinationVisitor{Level: 9, Class: 9}, "그 방에 있는 사용자가 너무 많습니다."},
		{DestinationVisitor{Level: 21, Class: 9}, "그곳으로 갈려면 레벨 21이하여야만 합니다."},
		{DestinationVisitor{Level: 21, Class: 10}, "그 방에 있는 사용자가 너무 많습니다."},
	} {
		if got := DestinationRestriction(r, []RoomPlayerView{{ID: "one"}}, tc.v); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}

func TestDestinationCountsHiddenButNotDMInvisible(t *testing.T) {
	r := LegacyRoomHeader{Flags: [8]byte{0, 64}}
	if got := DestinationRestriction(r, []RoomPlayerView{{Flags: [8]byte{6}}}, DestinationVisitor{}); got == "" {
		t.Fatal("hidden/invisible occupant not counted")
	}
	if got := DestinationRestriction(r, []RoomPlayerView{{Flags: [8]byte{0, 4}}}, DestinationVisitor{}); got != "" {
		t.Fatal(got)
	}
}

func TestDestinationFamilyAndPrivateLand(t *testing.T) {
	r := LegacyRoomHeader{Special: 7, Flags: [8]byte{0, 0, 0, 0, 96, 1}}
	if got := DestinationRestriction(r, nil, DestinationVisitor{Class: 12}); got != "그곳에는 패거리 가입자만 갈 수 있습니다." {
		t.Fatal("family membership rule", got)
	}
	if got := DestinationRestriction(r, nil, DestinationVisitor{FamilyMember: true}); got != "그곳은 당신이 갈 수 없는 곳입니다." {
		t.Fatal(got)
	}
	if got := DestinationRestriction(r, nil, DestinationVisitor{FamilyMember: true, FamilyID: 7}); got != "그곳은 사유지입니다." {
		t.Fatal(got)
	}
	if got := DestinationRestriction(r, nil, DestinationVisitor{FamilyMember: true, FamilyID: 7, Invited: true}); got != "" {
		t.Fatal(got)
	}
	if got := DestinationRestriction(r, nil, DestinationVisitor{FamilyMember: true, Class: 12}); got != "" {
		t.Fatal(got)
	}
}
