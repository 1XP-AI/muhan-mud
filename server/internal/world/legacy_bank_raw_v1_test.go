package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func legacyBankRawV1ObjectFixture(name string, value int64) PlayerSnapshotObjectV1 {
	var object PlayerSnapshotObjectV1
	copy(object.Name[:], name)
	object.Name[len(name)] = 0
	object.Description[0] = 0
	object.UseOutput[0] = 0
	object.Keys[0][0] = 0
	object.Keys[1][0] = 0
	object.Keys[2][0] = 0
	object.Value = value
	object.Weight = 2
	object.TypeCode = 7
	object.Adjustment = -1
	object.ShotsMax = 7
	object.ShotsCurrent = 9
	object.DiceCount = 1
	object.DiceSides = 6
	object.DicePlus = 2
	object.Armor = -2
	object.WearFlag = 3
	object.MagicPower = 4
	object.MagicRealm = 5
	object.Special = 6
	object.Flags[0] = 0x80
	object.QuestNum = 8
	return object
}

func writeLegacyBankRawV1Object(raw []byte, offset int, object PlayerSnapshotObjectV1) {
	copy(raw[offset:offset+80], object.Name[:])
	copy(raw[offset+80:offset+160], object.Description[:])
	copy(raw[offset+160:offset+180], object.Keys[0][:])
	copy(raw[offset+180:offset+200], object.Keys[1][:])
	copy(raw[offset+200:offset+220], object.Keys[2][:])
	copy(raw[offset+220:offset+300], object.UseOutput[:])
	binary.LittleEndian.PutUint64(raw[offset+304:offset+312], uint64(object.Value))
	binary.LittleEndian.PutUint16(raw[offset+312:offset+314], uint16(object.Weight))
	raw[offset+314] = byte(object.TypeCode)
	raw[offset+315] = byte(object.Adjustment)
	binary.LittleEndian.PutUint16(raw[offset+316:offset+318], uint16(object.ShotsMax))
	binary.LittleEndian.PutUint16(raw[offset+318:offset+320], uint16(object.ShotsCurrent))
	binary.LittleEndian.PutUint16(raw[offset+320:offset+322], uint16(object.DiceCount))
	binary.LittleEndian.PutUint16(raw[offset+322:offset+324], uint16(object.DiceSides))
	binary.LittleEndian.PutUint16(raw[offset+324:offset+326], uint16(object.DicePlus))
	raw[offset+326] = byte(object.Armor)
	raw[offset+327] = byte(object.WearFlag)
	raw[offset+328] = byte(object.MagicPower)
	raw[offset+329] = byte(object.MagicRealm)
	binary.LittleEndian.PutUint16(raw[offset+330:offset+332], uint16(object.Special))
	copy(raw[offset+332:offset+340], object.Flags[:])
	raw[offset+340] = byte(object.QuestNum)
	// These bytes are native pointer fields. They are intentionally hostile:
	// a correct raw decoder must ignore them rather than treat addresses as
	// stable data.
	for index := 344; index < legacyBankRawV1ObjectBytes; index++ {
		raw[offset+index] = byte(0xa0 + index%31)
	}
}

func writeLegacyBankRawV1Record(raw []byte, cursor int, object PlayerSnapshotObjectV1, children int) int {
	writeLegacyBankRawV1Object(raw, cursor, object)
	cursor += legacyBankRawV1ObjectBytes
	binary.LittleEndian.PutUint32(raw[cursor:cursor+legacyBankRawV1CountBytes], uint32(children))
	return cursor + legacyBankRawV1CountBytes
}

