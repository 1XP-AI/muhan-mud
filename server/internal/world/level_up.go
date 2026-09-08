package world

import "fmt"

// hpstart, mpstart, ndice, sdice, pdice from global.c class_stats.
var classInitial = [13][5]int{
	{1, 1, 1, 1, 1}, {55, 40, 1, 6, 0}, {57, 40, 2, 3, 1}, {54, 50, 1, 4, 0}, {56, 50, 1, 5, 0}, {54, 50, 1, 3, 0}, {55, 50, 1, 4, 0}, {56, 40, 2, 2, 0}, {55, 50, 2, 2, 1}, {400, 250, 2, 4, 0}, {50, 50, 5, 5, 5}, {50, 50, 5, 5, 5}, {50, 50, 5, 5, 5},
}

// RaisePlayerLevel ports one up_level call, including its early return for
// levels other than 1/multiples of 4. Not a complete new-character initializer.
func RaisePlayerLevel(player LegacyMonster) (LegacyMonster, error) {
	if player.Class < 1 || player.Class > 12 || player.Level == 255 {
		return LegacyMonster{}, fmt.Errorf("invalid level-up context")
	}
	p := player
	p.Level++
	initial := classInitial[p.Class]
	gains := classLevelGains[p.Class]
	p.DiceCount, p.DiceSides, p.DicePlus = int16(initial[2]), int16(initial[3]), int16(initial[4])
	hp, mp := int(p.HPMax), int(p.MPMax)
	if p.Level%2 != 0 {
		hp += gains[0]
	} else {
		mp += gains[1]
	}
	if p.Level == 1 {
		hp, mp = initial[0], initial[1]
	} else if p.Level%4 == 0 {
		index := levelStatCycle[p.Class][(int(p.Level)-2)%10]
		value := int(int8(p.Stats[index])) + 1
		if value < 0 || value > 127 {
			return LegacyMonster{}, fmt.Errorf("level-up stat overflow")
		}
		p.Stats[index] = byte(value)
	}
	if p.Level == 1 || p.Level%4 == 0 {
		hp = initial[0] + gains[0]*(int(p.Level)-1)/2
		mp = initial[1] + gains[1]*(int(p.Level)-1)/2
	}
	if hp > 32767 || mp > 32767 {
		return LegacyMonster{}, fmt.Errorf("level-up numeric overflow")
	}
	p.HPMax, p.MPMax = int16(hp), int16(mp)
	if p.Level == 1 || p.Level%4 == 0 {
		p.HPCurrent, p.MPCurrent = p.HPMax, p.MPMax
	}
	p.Inventory = cloneObjects(player.Inventory)
	return p, nil
}
