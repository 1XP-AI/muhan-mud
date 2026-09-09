package world

// This file is the Go-side reader for the portable CDTO PlayerSnapshotV1
// artifact.  It is deliberately an allocation-only migration boundary: it
// does not read legacy files, invoke C/Rust, or admit a character to a live
// session.  Callers must explicitly convert the reviewed snapshot into a
// PlayerState and persist that transition through the normal world receipt.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf8"
)

var (
	ErrPlayerSnapshotMalformed       = errors.New("malformed player snapshot CDTO")
	ErrPlayerSnapshotTruncated       = errors.New("truncated player snapshot CDTO")
	ErrPlayerSnapshotTrailingBytes   = errors.New("trailing player snapshot CDTO bytes")
	ErrPlayerSnapshotDigestMismatch  = errors.New("player snapshot CDTO digest mismatch")
	ErrPlayerSnapshotNonCanonical    = errors.New("non-canonical player snapshot CDTO")
	ErrPlayerSnapshotGraphInvalid    = errors.New("invalid player snapshot object graph")
	ErrPlayerSnapshotNumericOverflow = errors.New("player snapshot numeric overflow")
	ErrPlayerSnapshotTextConversion  = errors.New("player snapshot text conversion failed")
	ErrPlayerSnapshotItemConversion  = errors.New("player snapshot item conversion failed")
)

const (
	playerSnapshotCDTOMagic         = "MUHCDTO\x00"
	playerSnapshotCDTOWireVersion   = uint16(1)
	playerSnapshotKind              = uint16(7)
	playerSnapshotObjectGraphKind   = uint16(6)
	bankSnapshotKind                = uint16(8)
	playerSnapshotPrefixLength      = 16
	playerSnapshotDigestLength      = sha256.Size
	playerSnapshotFieldHeaderLength = 7
	playerSnapshotPayloadLimit      = 4 * 1024 * 1024
	playerSnapshotMaxEnvelope       = playerSnapshotPrefixLength + playerSnapshotPayloadLimit + playerSnapshotDigestLength
	playerSnapshotMaxFields         = 65536
	playerSnapshotMaxListItems      = 4096
	playerSnapshotMaxGraphDepth     = 64
	playerSnapshotMaxGraphNodes     = 8192
	playerSnapshotGraphNodeLength   = 349
)

const (
	playerSnapshotTypeU8    = uint8(1)
	playerSnapshotTypeU16   = uint8(2)
	playerSnapshotTypeU32   = uint8(3)
	playerSnapshotTypeU64   = uint8(4)
	playerSnapshotTypeI8    = uint8(5)
	playerSnapshotTypeI16   = uint8(6)
	playerSnapshotTypeI32   = uint8(7)
	playerSnapshotTypeI64   = uint8(8)
	playerSnapshotTypeBytes = uint8(9)
	playerSnapshotTypeText  = uint8(10)
	playerSnapshotTypeBool  = uint8(11)
	playerSnapshotOptional  = uint8(0x80)
)

// PlayerSnapshotV1 is the fully-owned, pointer-free CDTO projection of one
// legacy player.  Fixed strings retain their canonical NUL padding until the
// explicit conversion boundary so source bytes can be audited exactly.
type PlayerSnapshotV1 struct {
	Name         [80]byte
	Description  [80]byte
	Talk         [80]byte
	Keys         [3][20]byte
	Level        byte
	TypeCode     int8
	Class        int8
	Race         int8
	NumWander    int8
	Alignment    int16
	Strength     int8
	Dexterity    int8
	Constitution int8
	Intelligence int8
	Piety        int8
	HPMax        int16
	HPCurrent    int16
	MPMax        int16
	MPCurrent    int16
	Armor        int8
	Thaco        int8
	Experience   int64
	Gold         int64
	DiceCount    int16
	DiceSides    int16
	DicePlus     int16
	Special      int16
	Proficiency  [5]int64
	Realm        [4]int64
	Spells       [16]byte
	Flags        [8]byte
	Quests       [16]byte
	QuestNum     int8
	Carry        [10]int16
	RoomNumber   int16
	Daily        [10]PlayerSnapshotDailyV1
	LastTime     [45]PlayerSnapshotLastTimeV1
	Inventory    PlayerSnapshotObjectGraphV1
}

type PlayerSnapshotDailyV1 struct {
	Max      byte
	Current  byte
	LastTime int64
}

type PlayerSnapshotLastTimeV1 struct {
	Interval int64
	LastUsed int64
	Misc     int16
}

type PlayerSnapshotObjectV1 struct {
	Name         [80]byte
	Description  [80]byte
	Keys         [3][20]byte
	UseOutput    [80]byte
	Value        int64
	Weight       int16
	TypeCode     int8
	Adjustment   int8
	ShotsMax     int16
	ShotsCurrent int16
	DiceCount    int16
	DiceSides    int16
	DicePlus     int16
	Armor        int8
	WearFlag     int8
	MagicPower   int8
	MagicRealm   int8
	Special      int16
	Flags        [8]byte
	QuestNum     int8
}

type PlayerSnapshotObjectNodeV1 struct {
	Object      PlayerSnapshotObjectV1
	ParentIndex *uint32
	ChildIndex  uint32
}

type PlayerSnapshotObjectGraphV1 struct {
	Nodes []PlayerSnapshotObjectNodeV1
}

// PlayerSnapshotV1Inspection carries the immutable source bytes and digest as
// migration evidence.  Source is never sent to a game client.
type PlayerSnapshotV1Inspection struct {
	Snapshot PlayerSnapshotV1
	Source   []byte
	SHA256   [sha256.Size]byte
}

func (i PlayerSnapshotV1Inspection) Clone() PlayerSnapshotV1Inspection {
	i.Source = append([]byte(nil), i.Source...)
	i.Snapshot = clonePlayerSnapshot(i.Snapshot)
	return i
}

