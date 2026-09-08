package world

import "fmt"

type VitalResult struct {
	Player              LegacyMonster
	Messages            []string
	Death               bool
	DeathMessageOffsets []int
}

// PlanVitals ports update_ply's due healing/ailment/harm-room phase. Death is a
// mandatory transition barrier: callers must run the not-yet-ported die logic
// before any later tick phases. Do not persist HP alone as complete death.
// Source C calls die inline; this planner stops at the first such call because
// its mutations determine whether/how the rest may continue. No RNG retry, I/O
// or durable writes occur here. Final int16 overflow is rejected, not wrapped.
func PlanVitals(player LegacyMonster, room *LegacyRoom, now int32, roll func(int, int) int) (VitalResult, error) {
	return PlanVitalsWithDeath(player, room, now, roll, nil)
}

// PlanVitalsWithDeath resumes at each inline die site using the returned live
// player and room, while retaining the original local ill value. The callback
// must only build a candidate, never commit or publish intermediate changes.
func PlanVitalsWithDeath(player LegacyMonster, room *LegacyRoom, now int32, roll func(int, int) int, onDeath func(LegacyMonster) (LegacyMonster, *LegacyRoom, error)) (VitalResult, error) {
	r := VitalResult{Player: player}
	if room == nil || int64(now) <= int64(player.Timers[8].LastTime)+int64(player.Timers[8].Interval) {
		return r, nil
	}
	if player.Stats[2] > 63 || player.Stats[4] > 63 {
		return VitalResult{}, fmt.Errorf("invalid vital stat index")
	}
	bonus := legacyStatBonus[player.Stats[2]]
	ill := flag(player.Flags[:], 16) || flag(player.Flags[:], 41)
	setHP := func(value int) error {
		if value < -32768 || value > 32767 {
			return fmt.Errorf("HP outside legacy range")
		}
		r.Player.HPCurrent = int16(value)
		return nil
	}
	setMP := func(value int) error {
		if value < -32768 || value > 32767 {
			return fmt.Errorf("MP outside legacy range")
		}
		r.Player.MPCurrent = int16(value)
		return nil
	}
	timer := func(interval int) { r.Player.Timers[8].LastTime = now; r.Player.Timers[8].Interval = int32(interval) }
	damage := func(upper int) error {
		if upper < 1 {
			return fmt.Errorf("invalid damage random range")
		}
		value, err := randomIn(roll, 1, upper)
		if err != nil {
			return err
		}
		// C MAX(1,mrand(...)-bonus) evaluates its second argument again
		// when the first draw minus bonus is >=1, including equality.
		amount := 1
		if value-bonus >= 1 {
			value, err = randomIn(roll, 1, upper)
			if err != nil {
				return err
			}
			amount = value - bonus
		}
		if err = setHP(int(r.Player.HPCurrent) - amount); err != nil {
			return err
		}
		r.Death = r.Player.HPCurrent < 1
		return nil
	}
	disease := func(harm bool) error {
		r.Messages = append(r.Messages, "\n병이 당신의 마음을 잠식합니다.")
		if harm {
			r.Messages = append(r.Messages, "\n당신은 신경질적으로 됩니다.")
		} else {
			r.Messages = append(r.Messages, "몸이 피로해 집니다.\n")
		}
		value, err := randomIn(roll, 1, 6)
		if err != nil {
			return err
		}
		r.Player.Timers[3].LastTime = now
		r.Player.Timers[3].Interval = int32(value + 3)
		if err = damage(6); err != nil {
			return err
		}
		timer(30 - 3*bonus)
		return nil
	}
	barbarian, mage, intelligence := 0, 0, 0
	if player.Class == 2 {
		barbarian = 2
	}
	if player.Class == 5 {
		mage = 2
	}
	if int8(player.Stats[3]) > 17 {
		intelligence = 1
	}
	resume := func() (bool, error) {
		if !r.Death {
			return false, nil
		}
		if onDeath == nil {
			return true, nil
		}
		updated, destination, err := onDeath(r.Player)
		if err != nil {
			return false, err
		}
		if destination == nil || updated.Stats[2] > 63 || updated.Stats[4] > 63 {
			return false, fmt.Errorf("invalid death continuation")
		}
		r.Player = updated
		r.DeathMessageOffsets = append(r.DeathMessageOffsets, len(r.Messages))
		player = updated
		room = destination
		r.Death = false
		bonus = legacyStatBonus[updated.Stats[2]]
		barbarian, mage, intelligence = 0, 0, 0
		if updated.Class == 2 {
			barbarian = 2
		}
		if updated.Class == 5 {
			mage = 2
		}
		if int8(updated.Stats[3]) > 17 {
			intelligence = 1
		}
		return false, nil
	}
	if !flag(room.Flags[:], 24) {
		if !ill {
			hp, mp := int(player.HPCurrent)+max(4, 5+bonus+barbarian), int(player.MPCurrent)+max(4, 5+intelligence+mage)
			timer(5)
			if flag(room.Flags[:], 13) {
				hp += 100
				mp += 100
				timer(1)
			}
			if err := setHP(min(hp, int(player.HPMax))); err != nil {
				return VitalResult{}, err
			}
			if err := setMP(min(mp, int(player.MPMax))); err != nil {
				return VitalResult{}, err
			}
		} else {
			if flag(r.Player.Flags[:], 16) {
				r.Messages = append(r.Messages, "\n독이 당신의 핏줄로 스며듭니다.")
				if err := damage(int(player.HPMax) / 5); err != nil {
					return VitalResult{}, err
				}
				timer(30 - 3*bonus)
				if stop, err := resume(); err != nil {
					return VitalResult{}, err
				} else if stop {
					return r, nil
				}
			}
			if flag(r.Player.Flags[:], 41) {
				if err := disease(false); err != nil {
					return VitalResult{}, err
				}
				if stop, err := resume(); err != nil {
					return VitalResult{}, err
				} else if stop {
					return r, nil
				}
			}
		}
		return r, nil
	}
	if flag(room.Flags[:], 25) {
		r.Messages = append(r.Messages, "\n독기운이 당신을 중독시킵니다.")
		r.Player.Flags[2] |= 1
	}
	if flag(room.Flags[:], 27) {
		r.Messages = append(r.Messages, "\n방이 빙글빙글 도는것 감습니다.\n이제 정신을 차립니다.")
		a, err := randomIn(roll, 1, 6)
		if err != nil {
			return VitalResult{}, err
		}
		b, err := randomIn(roll, 1, 6)
		if err != nil {
			return VitalResult{}, err
		}
		interval := 6
		// C MAX(dice(2,6,0),6) rolls again only when the first sum >6.
		if a+b > 6 {
			a, err = randomIn(roll, 1, 6)
			if err != nil {
				return VitalResult{}, err
			}
			b, err = randomIn(roll, 1, 6)
			if err != nil {
				return VitalResult{}, err
			}
			interval = a + b
		}
		r.Player.Timers[3].LastTime = now
		r.Player.Timers[3].Interval = int32(interval)
	}
	if flag(r.Player.Flags[:], 16) {
		r.Messages = append(r.Messages, "\n독이 당신의 핏줄로 스며듭니다.")
		if err := damage(4); err != nil {
			return VitalResult{}, err
		}
		if stop, err := resume(); err != nil {
			return VitalResult{}, err
		} else if stop {
			return r, nil
		}
	}
	if flag(r.Player.Flags[:], 41) {
		if err := disease(true); err != nil {
			return VitalResult{}, err
		}
		if stop, err := resume(); err != nil {
			return VitalResult{}, err
		} else if stop {
			return r, nil
		}
	}
	if flag(room.Flags[:], 26) {
		r.Player.MPCurrent -= min(r.Player.MPCurrent, 3)
	} else if !ill {
		if err := setMP(min(int(player.MPMax), int(r.Player.MPCurrent)+max(1, 2+intelligence+mage))); err != nil {
			return VitalResult{}, err
		}
	}
	message := ""
	for _, hazard := range []struct {
		room, protection uint
		text             string
	}{
		{21, 30, "\n뜨거운 기운이 당신을 태웁니다."}, {22, 37, "\n물이 당신의 폐로 흘러듭니다."}, {19, 38, "\n흙이 무너져 당신을 덮칩니다."}, {20, 36, "\n차가운 기운이 뼈속까지 스며듭니다."},
	} {
		if flag(room.Flags[:], hazard.room) && !flag(r.Player.Flags[:], hazard.protection) {
			message = hazard.text
			break
		}
	}
	if message == "" {
		anyHazard := false
		for _, bit := range []uint{20, 19, 21, 22, 25, 27, 26} {
			anyHazard = anyHazard || flag(room.Flags[:], bit)
		}
		if !anyHazard {
			message = "\n보이지않는 무엇이 당신의 생명력을 빨아들입니다."
		}
	}
	if message != "" {
		r.Messages = append(r.Messages, message)
		if err := setHP(int(r.Player.HPCurrent) - (8 - min(bonus, 2))); err != nil {
			return VitalResult{}, err
		}
		r.Death = r.Player.HPCurrent < 1
		if stop, err := resume(); err != nil {
			return VitalResult{}, err
		} else if stop {
			return r, nil
		}
	} else if !ill {
		if err := setHP(min(int(player.HPMax), int(r.Player.HPCurrent)+max(1, 3+bonus+barbarian))); err != nil {
			return VitalResult{}, err
		}
	}
	timer(5 - 3*legacyStatBonus[player.Stats[4]])
	return r, nil
}
