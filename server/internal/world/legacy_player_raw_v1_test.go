package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func encodeLegacyPlayerRawV1Fixture(t *testing.T, snapshot PlayerSnapshotV1) []byte {
	t.Helper()
	if _, err := EncodePlayerSnapshotV1(snapshot); err != nil {
		t.Fatalf("fixture snapshot is not canonical: %v", err)
	}
	raw := make([]byte, legacyPlayerRawV1CreatureBytes+legacyPlayerRawV1CountBytes+
		len(snapshot.Inventory.Nodes)*(legacyPlayerRawV1ObjectBytes+legacyPlayerRawV1CountBytes))
	copy(raw[0:80], snapshot.Name[:])
	copy(raw[80:160], snapshot.Description[:])
	copy(raw[160:240], snapshot.Talk[:])
	copy(raw[240:255], []byte("legacy-secret"))
	for index := range snapshot.Keys {
		copy(raw[255+index*20:255+(index+1)*20], snapshot.Keys[index][:])
	}
	raw[318] = snapshot.Level
	raw[319] = byte(snapshot.TypeCode)
	raw[320] = byte(snapshot.Class)
	raw[321] = byte(snapshot.Race)
	raw[322] = byte(snapshot.NumWander)
	putRawI16(raw, 324, snapshot.Alignment)
	raw[326] = byte(snapshot.Strength)
	raw[327] = byte(snapshot.Dexterity)
	raw[328] = byte(snapshot.Constitution)
	raw[329] = byte(snapshot.Intelligence)
	raw[330] = byte(snapshot.Piety)
	putRawI16(raw, 332, snapshot.HPMax)
	putRawI16(raw, 334, snapshot.HPCurrent)
	putRawI16(raw, 336, snapshot.MPMax)
	putRawI16(raw, 338, snapshot.MPCurrent)
	raw[340] = byte(snapshot.Armor)
	raw[341] = byte(snapshot.Thaco)
	putRawI64(raw, 344, snapshot.Experience)
	putRawI64(raw, 352, snapshot.Gold)
	putRawI16(raw, 360, snapshot.DiceCount)
	putRawI16(raw, 362, snapshot.DiceSides)
	putRawI16(raw, 364, snapshot.DicePlus)
	putRawI16(raw, 366, snapshot.Special)
	for index, value := range snapshot.Proficiency {
		putRawI64(raw, 368+index*8, value)
	}
	for index, value := range snapshot.Realm {
		putRawI64(raw, 408+index*8, value)
	}
	copy(raw[440:456], snapshot.Spells[:])
	copy(raw[456:464], snapshot.Flags[:])
	copy(raw[464:480], snapshot.Quests[:])
	raw[480] = byte(snapshot.QuestNum)
	for index, value := range snapshot.Carry {
		putRawI16(raw, 482+index*2, value)
	}
	putRawI16(raw, 502, snapshot.RoomNumber)
	for index, value := range snapshot.Daily {
		at := 664 + index*16
		raw[at] = value.Max
		raw[at+1] = value.Current
		putRawI64(raw, at+8, value.LastTime)
	}
	for index, value := range snapshot.LastTime {
		at := 824 + index*24
		putRawI64(raw, at, value.Interval)
		putRawI64(raw, at+8, value.LastUsed)
		putRawI16(raw, at+16, value.Misc)
	}
	// Native links and the saved descriptor are intentionally hostile junk;
	// the decoder must ignore them just as read_crt_player detaches them.
	for index := 504; index < 664; index++ {
		raw[index] = 0xa1
	}
	for index := 1904; index < 1952; index++ {
		raw[index] = 0xb2
	}
	cursor := legacyPlayerRawV1CreatureBytes
	children := rawGraphChildren(snapshot.Inventory)
	roots := rawGraphRoots(snapshot.Inventory)
	binary.LittleEndian.PutUint32(raw[cursor:cursor+4], uint32(len(roots)))
	cursor += 4
	for _, root := range roots {
		cursor = writeLegacyRawObject(raw, cursor, snapshot.Inventory, children, root)
	}
	if cursor != len(raw) {
		raw = raw[:cursor]
	}
	return raw
}

func putRawI16(raw []byte, offset int, value int16) {
	binary.LittleEndian.PutUint16(raw[offset:offset+2], uint16(value))
}

