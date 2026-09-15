package world

// This file is a migration-only reader for the legacy C player save format.
// It deliberately accepts one explicitly audited native layout and converts
// it immediately to the pointer-free PlayerSnapshotV1 representation.  The
// Go game runtime never reads this format and no native pointer or password is
// published by the API.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// LegacyPlayerSnapshotRawV1ABI is the only native layout accepted by this
	// reader.  Raw files have no self-describing ABI marker, so callers must
	// bind the source to this contract during collection/approval.
	LegacyPlayerSnapshotRawV1ABI = "legacy-player-snapshot-v1/raw-v1;endian=little;char=8;short=16;int=32;long=64;ptr=64;creature=1952;object=376;creature.level=318;creature.type=319;creature.hpmax=332;creature.hpcur=334;creature.mpmax=336;creature.mpcur=338;creature.gold=352;creature.first_obj=1920;object.value=304;object.shotsmax=316;object.shotscur=318;object.first_obj=344"

	LegacyPlayerSnapshotRawV1ParserVersion = "go-player-snapshot-raw-v1"

	legacyPlayerRawV1CreatureBytes = 1952
	legacyPlayerRawV1ObjectBytes   = 376
	legacyPlayerRawV1CountBytes    = 4
	legacyPlayerRawV1PasswordOff   = 240
	legacyPlayerRawV1PasswordLen   = 15
	legacyPlayerRawV1MaxList       = 4096
	legacyPlayerRawV1MaxDepth      = 64
	legacyPlayerRawV1MaxObjects    = 8192
	// Every persisted object is followed by one native int child count.  This
	// bound is arithmetic rather than a looser 64 MiB transport bound so a raw
	// parser cannot retain an impossible oversized input.
	legacyPlayerRawV1MaxBytes = legacyPlayerRawV1CreatureBytes + legacyPlayerRawV1CountBytes +
		legacyPlayerRawV1MaxObjects*(legacyPlayerRawV1ObjectBytes+legacyPlayerRawV1CountBytes)
)

var (
	ErrLegacyPlayerSnapshotRawUnsupportedABI = errors.New("unsupported legacy player raw ABI")
	ErrLegacyPlayerSnapshotRawMalformed      = errors.New("malformed legacy player raw snapshot")
	ErrLegacyPlayerSnapshotRawTruncated      = errors.New("truncated legacy player raw snapshot")
	ErrLegacyPlayerSnapshotRawTrailingBytes  = errors.New("trailing legacy player raw snapshot bytes")
	// ErrLegacyPlayerPassword is a credential mismatch against the native
	// password field. It is not proof of identity by name.
	ErrLegacyPlayerPassword = errors.New("legacy player password mismatch")
)

// ValidateLegacyPlayerSnapshotRawV1ABI checks the operator-supplied source
// contract before a raw file is admitted.  Native bytes do not carry an ABI
// marker, so silently accepting a different layout would be unsafe.
func ValidateLegacyPlayerSnapshotRawV1ABI(contract string) error {
	if contract != LegacyPlayerSnapshotRawV1ABI {
		return ErrLegacyPlayerSnapshotRawUnsupportedABI
	}
	return nil
}

// LegacyPlayerSnapshotRawV1Inspection is the raw-file analogue of
// PlayerSnapshotV1Inspection.  Source is retained only in memory for an
// explicit migration evidence handoff; password bytes never enter Snapshot.
type LegacyPlayerSnapshotRawV1Inspection struct {
	Snapshot PlayerSnapshotV1
	Source   []byte
	SHA256   [sha256.Size]byte
}

func (i LegacyPlayerSnapshotRawV1Inspection) Clone() LegacyPlayerSnapshotRawV1Inspection {
	i.Source = append([]byte(nil), i.Source...)
	i.Snapshot = clonePlayerSnapshot(i.Snapshot)
	return i
}