type playerSnapshotField struct {
	id    uint16
	tag   uint8
	value []byte
}

type playerSnapshotRecord struct {
	kind   uint16
	fields []playerSnapshotField
}

// DecodePlayerSnapshotV1 validates one complete canonical CDTO envelope and
// returns only owned Go values.  Every schema and graph bound is checked before
// the result is published.
func DecodePlayerSnapshotV1(raw []byte) (PlayerSnapshotV1, error) {
	record, err := decodePlayerSnapshotRecord(raw)
	if err != nil {
		return PlayerSnapshotV1{}, err
	}
	if record.kind != playerSnapshotKind || len(record.fields) != 40 {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: expected player snapshot kind and 40 fields", ErrPlayerSnapshotMalformed)
	}
	expected := [40]struct {
		tag uint8
		len int
	}{
		{playerSnapshotTypeBytes, 80}, {playerSnapshotTypeBytes, 80}, {playerSnapshotTypeBytes, 80},
		{playerSnapshotTypeBytes, 20}, {playerSnapshotTypeBytes, 20}, {playerSnapshotTypeBytes, 20},
		{playerSnapshotTypeU8, 1}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1},
		{playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeI16, 2},
		{playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1},
		{playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeI16, 2},
		{playerSnapshotTypeI16, 2}, {playerSnapshotTypeI16, 2}, {playerSnapshotTypeI16, 2},
		{playerSnapshotTypeI8, 1}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeI64, 8},
		{playerSnapshotTypeI64, 8}, {playerSnapshotTypeI16, 2}, {playerSnapshotTypeI16, 2},
		{playerSnapshotTypeI16, 2}, {playerSnapshotTypeI16, 2}, {playerSnapshotTypeBytes, 40},
		{playerSnapshotTypeBytes, 32}, {playerSnapshotTypeBytes, 16}, {playerSnapshotTypeBytes, 8},
		{playerSnapshotTypeBytes, 16}, {playerSnapshotTypeI8, 1}, {playerSnapshotTypeBytes, 20},
		{playerSnapshotTypeI16, 2}, {playerSnapshotTypeBytes, 100}, {playerSnapshotTypeBytes, 810},
		{playerSnapshotTypeBytes, 0},
	}
	for index, rule := range expected {
		field := record.fields[index]
		if field.id != uint16(index+1) || field.tag != rule.tag || (index != 39 && len(field.value) != rule.len) {
			return PlayerSnapshotV1{}, fmt.Errorf("%w: field %d", ErrPlayerSnapshotMalformed, index+1)
		}
	}
	if len(record.fields[39].value) == 0 {
		return PlayerSnapshotV1{}, fmt.Errorf("%w: empty inventory graph", ErrPlayerSnapshotGraphInvalid)
	}

	value := func(index int) []byte { return record.fields[index].value }
	snapshot := PlayerSnapshotV1{
		Name:         array80(value(0)),
		Description:  array80(value(1)),
		Talk:         array80(value(2)),
		Keys:         [3][20]byte{array20(value(3)), array20(value(4)), array20(value(5))},
		Level:        value(6)[0],
		TypeCode:     int8(value(7)[0]),
		Class:        int8(value(8)[0]),
		Race:         int8(value(9)[0]),
		NumWander:    int8(value(10)[0]),
		Alignment:    int16(binary.BigEndian.Uint16(value(11))),
		Strength:     int8(value(12)[0]),
		Dexterity:    int8(value(13)[0]),
		Constitution: int8(value(14)[0]),
		Intelligence: int8(value(15)[0]),
		Piety:        int8(value(16)[0]),
		HPMax:        int16(binary.BigEndian.Uint16(value(17))),
		HPCurrent:    int16(binary.BigEndian.Uint16(value(18))),
		MPMax:        int16(binary.BigEndian.Uint16(value(19))),
		MPCurrent:    int16(binary.BigEndian.Uint16(value(20))),
		Armor:        int8(value(21)[0]),
		Thaco:        int8(value(22)[0]),
		Experience:   int64(binary.BigEndian.Uint64(value(23))),
		Gold:         int64(binary.BigEndian.Uint64(value(24))),
		DiceCount:    int16(binary.BigEndian.Uint16(value(25))),
		DiceSides:    int16(binary.BigEndian.Uint16(value(26))),
		DicePlus:     int16(binary.BigEndian.Uint16(value(27))),
		Special:      int16(binary.BigEndian.Uint16(value(28))),
		Proficiency:  readI64Array5(value(29)),
		Realm:        readI64Array4(value(30)),
		Spells:       array16(value(31)),
		Flags:        array8(value(32)),
		Quests:       array16(value(33)),
		QuestNum:     int8(value(34)[0]),
		Carry:        readI16Array(value(35), 10),
		RoomNumber:   int16(binary.BigEndian.Uint16(value(36))),
		Daily:        readDailyArray(value(37)),
		LastTime:     readLastTimeArray(value(38)),
	}
	snapshot.Inventory, err = decodePlayerSnapshotObjectGraph(value(39))
	if err != nil {
		return PlayerSnapshotV1{}, err
	}
	if err := validatePlayerSnapshot(snapshot); err != nil {
		return PlayerSnapshotV1{}, err
	}
	canonical, err := EncodePlayerSnapshotV1(snapshot)
	if err != nil {
		return PlayerSnapshotV1{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return PlayerSnapshotV1{}, ErrPlayerSnapshotNonCanonical
	}
	return snapshot, nil
}