func putRawI64(raw []byte, offset int, value int64) {
	binary.LittleEndian.PutUint64(raw[offset:offset+8], uint64(value))
}

func rawGraphChildren(graph PlayerSnapshotObjectGraphV1) [][]int {
	children := make([][]int, len(graph.Nodes))
	for index, node := range graph.Nodes {
		if node.ParentIndex != nil {
			children[*node.ParentIndex] = append(children[*node.ParentIndex], index)
		}
	}
	return children
}

func rawGraphRoots(graph PlayerSnapshotObjectGraphV1) []int {
	roots := make([]int, 0, len(graph.Nodes))
	for index, node := range graph.Nodes {
		if node.ParentIndex == nil {
			roots = append(roots, index)
		}
	}
	return roots
}

func writeLegacyRawObject(raw []byte, cursor int, graph PlayerSnapshotObjectGraphV1, children [][]int, index int) int {
	object := graph.Nodes[index].Object
	copy(raw[cursor:cursor+80], object.Name[:])
	copy(raw[cursor+80:cursor+160], object.Description[:])
	for key := range object.Keys {
		copy(raw[cursor+160+key*20:cursor+180+key*20], object.Keys[key][:])
	}
	copy(raw[cursor+220:cursor+300], object.UseOutput[:])
	putRawI64(raw, cursor+304, object.Value)
	putRawI16(raw, cursor+312, object.Weight)
	raw[cursor+314] = byte(object.TypeCode)
	raw[cursor+315] = byte(object.Adjustment)
	putRawI16(raw, cursor+316, object.ShotsMax)
	putRawI16(raw, cursor+318, object.ShotsCurrent)
	putRawI16(raw, cursor+320, object.DiceCount)
	putRawI16(raw, cursor+322, object.DiceSides)
	putRawI16(raw, cursor+324, object.DicePlus)
	raw[cursor+326] = byte(object.Armor)
	raw[cursor+327] = byte(object.WearFlag)
	raw[cursor+328] = byte(object.MagicPower)
	raw[cursor+329] = byte(object.MagicRealm)
	putRawI16(raw, cursor+330, object.Special)
	copy(raw[cursor+332:cursor+340], object.Flags[:])
	raw[cursor+340] = byte(object.QuestNum)
	for offset := 344; offset < legacyPlayerRawV1ObjectBytes; offset++ {
		raw[cursor+offset] = 0xc3
	}
	cursor += legacyPlayerRawV1ObjectBytes
	binary.LittleEndian.PutUint32(raw[cursor:cursor+4], uint32(len(children[index])))
	cursor += 4
	for _, child := range children[index] {
		cursor = writeLegacyRawObject(raw, cursor, graph, children, child)
	}
	return cursor
}

func TestLegacyPlayerSnapshotRawV1MatchesPortableProjection(t *testing.T) {
	for _, name := range []string{
		"player_snapshot_v1_legacy_decoder_canonical.hex",
		"player_snapshot_v1_legacy_decoder_minimal.hex",
		"player_snapshot_v1_legacy_decoder_persisted_graph.hex",
	} {
		t.Run(name, func(t *testing.T) {
			portable := readPlayerSnapshotFixture(t, name)
			want, err := DecodePlayerSnapshotV1(portable)
			if err != nil {
				t.Fatal(err)
			}
			raw := encodeLegacyPlayerRawV1Fixture(t, want)
			got, err := DecodeLegacyPlayerSnapshotRawV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("raw projection differs:\n got=%+v\nwant=%+v", got, want)
			}
			canonical, err := EncodePlayerSnapshotV1(got)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonical, portable) {
				t.Fatal("raw projection did not reproduce the portable CDTO")
			}
		})
	}
}