func legacyBankRawV1Fixture() []byte {
	root := legacyBankRawV1ObjectFixture("bank", 99)
	ruby := legacyBankRawV1ObjectFixture("ruby", 7)
	coin := legacyBankRawV1ObjectFixture("coin", 1)
	potion := legacyBankRawV1ObjectFixture("potion", 3)
	raw := make([]byte, 4*(legacyBankRawV1ObjectBytes+legacyBankRawV1CountBytes))
	cursor := writeLegacyBankRawV1Record(raw, 0, root, 2)
	cursor = writeLegacyBankRawV1Record(raw, cursor, ruby, 1)
	cursor = writeLegacyBankRawV1Record(raw, cursor, coin, 0)
	cursor = writeLegacyBankRawV1Record(raw, cursor, potion, 0)
	if cursor != len(raw) {
		panic("legacy bank fixture size mismatch")
	}
	return raw
}

func legacyBankRawV1CFixture() []byte {
	root := legacyBankRawV1ZeroObject()
	child := legacyBankRawV1ZeroObject()
	copy(root.Name[:], "bank-root")
	copy(child.Name[:], "ruby")
	root.ShotsMax, root.ShotsCurrent = 1, 1
	child.ShotsMax, child.ShotsCurrent = 1, 1
	raw := make([]byte, 2*(legacyBankRawV1ObjectBytes+legacyBankRawV1CountBytes))
	cursor := writeLegacyBankRawV1Record(raw, 0, root, 1)
	cursor = writeLegacyBankRawV1Record(raw, cursor, child, 0)
	if cursor != len(raw) {
		panic("legacy C bank fixture size mismatch")
	}
	// The C evidence fixture zeroes the complete object with memset before
	// setting its name/shots. Restore zero pointer fields for that exact
	// cross-check; the main fixture intentionally uses hostile pointer bytes.
	for _, offset := range []int{0, legacyBankRawV1ObjectBytes + legacyBankRawV1CountBytes} {
		for index := 344; index < legacyBankRawV1ObjectBytes; index++ {
			raw[offset+index] = 0
		}
	}
	return raw
}

func legacyBankRawV1ZeroObject() PlayerSnapshotObjectV1 {
	var object PlayerSnapshotObjectV1
	object.Name[0] = 0
	object.Description[0] = 0
	object.UseOutput[0] = 0
	object.Keys[0][0] = 0
	object.Keys[1][0] = 0
	object.Keys[2][0] = 0
	return object
}

func legacyBankRawV1DeepFixture(nodes int) []byte {
	raw := make([]byte, nodes*(legacyBankRawV1ObjectBytes+legacyBankRawV1CountBytes))
	object := legacyBankRawV1ZeroObject()
	cursor := 0
	for index := 0; index < nodes; index++ {
		children := 0
		if index+1 < nodes {
			children = 1
		}
		cursor = writeLegacyBankRawV1Record(raw, cursor, object, children)
	}
	return raw
}

func legacyBankRawV1WideFixture() []byte {
	// A 4,096-child root with one grandchild per child exceeds the global
	// 8,192-node budget while keeping every individual list within 4,096.
	const children = legacyBankRawV1MaxList
	nodes := 1 + children + children
	raw := make([]byte, nodes*(legacyBankRawV1ObjectBytes+legacyBankRawV1CountBytes))
	object := legacyBankRawV1ZeroObject()
	cursor := writeLegacyBankRawV1Record(raw, 0, object, children)
	for index := 0; index < children; index++ {
		cursor = writeLegacyBankRawV1Record(raw, cursor, object, 1)
		cursor = writeLegacyBankRawV1Record(raw, cursor, object, 0)
	}
	if cursor != len(raw) {
		panic("legacy bank wide fixture size mismatch")
	}
	return raw
}

