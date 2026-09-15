package world

import "fmt"

var classLevelGains = [13][2]int{
	{1, 1}, {5, 2}, {7, 1}, {4, 3}, {6, 1}, {4, 3}, {5, 2}, {6, 2}, {5, 2}, {4, 4}, {5, 5}, {5, 5}, {7, 4},
}

// Zero-based indices into LegacyMonster.Stats, translated from STR..PTY.
var levelStatCycle = [13][10]int{
	{},
	{2, 4, 0, 3, 1, 3, 1, 4, 0, 1},
	{3, 1, 4, 2, 0, 2, 1, 0, 4, 0},
	{0, 1, 2, 4, 3, 4, 3, 1, 2, 3},
	{4, 3, 1, 2, 0, 2, 3, 0, 1, 0},
	{0, 1, 4, 2, 3, 2, 3, 1, 4, 3},
	{1, 3, 2, 0, 4, 0, 3, 4, 2, 4},
	{4, 0, 3, 2, 1, 2, 1, 0, 3, 1},
	{3, 2, 4, 0, 1, 0, 2, 1, 4, 1},
	{0, 1, 3, 2, 4, 0, 1, 3, 2, 4},
	{0, 1, 3, 2, 4, 0, 1, 3, 2, 4},
	{0, 1, 3, 2, 4, 0, 1, 3, 2, 4},
	{0, 1, 3, 2, 4, 0, 1, 3, 2, 4},
}

// LowerPlayerLevel repeats down_level in its original order. It is not a
// level-up or complete death function. Invalid final ranges abort the candidate
// instead of wrapping numeric state. No output, storage or input mutation.
func LowerPlayerLevel(player LegacyMonster, target int) (LegacyMonster, error) {
	if target < 1 || target > int(player.Level) || player.Class < 1 || player.Class > 12 {
		return LegacyMonster{}, fmt.Errorf("invalid level-down context")
	}
	r := player
	for int(r.Level) > target {
		r.Level--
		hp, mp, plus := int(r.HPMax), int(r.MPMax), int(r.DicePlus)
		if flag(r.Flags[:], 59) {
			r.Flags[7] &^= 8
			hp -= 100
			mp -= 100
			plus -= 5
		}
		if (int(r.Level)-1)%2 != 0 {
			hp -= classLevelGains[r.Class][0]
		} else {
			mp -= classLevelGains[r.Class][1]
		}
		if hp < -32768 || mp < -32768 || plus < -32768 {
			return LegacyMonster{}, fmt.Errorf("level-down numeric underflow")
		}
		r.HPMax, r.MPMax, r.DicePlus = int16(hp), int16(mp), int16(plus)
		r.HPCurrent, r.MPCurrent = r.HPMax, r.MPMax
		if (int(r.Level)+1)%4 == 0 {
			index := levelStatCycle[r.Class][(int(r.Level)-1)%10]
			value := int(int8(r.Stats[index])) - 1
			if value < 0 {
				return LegacyMonster{}, fmt.Errorf("level-down negative stat")
			}
			r.Stats[index] = byte(value)
		}
	}
	r.Inventory = cloneObjects(player.Inventory)
	return r, nil
}

func ApplyDeathProgression(player LegacyMonster, rules DeathProgressionRules) (LegacyMonster, error) {
	plan, err := PlanDeathProgression(player, rules)
	if err != nil {
		return LegacyMonster{}, err
	}
	next, err := LowerPlayerLevel(player, plan.TargetLevel)
	if err != nil {
		return LegacyMonster{}, err
	}
	next.Experience, next.Proficiency, next.Realm = plan.Experience, plan.Proficiency, plan.Realm
	return next, nil
}
