package world

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginalRoomBody(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	room, err := DecodeLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(room.Monsters) != 0 || len(room.Objects) != 0 || !strings.HasPrefix(room.ShortDescription, "모든일에는 시작과 끝이 있다\n") {
		t.Fatalf("unexpected body: %+v", room)
	}
	for _, b := range [][]byte{raw[:len(raw)-1], append(append([]byte{}, raw...), 1)} {
		if _, err := DecodeLegacyRoom(b); err == nil {
			t.Fatal("accepted truncated/trailing data")
		}
	}
}

func TestMonsterNumericLayoutAndInventory(t *testing.T) {
	raw := make([]byte, 484)
	raw = binary.LittleEndian.AppendUint32(raw, 1)
	b := make([]byte, 1184)
	copy(b, "sentinel")
	b[318] = 7
	b[326] = 18
	b[436] = 9
	binary.LittleEndian.PutUint16(b[332:], 150)
	binary.LittleEndian.PutUint16(b[334:], 175) // Preserve, do not silently clamp.
	binary.LittleEndian.PutUint32(b[348:], 123456)
	binary.LittleEndian.PutUint32(b[540+4:], 876)
	binary.LittleEndian.PutUint32(b[620:], 987)
	for i := 460; i < 540; i++ {
		b[i] = 0xff
	} // Saved equipment addresses are not inventory.
	raw = append(raw, b...)
	raw = binary.LittleEndian.AppendUint32(raw, 1)
	o := make([]byte, 352)
	copy(o, "sword")
	raw = append(raw, o...)
	for i := 0; i < 5; i++ {
		raw = binary.LittleEndian.AppendUint32(raw, 0)
	}
	r, err := DecodeLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	m := r.Monsters[0]
	if m.Level != 7 || m.Stats[0] != 18 || m.Quest != 9 || m.HPMax != 150 || m.HPCurrent != 175 || m.Gold != 123456 || m.Daily[0].LastTime != 876 || m.Timers[0].Interval != 987 || len(m.Inventory) != 1 || m.Inventory[0].Name != "sword" {
		t.Fatalf("numeric layout mismatch: %+v", m)
	}
}

func TestRoomDescriptionLengthAndTreeLimits(t *testing.T) {
	for _, n := range []uint32{0xffffffff, 1024*1024 + 1} {
		raw := make([]byte, 492)
		raw = binary.LittleEndian.AppendUint32(raw, n)
		if _, err := DecodeLegacyRoom(raw); err == nil {
			t.Fatal("accepted invalid description length")
		}
	}
	raw := make([]byte, 488)
	raw = binary.LittleEndian.AppendUint32(raw, 1)
	for i := 0; i < 66; i++ {
		raw = append(raw, make([]byte, 352)...)
		raw = binary.LittleEndian.AppendUint32(raw, 1)
	}
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("accepted excessive nesting")
	}
}

func FuzzLegacyRoom(f *testing.F) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(raw)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = DecodeLegacyRoom(b) })
}

func TestRoomBodyCorpus(t *testing.T) {
	paths, _ := filepath.Glob("../../../rooms/r*/r?????")
	if len(paths) == 0 {
		t.Fatal("missing corpus")
	}
	failed, monsters, objects := 0, 0, 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r, err := DecodeLegacyRoom(raw)
		if err != nil {
			failed++
			if failed <= 10 {
				t.Logf("%s: %v", path, err)
			}
			continue
		}
		monsters += len(r.Monsters)
		objects += len(r.Objects)
	}
	t.Logf("rooms=%d failed=%d monsters=%d top-level objects=%d", len(paths), failed, monsters, objects)
	if failed != 0 {
		t.Fatalf("%d unsupported bodies; world admission remains disabled", failed)
	}
}

func TestNestedRoomObjects(t *testing.T) {
	raw := make([]byte, 484)
	put := func(n uint32) { raw = binary.LittleEndian.AppendUint32(raw, n) }
	put(0)
	put(1) // monsters, room objects
	obj := make([]byte, 352)
	copy(obj, "box")
	raw = append(raw, obj...)
	put(1)
	copy(obj, "gem")
	raw = append(raw, obj...)
	put(0)
	put(0)
	put(0)
	put(0)
	room, err := DecodeLegacyRoom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(room.Objects) != 1 || len(room.Objects[0].Contents) != 1 || room.Objects[0].Contents[0].Name != "gem" {
		t.Fatal("lost nested object")
	}
	binary.LittleEndian.PutUint32(raw[484:], 0xffffffff)
	if _, err := DecodeLegacyRoom(raw); err == nil {
		t.Fatal("accepted negative monster count")
	}
}
