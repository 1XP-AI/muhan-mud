package world

import "testing"

func TestOtherPlayersVisibility(t *testing.T) {
	players := []RoomPlayerView{
		{ID: "self", Name: "나"},
		{ID: "one", Name: "하나", Description: "기대어 "},
		{ID: "two", Name: "둘", Flags: [8]byte{4}},
		{ID: "hidden", Name: "숨김", Flags: [8]byte{2}},
		{ID: "admin", Name: "운영", Flags: [8]byte{0, 4}},
	}
	if got := RenderRoomPlayers(players, "self", false, 1, false); got != "하나님이 서 있습니다.\n" {
		t.Fatalf("visibility: %q", got)
	}
	if got := RenderRoomPlayers(players, "self", true, 12, false); got != "하나, 둘, 운영님이 서 있습니다.\n" {
		t.Fatalf("admin visibility: %q", got)
	}
	if got := RenderRoomPlayers(players, "self", false, 1, true); got != "하나님이 기대어 서 있습니다.\n" {
		t.Fatalf("description: %q", got)
	}
	if got := RenderRoomPlayers(players[:1], "self", true, 12, false); got != "" {
		t.Fatalf("self rendered: %q", got)
	}
}
