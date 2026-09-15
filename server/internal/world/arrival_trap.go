package world

import "fmt"

// These values are the on-disk room trap numbers from src/mtype.h.  Keep the
// numeric values stable: room resources and migration fixtures contain them.
const (
	TrapPit   byte = 1
	TrapDart  byte = 2
	TrapBlock byte = 3
	TrapMPDam byte = 4
	TrapRMSpl byte = 5
	TrapNaked byte = 6
	TrapAlarm byte = 7
)

// ArrivalTrapInput is the actor-owned portion of check_traps.  It is built
// from the authoritative player snapshot; a client cannot supply any field.
type ArrivalTrapInput struct {
	HP, HPMax               int
	MP, MPMax               int
	Dexterity, Intelligence int
	Levitating, Prepared    bool
}

// ArrivalTrapResult is a deterministic effect proposal.  It is deliberately
// separate from State so callers can test the C ordering and commit the whole
// result through the normal world command receipt.
type ArrivalTrapResult struct {
	Trap               byte
	PreparationCleared bool
	Avoided            bool
	Triggered          bool
	Suppressed         bool
	Relocate           bool
	RelocateTo         int16
	HP, MP             int
	Damage, MPDamage   int
	Poisoned           bool
	ClearSpells        bool
	LoseItems          bool
	Alarm              bool
	Dead               bool
}

func trapRoll(roll func(int, int) int, low, high int) (int, error) {
	if roll == nil {
		return 0, fmt.Errorf("missing arrival trap random source")
	}
	value := roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("arrival trap random value outside requested range")
	}
	return value, nil
}

// PlanArrivalTrap ports room.c:check_traps after a successful arrival.  The
// caller must not invoke it for denied, guard-blocked, fall-stopped, or
// destination-admission-failed movement: C reaches check_traps only after the
// actor has been inserted into the destination room.
func PlanArrivalTrap(room LegacyRoom, in ArrivalTrapInput, roll func(int, int) int) (ArrivalTrapResult, error) {
	r := ArrivalTrapResult{Trap: room.Trap, HP: in.HP, MP: in.MP}
	if room.Trap == 0 {
		// C clears PPREPA even when the destination has no trap.
		r.PreparationCleared = in.Prepared
		return r, nil
	}
	if in.HP < 0 || in.HPMax < 0 || in.MP < 0 || in.MPMax < 0 {
		return ArrivalTrapResult{}, fmt.Errorf("negative actor vitals")
	}

	// PIT/DART/BLOCK/NAKED/ALARM use dexterity for both prepared and normal
	// avoidance.  MPDAM/RMSPL use intelligence and a 1..25 prepared roll.
	stat, preparedHigh := in.Dexterity, 20
	switch room.Trap {
	case TrapPit, TrapDart, TrapBlock, TrapNaked, TrapAlarm:
		// keep dexterity group
	case TrapMPDam, TrapRMSpl:
		stat, preparedHigh = in.Intelligence, 25
	default:
		return ArrivalTrapResult{}, fmt.Errorf("unknown room trap %d", room.Trap)
	}
	r.PreparationCleared = true
	if in.Prepared {
		value, err := trapRoll(roll, 1, preparedHigh)
		if err != nil {
			return ArrivalTrapResult{}, err
		}
		if value < stat {
			r.Avoided = true
			return r, nil
		}
	}
	value, err := trapRoll(roll, 1, 100)
	if err != nil {
		return ArrivalTrapResult{}, err
	}
	if value < stat {
		r.Avoided = true
		return r, nil
	}
	r.Triggered = true

	switch room.Trap {
	case TrapPit:
		if in.Levitating {
			r.Suppressed = true
			return r, nil
		}
		r.RelocateTo = room.TrapExit
		r.Relocate = true
		r.Damage, err = trapRoll(roll, 1, 15)
		if err != nil {
			return ArrivalTrapResult{}, err
		}
	case TrapDart:
		r.Poisoned = true
		r.Damage, err = trapRoll(roll, 1, 10)
		if err != nil {
			return ArrivalTrapResult{}, err
		}
	case TrapBlock:
		r.Damage = in.HPMax / 3
	case TrapMPDam:
		r.MPDamage = in.MPMax / 2
		if r.MPDamage > in.MP {
			r.MPDamage = in.MP
		}
		r.Damage, err = trapRoll(roll, 1, 6)
		if err != nil {
			return ArrivalTrapResult{}, err
		}
	case TrapRMSpl:
		r.ClearSpells = true
	case TrapNaked:
		r.LoseItems = true
	case TrapAlarm:
		r.Alarm = true
	}
	r.HP -= r.Damage
	r.MP -= r.MPDamage
	r.Dead = r.HP < 1
	return r, nil
}
