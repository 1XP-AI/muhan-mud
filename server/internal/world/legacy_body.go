package world

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/korean"
)

// These are migration resource values, not live entities. Saved pointers and
// socket/password slots are deliberately not represented. No load-time clamps
// are applied: conversion must preserve the original numeric values for audit.
type LegacyTimer struct {
	Interval, LastTime int32
	Misc               int16
}
type LegacyDaily struct {
	Max, Current byte
	LastTime     int32
}
type LegacyObject struct {
	Name, Description, UseOutput                                            string
	Keys                                                                    [3]string
	Value                                                                   int32
	Weight, ShotsMax, ShotsCurrent, DiceCount, DiceSides, DicePlus, Special int16
	Type, Adjustment, Armor, Wear, MagicPower, MagicRealm, Quest            byte
	Flags                                                                   [8]byte
	Contents                                                                []LegacyObject
}
type LegacyMonster struct {
	Name, Description, Talk                 string
	Keys                                    [3]string
	Level, Type, Class, Race, Wander        byte
	Alignment                               int16
	Stats                                   [5]byte
	HPMax, HPCurrent, MPMax, MPCurrent      int16
	Armor, Thaco                            byte
	Experience, Gold, WimpyValue            int32
	DiceCount, DiceSides, DicePlus, Special int16
	Proficiency                             [5]int32
	Realm                                   [4]int32
	Spells                                  [16]byte
	Flags                                   [8]byte
	Quests                                  [16]byte
	Quest                                   byte
	Carry                                   [10]int16
	RoomID                                  int16
	Daily                                   [10]LegacyDaily
	Timers                                  [45]LegacyTimer
	Inventory                               []LegacyObject
}
type LegacyRoom struct {
	LegacyRoomHeader
	ShortDescriptionPresent, LongDescriptionPresent, ObjectDescriptionPresent bool
	Track                                                                     string
	Random                                                                    [10]int16
	Traffic                                                                   byte
	PermanentMonsters, PermanentObjects                                       [10]LegacyTimer
	BeenHere, Established                                                     int32
	Monsters                                                                  []LegacyMonster
	Objects                                                                   []LegacyObject
	ShortDescription, LongDescription, ObjectDescription                      string
}

type roomReader struct {
	template               bool
	lastDescriptionPresent bool
	raw                    []byte
	offset, objects        int
	err                    error
	issues                 *[]LegacyIssue
}

type LegacyIssue struct {
	Kind   string
	Offset int
}

// legacyCFixedTextFieldBytes is the C char[80] text field width used by
// object name/description/use_output and matching monster/room text arrays.
const legacyCFixedTextFieldBytes = 80

// LegacyInspection is migration evidence only, never permission to admit a
// room to the game. Invalid text uses replacement characters in the preview;
// Source retains every original byte, including non-semantic trailing data.
// Treat Source as potentially sensitive legacy memory; do not log or send it
// to a game client. A caller must review/resolve Issues before conversion.
type LegacyInspection struct {
	Room     LegacyRoom
	Source   []byte
	SHA256   [32]byte
	Consumed int
	Issues   []LegacyIssue
}

func InspectLegacyRoom(raw []byte) (LegacyInspection, error) {
	var issues []LegacyIssue
	room, consumed, err := decodeLegacyRoom(raw, &issues)
	if err != nil {
		return LegacyInspection{}, err
	}
	return LegacyInspection{Room: room, Source: append([]byte(nil), raw...), SHA256: sha256.Sum256(raw), Consumed: consumed, Issues: issues}, nil
}

func (r *roomReader) fail(reason string) {
	if r.err == nil {
		r.err = fmt.Errorf("%w at byte %d: %s", ErrLegacyRoom, r.offset, reason)
	}
}
func (r *roomReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || n > len(r.raw)-r.offset {
		r.fail("truncated record")
		return nil
	}
	b := r.raw[r.offset : r.offset+n]
	r.offset += n
	return b
}
func (r *roomReader) count(max int) int {
	b := r.take(4)
	if b == nil {
		return 0
	}
	n := int64(int32(binary.LittleEndian.Uint32(b)))
	if n < 0 || n > int64(max) {
		r.fail("invalid count")
		return 0
	}
	return int(n)
}
func s16(b []byte, at int) int16 { return int16(binary.LittleEndian.Uint16(b[at:])) }
func s32(b []byte, at int) int32 { return int32(binary.LittleEndian.Uint32(b[at:])) }
func timer(b []byte) LegacyTimer { return LegacyTimer{s32(b, 0), s32(b, 4), s16(b, 8)} }
func (r *roomReader) text(b []byte, offset int) string {
	text, err := legacyText(b)
	if err != nil {
		end := bytes.IndexByte(b, 0)
		if r.issues != nil {
			kind := "invalid-euc-kr"
			if end < 0 {
				end = len(b)
				kind = "missing-text-terminator"
			}
			*r.issues = append(*r.issues, LegacyIssue{Kind: kind, Offset: offset})
			decoded, decodeErr := decodeLegacyEUCKRPreview(b[:end])
			if decodeErr == nil {
				return decoded
			}
		}
		r.fail("invalid EUC-KR text")
	}
	return text
}