// InspectPlayerSnapshotV1 is DecodePlayerSnapshotV1 plus immutable source
// evidence for an importer or audit record.
func InspectPlayerSnapshotV1(raw []byte) (PlayerSnapshotV1Inspection, error) {
	snapshot, err := DecodePlayerSnapshotV1(raw)
	if err != nil {
		return PlayerSnapshotV1Inspection{}, err
	}
	return PlayerSnapshotV1Inspection{
		Snapshot: snapshot,
		Source:   append([]byte(nil), raw...),
		SHA256:   sha256.Sum256(raw),
	}, nil
}

// EncodePlayerSnapshotV1 emits the canonical CDTO v1 wire representation. It
// exists for deterministic differential tests and never performs persistence.
func EncodePlayerSnapshotV1(snapshot PlayerSnapshotV1) ([]byte, error) {
	if err := validatePlayerSnapshot(snapshot); err != nil {
		return nil, err
	}
	graph, err := encodePlayerSnapshotObjectGraph(snapshot.Inventory)
	if err != nil {
		return nil, err
	}
	fields := make([]playerSnapshotField, 0, 40)
	add := func(id uint16, tag uint8, value []byte) {
		fields = append(fields, playerSnapshotField{id: id, tag: tag, value: value})
	}
	add(1, playerSnapshotTypeBytes, snapshot.Name[:])
	add(2, playerSnapshotTypeBytes, snapshot.Description[:])
	add(3, playerSnapshotTypeBytes, snapshot.Talk[:])
	add(4, playerSnapshotTypeBytes, snapshot.Keys[0][:])
	add(5, playerSnapshotTypeBytes, snapshot.Keys[1][:])
	add(6, playerSnapshotTypeBytes, snapshot.Keys[2][:])
	add(7, playerSnapshotTypeU8, []byte{snapshot.Level})
	add(8, playerSnapshotTypeI8, []byte{byte(snapshot.TypeCode)})
	add(9, playerSnapshotTypeI8, []byte{byte(snapshot.Class)})
	add(10, playerSnapshotTypeI8, []byte{byte(snapshot.Race)})
	add(11, playerSnapshotTypeI8, []byte{byte(snapshot.NumWander)})
	add(12, playerSnapshotTypeI16, i16Bytes(snapshot.Alignment))
	add(13, playerSnapshotTypeI8, []byte{byte(snapshot.Strength)})
	add(14, playerSnapshotTypeI8, []byte{byte(snapshot.Dexterity)})
	add(15, playerSnapshotTypeI8, []byte{byte(snapshot.Constitution)})
	add(16, playerSnapshotTypeI8, []byte{byte(snapshot.Intelligence)})
	add(17, playerSnapshotTypeI8, []byte{byte(snapshot.Piety)})
	add(18, playerSnapshotTypeI16, i16Bytes(snapshot.HPMax))
	add(19, playerSnapshotTypeI16, i16Bytes(snapshot.HPCurrent))
	add(20, playerSnapshotTypeI16, i16Bytes(snapshot.MPMax))
	add(21, playerSnapshotTypeI16, i16Bytes(snapshot.MPCurrent))
	add(22, playerSnapshotTypeI8, []byte{byte(snapshot.Armor)})
	add(23, playerSnapshotTypeI8, []byte{byte(snapshot.Thaco)})
	add(24, playerSnapshotTypeI64, i64Bytes(snapshot.Experience))
	add(25, playerSnapshotTypeI64, i64Bytes(snapshot.Gold))
	add(26, playerSnapshotTypeI16, i16Bytes(snapshot.DiceCount))
	add(27, playerSnapshotTypeI16, i16Bytes(snapshot.DiceSides))
	add(28, playerSnapshotTypeI16, i16Bytes(snapshot.DicePlus))
	add(29, playerSnapshotTypeI16, i16Bytes(snapshot.Special))
	add(30, playerSnapshotTypeBytes, i64ArrayBytes(snapshot.Proficiency[:]))
	add(31, playerSnapshotTypeBytes, i64ArrayBytes(snapshot.Realm[:]))
	add(32, playerSnapshotTypeBytes, append([]byte(nil), snapshot.Spells[:]...))
	add(33, playerSnapshotTypeBytes, append([]byte(nil), snapshot.Flags[:]...))
	add(34, playerSnapshotTypeBytes, append([]byte(nil), snapshot.Quests[:]...))
	add(35, playerSnapshotTypeI8, []byte{byte(snapshot.QuestNum)})
	add(36, playerSnapshotTypeBytes, i16ArrayBytes(snapshot.Carry[:]))
	add(37, playerSnapshotTypeI16, i16Bytes(snapshot.RoomNumber))
	add(38, playerSnapshotTypeBytes, dailyBytes(snapshot.Daily))
	add(39, playerSnapshotTypeBytes, lastTimeBytes(snapshot.LastTime))
	add(40, playerSnapshotTypeBytes, graph)
	return encodePlayerSnapshotRecord(playerSnapshotKind, fields)
}

