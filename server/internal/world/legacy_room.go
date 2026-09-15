// Package world decodes world resources without legacy process dependencies.
package world

import (
	"bytes"
	"encoding/binary"
	"errors"
	"golang.org/x/text/encoding/korean"
	"strings"
)

var ErrLegacyRoom = errors.New("invalid ILP32 little-endian room resource")

type LegacyExit struct {
	Name               string
	Destination        int16
	Flags              [4]byte
	Interval, LastTime int32
	Misc               int16
	Key                byte
}
type LegacyRoomHeader struct {
	ID                  int16
	Name                string
	LowLevel, HighLevel byte
	Special, TrapExit   int16
	Trap                byte
	Flags               [8]byte
	Exits               []LegacyExit
	// The body (monsters, objects, descriptions) still requires decoding.
	BodyOffset int
}

func legacyText(raw []byte) (string, error) {
	end := bytes.IndexByte(raw, 0)
	if end < 0 {
		return "", ErrLegacyRoom
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(raw[:end])
	if err != nil || strings.ContainsRune(string(decoded), '\ufffd') {
		return "", ErrLegacyRoom
	}
	return string(decoded), nil
}

// DecodeLegacyRoomHeader accepts only the original ILP32 little-endian layout:
// room=480 bytes, exit=44 bytes. Never infer host ABI or follow saved pointers.
// This is NOT a whole-room validator: the returned BodyOffset is unparsed data.
func DecodeLegacyRoomHeader(raw []byte) (LegacyRoomHeader, error) {
	if len(raw) < 484 {
		return LegacyRoomHeader{}, ErrLegacyRoom
	}
	count := int32(binary.LittleEndian.Uint32(raw[480:484]))
	if count < 0 || count > 1024 || int64(count)*44 > int64(len(raw)-484) {
		return LegacyRoomHeader{}, ErrLegacyRoom
	}
	name, err := legacyText(raw[2:82])
	if err != nil {
		return LegacyRoomHeader{}, err
	}
	room := LegacyRoomHeader{ID: int16(binary.LittleEndian.Uint16(raw)), Name: name, LowLevel: raw[96], HighLevel: raw[97], Special: int16(binary.LittleEndian.Uint16(raw[98:])), Trap: raw[100], TrapExit: int16(binary.LittleEndian.Uint16(raw[102:])), BodyOffset: 484 + int(count)*44}
	copy(room.Flags[:], raw[184:192])
	for i := 0; i < int(count); i++ {
		part := raw[484+i*44 : 484+(i+1)*44]
		name, err := legacyText(part[:20])
		if err != nil {
			return LegacyRoomHeader{}, err
		}
		exit := LegacyExit{Name: name, Destination: int16(binary.LittleEndian.Uint16(part[20:])), Interval: int32(binary.LittleEndian.Uint32(part[28:])), LastTime: int32(binary.LittleEndian.Uint32(part[32:])), Misc: int16(binary.LittleEndian.Uint16(part[36:])), Key: part[40]}
		copy(exit.Flags[:], part[22:26])
		room.Exits = append(room.Exits, exit)
	}
	return room, nil
}
