package world

import "fmt"

// Invited is resolved by authoritative game data, never a client request flag.
// FamilyID/MarriageID represent legacy daily.max values after migration.
type DestinationVisitor struct {
	Level, Class          byte
	FamilyID, MarriageID  int16
	FamilyMember, Invited bool
}

// DestinationRestriction ports go()'s destination checks after load_rom. The
// world command loop must check and apply the move atomically against the same
// occupancy snapshot. Success here does not replace earlier traversal checks.
func DestinationRestriction(room LegacyRoomHeader, occupants []RoomPlayerView, v DestinationVisitor) string {
	return destinationRestriction(room, occupants, v, false)
}
func destinationRestriction(room LegacyRoomHeader, occupants []RoomPlayerView, v DestinationVisitor, directional bool) string {
	// Room limits were signed chars in the original ILP32 layout; player level
	// was explicitly unsigned. Preserve raw bytes in the migration DTO.
	low, high := int(int8(room.LowLevel)), int(int8(room.HighLevel))
	// Legacy count_vis_ply excludes ONLY PDMINV, not hidden/invisible players.
	count := 0
	for _, p := range occupants {
		if !flag(p.Flags[:], 10) {
			count++
		}
	}
	switch {
	case low > int(v.Level) && v.Class < 9:
		return fmt.Sprintf("레벨 %d 이상이어야 그 곳으로 갈 수 있습니다.", low)
	case high != 0 && int(v.Level) > high && v.Class < 10:
		if directional {
			return fmt.Sprintf("그곳으로 갈려면 레벨 %d이하여야만 합니다.", high)
		}
		// The historical message says high+1 although the comparison uses high.
		return fmt.Sprintf("그곳으로 갈려면 레벨 %d이하여야만 합니다.", high+1)
	case (flag(room.Flags[:], 14) && count > 0) || (flag(room.Flags[:], 15) && count > 1) || (flag(room.Flags[:], 16) && count > 2):
		return "그 방에 있는 사용자가 너무 많습니다."
	case flag(room.Flags[:], 37) && !v.FamilyMember:
		if directional {
			return "그곳에는 패거리 가입자만 갈 수 있습니다. "
		}
		return "그곳에는 패거리 가입자만 갈 수 있습니다."
	case v.Class < 12 && flag(room.Flags[:], 38) && v.FamilyID != room.Special:
		if directional {
			return "그곳은 당신이 갈수 없는 곳입니다."
		}
		return "그곳은 당신이 갈 수 없는 곳입니다."
	case v.Class < 12 && flag(room.Flags[:], 40) && v.MarriageID != room.Special && !v.Invited:
		return "그곳은 사유지입니다."
	default:
		return ""
	}
}