func decodePlayerSnapshotRecord(raw []byte) (playerSnapshotRecord, error) {
	if len(raw) > playerSnapshotMaxEnvelope {
		return playerSnapshotRecord{}, fmt.Errorf("%w: envelope exceeds limit", ErrPlayerSnapshotMalformed)
	}
	if len(raw) < playerSnapshotPrefixLength {
		return playerSnapshotRecord{}, ErrPlayerSnapshotTruncated
	}
	if string(raw[:8]) != playerSnapshotCDTOMagic {
		return playerSnapshotRecord{}, fmt.Errorf("%w: bad magic", ErrPlayerSnapshotMalformed)
	}
	if binary.BigEndian.Uint16(raw[8:10]) != playerSnapshotCDTOWireVersion {
		return playerSnapshotRecord{}, fmt.Errorf("%w: unsupported wire version", ErrPlayerSnapshotMalformed)
	}
	kind := binary.BigEndian.Uint16(raw[10:12])
	if kind != playerSnapshotKind && kind != playerSnapshotObjectGraphKind && kind != bankSnapshotKind {
		return playerSnapshotRecord{}, fmt.Errorf("%w: unsupported kind %d", ErrPlayerSnapshotMalformed, kind)
	}
	payloadLength := int(binary.BigEndian.Uint32(raw[12:16]))
	if payloadLength > playerSnapshotPayloadLimit {
		return playerSnapshotRecord{}, fmt.Errorf("%w: payload exceeds limit", ErrPlayerSnapshotMalformed)
	}
	payloadEnd := playerSnapshotPrefixLength + payloadLength
	expectedEnd := payloadEnd + playerSnapshotDigestLength
	if len(raw) < expectedEnd {
		return playerSnapshotRecord{}, ErrPlayerSnapshotTruncated
	}
	if len(raw) > expectedEnd {
		return playerSnapshotRecord{}, ErrPlayerSnapshotTrailingBytes
	}
	digest := sha256.Sum256(raw[playerSnapshotPrefixLength:payloadEnd])
	if !bytes.Equal(digest[:], raw[payloadEnd:expectedEnd]) {
		return playerSnapshotRecord{}, ErrPlayerSnapshotDigestMismatch
	}
	payload := raw[playerSnapshotPrefixLength:payloadEnd]
	fields := make([]playerSnapshotField, 0, minInt(playerSnapshotMaxFields, len(payload)/playerSnapshotFieldHeaderLength))
	for cursor, previous := 0, uint16(0); cursor < len(payload); {
		if len(payload)-cursor < playerSnapshotFieldHeaderLength {
			return playerSnapshotRecord{}, fmt.Errorf("%w: field header", ErrPlayerSnapshotTruncated)
		}
		id := binary.BigEndian.Uint16(payload[cursor : cursor+2])
		tag := payload[cursor+2]
		length := int(binary.BigEndian.Uint32(payload[cursor+3 : cursor+7]))
		cursor += playerSnapshotFieldHeaderLength
		if length < 0 || length > len(payload)-cursor {
			return playerSnapshotRecord{}, fmt.Errorf("%w: field %d length", ErrPlayerSnapshotMalformed, id)
		}
		if len(fields) > 0 {
			if id == previous {
				return playerSnapshotRecord{}, fmt.Errorf("%w: duplicate field %d", ErrPlayerSnapshotMalformed, id)
			}
			if id < previous {
				return playerSnapshotRecord{}, fmt.Errorf("%w: out-of-order field %d", ErrPlayerSnapshotMalformed, id)
			}
		}
		field := playerSnapshotField{id: id, tag: tag, value: payload[cursor : cursor+length]}
		if err := validatePlayerSnapshotFieldType(field); err != nil {
			return playerSnapshotRecord{}, err
		}
		if len(fields) == playerSnapshotMaxFields {
			return playerSnapshotRecord{}, fmt.Errorf("%w: too many fields", ErrPlayerSnapshotMalformed)
		}
		fields = append(fields, field)
		previous = id
		cursor += length
	}
	return playerSnapshotRecord{kind: kind, fields: fields}, nil
}

func validatePlayerSnapshotFieldType(field playerSnapshotField) error {
	base := field.tag &^ playerSnapshotOptional
	validLength := func(length int) error {
		if len(field.value) != length {
			return fmt.Errorf("%w: field %d length", ErrPlayerSnapshotMalformed, field.id)
		}
		return nil
	}
	switch base {
	case playerSnapshotTypeU8, playerSnapshotTypeI8, playerSnapshotTypeBool:
		if err := validLength(1); err != nil {
			return err
		}
		if base == playerSnapshotTypeBool && field.value[0] != 0 && field.value[0] != 1 {
			return fmt.Errorf("%w: invalid boolean field %d", ErrPlayerSnapshotMalformed, field.id)
		}
	case playerSnapshotTypeU16, playerSnapshotTypeI16:
		return validLength(2)
	case playerSnapshotTypeU32, playerSnapshotTypeI32:
		return validLength(4)
	case playerSnapshotTypeU64, playerSnapshotTypeI64:
		return validLength(8)
	case playerSnapshotTypeBytes:
		return nil
	case playerSnapshotTypeText:
		if !utf8Valid(field.value) {
			return fmt.Errorf("%w: invalid UTF-8 field %d", ErrPlayerSnapshotMalformed, field.id)
		}
	default:
		if field.tag&playerSnapshotOptional == 0 {
			return fmt.Errorf("%w: unknown mandatory field type %d", ErrPlayerSnapshotMalformed, field.tag)
		}
	}
	return nil
}

// utf8Valid is kept local so the CDTO reader's byte fields remain opaque while
// still applying the common envelope rule to TYPE_TEXT extension fields.
func utf8Valid(value []byte) bool { return utf8.Valid(value) }