// InspectLegacyPlayerSnapshotRawV1 parses one complete legacy player file,
// applies the same load-time clamping as read_crt_player, and returns an
// owned portable snapshot plus source evidence.  The parser is deterministic
// and publishes no partial snapshot when any bound or record is invalid.
func InspectLegacyPlayerSnapshotRawV1(raw []byte) (LegacyPlayerSnapshotRawV1Inspection, error) {
	snapshot, err := DecodeLegacyPlayerSnapshotRawV1(raw)
	if err != nil {
		return LegacyPlayerSnapshotRawV1Inspection{}, err
	}
	return LegacyPlayerSnapshotRawV1Inspection{
		Snapshot: snapshot,
		Source:   append([]byte(nil), raw...),
		SHA256:   sha256.Sum256(raw),
	}, nil
}

// DecodeLegacyPlayerSnapshotRawV1 converts the audited little-endian native
// creature/object stream to PlayerSnapshotV1.  The raw ABI is intentionally
// not inferred from the host architecture; it is the fixed contract above.
func DecodeLegacyPlayerSnapshotRawV1(raw []byte) (PlayerSnapshotV1, error) {
	if len(raw) < legacyPlayerRawV1CreatureBytes+legacyPlayerRawV1CountBytes {
		return PlayerSnapshotV1{}, ErrLegacyPlayerSnapshotRawTruncated
	}
	if len(raw) > legacyPlayerRawV1MaxBytes {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: file exceeds audited object bound", ErrLegacyPlayerSnapshotRawMalformed)
	}

	reader := legacyPlayerRawV1Reader{raw: raw}
	creature, err := reader.take(legacyPlayerRawV1CreatureBytes)
	if err != nil {
		return PlayerSnapshotV1{}, err
	}
	snapshot, err := decodeLegacyPlayerRawV1Creature(creature)
	if err != nil {
		return PlayerSnapshotV1{}, err
	}

	rootCount, err := reader.count()
	if err != nil {
		return PlayerSnapshotV1{}, err
	}
	if rootCount < 0 || rootCount > legacyPlayerRawV1MaxList {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: root count %d", ErrLegacyPlayerSnapshotRawMalformed, rootCount)
	}
	if rootCount > legacyPlayerRawV1MaxObjects {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: object count %d", ErrLegacyPlayerSnapshotRawMalformed, rootCount)
	}

	nodes := make([]PlayerSnapshotObjectNodeV1, 0, rootCount)
	for index := 0; index < rootCount; index++ {
		if err := reader.object(&nodes, 1, nil, uint32(index)); err != nil {
			return PlayerSnapshotV1{}, err
		}
	}
	snapshot.Inventory = PlayerSnapshotObjectGraphV1{Nodes: nodes}

	if reader.remaining() != 0 {
		return PlayerSnapshotV1{}, ErrLegacyPlayerSnapshotRawTrailingBytes
	}
	if err := validatePlayerSnapshot(snapshot); err != nil {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: %v", ErrLegacyPlayerSnapshotRawMalformed, err)
	}
	return snapshot, nil
}

type legacyPlayerRawV1Reader struct {
	raw     []byte
	offset  int
	objects int
}

func (r *legacyPlayerRawV1Reader) remaining() int { return len(r.raw) - r.offset }

func (r *legacyPlayerRawV1Reader) take(length int) ([]byte, error) {
	if length < 0 || length > r.remaining() {
		return nil, ErrLegacyPlayerSnapshotRawTruncated
	}
	value := r.raw[r.offset : r.offset+length]
	r.offset += length
	return value, nil
}

func (r *legacyPlayerRawV1Reader) count() (int, error) {
	value, err := r.take(legacyPlayerRawV1CountBytes)
	if err != nil {
		return 0, err
	}
	return int(int32(binary.LittleEndian.Uint32(value))), nil
}

