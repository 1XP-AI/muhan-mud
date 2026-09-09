package world

// This file is a read-only migration boundary for one audited legacy bank
// file. The legacy writer stores a native object image followed by a native
// int child count, recursively. The bytes have no self-describing ABI, so the
// parser accepts exactly the layout below and never infers a host ABI.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// LegacyBankSnapshotRawV1ABI is the ABI fingerprint established by the
	// current C/macOS LP64 build and its bank evidence/legacy ABI tests. The
	// pointer fields are present in the native image but are deliberately not
	// interpreted by this reader.
	LegacyBankSnapshotRawV1ABI = "legacy-bank-snapshot-v1/raw-v1;endian=little;char=signed8;short=16;int=32;long=64;ptr=64;object=376;object.name=0;object.description=80;object.key=160;object.use_output=220;object.value=304;object.weight=312;object.type=314;object.adjustment=315;object.shotsmax=316;object.shotscur=318;object.ndice=320;object.sdice=322;object.pdice=324;object.armor=326;object.wearflag=327;object.magicpower=328;object.magicrealm=329;object.special=330;object.flags=332;object.questnum=340;object.first_obj=344;object.parent_obj=352;object.parent_rom=360;object.parent_crt=368"

	LegacyBankSnapshotRawV1ParserVersion = "go-bank-snapshot-raw-v1"

	legacyBankRawV1ObjectBytes = 376
	legacyBankRawV1CountBytes  = 4
	legacyBankRawV1MaxList     = 4096
	legacyBankRawV1MaxDepth    = 64
	legacyBankRawV1MaxObjects  = 8192
	// Match the existing C bank evidence octet cap. The stricter graph bounds
	// below reject an input that contains more native nodes before it can be
	// converted to a portable artifact.
	legacyBankRawV1MaxOctets = 4 * 1024 * 1024
)

var (
	ErrLegacyBankSnapshotRawUnsupportedABI = errors.New("unsupported legacy bank raw ABI")
	ErrLegacyBankSnapshotRawMalformed      = errors.New("malformed legacy bank raw snapshot")
	ErrLegacyBankSnapshotRawTruncated      = errors.New("truncated legacy bank raw snapshot")
	ErrLegacyBankSnapshotRawTrailingBytes  = errors.New("trailing legacy bank raw snapshot bytes")
	ErrLegacyBankSnapshotRawSizeLimit      = errors.New("legacy bank raw snapshot exceeds size limit")
)

// LegacyBankSnapshotRawV1Inspection is evidence for an operator-controlled
// conversion step. Source and Canonical are owned copies; neither is used by
// the game runtime or persisted by this package.
type LegacyBankSnapshotRawV1Inspection struct {
	Snapshot  BankSnapshotV1
	Canonical []byte
	Source    []byte
	SHA256    [sha256.Size]byte
}

func (i LegacyBankSnapshotRawV1Inspection) Clone() LegacyBankSnapshotRawV1Inspection {
	i.Canonical = append([]byte(nil), i.Canonical...)
	i.Source = append([]byte(nil), i.Source...)
	i.Snapshot.Root = clonePlayerSnapshotObjectGraph(i.Snapshot.Root)
	return i
}

// ValidateLegacyBankSnapshotRawV1ABI requires the exact ABI fingerprint that
// was established by the native object layout probe and focused C tests.
// Legacy files carry no ABI marker, so a different contract is unsafe.
func ValidateLegacyBankSnapshotRawV1ABI(contract string) error {
	if contract != LegacyBankSnapshotRawV1ABI {
		return ErrLegacyBankSnapshotRawUnsupportedABI
	}
	return nil
}

// InspectLegacyBankSnapshotRawV1 decodes one raw bank file, emits the
// canonical kind-8 artifact, and retains an exact source digest for review.
// It performs no account mapping, state admission, or persistence.
func InspectLegacyBankSnapshotRawV1(raw []byte) (LegacyBankSnapshotRawV1Inspection, error) {
	snapshot, err := DecodeLegacyBankSnapshotRawV1(raw)
	if err != nil {
		return LegacyBankSnapshotRawV1Inspection{}, err
	}
	canonical, err := EncodeBankSnapshotV1(snapshot)
	if err != nil {
		return LegacyBankSnapshotRawV1Inspection{}, fmt.Errorf("encode bank snapshot: %w", err)
	}
	return LegacyBankSnapshotRawV1Inspection{
		Snapshot:  snapshot,
		Canonical: canonical,
		Source:    append([]byte(nil), raw...),
		SHA256:    sha256.Sum256(raw),
	}, nil
}

