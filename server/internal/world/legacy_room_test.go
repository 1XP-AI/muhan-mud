package world

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestOriginalStartingRoomHeader(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	room, err := DecodeLegacyRoomHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	if room.ID != 1 || room.Name != "무한대전" || len(room.Exits) != 1 || room.Exits[0].Name != "밑" || room.Exits[0].Destination != 1001 || room.BodyOffset != 528 {
		t.Fatalf("wrong original room: %+v", room)
	}
	// Old pointer addresses do not affect semantic output.
	for i := 84; i < 96; i++ {
		raw[i] = 0xff
	}
	again, err := DecodeLegacyRoomHeader(raw)
	if err != nil || again.Name != room.Name || again.ID != room.ID {
		t.Fatal("pointer bytes interpreted")
	}
}

func TestTrackedRoomHeaderCorpus(t *testing.T) {
	paths, err := filepath.Glob("../../../rooms/r*/r?????")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("room corpus missing")
	}
	failed := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeLegacyRoomHeader(raw); err != nil {
			failed++
			if failed <= 3 {
				t.Logf("unsupported header: %s", path)
			}
		}
	}
	if failed > 0 {
		t.Fatalf("unsupported headers: %d/%d; decoder must not be enabled for world admission", failed, len(paths))
	}
	t.Logf("decoded %d original room headers (body not validated)", len(paths))
}

func TestRoomHeaderRejectsTruncationAndCounts(t *testing.T) {
	raw, err := os.ReadFile("../../../rooms/r00/r00001")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 82, 479, 480, 483, 527} {
		if _, err := DecodeLegacyRoomHeader(raw[:size]); err == nil {
			t.Fatalf("accepted %d bytes", size)
		}
	}
	for _, count := range []uint32{0xffffffff, 65535} {
		copyRaw := append([]byte{}, raw...)
		binary.LittleEndian.PutUint32(copyRaw[480:], count)
		if _, err := DecodeLegacyRoomHeader(copyRaw); err == nil {
			t.Fatal("invalid exit count accepted")
		}
	}
}
