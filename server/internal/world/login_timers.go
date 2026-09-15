package world

import "fmt"

// LoginTimers preserves init_ply's ordering: set save timer, then shift all 45
// timestamps by time since LT_HOURS and cap them at now. Intervals and Misc are
// unchanged except SAVEINTERVAL. Widened arithmetic deliberately avoids C's
// signed-overflow UB; an unrepresentable final value aborts instead of wrapping.
func LoginTimers(before [45]LegacyTimer, now int32) ([45]LegacyTimer, error) {
	out := before
	out[22].LastTime = now
	out[22].Interval = 600
	delta := int64(now) - int64(before[28].LastTime)
	for i := range out {
		updated := int64(out[i].LastTime) + delta
		if updated > int64(now) {
			updated = int64(now)
		}
		if updated < -2147483648 {
			return [45]LegacyTimer{}, fmt.Errorf("login timer outside legacy range")
		}
		out[i].LastTime = int32(updated)
	}
	return out, nil
}