func TestLegacyPlayerSnapshotRawV1NormalizesAndDetachesNativeState(t *testing.T) {
	portable := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_persisted_graph.hex")
	want, err := DecodePlayerSnapshotV1(portable)
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeLegacyPlayerRawV1Fixture(t, want)
	// These bytes are legal in the legacy image after the first NUL and must
	// disappear from the canonical projection.
	raw[13] = 0xa5
	raw[80+23+1] = 0x5a
	raw[160+14+1] = 0x7e
	objectOffset := legacyPlayerRawV1CreatureBytes + legacyPlayerRawV1CountBytes
	raw[objectOffset+11] = 0x91
	raw[objectOffset+80+9] = 0x92
	raw[objectOffset+160+4] = 0x93
	raw[objectOffset+220+9] = 0x94
	putRawI16(raw, 334, want.HPMax+7)
	putRawI16(raw, 338, want.MPMax+7)
	putRawI16(raw, objectOffset+318, want.Inventory.Nodes[0].Object.ShotsMax+7)
	decoded, err := DecodeLegacyPlayerSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("normalization/clamping changed logical snapshot:\n got=%+v\nwant=%+v", decoded, want)
	}
	canonical, err := EncodePlayerSnapshotV1(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, portable) {
		t.Fatal("native tail or runtime values entered the canonical CDTO")
	}
	if bytes.Contains(canonical, []byte("legacy-secret")) {
		t.Fatal("native password entered the portable projection")
	}
	inspection, err := InspectLegacyPlayerSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.SHA256 != sha256.Sum256(raw) || !bytes.Equal(inspection.Source, raw) {
		t.Fatal("raw inspection evidence does not cover the exact source bytes")
	}
}