// ConvertLegacyBankSnapshotRawV1 is a pure raw-to-kind-8 conversion. The
// returned bytes are canonical CDTO and are independent of raw pointer bytes.
func ConvertLegacyBankSnapshotRawV1(raw []byte) ([]byte, error) {
	snapshot, err := DecodeLegacyBankSnapshotRawV1(raw)
	if err != nil {
		return nil, err
	}
	return EncodeBankSnapshotV1(snapshot)
}

// DecodeLegacyBankSnapshotRawV1 converts the audited native object stream to
// a detached BankSnapshotV1. The native object pointer fields and padding are
// never interpreted. read_obj's shots-current clamp is reproduced before the
// portable graph is validated and encoded by callers.
func DecodeLegacyBankSnapshotRawV1(raw []byte) (BankSnapshotV1, error) {
	if len(raw) < legacyBankRawV1ObjectBytes+legacyBankRawV1CountBytes {
		return BankSnapshotV1{}, ErrLegacyBankSnapshotRawTruncated
	}
	if len(raw) > legacyBankRawV1MaxOctets {
		return BankSnapshotV1{}, ErrLegacyBankSnapshotRawSizeLimit
	}

	reader := legacyBankRawV1Reader{raw: raw}
	root, err := reader.object(nil, 1, 0)
	if err != nil {
		return BankSnapshotV1{}, err
	}
	if root.ParentIndex != nil || root.ChildIndex != 0 {
		return BankSnapshotV1{}, fmt.Errorf("%w: invalid root topology", ErrLegacyBankSnapshotRawMalformed)
	}
	graph := PlayerSnapshotObjectGraphV1{Nodes: reader.nodes}
	if reader.remaining() != 0 {
		return BankSnapshotV1{}, ErrLegacyBankSnapshotRawTrailingBytes
	}
	snapshot := BankSnapshotV1{Root: graph}
	if _, err := EncodeBankSnapshotV1(snapshot); err != nil {
		return BankSnapshotV1{}, fmt.Errorf("%w: portable graph: %v", ErrLegacyBankSnapshotRawMalformed, err)
	}
	return snapshot, nil
}

type legacyBankRawV1Reader struct {
	raw    []byte
	offset int
	nodes  []PlayerSnapshotObjectNodeV1
}

func (r *legacyBankRawV1Reader) remaining() int {
	return len(r.raw) - r.offset
}

func (r *legacyBankRawV1Reader) take(length int) ([]byte, error) {
	if length < 0 || length > r.remaining() {
		return nil, ErrLegacyBankSnapshotRawTruncated
	}
	value := r.raw[r.offset : r.offset+length]
	r.offset += length
	return value, nil
}

func (r *legacyBankRawV1Reader) count() (int, error) {
	value, err := r.take(legacyBankRawV1CountBytes)
	if err != nil {
		return 0, err
	}
	return int(int32(binary.LittleEndian.Uint32(value))), nil
}