// decodeLegacyEUCKRPreview is the documented substitution for invalid-euc-kr
// admission. Invalid sequences become U+FFFD; the mapping must not invent
// Hangul from bytes outside the EUC-KR table. Original source bytes stay in
// Evidence.Source.
func decodeLegacyEUCKRPreview(raw []byte) (string, error) {
	decoded, err := korean.EUCKR.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func requireInvalidEUCKRSubstitution(raw []byte, issues []LegacyIssue) error {
	found := false
	for _, issue := range issues {
		if issue.Kind != "invalid-euc-kr" {
			continue
		}
		found = true
		if issue.Offset < 0 || issue.Offset >= len(raw) {
			return fmt.Errorf("%w at byte %d: invalid-euc-kr field is outside source", ErrLegacyRoom, issue.Offset)
		}
		end := bytes.IndexByte(raw[issue.Offset:], 0)
		if end < 0 {
			return fmt.Errorf("%w at byte %d: invalid-euc-kr field has no NUL", ErrLegacyRoom, issue.Offset)
		}
		preview, err := decodeLegacyEUCKRPreview(raw[issue.Offset : issue.Offset+end])
		if err != nil {
			return fmt.Errorf("%w at byte %d: %v", ErrLegacyRoom, issue.Offset, err)
		}
		if !strings.ContainsRune(preview, '\ufffd') {
			return fmt.Errorf("%w at byte %d: invalid-euc-kr decoded without U+FFFD substitution", ErrLegacyRoom, issue.Offset)
		}
	}
	if !found {
		return fmt.Errorf("%w: invalid-euc-kr conversion without issues", ErrLegacyRoom)
	}
	return nil
}

func requireUnterminatedTextAtCFieldBoundary(raw []byte, issues []LegacyIssue) error {
	found := false
	for _, issue := range issues {
		if issue.Kind != "missing-text-terminator" {
			continue
		}
		found = true
		if issue.Offset < 0 || issue.Offset+legacyCFixedTextFieldBytes > len(raw) {
			return fmt.Errorf("%w at byte %d: unterminated field exceeds C field boundary", ErrLegacyRoom, issue.Offset)
		}
		field := raw[issue.Offset : issue.Offset+legacyCFixedTextFieldBytes]
		if bytes.IndexByte(field, 0) >= 0 {
			return fmt.Errorf("%w at byte %d: unterminated field contains NUL before C field boundary", ErrLegacyRoom, issue.Offset)
		}
	}
	if !found {
		return fmt.Errorf("%w: missing-text-terminator conversion without issues", ErrLegacyRoom)
	}
	return nil
}

func (r *roomReader) object(depth int) LegacyObject {
	if depth > 64 || r.objects >= 8192 {
		r.fail("object tree limit")
		return LegacyObject{}
	}
	r.objects++
	b := r.take(352)
	if b == nil {
		return LegacyObject{}
	}
	at := r.offset - 352
	o := LegacyObject{Name: r.text(b[:80], at), Description: r.text(b[80:160], at+80), UseOutput: r.text(b[220:300], at+220), Value: s32(b, 300), Weight: s16(b, 304), Type: b[306], Adjustment: b[307], ShotsMax: s16(b, 308), ShotsCurrent: s16(b, 310), DiceCount: s16(b, 312), DiceSides: s16(b, 314), DicePlus: s16(b, 316), Armor: b[318], Wear: b[319], MagicPower: b[320], MagicRealm: b[321], Special: s16(b, 322), Quest: b[332]}
	for i := range o.Keys {
		o.Keys[i] = r.text(b[160+i*20:180+i*20], at+160+i*20)
	}
	copy(o.Flags[:], b[324:332])
	if r.template {
		return o
	}
	n := r.count(4096)
	for i := 0; i < n && r.err == nil; i++ {
		o.Contents = append(o.Contents, r.object(depth+1))
	}
	return o
}
func (r *roomReader) monster() LegacyMonster {
	b := r.take(1184)
	if b == nil {
		return LegacyMonster{}
	}
	at := r.offset - 1184
	m := LegacyMonster{Name: r.text(b[:80], at), Description: r.text(b[80:160], at+80), Talk: r.text(b[160:240], at+160), Level: b[318], Type: b[319], Class: b[320], Race: b[321], Wander: b[322], Alignment: s16(b, 324), HPMax: s16(b, 332), HPCurrent: s16(b, 334), MPMax: s16(b, 336), MPCurrent: s16(b, 338), Armor: b[340], Thaco: b[341], Experience: s32(b, 344), Gold: s32(b, 348), DiceCount: s16(b, 352), DiceSides: s16(b, 354), DicePlus: s16(b, 356), Special: s16(b, 358), Quest: b[436], RoomID: s16(b, 458)}
	for i := range m.Keys {
		m.Keys[i] = r.text(b[255+i*20:275+i*20], at+255+i*20)
	}
	copy(m.Stats[:], b[326:331])
	copy(m.Spells[:], b[396:412])
	copy(m.Flags[:], b[412:420])
	copy(m.Quests[:], b[420:436])
	for i := range m.Proficiency {
		m.Proficiency[i] = s32(b, 360+i*4)
	}
	for i := range m.Realm {
		m.Realm[i] = s32(b, 380+i*4)
	}
	for i := range m.Carry {
		m.Carry[i] = s16(b, 438+i*2)
	}
	for i := range m.Daily {
		at := 540 + i*8
		m.Daily[i] = LegacyDaily{b[at], b[at+1], s32(b, at+4)}
	}
	for i := range m.Timers {
		m.Timers[i] = timer(b[620+i*12:])
	}
	if r.template {
		return m
	}
	n := r.count(4096)
	for i := 0; i < n && r.err == nil; i++ {
		m.Inventory = append(m.Inventory, r.object(0))
	}
	return m
}
func (r *roomReader) description() string {
	n := r.count(1024 * 1024)
	r.lastDescriptionPresent = n > 0
	if n == 0 {
		return ""
	}
	b := r.take(n)
	if b == nil {
		return ""
	}
	if bytes.IndexByte(b, 0) != n-1 {
		r.fail("description terminator/length mismatch")
		return ""
	}
	return r.text(b, r.offset-n)
}

// DecodeLegacyRoom consumes exactly one original ILP32 little-endian resource.
// It never treats malformed counts as empty collections. Bounds are migration
// reader limits, not new gameplay rules. All-or-nothing output on failure.
func DecodeLegacyRoom(raw []byte) (LegacyRoom, error) {
	room, _, err := decodeLegacyRoom(raw, nil)
	return room, err
}

func decodeLegacyRoom(raw []byte, issues *[]LegacyIssue) (LegacyRoom, int, error) {
	h, err := DecodeLegacyRoomHeader(raw)
	if err != nil {
		return LegacyRoom{}, 0, err
	}
	r := roomReader{raw: raw, offset: h.BodyOffset, issues: issues}
	room := LegacyRoom{LegacyRoomHeader: h, Track: r.text(raw[104:184], 104), Traffic: raw[212], BeenHere: s32(raw, 456), Established: s32(raw, 460)}
	for i := range room.Random {
		room.Random[i] = s16(raw, 192+i*2)
	}
	for i := range room.PermanentMonsters {
		room.PermanentMonsters[i] = timer(raw[216+i*12:])
		room.PermanentObjects[i] = timer(raw[336+i*12:])
	}
	n := r.count(4096)
	for i := 0; i < n && r.err == nil; i++ {
		room.Monsters = append(room.Monsters, r.monster())
	}
	n = r.count(8192)
	for i := 0; i < n && r.err == nil; i++ {
		room.Objects = append(room.Objects, r.object(0))
	}
	room.ShortDescription = r.description()
	room.ShortDescriptionPresent = r.lastDescriptionPresent
	room.LongDescription = r.description()
	room.LongDescriptionPresent = r.lastDescriptionPresent
	room.ObjectDescription = r.description()
	room.ObjectDescriptionPresent = r.lastDescriptionPresent
	if r.err == nil && r.offset != len(raw) {
		if issues == nil {
			r.fail("trailing data")
		} else {
			*issues = append(*issues, LegacyIssue{Kind: "trailing-data", Offset: r.offset})
		}
	}
	if r.err != nil {
		return LegacyRoom{}, 0, r.err
	}
	return room, r.offset, nil
}
