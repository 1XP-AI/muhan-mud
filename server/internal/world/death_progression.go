package world

import "fmt"

var neededExperience = [128]int32{
	128, 256, 384, 512, 640, 768, 896, 1024, 1280, 1536, 1792, 2048, 2560, 3072, 3584, 4096,
	5120, 6144, 7168, 8192, 10240, 12288, 14336, 16384, 20480, 24576, 28672, 32768, 40960, 49152, 57344, 65536,
	74152, 82768, 91384, 100000, 111602, 123205, 134807, 146410, 161647, 176885, 192122, 207360, 234062, 260765, 287468, 314171,
	350876, 387581, 424286, 460992, 510275, 559558, 608841, 658125, 715469, 772814, 830159, 887504, 966331, 1045159, 1123987, 1202815,
	1327015, 1451215, 1575415, 1699616, 1825576, 1951536, 2077496, 2120345, 2272342, 2421228, 2570114, 2729000, 2875534, 3022069, 3178604, 3325139,
	3475134, 3825129, 3975124, 4125120, 4272005, 4428890, 4570775, 4722661, 4894263, 5045866, 5197469, 5529072, 5857897, 6196723, 6435549, 6774375,
	7005781, 7337187, 7676859, 7984959, 8229756, 9079416, 9441769, 10000000, 12728240, 14066499, 16511819, 18072765, 20758586, 22579273, 24545614, 26669264,
	28962805, 30439829, 32115015, 34920766, 36333635, 39053662, 43108492, 48528256, 54345799, 60596921, 65320644, 71559502, 76359857, 86000000, 100000000, 190000000,
}

func ExperienceLevel(xp int32) int {
	level := 1
	for level < 128 && xp >= neededExperience[level-1] {
		level++
	}
	if level >= 128 {
		level = int((int64(xp)-int64(neededExperience[126]))/5000000) + 128
	}
	return max(1, level)
}

// ExperienceThreshold returns the C health command's next-level threshold.
// Levels above the static table retain the legacy five-million progression.
func ExperienceThreshold(level byte) int32 {
	if level == 0 {
		return neededExperience[0]
	}
	if level < 128 {
		return neededExperience[int(level)-1]
	}
	value := int64(neededExperience[126]) + (int64(level)-127)*5000000
	if value > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(value)
}

// SkillLoss is the authoritative !survival && check_war(...) result. A true
// check_war return permits loss; it does not mean the two families are at war.
type DeathProgressionRules struct{ AttackerPlayer, Self, SkillLoss bool }
type DeathProgression struct {
	Experience  int32
	TargetLevel int
	Proficiency [5]int32
	Realm       [4]int32
}

// PlanDeathProgression calculates XP/skill changes and a level-down target,
// not down_level's HP/MP/stat mutations or the rest of death. Wide arithmetic
// deliberately avoids legacy long overflow on summed proficiency values.
func PlanDeathProgression(player LegacyMonster, rules DeathProgressionRules) (DeathProgression, error) {
	if player.Level == 0 || (rules.Self && !rules.AttackerPlayer) {
		return DeathProgression{}, fmt.Errorf("invalid death progression context")
	}
	xp := int64(player.Experience)
	if !rules.AttackerPlayer || rules.Self {
		if player.Level < 20 {
			xp -= xp / 20
		} else {
			xp -= min(xp/15, 100000)
			if (int(player.Level)+3)/4-ExperienceLevel(int32(xp)) > 1 {
				if player.Level < 130 {
					xp = int64(neededExperience[int(player.Level)-3])
				} else {
					xp = int64(neededExperience[126]) + (int64(player.Level)-129)*5000000
				}
			}
		}
	}
	xp = max(0, xp)
	r := DeathProgression{Experience: int32(xp), TargetLevel: min(int(player.Level), ExperienceLevel(int32(xp))), Proficiency: player.Proficiency, Realm: player.Realm}
	if !rules.SkillLoss {
		return r, nil
	}
	var values [9]int64
	var total int64
	for i := range values {
		if i < 5 {
			values[i] = int64(player.Proficiency[i])
		} else {
			values[i] = int64(player.Realm[i-5])
		}
		if values[i] < 0 {
			return DeathProgression{}, fmt.Errorf("negative skill experience")
		}
		total += values[i]
	}
	loss := max(int64(0), total-xp-1024)
	negative := 0
	for loss > 9 && negative < 9 {
		negative = 0
		for i := range values {
			share := loss / int64(9-i)
			values[i] -= share
			loss -= share
			if values[i] < 0 {
				negative++
				loss -= values[i]
				values[i] = 0
			}
		}
	}
	best := 0
	for i := 1; i < 5; i++ {
		if values[i] > values[best] {
			best = i
		}
	}
	values[best] = max(1024, values[best])
	for i, value := range values {
		if i < 5 {
			r.Proficiency[i] = int32(value)
		} else {
			r.Realm[i-5] = int32(value)
		}
	}
	return r, nil
}