func (r *legacyPlayerRawV1Reader) object(nodes *[]PlayerSnapshotObjectNodeV1, depth int, parent *uint32, childIndex uint32) error {
	if depth < 1 || depth > legacyPlayerRawV1MaxDepth {
		return fmt.Errorf("%w: object depth %d", ErrLegacyPlayerSnapshotRawMalformed, depth)
	}
	if r.objects >= legacyPlayerRawV1MaxObjects {
		return fmt.Errorf("%w: object count limit", ErrLegacyPlayerSnapshotRawMalformed)
	}
	value, err := r.take(legacyPlayerRawV1ObjectBytes)
	if err != nil {
		return err
	}
	object, err := decodeLegacyPlayerRawV1Object(value)
	if err != nil {
		return err
	}
	index := uint32(len(*nodes))
	r.objects++
	*nodes = append(*nodes, PlayerSnapshotObjectNodeV1{
		Object: object, ParentIndex: cloneRawParent(parent), ChildIndex: childIndex,
	})

	childCount, err := r.count()
	if err != nil {
		return err
	}
	if childCount < 0 || childCount > legacyPlayerRawV1MaxList || childCount > legacyPlayerRawV1MaxObjects-r.objects {
		return fmt.Errorf("%w: child count %d", ErrLegacyPlayerSnapshotRawMalformed, childCount)
	}
	for child := 0; child < childCount; child++ {
		parentIndex := index
		if err := r.object(nodes, depth+1, &parentIndex, uint32(child)); err != nil {
			return err
		}
	}
	return nil
}

func cloneRawParent(parent *uint32) *uint32 {
	if parent == nil {
		return nil
	}
	value := *parent
	return &value
}