func TestLegacyPlayerSnapshotRawV1RejectsMalformedStreams(t *testing.T) {
	portable := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_canonical.hex")
	snapshot, err := DecodePlayerSnapshotV1(portable)
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeLegacyPlayerRawV1Fixture(t, snapshot)
	base := func() []byte { return append([]byte(nil), raw...) }
	cases := []struct {
		name   string
		mutate func([]byte)
		want   error
	}{
		{name: "truncated-creature", mutate: func(value []byte) { value = value[:legacyPlayerRawV1CreatureBytes-1] }, want: ErrLegacyPlayerSnapshotRawTruncated},
		{name: "negative-root-count", mutate: func(value []byte) { binary.LittleEndian.PutUint32(value[legacyPlayerRawV1CreatureBytes:], ^uint32(0)) }, want: ErrLegacyPlayerSnapshotRawMalformed},
		{name: "wrong-player-type", mutate: func(value []byte) { value[319] = 1 }, want: ErrLegacyPlayerSnapshotRawMalformed},
		{name: "missing-name-terminator", mutate: func(value []byte) {
			for index := 0; index < 80; index++ {
				value[index] = 0x41
			}
		}, want: ErrLegacyPlayerSnapshotRawMalformed},
		{name: "negative-child-count", mutate: func(value []byte) {
			object := legacyPlayerRawV1CreatureBytes + legacyPlayerRawV1CountBytes
			binary.LittleEndian.PutUint32(value[object+legacyPlayerRawV1ObjectBytes:], ^uint32(0))
		}, want: ErrLegacyPlayerSnapshotRawMalformed},
		{name: "trailing-byte", mutate: func(value []byte) { value = append(value, 0x7f) }, want: ErrLegacyPlayerSnapshotRawTrailingBytes},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := base()
			if tc.name == "truncated-creature" {
				value = value[:legacyPlayerRawV1CreatureBytes-1]
			} else if tc.name == "trailing-byte" {
				value = append(value, 0x7f)
			} else {
				tc.mutate(value)
			}
			if _, err := DecodeLegacyPlayerSnapshotRawV1(value); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestLegacyPlayerSnapshotRawV1ContractIsExplicit(t *testing.T) {
	if LegacyPlayerSnapshotRawV1ABI == "" || LegacyPlayerSnapshotRawV1ParserVersion == "" {
		t.Fatal("raw ABI/parser contract must be versioned")
	}
	if err := ValidateLegacyPlayerSnapshotRawV1ABI(LegacyPlayerSnapshotRawV1ABI); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyPlayerSnapshotRawV1ABI(LegacyPlayerSnapshotRawV1ABI + ":other"); !errors.Is(err, ErrLegacyPlayerSnapshotRawUnsupportedABI) {
		t.Fatalf("unsupported ABI err=%v", err)
	}
	if legacyPlayerRawV1MaxBytes <= legacyPlayerRawV1CreatureBytes {
		t.Fatal("raw bound must include the object stream")
	}
	if _, err := DecodeLegacyPlayerSnapshotRawV1(make([]byte, legacyPlayerRawV1MaxBytes+1)); !errors.Is(err, ErrLegacyPlayerSnapshotRawMalformed) {
		t.Fatalf("oversized raw stream err=%v", err)
	}
}

func TestLegacyPlayerPasswordMatchesUsesNativeFieldWithoutPublishing(t *testing.T) {
	portable := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_minimal.hex")
	snapshot, err := DecodePlayerSnapshotV1(portable)
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeLegacyPlayerRawV1Fixture(t, snapshot)
	matched, err := LegacyPlayerPasswordMatches(raw, []byte("legacy-secret"))
	if err != nil || !matched {
		t.Fatalf("stored password rejected: %v", err)
	}
	matched, err = LegacyPlayerPasswordMatches(raw, []byte("wrong-secret"))
	if err != nil || matched {
		t.Fatal("wrong password accepted")
	}
	matched, err = LegacyPlayerPasswordMatches(raw, nil)
	if err != nil || matched {
		t.Fatal("empty password granted ownership")
	}
	canonical, err := CanonicalPlayerSnapshotFromLegacyRaw(raw, []byte("legacy-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(canonical, []byte("legacy-secret")) {
		t.Fatal("native password entered the portable projection")
	}
	got, err := DecodePlayerSnapshotV1(canonical)
	if err != nil || !reflect.DeepEqual(got, snapshot) {
		t.Fatalf("canonical snapshot drifted: %v", err)
	}
	if _, err := CanonicalPlayerSnapshotFromLegacyRaw(raw, []byte("wrong-secret")); !errors.Is(err, ErrLegacyPlayerPassword) {
		t.Fatalf("wrong password err=%v", err)
	}
}

func TestLegacyPlayerSnapshotRawV1InspectionCloneOwnsSource(t *testing.T) {
	portable := readPlayerSnapshotFixture(t, "player_snapshot_v1_legacy_decoder_minimal.hex")
	snapshot, err := DecodePlayerSnapshotV1(portable)
	if err != nil {
		t.Fatal(err)
	}
	raw := encodeLegacyPlayerRawV1Fixture(t, snapshot)
	inspection, err := InspectLegacyPlayerSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	clone := inspection.Clone()
	clone.Source[0] ^= 0xff
	if bytes.Equal(clone.Source, inspection.Source) {
		t.Fatal("inspection clone aliases source bytes")
	}
}

func TestLegacyPlayerSnapshotRawV1OracleProfiles(t *testing.T) {
	directory := os.Getenv("LEGACY_PLAYER_SNAPSHOT_V1_RAW_FIXTURE_DIR")
	if directory == "" {
		t.Skip("set LEGACY_PLAYER_SNAPSHOT_V1_RAW_FIXTURE_DIR to run C raw fixture differential")
	}
	for _, profile := range []struct {
		name    string
		fixture string
	}{
		{name: "rich", fixture: "player_snapshot_v1_legacy_decoder_canonical.hex"},
		{name: "minimal", fixture: "player_snapshot_v1_legacy_decoder_minimal.hex"},
		{name: "persisted-graph", fixture: "player_snapshot_v1_legacy_decoder_persisted_graph.hex"},
	} {
		t.Run(profile.name, func(t *testing.T) {
			rawPath := filepath.Join(directory, profile.name+".raw")
			raw, err := os.ReadFile(rawPath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeLegacyPlayerSnapshotRawV1(raw)
			if err != nil {
				t.Fatal(err)
			}
			want, err := DecodePlayerSnapshotV1(readPlayerSnapshotFixture(t, profile.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("C raw profile differs:\n got=%+v\nwant=%+v", got, want)
			}
			oracle := os.Getenv("LEGACY_PLAYER_SNAPSHOT_V1_C_ORACLE")
			if oracle == "" {
				return
			}
			output, err := exec.Command(oracle, "project-raw-file", rawPath).CombinedOutput()
			if err != nil {
				t.Fatalf("C raw projection oracle failed: %v (%s)", err, strings.TrimSpace(string(output)))
			}
			text := strings.TrimSpace(string(output))
			encoded, ok := strings.CutPrefix(text, "accept ")
			if !ok {
				t.Fatalf("C raw projection rejected a valid profile: %q", text)
			}
			cWire, err := hex.DecodeString(encoded)
			if err != nil {
				t.Fatalf("C raw projection is not hex: %v", err)
			}
			goWire, err := EncodePlayerSnapshotV1(got)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(cWire, goWire) {
				t.Fatal("C and Go raw projections differ")
			}
		})
	}
}
