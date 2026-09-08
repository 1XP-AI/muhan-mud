package world

import "fmt"

type DoorResult struct {
	Exit            LegacyExit
	Hidden, Changed bool
	Message         string
}

// OpenDoor/CloseDoor operate on an already selected exit. The command layer
// uses SelectExit first. On success the caller must atomically commit the exit
// and actor visibility and emit the room broadcast; failure changes neither.
func OpenDoor(e LegacyExit, hidden bool, now int32) DoorResult {
	r := DoorResult{Exit: e, Hidden: hidden}
	if flag(e.Flags[:], 2) {
		r.Message = "그것은 잠겨져 있습니다."
		return r
	}
	if !flag(e.Flags[:], 3) {
		r.Message = "벌써 열려져 있습니다."
		return r
	}
	r.Exit.Flags[0] &= ^byte(8)
	r.Exit.LastTime = now
	r.Hidden = false
	r.Changed = true
	r.Message = fmt.Sprintf("당신은 %s쪽 출구를 열었습니다.", e.Name)
	return r
}

func CloseDoor(e LegacyExit, hidden bool) DoorResult {
	r := DoorResult{Exit: e, Hidden: hidden}
	if flag(e.Flags[:], 3) {
		r.Message = "벌써 닫혀져 있습니다."
		return r
	}
	if !flag(e.Flags[:], 5) {
		r.Message = "당신은 그 출구를 닫을 수 없습니다."
		return r
	}
	r.Exit.Flags[0] |= 8
	r.Hidden = false
	r.Changed = true
	r.Message = fmt.Sprintf("당신은 %s쪽 출구를 닫습니다.", e.Name)
	return r
}

// RefreshDoors ports check_exits, run on room entry before the scene is shown.
// Equality is not due. Widen saved ILP32 operands before adding; never wrap.
// Returns a fresh slice; door deadlines are unchanged by automatic resets.
func RefreshDoors(exits []LegacyExit, now int64) []LegacyExit {
	result := append([]LegacyExit(nil), exits...)
	for i, e := range result {
		if int64(e.LastTime)+int64(e.Interval) >= now {
			continue
		}
		if flag(e.Flags[:], 4) {
			result[i].Flags[0] |= 12
		} else if flag(e.Flags[:], 5) {
			result[i].Flags[0] |= 8
		}
	}
	return result
}
