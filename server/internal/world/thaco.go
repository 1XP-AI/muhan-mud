package world

import "fmt"

// WeaponProficiency ports profic's piecewise integer interpolation. Values at
// or above its terminal sentinel and negative XP are rejected: the legacy loop
// otherwise uses an uninitialized percentage/out-of-bounds threshold.
func WeaponProficiency(class byte, xp int32) (int, error) {
	var thresholds [12]int64
	switch class {
	case 4, 9, 10, 11, 12:
		thresholds = [12]int64{0, 768, 1024, 1440, 1910, 16000, 31214, 167000, 268488, 695000, 934808, 500000000}
	case 2:
		thresholds = [12]int64{0, 1536, 2048, 2880, 3820, 32000, 62428, 334000, 536976, 1390000, 1869616, 500000000}
	case 8, 7:
		thresholds = [12]int64{0, 2304, 3072, 4320, 5730, 48000, 93642, 501000, 805464, 2085000, 2804424, 500000000}
	case 3, 6, 1:
		thresholds = [12]int64{0, 3072, 4096, 5076, 7640, 64000, 124856, 668000, 1073952, 2780000, 3939232, 500000000}
	case 5:
		thresholds = [12]int64{0, 5376, 7168, 10080, 13370, 112000, 218498, 1169000, 1879416, 4865000, 6543656, 500000000}
	default:
		return 0, fmt.Errorf("invalid proficiency class")
	}
	if xp < 0 || xp >= 500000000 {
		return 0, fmt.Errorf("proficiency outside defined legacy range")
	}
	for i := 0; i < 11; i++ {
		if int64(xp) < thresholds[i+1] {
			return 10*i + int((int64(xp)-thresholds[i])*10/(thresholds[i+1]-thresholds[i])), nil
		}
	}
	return 0, fmt.Errorf("unreachable proficiency interval")
}

var legacyThaco = [13][20]int{
	{20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20, 20},
	{18, 18, 18, 17, 17, 16, 16, 15, 15, 14, 14, 13, 13, 12, 12, 11, 10, 10, 9, 9},
	{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 3, 2},
	{20, 20, 19, 18, 18, 17, 16, 16, 15, 14, 14, 13, 13, 12, 12, 11, 10, 10, 9, 8},
	{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 3, 3},
	{20, 20, 19, 19, 18, 18, 18, 17, 17, 16, 16, 16, 15, 15, 14, 14, 14, 13, 13, 11},
	{19, 19, 18, 18, 17, 16, 16, 15, 15, 14, 14, 13, 13, 12, 11, 11, 10, 9, 8, 7},
	{19, 19, 18, 17, 16, 16, 15, 15, 14, 14, 13, 12, 12, 11, 11, 10, 9, 9, 8, 7},
	{20, 20, 19, 19, 18, 18, 17, 17, 16, 16, 15, 15, 14, 14, 13, 13, 12, 12, 11, 11},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5},
	{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5},
	{-5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5, -5},
}

// ComputeThaco returns compute_thaco's STORED value. PBLESS only changes a
// local variable after assignment in C, so deliberately has no effect here.
// weapon is the WIELD slot only, not HELD. No player or item is modified.
func ComputeThaco(player LegacyMonster, weapon *LegacyObject) (int8, error) {
	if player.Class < 1 || player.Class > 12 || player.Level == 0 || player.Stats[0] > 63 {
		return 0, fmt.Errorf("undefined legacy thaco index")
	}
	n := (int(player.Level)+3)/4 - 1
	if n > 19 {
		n = 19
	}
	index, adjustment := 2, 0
	if weapon != nil {
		adjustment = int(int8(weapon.Adjustment))
		if int8(weapon.Type) < 0 {
			return 0, fmt.Errorf("negative weapon proficiency index")
		}
		if weapon.Type <= 4 {
			index = int(weapon.Type)
		}
	}
	proficiency, err := WeaponProficiency(player.Class, player.Proficiency[index])
	if err != nil {
		return 0, err
	}
	divisor := 40
	switch player.Class {
	case 4, 2, 9, 10:
		divisor = 20
	case 7, 6:
		divisor = 25
	case 8, 1, 3:
		divisor = 30
	}
	thaco := legacyThaco[player.Class][n] - adjustment - proficiency/divisor - legacyStatBonus[player.Stats[0]]
	floor := 0
	if player.Level >= 101 {
		floor = -5
	}
	if player.Class >= 10 {
		floor = -10
	}
	if thaco < floor {
		thaco = floor
	}
	// The old assignment narrows to signed char. Define that conversion explicitly.
	return int8(thaco), nil
}
