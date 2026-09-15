package world

import (
	"encoding/binary"
	"testing"
)

func TestEmptyDescriptionPresence(t *testing.T) {
	raw := make([]byte, 492)
	raw = binary.LittleEndian.AppendUint32(raw, 1)
	raw = append(raw, 0)
	raw = append(raw, make([]byte, 8)...)
	r, err := DecodeLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !r.ShortDescriptionPresent || r.LongDescriptionPresent {
		t.Fatal("absent and present-empty descriptions collapsed")
	}
	if got := RenderRoomEnvironment(r, ViewOptions{HideName: true}); got != "\n\n[ 출구 : 없음  ]\n" {
		t.Fatalf("empty description line lost: %q", got)
	}
}

func TestRoomMonsterDescriptions(t *testing.T) {
	monsters := []LegacyMonster{
		{Name: "늑대", Description: "늑대가 있습니다.", Alignment: -1},
		{Name: "늑대", Description: "늑대가 서 있습니다.", Alignment: 0},
		{Name: "비밀", Description: "보이면 안 됨", Flags: [8]byte{2}},
		{Name: "유령", Description: "유령이 떠 있습니다.", Flags: [8]byte{4}},
	}
	if got := RenderRoomMonsters(monsters, false, true); got != "(x2) 늑대가 서 있습니다. (푸른 광채)\n" {
		t.Fatalf("grouping/hidden: %q", got)
	}
	if got := RenderRoomMonsters(monsters, true, false); got != "(x2) 늑대가 서 있습니다.\n유령이 떠 있습니다.\n" {
		t.Fatalf("detect invisible: %q", got)
	}
}

func TestRoomEnvironment(t *testing.T) {
	r := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "광장", Exits: []LegacyExit{{Name: "동"}, {Name: "서", Flags: [4]byte{1}}, {Name: "북", Flags: [4]byte{2}}, {Name: "위", Flags: [4]byte{0, 0, 8}}}}, ShortDescription: "짧은 설명", LongDescription: "긴 설명"}
	got := RenderRoomEnvironment(r, ViewOptions{})
	want := "\n== 광장 ==\n\n짧은 설명\n긴 설명\n[ 출구 : 동 ]\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = RenderRoomEnvironment(r, ViewOptions{DetectInvisible: true, HideName: true, HideShort: true, HideLong: true})
	if got != "\n[ 출구 : 동, 북 ]\n" {
		t.Fatalf("visibility %q", got)
	}
}

func TestRoomDarkness(t *testing.T) {
	r := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Name: "밤", Flags: [8]byte{0, 2}}}
	for _, v := range []ViewOptions{{Hour: 5}, {Hour: 21}, {Blind: true, HasLight: true}} {
		got := RenderRoomEnvironment(r, v)
		want := "\n너무 어두워서 볼 수가 없습니다.\n"
		if v.Blind {
			want = "\n당신은 눈이 멀어 아무것도 볼 수 없습니다.\n너무 어두워서 볼 수가 없습니다.\n"
		}
		if got != want {
			t.Fatalf("%+v: %q", v, got)
		}
	}
	for _, v := range []ViewOptions{{Hour: 6}, {Hour: 20}, {Hour: 0, HasLight: true}, {Hour: 0, OtherPlayerLight: true}, {Hour: 0, Race: 1}, {Hour: 0, Race: 2}, {Hour: 0, Class: 10}} {
		if got := RenderRoomEnvironment(r, v); got != "\n== 밤 ==\n\n[ 출구 : 없음  ]\n" {
			t.Fatalf("%+v: %q", v, got)
		}
	}
}

func TestRoomExitDiagram(t *testing.T) {
	r := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{Exits: []LegacyExit{{Name: "동"}, {Name: "서"}, {Name: "북"}, {Name: "남"}, {Name: "위"}, {Name: "밑"}, {Name: "문"}}}}
	got := RenderRoomEnvironment(r, ViewOptions{HideName: true, ExitDiagram: true})
	want := "\n[    | 위 ]\n[ -- O -- ] [ 출구 : 문 ]\n[    | 밑 ]\n"
	if got != want {
		t.Fatalf("%q != %q", got, want)
	}
}