func decodePlayerSnapshotObjectGraph(raw []byte) (PlayerSnapshotObjectGraphV1, error) {
	record, err := decodePlayerSnapshotRecord(raw)
	if err != nil {
		return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: %v", ErrPlayerSnapshotGraphInvalid, err)
	}
	if record.kind != playerSnapshotObjectGraphKind || len(record.fields) == 0 {
		return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: wrong graph envelope", ErrPlayerSnapshotGraphInvalid)
	}
	countField := record.fields[0]
	if countField.id != 1 || countField.tag != playerSnapshotTypeU32 || len(countField.value) != 4 {
		return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: graph count", ErrPlayerSnapshotGraphInvalid)
	}
	count := int(binary.BigEndian.Uint32(countField.value))
	if count > playerSnapshotMaxGraphNodes || len(record.fields) != count+1 {
		return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: graph node count", ErrPlayerSnapshotGraphInvalid)
	}
	nodes := make([]PlayerSnapshotObjectNodeV1, count)
	for index := 0; index < count; index++ {
		field := record.fields[index+1]
		if field.id != uint16(index+2) || field.tag != playerSnapshotTypeBytes || len(field.value) != playerSnapshotGraphNodeLength {
			return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: node %d envelope", ErrPlayerSnapshotGraphInvalid, index)
		}
		value := field.value
		if int(binary.BigEndian.Uint32(value[:4])) != index {
			return PlayerSnapshotObjectGraphV1{}, fmt.Errorf("%w: node %d index", ErrPlayerSnapshotGraphInvalid, index)
		}
		parentRaw := binary.BigEndian.Uint32(value[4:8])
		var parent *uint32
		if parentRaw != ^uint32(0) {
			copyParent := parentRaw
			parent = &copyParent
		}
		object := PlayerSnapshotObjectV1{
			Name:         array80(value[12:92]),
			Description:  array80(value[92:172]),
			Keys:         [3][20]byte{array20(value[172:192]), array20(value[192:212]), array20(value[212:232])},
			UseOutput:    array80(value[232:312]),
			Value:        int64(binary.BigEndian.Uint64(value[312:320])),
			Weight:       int16(binary.BigEndian.Uint16(value[320:322])),
			TypeCode:     int8(value[322]),
			Adjustment:   int8(value[323]),
			ShotsMax:     int16(binary.BigEndian.Uint16(value[324:326])),
			ShotsCurrent: int16(binary.BigEndian.Uint16(value[326:328])),
			DiceCount:    int16(binary.BigEndian.Uint16(value[328:330])),
			DiceSides:    int16(binary.BigEndian.Uint16(value[330:332])),
			DicePlus:     int16(binary.BigEndian.Uint16(value[332:334])),
			Armor:        int8(value[334]),
			WearFlag:     int8(value[335]),
			MagicPower:   int8(value[336]),
			MagicRealm:   int8(value[337]),
			Special:      int16(binary.BigEndian.Uint16(value[338:340])),
			Flags:        array8(value[340:348]),
			QuestNum:     int8(value[348]),
		}
		nodes[index] = PlayerSnapshotObjectNodeV1{Object: object, ParentIndex: parent, ChildIndex: binary.BigEndian.Uint32(value[8:12])}
	}
	graph := PlayerSnapshotObjectGraphV1{Nodes: nodes}
	if err := validatePlayerSnapshotObjectGraph(graph, true); err != nil {
		return PlayerSnapshotObjectGraphV1{}, err
	}
	canonical, err := encodePlayerSnapshotObjectGraph(graph)
	if err != nil {
		return PlayerSnapshotObjectGraphV1{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return PlayerSnapshotObjectGraphV1{}, ErrPlayerSnapshotNonCanonical
	}
	return graph, nil
}

func encodePlayerSnapshotObjectGraph(graph PlayerSnapshotObjectGraphV1) ([]byte, error) {
	if err := validatePlayerSnapshotObjectGraph(graph, true); err != nil {
		return nil, err
	}
	fields := make([]playerSnapshotField, 0, len(graph.Nodes)+1)
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, uint32(len(graph.Nodes)))
	fields = append(fields, playerSnapshotField{id: 1, tag: playerSnapshotTypeU32, value: count})
	for index, node := range graph.Nodes {
		value, err := encodePlayerSnapshotObjectNode(index, node)
		if err != nil {
			return nil, err
		}
		fields = append(fields, playerSnapshotField{id: uint16(index + 2), tag: playerSnapshotTypeBytes, value: value})
	}
	return encodePlayerSnapshotRecord(playerSnapshotObjectGraphKind, fields)
}

func encodePlayerSnapshotObjectNode(index int, node PlayerSnapshotObjectNodeV1) ([]byte, error) {
	if index < 0 || index > playerSnapshotMaxGraphNodes || !fixedSnapshotStringsCanonical(node.Object, true) || node.Object.ShotsCurrent > node.Object.ShotsMax {
		return nil, ErrPlayerSnapshotGraphInvalid
	}
	value := make([]byte, playerSnapshotGraphNodeLength)
	binary.BigEndian.PutUint32(value[:4], uint32(index))
	parent := ^uint32(0)
	if node.ParentIndex != nil {
		parent = *node.ParentIndex
	}
	binary.BigEndian.PutUint32(value[4:8], parent)
	binary.BigEndian.PutUint32(value[8:12], node.ChildIndex)
	copy(value[12:92], node.Object.Name[:])
	copy(value[92:172], node.Object.Description[:])
	copy(value[172:192], node.Object.Keys[0][:])
	copy(value[192:212], node.Object.Keys[1][:])
	copy(value[212:232], node.Object.Keys[2][:])
	copy(value[232:312], node.Object.UseOutput[:])
	binary.BigEndian.PutUint64(value[312:320], uint64(node.Object.Value))
	binary.BigEndian.PutUint16(value[320:322], uint16(node.Object.Weight))
	value[322] = byte(node.Object.TypeCode)
	value[323] = byte(node.Object.Adjustment)
	binary.BigEndian.PutUint16(value[324:326], uint16(node.Object.ShotsMax))
	binary.BigEndian.PutUint16(value[326:328], uint16(node.Object.ShotsCurrent))
	binary.BigEndian.PutUint16(value[328:330], uint16(node.Object.DiceCount))
	binary.BigEndian.PutUint16(value[330:332], uint16(node.Object.DiceSides))
	binary.BigEndian.PutUint16(value[332:334], uint16(node.Object.DicePlus))
	value[334] = byte(node.Object.Armor)
	value[335] = byte(node.Object.WearFlag)
	value[336] = byte(node.Object.MagicPower)
	value[337] = byte(node.Object.MagicRealm)
	binary.BigEndian.PutUint16(value[338:340], uint16(node.Object.Special))
	copy(value[340:348], node.Object.Flags[:])
	value[348] = byte(node.Object.QuestNum)
	return value, nil
}