func decodeLegacyPlayerRawV1Creature(raw []byte) (PlayerSnapshotV1, error) {
	if len(raw) != legacyPlayerRawV1CreatureBytes {
		return PlayerSnapshotV1{}, ErrLegacyPlayerSnapshotRawTruncated
	}
	name, err := canonicalLegacyPlayerRawV1Fixed(raw[0:80])
	if err != nil {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: creature name", err)
	}
	description, err := canonicalLegacyPlayerRawV1Fixed(raw[80:160])
	if err != nil {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: creature description", err)
	}
	talk, err := canonicalLegacyPlayerRawV1Fixed(raw[160:240])
	if err != nil {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: creature talk", err)
	}
	// The password is intentionally validated and then discarded.  It is part
	// of the native reader's structural contract but never part of CDTO.
	if _, err := canonicalLegacyPlayerRawV1Fixed(raw[legacyPlayerRawV1PasswordOff : legacyPlayerRawV1PasswordOff+legacyPlayerRawV1PasswordLen]); err != nil {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: creature password", ErrLegacyPlayerSnapshotRawMalformed)
	}
	keys := [3][20]byte{}
	for index := range keys {
		key, keyErr := canonicalLegacyPlayerRawV1Fixed(raw[255+index*20 : 255+(index+1)*20])
		if keyErr != nil {
			return PlayerSnapshotV1{}, fmt.Errorf("%w: creature key %d", ErrLegacyPlayerSnapshotRawMalformed, index)
		}
		copy(keys[index][:], key)
	}
	if raw[319] != 0 { // PLAYER in the audited C build is zero.
		return PlayerSnapshotV1{}, fmt.Errorf("%w: creature type %d", ErrLegacyPlayerSnapshotRawMalformed, raw[319])
	}

	snapshot := PlayerSnapshotV1{
		Name:         array80(name),
		Description:  array80(description),
		Talk:         array80(talk),
		Keys:         keys,
		Level:        raw[318],
		TypeCode:     int8(raw[319]),
		Class:        int8(raw[320]),
		Race:         int8(raw[321]),
		NumWander:    int8(raw[322]),
		Alignment:    legacyPlayerRawV1I16(raw, 324),
		Strength:     int8(raw[326]),
		Dexterity:    int8(raw[327]),
		Constitution: int8(raw[328]),
		Intelligence: int8(raw[329]),
		Piety:        int8(raw[330]),
		HPMax:        legacyPlayerRawV1I16(raw, 332),
		HPCurrent:    legacyPlayerRawV1I16(raw, 334),
		MPMax:        legacyPlayerRawV1I16(raw, 336),
		MPCurrent:    legacyPlayerRawV1I16(raw, 338),
		Armor:        int8(raw[340]),
		Thaco:        int8(raw[341]),
		Experience:   legacyPlayerRawV1I64(raw, 344),
		Gold:         legacyPlayerRawV1I64(raw, 352),
		DiceCount:    legacyPlayerRawV1I16(raw, 360),
		DiceSides:    legacyPlayerRawV1I16(raw, 362),
		DicePlus:     legacyPlayerRawV1I16(raw, 364),
		Special:      legacyPlayerRawV1I16(raw, 366),
		QuestNum:     int8(raw[480]),
		RoomNumber:   legacyPlayerRawV1I16(raw, 502),
	}
	for index := range snapshot.Proficiency {
		snapshot.Proficiency[index] = legacyPlayerRawV1I64(raw, 368+index*8)
	}
	for index := range snapshot.Realm {
		snapshot.Realm[index] = legacyPlayerRawV1I64(raw, 408+index*8)
	}
	copy(snapshot.Spells[:], raw[440:456])
	copy(snapshot.Flags[:], raw[456:464])
	copy(snapshot.Quests[:], raw[464:480])
	for index := range snapshot.Carry {
		snapshot.Carry[index] = legacyPlayerRawV1I16(raw, 482+index*2)
	}
	for index := range snapshot.Daily {
		at := 664 + index*16
		snapshot.Daily[index] = PlayerSnapshotDailyV1{
			Max: raw[at], Current: raw[at+1], LastTime: legacyPlayerRawV1I64(raw, at+8),
		}
	}
	for index := range snapshot.LastTime {
		at := 824 + index*24
		snapshot.LastTime[index] = PlayerSnapshotLastTimeV1{
			Interval: legacyPlayerRawV1I64(raw, at),
			LastUsed: legacyPlayerRawV1I64(raw, at+8),
			Misc:     legacyPlayerRawV1I16(raw, at+16),
		}
	}
	// fd, ready[], and every native pointer from the creature image are
	// deliberately ignored. read_crt_player detaches those values as well.
	if snapshot.HPCurrent > snapshot.HPMax {
		snapshot.HPCurrent = snapshot.HPMax
	}
	if snapshot.MPCurrent > snapshot.MPMax {
		snapshot.MPCurrent = snapshot.MPMax
	}
	return snapshot, nil
}

