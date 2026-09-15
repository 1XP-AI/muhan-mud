package world

import "testing"

func TestRoomSceneVisibility(t *testing.T) {
	room := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "방", Flags: [8]byte{0, 1}}, Monsters: []LegacyMonster{{Name: "늑대", Description: "늑대가 있습니다."}}, Objects: []LegacyObject{{Name: "검"}}}
	players := []RoomPlayerView{{ID: "other", Name: "동료"}}
	if got := RenderRoomScene(room, players, SceneOptions{}); got != "\n너무 어두워서 볼 수가 없습니다.\n" {
		t.Fatal("dark-room disclosure:", got)
	}
	got := RenderRoomScene(room, players, SceneOptions{ViewOptions: ViewOptions{HasLight: true}})
	want := "\n== 방 ==\n\n[ 출구 : 없음  ]\n동료님이 서 있습니다.\n늑대가 있습니다.\n검이 놓여져 있습니다.\n"
	if got != want {
		t.Fatalf("%q != %q", got, want)
	}
}