func TestLegacyBankSnapshotRawV1ConvertsToCanonicalKind8(t *testing.T) {
	raw := legacyBankRawV1Fixture()
	got, err := DecodeLegacyBankSnapshotRawV1(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Root.Nodes) != 4 {
		t.Fatalf("nodes=%d", len(got.Root.Nodes))
	}
	if got.Root.Nodes[0].Object.Value != 99 || got.Root.Nodes[0].Object.ShotsCurrent != 7 {
		t.Fatalf("root=%+v", got.Root.Nodes[0].Object)
	}
	if got.Root.Nodes[1].ParentIndex == nil || *got.Root.Nodes[1].ParentIndex != 0 || got.Root.Nodes[1].ChildIndex != 0 {
		t.Fatalf("first child topology=%+v", got.Root.Nodes[1])
	}
	if got.Root.Nodes[2].ParentIndex == nil || *got.Root.Nodes[2].ParentIndex != 1 || got.Root.Nodes[2].ChildIndex != 0 {
		t.Fatalf("nested child topology=%+v", got.Root.Nodes[2])
	}
	if got.Root.Nodes[3].ParentIndex == nil || *got.Root.Nodes[3].ParentIndex != 0 || got.Root.Nodes[3].ChildIndex != 1 {
		t.Fatalf("second child topology=%+v", got.Root.Nodes[3])
	}

	wire, err := ConvertLegacyBankSnapshotRawV1(raw)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	roundTrip, err := DecodeBankSnapshotV1(wire)
	if err != nil {
		t.Fatalf("kind-8 decode: %v", err)
	}
	if !reflect.DeepEqual(roundTrip, got) {
		t.Fatalf("kind-8 projection differs:\n got=%+v\nwant=%+v", roundTrip, got)
	}
	nextID := 0
	if account, err := got.ToBankAccount(func() (string, error) {
		nextID++
		return "item-" + string(rune('0'+nextID)), nil
	}); err != nil || account.Balance != 99 || len(account.Items.Inventory) != 2 || len(account.Items.Items) != 3 {
		t.Fatalf("bank account conversion: account=%+v err=%v", account, err)
	}
}

func TestLegacyBankSnapshotRawV1MatchesExistingCEvidenceFixture(t *testing.T) {
	raw := legacyBankRawV1CFixture()
	// This is the standard SHA-256 of the exact zeroed-object fixture emitted
	// by bank_evidence_test.c. That C test also records fa885..., but that
	// string comes from its private SHA implementation and is not a portable
	// digest contract.
	if got := sha256.Sum256(raw); got != mustLegacyBankRawV1Digest("9ff5ef5b03f6c01e4167f1232e126be90dcbc1e4cf918db0207e7a62deb16cbc") {
		t.Fatalf("C evidence fixture digest changed: %x", got)
	}
	snapshot, err := DecodeLegacyBankSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Root.Nodes) != 2 || string(snapshot.Root.Nodes[0].Object.Name[:9]) != "bank-root" || string(snapshot.Root.Nodes[1].Object.Name[:4]) != "ruby" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func mustLegacyBankRawV1Digest(value string) [sha256.Size]byte {
	var digest [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != len(digest) {
		panic("invalid legacy bank digest fixture")
	}
	copy(digest[:], decoded)
	return digest
}

func TestLegacyBankSnapshotRawV1IgnoresNativePointerBytes(t *testing.T) {
	raw := legacyBankRawV1Fixture()
	wire, err := ConvertLegacyBankSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	zeroPointers := append([]byte(nil), raw...)
	for offset := 0; offset < len(zeroPointers); offset += legacyBankRawV1ObjectBytes + legacyBankRawV1CountBytes {
		for index := 344; index < legacyBankRawV1ObjectBytes; index++ {
			zeroPointers[offset+index] = 0
		}
	}
	zeroWire, err := ConvertLegacyBankSnapshotRawV1(zeroPointers)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, zeroWire) {
		t.Fatal("native pointer bytes changed the kind-8 artifact")
	}
}