func decodeLegacyPlayerRawV1Object(raw []byte) (PlayerSnapshotObjectV1, error) {
	if len(raw) != legacyPlayerRawV1ObjectBytes {
		return PlayerSnapshotObjectV1{}, ErrLegacyPlayerSnapshotRawTruncated
	}
	name, err := canonicalLegacyPlayerRawV1Fixed(raw[0:80])
	if err != nil {
		return PlayerSnapshotObjectV1{}, fmt.Errorf("%w: object name", ErrLegacyPlayerSnapshotRawMalformed)
	}
	description, err := canonicalLegacyPlayerRawV1Fixed(raw[80:160])
	if err != nil {
		return PlayerSnapshotObjectV1{}, fmt.Errorf("%w: object description", ErrLegacyPlayerSnapshotRawMalformed)
	}
	useOutput, err := canonicalLegacyPlayerRawV1Fixed(raw[220:300])
	if err != nil {
		return PlayerSnapshotObjectV1{}, fmt.Errorf("%w: object use output", ErrLegacyPlayerSnapshotRawMalformed)
	}
	keys := [3][20]byte{}
	for index := range keys {
		key, keyErr := canonicalLegacyPlayerRawV1Fixed(raw[160+index*20 : 160+(index+1)*20])
		if keyErr != nil {
			return PlayerSnapshotObjectV1{}, fmt.Errorf("%w: object key %d", ErrLegacyPlayerSnapshotRawMalformed, index)
		}
		copy(keys[index][:], key)
	}
	object := PlayerSnapshotObjectV1{
		Name:         array80(name),
		Description:  array80(description),
		Keys:         keys,
		UseOutput:    array80(useOutput),
		Value:        legacyPlayerRawV1I64(raw, 304),
		Weight:       legacyPlayerRawV1I16(raw, 312),
		TypeCode:     int8(raw[314]),
		Adjustment:   int8(raw[315]),
		ShotsMax:     legacyPlayerRawV1I16(raw, 316),
		ShotsCurrent: legacyPlayerRawV1I16(raw, 318),
		DiceCount:    legacyPlayerRawV1I16(raw, 320),
		DiceSides:    legacyPlayerRawV1I16(raw, 322),
		DicePlus:     legacyPlayerRawV1I16(raw, 324),
		Armor:        int8(raw[326]),
		WearFlag:     int8(raw[327]),
		MagicPower:   int8(raw[328]),
		MagicRealm:   int8(raw[329]),
		Special:      legacyPlayerRawV1I16(raw, 330),
		QuestNum:     int8(raw[340]),
	}
	copy(object.Flags[:], raw[332:340])
	if object.ShotsCurrent > object.ShotsMax {
		object.ShotsCurrent = object.ShotsMax
	}
	return object, nil
}

func canonicalLegacyPlayerRawV1Fixed(raw []byte) ([]byte, error) {
	terminator := -1
	for index, value := range raw {
		if value == 0 {
			terminator = index
			break
		}
	}
	if terminator < 0 {
		return nil, ErrLegacyPlayerSnapshotRawMalformed
	}
	canonical := make([]byte, len(raw))
	copy(canonical, raw[:terminator+1])
	return canonical, nil
}

// LegacyPlayerPasswordMatches compares a candidate with the NUL-terminated
// native password field using C strcmp semantics. The password never enters
// PlayerSnapshotV1. Malformed password fields fail closed rather than matching.
func LegacyPlayerPasswordMatches(raw, password []byte) (bool, error) {
	if len(raw) < legacyPlayerRawV1CreatureBytes {
		return false, ErrLegacyPlayerSnapshotRawTruncated
	}
	stored, err := canonicalLegacyPlayerRawV1Fixed(raw[legacyPlayerRawV1PasswordOff : legacyPlayerRawV1PasswordOff+legacyPlayerRawV1PasswordLen])
	if err != nil {
		return false, fmt.Errorf("%w: creature password", ErrLegacyPlayerSnapshotRawMalformed)
	}
	end := bytes.IndexByte(stored, 0)
	if end < 0 {
		return false, fmt.Errorf("%w: creature password", ErrLegacyPlayerSnapshotRawMalformed)
	}
	return bytes.Equal(stored[:end], password), nil
}

// CanonicalPlayerSnapshotFromLegacyRaw verifies the native password, decodes
// the audited C player file, and returns a canonical PlayerSnapshotV1 CDTO.
// Ownership is not granted by name: a password mismatch is ErrLegacyPlayerPassword.
func CanonicalPlayerSnapshotFromLegacyRaw(raw, password []byte) ([]byte, error) {
	matched, err := LegacyPlayerPasswordMatches(raw, password)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, ErrLegacyPlayerPassword
	}
	snapshot, err := DecodeLegacyPlayerSnapshotRawV1(raw)
	if err != nil {
		return nil, err
	}
	return EncodePlayerSnapshotV1(snapshot)
}

func legacyPlayerRawV1I16(raw []byte, offset int) int16 {
	return int16(binary.LittleEndian.Uint16(raw[offset : offset+2]))
}

func legacyPlayerRawV1I64(raw []byte, offset int) int64 {
	return int64(binary.LittleEndian.Uint64(raw[offset : offset+8]))
}
