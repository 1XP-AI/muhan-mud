package world

import "fmt"

type EffectEvent struct {
	Text string
	Room bool
}
type EffectResult struct {
	Player LegacyMonster
	Events []EffectEvent
}

// ExpireEffects ports update_ply's ordered temporary-effect phase only. It does
// not heal, damage, save, tick hours, consume light, broadcast or commit. Events
// are plain text (ANSI omitted); Room events need actor-aware rendering later.
// Widened deadlines avoid signed overflow. Invalid final numeric values abort
// the entire candidate instead of retaining half-applied effects.
func ExpireEffects(player LegacyMonster, ready [20]*LegacyObject, now int32) (EffectResult, error) {
	r := EffectResult{Player: player}
	type rule struct {
		bit      uint
		timer    int
		dmExempt bool
		text     string
	}
	rules := []rule{
		{19, 16, false, "당신의 몸이 느려졌습니다."},
		{52, 37, false, "당신의 힘이 약해졌습니다."},
		{59, 42, false, "당신의 기가 빠져나갑니다."},
		{53, 38, false, "당신의 무기가 살기를 잃었습니다."},
		{54, 39, false, "참선의 영향력이 떨어졌습니다."},
		{22, 19, false, "당신의 믿음이 약해졌습니다."},
		{2, 0, true, "당신은 이제 눈에 보입니다."},
		{21, 17, true, "당신의 눈이 침침해졌습니다."},
		{20, 18, true, "당신의 감지력이 떨어졌습니다."},
		{1, 14, false, ""},
		{8, 1, false, "당신의 보호력이 떨어졌습니다."},
		{25, 21, true, "당신은 땅에 내려섰습니다."},
		{0, 2, false, "축복력이 떨어졌습니다."},
		{30, 23, false, "당신의 피부가 돌아왔습니다."},
		{36, 29, false, "차가운 기운이 몸을 휩쌉니다."},
		{37, 30, false, "당신의 폐가 줄어들었습니다."},
		{38, 31, false, "당신의 주술 방패가 사라졌습니다."},
		{31, 24, true, "당신은 더이상 날수 없습니다."},
		{32, 25, false, "마법의 방어력이 사라졌습니다."},
		{44, 34, false, "당신의 목소리를 되찾았습니다!"},
		{43, 33, false, "당신은 용기를 되찾았습니다."},
		{33, 27, true, "당신의 분별력이 감퇴되었습니다."},
		{17, 13, true, "마법의 빛이 사라졌습니다."},
		{45, 36, false, "당신의 행동이 정상적으로 되었습니다."},
	}
	reduceStat := func(index, amount int) error {
		value := int(int8(r.Player.Stats[index])) - amount
		if value < 0 || value > 127 {
			return fmt.Errorf("expired effect leaves invalid stat")
		}
		r.Player.Stats[index] = byte(value)
		return nil
	}
	for _, effect := range rules {
		if !flag(r.Player.Flags[:], effect.bit) || (effect.dmExempt && int8(r.Player.Class) >= 12) {
			continue
		}
		timer := r.Player.Timers[effect.timer]
		deadline := int64(timer.LastTime) + int64(timer.Interval)
		if effect.bit == 1 {
			deadline = int64(timer.LastTime) + 300
		}
		if int64(now) <= deadline {
			continue
		}
		r.Player.Flags[effect.bit/8] &^= 1 << (effect.bit % 8)
		var err error
		switch effect.bit {
		case 19:
			err = reduceStat(1, 15)
		case 52:
			err = reduceStat(0, 3)
		case 54:
			err = reduceStat(3, 3)
		case 22:
			err = reduceStat(4, 5)
		case 59:
			if int(r.Player.DicePlus)-5 < -32768 || int(r.Player.HPMax)-100 < -32768 || int(r.Player.MPMax)-100 < -32768 {
				return EffectResult{}, fmt.Errorf("expired effect underflows creature")
			}
			r.Player.DicePlus -= 5
			r.Player.HPMax -= 100
			r.Player.MPMax -= 100
		case 53:
			r.Player.Thaco += 3
		}
		if err != nil {
			return EffectResult{}, err
		}
		switch effect.bit {
		case 19, 52, 59, 53, 54, 8:
			ac, err := ComputeArmorClass(r.Player.Stats[1], ready, flag(r.Player.Flags[:], 8))
			if err != nil {
				return EffectResult{}, err
			}
			r.Player.Armor = byte(ac)
		case 0:
			thaco, err := ComputeThaco(r.Player, ready[19])
			if err != nil {
				return EffectResult{}, err
			}
			r.Player.Thaco = byte(thaco)
		}
		if effect.text != "" {
			r.Events = append(r.Events, EffectEvent{Text: "\n" + effect.text})
		}
		if effect.bit == 17 {
			r.Events = append(r.Events, EffectEvent{Text: "\n%M의 마법의 빛이 사라졌습니다.", Room: true})
		}
	}
	r.Player.Inventory = cloneObjects(player.Inventory)
	return r, nil
}