func TestLegacyBankSnapshotRawV1InspectionOwnsEvidence(t *testing.T) {
	raw := legacyBankRawV1Fixture()
	report, err := InspectLegacyBankSnapshotRawV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if report.SHA256 != sha256.Sum256(raw) || !bytes.Equal(report.Source, raw) || len(report.Canonical) == 0 {
		t.Fatal("inspection evidence incomplete")
	}
	clone := report.Clone()
	clone.Source[0] ^= 0xff
	clone.Canonical[0] ^= 0xff
	clone.Snapshot.Root.Nodes[0].Object.Value++
	if bytes.Equal(clone.Source, report.Source) || bytes.Equal(clone.Canonical, report.Canonical) || clone.Snapshot.Root.Nodes[0].Object.Value == report.Snapshot.Root.Nodes[0].Object.Value {
		t.Fatal("inspection clone aliases owned values")
	}
}

func TestLegacyBankSnapshotRawV1RejectsMalformedStreams(t *testing.T) {
	base := legacyBankRawV1Fixture()
	cases := []struct {
		name  string
		input func() []byte
		want  error
	}{
		{name: "nil", input: func() []byte { return nil }, want: ErrLegacyBankSnapshotRawTruncated},
		{name: "truncated", input: func() []byte { return base[:len(base)-1] }, want: ErrLegacyBankSnapshotRawTruncated},
		{name: "trailing", input: func() []byte { return append(append([]byte(nil), base...), 0x7f) }, want: ErrLegacyBankSnapshotRawTrailingBytes},
		{name: "negative-root-count", input: func() []byte {
			value := append([]byte(nil), base...)
			binary.LittleEndian.PutUint32(value[legacyBankRawV1ObjectBytes:], ^uint32(0))
			return value
		}, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "negative-child-count", input: func() []byte {
			value := append([]byte(nil), base...)
			count := legacyBankRawV1ObjectBytes + legacyBankRawV1CountBytes + legacyBankRawV1ObjectBytes
			binary.LittleEndian.PutUint32(value[count:], ^uint32(0))
			return value
		}, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "list-limit", input: func() []byte {
			value := append([]byte(nil), base...)
			binary.LittleEndian.PutUint32(value[legacyBankRawV1ObjectBytes:], uint32(legacyBankRawV1MaxList+1))
			return value
		}, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "fixed-tail", input: func() []byte {
			value := append([]byte(nil), base...)
			value[1] = 0
			value[2] = 0x7f
			return value
		}, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "missing-terminator", input: func() []byte {
			value := append([]byte(nil), base...)
			for index := 0; index < 80; index++ {
				value[index] = 'x'
			}
			return value
		}, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "depth-limit", input: func() []byte { return legacyBankRawV1DeepFixture(legacyBankRawV1MaxDepth + 1) }, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "node-limit", input: func() []byte { return legacyBankRawV1WideFixture() }, want: ErrLegacyBankSnapshotRawMalformed},
		{name: "octet-limit", input: func() []byte { return make([]byte, legacyBankRawV1MaxOctets+1) }, want: ErrLegacyBankSnapshotRawSizeLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeLegacyBankSnapshotRawV1(tc.input()); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
		})
	}
}

func TestLegacyBankSnapshotRawV1ContractIsExplicit(t *testing.T) {
	if LegacyBankSnapshotRawV1ABI == "" || LegacyBankSnapshotRawV1ParserVersion == "" {
		t.Fatal("raw ABI/parser contract must be versioned")
	}
	if err := ValidateLegacyBankSnapshotRawV1ABI(LegacyBankSnapshotRawV1ABI); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLegacyBankSnapshotRawV1ABI(LegacyBankSnapshotRawV1ABI + ":other"); !errors.Is(err, ErrLegacyBankSnapshotRawUnsupportedABI) {
		t.Fatalf("unsupported ABI err=%v", err)
	}
	if legacyBankRawV1ObjectBytes != 376 || legacyBankRawV1CountBytes != 4 || legacyBankRawV1MaxDepth != 64 || legacyBankRawV1MaxObjects != 8192 {
		t.Fatal("raw limits/layout drifted from the audited contract")
	}
}
