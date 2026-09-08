package world

import (
	"errors"
	"fmt"
)

type TraversalInput struct {
	PassageOptions
	HP, CarriedWeight           int
	FallSkill                   int32
	Class                       byte
	Invisible, Male, Levitating bool
}
type TraversalResult struct {
	HP, Damage, GuardIndex int
	Fell, Dead, Stop       bool
	Message                string
}

// EvaluateTraversal ports go() through its climbing/fall phase, in order.
// The caller owns RNG and must commit the returned damage/death event, even
// when movement stops. Stop=false is NOT final admission: cooldown, stealth,
// destination rules, followers and persistence are later phases.
func EvaluateTraversal(e LegacyExit, monsters []LegacyMonster, v TraversalInput, roll func(int, int) int) (TraversalResult, error) {
	r := TraversalResult{HP: v.HP, GuardIndex: -1}
	reject := func(message string) (TraversalResult, error) { r.Stop = true; r.Message = message; return r, nil }
	if message := ExitRestriction(e, v.PassageOptions); message != "" {
		return reject(message)
	}
	if flag(e.Flags[:], 18) && v.Class < 11 {
		for i, m := range monsters {
			if flag(m.Flags[:], 38) && (!v.Invisible || flag(m.Flags[:], 21)) {
				r.GuardIndex = i
				r.Stop = true
				return r, nil
			}
		}
	}
	if flag(e.Flags[:], 7) && v.CarriedWeight != 0 {
		return reject("뭘 가지고는 들어갈 수 없습니다.")
	}
	if flag(e.Flags[:], 12) && v.Male {
		return reject("여성만 들어갈 수 있습니다. 여탕인가~~")
	}
	if flag(e.Flags[:], 13) && !v.Male {
		return reject("남성만 들어갈 수 있습니다.")
	}
	return evaluateTraversalFall(e, v, roll, false)
}

// Shared arithmetic; move and go differ in nonfatal fall punctuation.
func evaluateTraversalFall(e LegacyExit, v TraversalInput, roll func(int, int) int, directional bool) (TraversalResult, error) {
	r := TraversalResult{HP: v.HP, GuardIndex: -1}
	if (!flag(e.Flags[:], 8) && !flag(e.Flags[:], 9)) || v.Levitating {
		return r, nil
	}
	if roll == nil {
		return TraversalResult{}, errors.New("missing traversal random source")
	}
	fall := int64(50) - int64(v.FallSkill)
	if flag(e.Flags[:], 10) {
		fall += 50
	}
	chance := roll(1, 100)
	if chance < 1 || chance > 100 {
		return TraversalResult{}, errors.New("traversal random value outside requested range")
	}
	if int64(chance) >= fall {
		return r, nil
	}
	upper := int(15 + fall/10)
	damage := roll(5, upper)
	if damage < 5 || damage > upper {
		return TraversalResult{}, errors.New("traversal damage outside requested range")
	}
	r.Fell = true
	r.Damage = damage
	if v.HP <= damage {
		r.HP = 0
		r.Dead = true
		r.Stop = true
		r.Message = "당신은 죽음이 다가오는것같은 느낌이 듭니다."
		return r, nil
	}
	r.HP -= damage
	r.Stop = flag(e.Flags[:], 8)
	r.Message = fmt.Sprintf("당신은 구덩이에 떨어져서 %d 만큼의 상처를 입었습니다.", damage)
	if directional {
		r.Message = fmt.Sprintf("당신은 구덩이에 떨어져서 %d 만큼의 상처를 입었습니다", damage)
	}
	return r, nil
}
