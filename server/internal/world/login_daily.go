package world

// LoginDaily ports init_ply's daily maximum refresh, not a daily reset.
// Current and LastTime must survive reconnect so login cannot replenish uses.
func LoginDaily(before [10]LegacyDaily, level, class byte) [10]LegacyDaily {
	out := before
	if int8(class) >= 10 {
		return out
	}
	tier := (int(level) + 3) / 4
	out[0].Max = byte(25 + tier/2)
	out[1].Max = 10
	out[2].Max = byte(max(10, 10+(tier-5)/3))
	out[3].Max = byte(max(10, 10+(tier-5)/4))
	out[4].Max = 1
	return out
}
