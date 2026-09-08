package world

import "errors"

// MovementInput is one authoritative snapshot, never assembled from client
// supplied capabilities. Destination may be nil when its resource is missing.
type MovementInput struct {
	Source                         LegacyRoom
	Destination                    *LegacyRoom
	Occupants                      []RoomPlayerView
	EnemyOfPlayer                  []bool
	Prefix                         string
	Occurrence                     int
	DetectInvisible, Hidden, Blind bool
	DexterityBonus                 int32
	Now, AttackReadyAt             int64
	Traversal                      TraversalInput
	Visitor                        DestinationVisitor
}

// MovementProposal is NOT a committed move. The world transaction must apply
// actor HP/hidden/location and source track together with output/death events.
// Arrival traps/follower orchestration are applied by DirectionalStep after
// this single-actor proposal is committed in memory.
type MovementProposal struct {
	RoomID        int16
	HP            int
	Hidden, Moved bool
	Track         string
	WaitUntil     int64
	Traversal     TraversalResult
	BlockerIndex  int
	Messages      []string
}

func PlanMovement(in MovementInput, roll func(int, int) int) (MovementProposal, error) {
	return planMovement(in, roll, false)
}

// PlanDirectionalMovement ports move through destination admission, preserving
// its exact lookup and absent attack cooldown. Arrival/followers/traps and death
// application remain the caller's unfinished orchestration responsibilities.
func PlanDirectionalMovement(in MovementInput, roll func(int, int) int) (MovementProposal, error) {
	return planMovement(in, roll, true)
}

func planMovement(in MovementInput, roll func(int, int) int, directional bool) (MovementProposal, error) {
	r := MovementProposal{RoomID: in.Source.ID, HP: in.Traversal.HP, Hidden: in.Hidden, Track: in.Source.Track, BlockerIndex: -1, Traversal: TraversalResult{GuardIndex: -1}}
	if in.Traversal.Class != in.Visitor.Class || (len(in.EnemyOfPlayer) != 0 && len(in.EnemyOfPlayer) != len(in.Source.Monsters)) {
		return MovementProposal{}, errors.New("inconsistent movement snapshot")
	}
	if in.Prefix == "" && !directional {
		r.Messages = append(r.Messages, "어디로 가고 싶으세요?")
		return r, nil
	}
	index := SelectExit(in.Source.Exits, in.Prefix, in.Occurrence, in.DetectInvisible)
	if directional {
		index = SelectDirectionalExit(in.Source.Exits, in.Prefix)
	}
	if index < 0 {
		message := "그런 출구는 없습니다."
		if directional {
			message = "길이 막혀 있습니다."
		}
		r.Messages = append(r.Messages, message)
		return r, nil
	}
	e := in.Source.Exits[index]
	evaluate := EvaluateTraversal
	if directional {
		evaluate = EvaluateDirectionalTraversal
	}
	traversal, err := evaluate(e, in.Source.Monsters, in.Traversal, roll)
	if err != nil {
		return MovementProposal{}, err
	}
	r.Traversal = traversal
	r.HP = traversal.HP
	if traversal.Message != "" {
		r.Messages = append(r.Messages, traversal.Message)
	}
	if traversal.Stop {
		return r, nil
	}
	if !directional && in.Now < in.AttackReadyAt {
		r.WaitUntil = in.AttackReadyAt
		return r, nil
	}
	class := in.Traversal.Class
	if r.Hidden && (class == 1 || class == 8 || class > 9) {
		chance := int64(5+6*((int(in.Visitor.Level)+3)/4)) + 3*int64(in.DexterityBonus)
		if chance > 85 {
			chance = 85
		}
		if in.Blind && chance > 20 {
			chance = 20
		}
		if roll == nil {
			return MovementProposal{}, errors.New("missing stealth random source")
		}
		value := roll(1, 100)
		if value < 1 || value > 100 {
			return MovementProposal{}, errors.New("stealth random value outside requested range")
		}
		if int64(value) > chance {
			r.Hidden = false
			r.Messages = append(r.Messages, "당신은 은신술을 사용하는데 실패하였습니다.\n")
			for i, m := range in.Source.Monsters {
				if flag(m.Flags[:], 8) && len(in.EnemyOfPlayer) > 0 && in.EnemyOfPlayer[i] && in.Traversal.Invisible && class < 11 {
					r.BlockerIndex = i
					return r, nil
				}
			}
		}
	} else {
		r.Hidden = false
	}
	// Legacy records this before loading/checking the destination, even on denial.
	if !flag(in.Source.Flags[:], 18) {
		r.Track = e.Name
	}
	if in.Destination == nil || in.Destination.ID == in.Source.ID {
		message := "그 방향의 지도가 없습니다."
		if directional {
			message = "그쪽으로 지도가 없습니다. 신에게 연락해 주세요."
		}
		r.Messages = append(r.Messages, message)
		return r, nil
	}
	if in.Destination.ID != e.Destination {
		return MovementProposal{}, errors.New("destination does not match selected exit")
	}
	if message := destinationRestriction(in.Destination.LegacyRoomHeader, in.Occupants, in.Visitor, directional); message != "" {
		r.Messages = append(r.Messages, message)
		return r, nil
	}
	r.RoomID = in.Destination.ID
	r.Moved = true
	return r, nil
}