func validatePlayerSnapshot(snapshot PlayerSnapshotV1) error {
	if !fixedStringCanonical(snapshot.Name[:], true) || !fixedStringCanonical(snapshot.Description[:], true) || !fixedStringCanonical(snapshot.Talk[:], true) {
		return fmt.Errorf("%w: player text", ErrPlayerSnapshotMalformed)
	}
	for _, key := range snapshot.Keys {
		if !fixedStringCanonical(key[:], true) {
			return fmt.Errorf("%w: player key", ErrPlayerSnapshotMalformed)
		}
	}
	if snapshot.TypeCode != 0 {
		return fmt.Errorf("%w: player type is not PLAYER", ErrPlayerSnapshotMalformed)
	}
	if snapshot.HPCurrent > snapshot.HPMax || snapshot.MPCurrent > snapshot.MPMax {
		return fmt.Errorf("%w: current vital exceeds maximum", ErrPlayerSnapshotMalformed)
	}
	for _, daily := range snapshot.Daily {
		if daily.Current > daily.Max {
			return fmt.Errorf("%w: daily current exceeds maximum", ErrPlayerSnapshotMalformed)
		}
	}
	return validatePlayerSnapshotObjectGraph(snapshot.Inventory, true)
}

func validatePlayerSnapshotObjectGraph(graph PlayerSnapshotObjectGraphV1, requireTerminator bool) error {
	if len(graph.Nodes) > playerSnapshotMaxGraphNodes {
		return fmt.Errorf("%w: node limit", ErrPlayerSnapshotGraphInvalid)
	}
	childCounts := make([]uint32, len(graph.Nodes))
	depths := make([]int, len(graph.Nodes))
	ancestors := make([]int, 0, len(graph.Nodes))
	var roots uint32
	for index, node := range graph.Nodes {
		var expected uint32
		if node.ParentIndex == nil {
			ancestors = ancestors[:0]
			depths[index] = 1
			expected = roots
			roots++
		} else {
			parent := *node.ParentIndex
			if parent >= uint32(index) {
				return fmt.Errorf("%w: node %d parent order", ErrPlayerSnapshotGraphInvalid, index)
			}
			position := -1
			for candidate, ancestor := range ancestors {
				if uint32(ancestor) == parent {
					position = candidate
					break
				}
			}
			if position < 0 {
				return fmt.Errorf("%w: node %d parent is not active", ErrPlayerSnapshotGraphInvalid, index)
			}
			ancestors = ancestors[:position+1]
			depths[index] = depths[parent] + 1
			expected = childCounts[parent]
			childCounts[parent]++
		}
		if node.ChildIndex != expected {
			return fmt.Errorf("%w: node %d sibling index", ErrPlayerSnapshotGraphInvalid, index)
		}
		if depths[index] > playerSnapshotMaxGraphDepth {
			return fmt.Errorf("%w: depth limit", ErrPlayerSnapshotGraphInvalid)
		}
		if !fixedSnapshotStringsCanonical(node.Object, requireTerminator) || node.Object.ShotsCurrent > node.Object.ShotsMax {
			return fmt.Errorf("%w: node %d object", ErrPlayerSnapshotGraphInvalid, index)
		}
		ancestors = append(ancestors, index)
	}
	return nil
}

func fixedSnapshotStringsCanonical(object PlayerSnapshotObjectV1, requireTerminator bool) bool {
	return fixedStringCanonical(object.Name[:], requireTerminator) &&
		fixedStringCanonical(object.Description[:], requireTerminator) &&
		fixedStringCanonical(object.UseOutput[:], requireTerminator) &&
		fixedStringCanonical(object.Keys[0][:], requireTerminator) &&
		fixedStringCanonical(object.Keys[1][:], requireTerminator) &&
		fixedStringCanonical(object.Keys[2][:], requireTerminator)
}

func fixedStringCanonical(value []byte, requireTerminator bool) bool {
	terminated := false
	for _, b := range value {
		if terminated && b != 0 {
			return false
		}
		if b == 0 {
			terminated = true
		}
	}
	return !requireTerminator || terminated
}

func encodePlayerSnapshotRecord(kind uint16, fields []playerSnapshotField) ([]byte, error) {
	if len(fields) > playerSnapshotMaxFields {
		return nil, ErrPlayerSnapshotMalformed
	}
	var payloadLength int
	previous := uint16(0)
	for index, field := range fields {
		if index > 0 && field.id <= previous {
			return nil, ErrPlayerSnapshotMalformed
		}
		if err := validatePlayerSnapshotFieldType(field); err != nil {
			return nil, err
		}
		previous = field.id
		payloadLength += playerSnapshotFieldHeaderLength + len(field.value)
	}
	if payloadLength > playerSnapshotPayloadLimit {
		return nil, ErrPlayerSnapshotMalformed
	}
	wire := make([]byte, playerSnapshotPrefixLength+payloadLength+playerSnapshotDigestLength)
	copy(wire[:8], playerSnapshotCDTOMagic)
	binary.BigEndian.PutUint16(wire[8:10], playerSnapshotCDTOWireVersion)
	binary.BigEndian.PutUint16(wire[10:12], kind)
	binary.BigEndian.PutUint32(wire[12:16], uint32(payloadLength))
	cursor := playerSnapshotPrefixLength
	for _, field := range fields {
		binary.BigEndian.PutUint16(wire[cursor:cursor+2], field.id)
		wire[cursor+2] = field.tag
		binary.BigEndian.PutUint32(wire[cursor+3:cursor+7], uint32(len(field.value)))
		copy(wire[cursor+7:], field.value)
		cursor += playerSnapshotFieldHeaderLength + len(field.value)
	}
	digest := sha256.Sum256(wire[playerSnapshotPrefixLength:cursor])
	copy(wire[cursor:], digest[:])
	return wire, nil
}