func (r *legacyBankRawV1Reader) object(parent *uint32, depth int, childIndex uint32) (PlayerSnapshotObjectNodeV1, error) {
	if depth < 1 || depth > legacyBankRawV1MaxDepth {
		return PlayerSnapshotObjectNodeV1{}, fmt.Errorf("%w: object depth %d", ErrLegacyBankSnapshotRawMalformed, depth)
	}
	if len(r.nodes) >= legacyBankRawV1MaxObjects {
		return PlayerSnapshotObjectNodeV1{}, fmt.Errorf("%w: object count limit", ErrLegacyBankSnapshotRawMalformed)
	}
	value, err := r.take(legacyBankRawV1ObjectBytes)
	if err != nil {
		return PlayerSnapshotObjectNodeV1{}, err
	}
	object, err := decodeLegacyBankRawV1Object(value)
	if err != nil {
		return PlayerSnapshotObjectNodeV1{}, err
	}
	index := uint32(len(r.nodes))
	node := PlayerSnapshotObjectNodeV1{
		Object: object, ParentIndex: cloneRawBankParent(parent), ChildIndex: childIndex,
	}
	r.nodes = append(r.nodes, node)

	childCount, err := r.count()
	if err != nil {
		return PlayerSnapshotObjectNodeV1{}, err
	}
	if childCount < 0 || childCount > legacyBankRawV1MaxList {
		return PlayerSnapshotObjectNodeV1{}, fmt.Errorf("%w: child count %d", ErrLegacyBankSnapshotRawMalformed, childCount)
	}
	if childCount > legacyBankRawV1MaxObjects-len(r.nodes) {
		return PlayerSnapshotObjectNodeV1{}, fmt.Errorf("%w: object count %d", ErrLegacyBankSnapshotRawMalformed, childCount)
	}
	for child := 0; child < childCount; child++ {
		parentIndex := index
		if _, err := r.object(&parentIndex, depth+1, uint32(child)); err != nil {
			return PlayerSnapshotObjectNodeV1{}, err
		}
	}
	return node, nil
}

func cloneRawBankParent(parent *uint32) *uint32 {
	if parent == nil {
		return nil
	}
	value := *parent
	return &value
}

func decodeLegacyBankRawV1Object(raw []byte) (PlayerSnapshotObjectV1, error) {
	if len(raw) != legacyBankRawV1ObjectBytes {
		return PlayerSnapshotObjectV1{}, ErrLegacyBankSnapshotRawTruncated
	}
	fields := [][]byte{
		raw[0:80], raw[80:160], raw[160:180], raw[180:200], raw[200:220], raw[220:300],
	}
	for index, field := range fields {
		if err := canonicalLegacyBankRawV1Fixed(field); err != nil {
			return PlayerSnapshotObjectV1{}, fmt.Errorf("%w: object fixed field %d", ErrLegacyBankSnapshotRawMalformed, index)
		}
	}
	object := PlayerSnapshotObjectV1{
		Value:        legacyBankRawV1I64(raw, 304),
		Weight:       legacyBankRawV1I16(raw, 312),
		TypeCode:     int8(raw[314]),
		Adjustment:   int8(raw[315]),
		ShotsMax:     legacyBankRawV1I16(raw, 316),
		ShotsCurrent: legacyBankRawV1I16(raw, 318),
		DiceCount:    legacyBankRawV1I16(raw, 320),
		DiceSides:    legacyBankRawV1I16(raw, 322),
		DicePlus:     legacyBankRawV1I16(raw, 324),
		Armor:        int8(raw[326]),
		WearFlag:     int8(raw[327]),
		MagicPower:   int8(raw[328]),
		MagicRealm:   int8(raw[329]),
		Special:      legacyBankRawV1I16(raw, 330),
		QuestNum:     int8(raw[340]),
	}
	copy(object.Name[:], raw[0:80])
	copy(object.Description[:], raw[80:160])
	copy(object.Keys[0][:], raw[160:180])
	copy(object.Keys[1][:], raw[180:200])
	copy(object.Keys[2][:], raw[200:220])
	copy(object.UseOutput[:], raw[220:300])
	copy(object.Flags[:], raw[332:340])
	if object.ShotsCurrent > object.ShotsMax {
		// read_obj clamps current shots to the stored maximum before callers
		// inspect or export the object.
		object.ShotsCurrent = object.ShotsMax
	}
	return object, nil
}

func canonicalLegacyBankRawV1Fixed(raw []byte) error {
	terminator := bytes.IndexByte(raw, 0)
	if terminator < 0 {
		return ErrLegacyBankSnapshotRawMalformed
	}
	for _, value := range raw[terminator+1:] {
		if value != 0 {
			return ErrLegacyBankSnapshotRawMalformed
		}
	}
	return nil
}

func legacyBankRawV1I16(raw []byte, offset int) int16 {
	return int16(binary.LittleEndian.Uint16(raw[offset : offset+2]))
}

func legacyBankRawV1I64(raw []byte, offset int) int64 {
	return int64(binary.LittleEndian.Uint64(raw[offset : offset+8]))
}
