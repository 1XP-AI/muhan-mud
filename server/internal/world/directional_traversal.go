package world

// EvaluateDirectionalTraversal ports move through the fall phase. Unlike go,
// sex restrictions precede carried-weight checks. GuardIndex identifies the
// actor for later localized output; death/broadcast/arrival are not applied here.
func EvaluateDirectionalTraversal(e LegacyExit, monsters []LegacyMonster, v TraversalInput, roll func(int, int) int) (TraversalResult, error) {
	r := TraversalResult{HP: v.HP, GuardIndex: -1}
	reject := func(message string) (TraversalResult, error) { r.Stop = true; r.Message = message; return r, nil }
	if message := DirectionalExitRestriction(e, v.PassageOptions); message != "" {
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
	if flag(e.Flags[:], 12) && v.Male {
		return reject("여성만 들어갈수 있습니다. 여탕인가~~")
	}
	if flag(e.Flags[:], 13) && !v.Male {
		return reject("남성만 들어갈수 있습니다.")
	}
	if flag(e.Flags[:], 7) && v.CarriedWeight != 0 {
		return reject("뭘 가지고는 들어갈수 없습니다.")
	}
	return evaluateTraversalFall(e, v, roll, true)
}