func (s PlayerSnapshotV1) ToLegacyMonster() (LegacyMonster, error) {
	if err := validatePlayerSnapshot(s); err != nil {
		return LegacyMonster{}, err
	}
	name, err := snapshotText(s.Name[:])
	if err != nil {
		return LegacyMonster{}, err
	}
	description, err := snapshotText(s.Description[:])
	if err != nil {
		return LegacyMonster{}, err
	}
	talk, err := snapshotText(s.Talk[:])
	if err != nil {
		return LegacyMonster{}, err
	}
	keys, err := snapshotKeys(s.Keys)
	if err != nil {
		return LegacyMonster{}, err
	}
	experience, err := snapshotInt32(s.Experience)
	if err != nil {
		return LegacyMonster{}, err
	}
	gold, err := snapshotInt32(s.Gold)
	if err != nil {
		return LegacyMonster{}, err
	}
	body := LegacyMonster{
		Name: name, Description: description, Talk: talk, Keys: keys,
		Level: s.Level, Type: byte(s.TypeCode), Class: byte(s.Class), Race: byte(s.Race), Wander: byte(s.NumWander),
		Alignment: s.Alignment, Stats: [5]byte{byte(s.Strength), byte(s.Dexterity), byte(s.Constitution), byte(s.Intelligence), byte(s.Piety)},
		HPMax: s.HPMax, HPCurrent: s.HPCurrent, MPMax: s.MPMax, MPCurrent: s.MPCurrent,
		Armor: byte(s.Armor), Thaco: byte(s.Thaco), Experience: experience, Gold: gold,
		DiceCount: s.DiceCount, DiceSides: s.DiceSides, DicePlus: s.DicePlus, Special: s.Special,
		Quest: byte(s.QuestNum), RoomID: s.RoomNumber,
	}
	for index, value := range s.Proficiency {
		body.Proficiency[index], err = snapshotInt32(value)
		if err != nil {
			return LegacyMonster{}, err
		}
	}
	for index, value := range s.Realm {
		body.Realm[index], err = snapshotInt32(value)
		if err != nil {
			return LegacyMonster{}, err
		}
	}
	body.Spells = s.Spells
	body.Flags = s.Flags
	body.Quests = s.Quests
	body.Carry = s.Carry
	for index, daily := range s.Daily {
		last, conversionErr := snapshotInt32(daily.LastTime)
		if conversionErr != nil {
			return LegacyMonster{}, conversionErr
		}
		body.Daily[index] = LegacyDaily{Max: daily.Max, Current: daily.Current, LastTime: last}
	}
	for index, timer := range s.LastTime {
		interval, conversionErr := snapshotInt32(timer.Interval)
		if conversionErr != nil {
			return LegacyMonster{}, conversionErr
		}
		last, conversionErr := snapshotInt32(timer.LastUsed)
		if conversionErr != nil {
			return LegacyMonster{}, conversionErr
		}
		body.Timers[index] = LegacyTimer{Interval: interval, LastTime: last, Misc: timer.Misc}
	}
	return body, nil
}

func (s PlayerSnapshotV1) ToItemCollection(allocate func() (string, error)) (ItemCollection, error) {
	if err := validatePlayerSnapshot(s); err != nil {
		return ItemCollection{}, err
	}
	if len(s.Inventory.Nodes) == 0 {
		return ItemCollection{Items: map[string]Item{}}, nil
	}
	if allocate == nil {
		return ItemCollection{}, fmt.Errorf("%w: missing item allocator", ErrPlayerSnapshotItemConversion)
	}
	objects, err := s.legacyInventoryRoots()
	if err != nil {
		return ItemCollection{}, err
	}
	items, err := ImportItems(objects, allocate)
	if err != nil {
		return ItemCollection{}, fmt.Errorf("%w: %v", ErrPlayerSnapshotItemConversion, err)
	}
	return items, nil
}

func (s PlayerSnapshotV1) ToPlayerState(allocate func() (string, error)) (PlayerState, error) {
	body, err := s.ToLegacyMonster()
	if err != nil {
		return PlayerState{}, err
	}
	items, err := s.ToItemCollection(allocate)
	if err != nil {
		return PlayerState{}, err
	}
	return PlayerState{Body: body, Items: &items}, nil
}

func (s PlayerSnapshotV1) legacyInventoryRoots() ([]LegacyObject, error) {
	if err := validatePlayerSnapshotObjectGraph(s.Inventory, true); err != nil {
		return nil, err
	}
	objects := make([]LegacyObject, len(s.Inventory.Nodes))
	children := make([][]int, len(objects))
	var roots []int
	for index, node := range s.Inventory.Nodes {
		object, err := node.Object.toLegacyObject()
		if err != nil {
			return nil, err
		}
		objects[index] = object
		if node.ParentIndex == nil {
			roots = append(roots, index)
		} else {
			children[*node.ParentIndex] = append(children[*node.ParentIndex], index)
		}
	}
	var build func(int) LegacyObject
	build = func(index int) LegacyObject {
		object := objects[index]
		for _, child := range children[index] {
			object.Contents = append(object.Contents, build(child))
		}
		return object
	}
	result := make([]LegacyObject, 0, len(roots))
	for _, root := range roots {
		result = append(result, build(root))
	}
	return result, nil
}

