package world

import (
	"strings"
	"testing"
)

func TestRoomObjectListing(t *testing.T) {
	objects := []LegacyObject{{Name: "검", Adjustment: 1}, {Name: "검", Adjustment: 255}, {Name: "은신", Flags: [8]byte{2}}, {Name: "투명", Flags: [8]byte{4}}, {Name: "풍경", Flags: [8]byte{0, 0, 4}}, {Name: "보석   ", MagicPower: 1}}
	if got := ListRoomObjects(objects, false, false); got.Text != "(x2) 검, 보석" || got.Truncated {
		t.Fatalf("%+v", got)
	}
	if got := ListRoomObjects(objects, true, true); got.Text != "검(+1), 검(-1), 투명, 보석(주문)" {
		t.Fatalf("%+v", got)
	}
	if objects[5].Name != "보석   " {
		t.Fatal("render mutated world")
	}
}

func TestRoomObjectsSubjectParticle(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"검", "검이 놓여져 있습니다.\n"}, {"보따리", "보따리가 놓여져 있습니다.\n"}, {"검(+1)", "검(+1)이 놓여져 있습니다.\n"}} {
		if got := RenderRoomObjects([]LegacyObject{{Name: tc.name}}, false, false); got != tc.want {
			t.Fatalf("%q != %q", got, tc.want)
		}
	}
	if got := RenderRoomObjects(nil, false, false); got != "" {
		t.Fatal(got)
	}
}

func TestObjectListingRetainsLegacyDisplayLimit(t *testing.T) {
	objects := make([]LegacyObject, 40)
	for i := range objects {
		objects[i].Name = strings.Repeat("x", 78) + string(rune('A'+i))
	}
	listing := ListRoomObjects(objects, false, false)
	if !listing.Truncated || len(listing.Text) < 1970 || len(listing.Text) > 2050 {
		t.Fatalf("unexpected display bound: %d %+v", len(listing.Text), listing.Truncated)
	}
}