func (o PlayerSnapshotObjectV1) toLegacyObject() (LegacyObject, error) {
	name, err := snapshotText(o.Name[:])
	if err != nil {
		return LegacyObject{}, err
	}
	description, err := snapshotText(o.Description[:])
	if err != nil {
		return LegacyObject{}, err
	}
	useOutput, err := snapshotText(o.UseOutput[:])
	if err != nil {
		return LegacyObject{}, err
	}
	keys, err := snapshotKeys(o.Keys)
	if err != nil {
		return LegacyObject{}, err
	}
	value, err := snapshotInt32(o.Value)
	if err != nil {
		return LegacyObject{}, err
	}
	return LegacyObject{
		Name: name, Description: description, UseOutput: useOutput, Keys: keys, Value: value,
		Weight: o.Weight, ShotsMax: o.ShotsMax, ShotsCurrent: o.ShotsCurrent,
		DiceCount: o.DiceCount, DiceSides: o.DiceSides, DicePlus: o.DicePlus,
		Type: byte(o.TypeCode), Adjustment: byte(o.Adjustment), Armor: byte(o.Armor), Wear: byte(o.WearFlag),
		MagicPower: byte(o.MagicPower), MagicRealm: byte(o.MagicRealm), Special: o.Special, Flags: o.Flags, Quest: byte(o.QuestNum),
	}, nil
}

func snapshotText(value []byte) (string, error) {
	if !fixedStringCanonical(value, true) {
		return "", fmt.Errorf("%w: non-canonical text", ErrPlayerSnapshotTextConversion)
	}
	text, err := legacyText(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPlayerSnapshotTextConversion, err)
	}
	return text, nil
}

func snapshotKeys(values [3][20]byte) ([3]string, error) {
	var result [3]string
	for index, value := range values {
		text, err := snapshotText(value[:])
		if err != nil {
			return [3]string{}, err
		}
		result[index] = text
	}
	return result, nil
}

func snapshotInt32(value int64) (int32, error) {
	if value < -2147483648 || value > 2147483647 {
		return 0, fmt.Errorf("%w: %d", ErrPlayerSnapshotNumericOverflow, value)
	}
	return int32(value), nil
}

func clonePlayerSnapshot(snapshot PlayerSnapshotV1) PlayerSnapshotV1 {
	if snapshot.Inventory.Nodes != nil {
		snapshot.Inventory.Nodes = append([]PlayerSnapshotObjectNodeV1(nil), snapshot.Inventory.Nodes...)
		for index := range snapshot.Inventory.Nodes {
			if snapshot.Inventory.Nodes[index].ParentIndex != nil {
				parent := *snapshot.Inventory.Nodes[index].ParentIndex
				snapshot.Inventory.Nodes[index].ParentIndex = &parent
			}
		}
	}
	return snapshot
}

func array8(value []byte) [8]byte   { var result [8]byte; copy(result[:], value); return result }
func array16(value []byte) [16]byte { var result [16]byte; copy(result[:], value); return result }
func array20(value []byte) [20]byte { var result [20]byte; copy(result[:], value); return result }
func array80(value []byte) [80]byte { var result [80]byte; copy(result[:], value); return result }

func readI16Array(value []byte, count int) (result [10]int16) {
	for index := 0; index < count && index < len(result); index++ {
		result[index] = int16(binary.BigEndian.Uint16(value[index*2 : index*2+2]))
	}
	return result
}

func readI64Array5(value []byte) (result [5]int64) {
	for index := range result {
		result[index] = int64(binary.BigEndian.Uint64(value[index*8 : index*8+8]))
	}
	return result
}

func readI64Array4(value []byte) (result [4]int64) {
	for index := range result {
		result[index] = int64(binary.BigEndian.Uint64(value[index*8 : index*8+8]))
	}
	return result
}

func readDailyArray(value []byte) (result [10]PlayerSnapshotDailyV1) {
	for index := range result {
		at := index * 10
		result[index] = PlayerSnapshotDailyV1{Max: value[at], Current: value[at+1], LastTime: int64(binary.BigEndian.Uint64(value[at+2 : at+10]))}
	}
	return result
}

func readLastTimeArray(value []byte) (result [45]PlayerSnapshotLastTimeV1) {
	for index := range result {
		at := index * 18
		result[index] = PlayerSnapshotLastTimeV1{
			Interval: int64(binary.BigEndian.Uint64(value[at : at+8])),
			LastUsed: int64(binary.BigEndian.Uint64(value[at+8 : at+16])),
			Misc:     int16(binary.BigEndian.Uint16(value[at+16 : at+18])),
		}
	}
	return result
}

func i16Bytes(value int16) []byte {
	var result [2]byte
	binary.BigEndian.PutUint16(result[:], uint16(value))
	return result[:]
}
func i64Bytes(value int64) []byte {
	var result [8]byte
	binary.BigEndian.PutUint64(result[:], uint64(value))
	return result[:]
}

func i16ArrayBytes(values []int16) []byte {
	result := make([]byte, len(values)*2)
	for index, value := range values {
		binary.BigEndian.PutUint16(result[index*2:index*2+2], uint16(value))
	}
	return result
}

func i64ArrayBytes(values []int64) []byte {
	result := make([]byte, len(values)*8)
	for index, value := range values {
		binary.BigEndian.PutUint64(result[index*8:index*8+8], uint64(value))
	}
	return result
}

func dailyBytes(values [10]PlayerSnapshotDailyV1) []byte {
	result := make([]byte, 100)
	for index, value := range values {
		at := index * 10
		result[at], result[at+1] = value.Max, value.Current
		binary.BigEndian.PutUint64(result[at+2:at+10], uint64(value.LastTime))
	}
	return result
}

func lastTimeBytes(values [45]PlayerSnapshotLastTimeV1) []byte {
	result := make([]byte, 810)
	for index, value := range values {
		at := index * 18
		binary.BigEndian.PutUint64(result[at:at+8], uint64(value.Interval))
		binary.BigEndian.PutUint64(result[at+8:at+16], uint64(value.LastUsed))
		binary.BigEndian.PutUint16(result[at+16:at+18], uint16(value.Misc))
	}
	return result
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
