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

// ArrivalTrapEvent is the reducer-owned projection of one triggered arrival
// trap. It is intentionally separate from ArrivalTrapResult and State:
// transport may fan it out after the candidate commits, while the follower
// subset can be persisted in a private command projection without changing the
// public command response. RoomID is the room in which check_traps ran, before
// a PIT relocation or a lethal death transition.
type ArrivalTrapEvent struct {
	ActorID          string   `json:"actor_id"`
	ActorName        string   `json:"actor_name"`
	RoomID           int16    `json:"room_id"`
	Trap             byte     `json:"trap"`
	ActorText        string   `json:"actor_text"`
	RoomText         string   `json:"room_text"`
	RoomRecipientIDs []string `json:"room_recipient_ids,omitempty"`
}

// arrivalTrapEventFor renders the C check_traps actor and room output after
// the effect has been planned. The caller supplies the pre-effect room and
// actor identity because a PIT/death may leave the actor elsewhere by commit.
// Avoided and levitating PIT results deliberately produce no event: C returns
// before the trap-output switch in both cases.
func arrivalTrapEventFor(actorID, actorName string, roomID int16, effect ArrivalTrapResult, roomRecipientIDs []string) (ArrivalTrapEvent, bool) {
	if actorID == "" || actorName == "" || roomID == 0 || !effect.Triggered || effect.Avoided || effect.Suppressed {
		return ArrivalTrapEvent{}, false
	}
	event := ArrivalTrapEvent{
		ActorID:          actorID,
		ActorName:        actorName,
		RoomID:           roomID,
		Trap:             effect.Trap,
		RoomRecipientIDs: append([]string(nil), roomRecipientIDs...),
	}
	switch effect.Trap {
	case TrapPit:
		event.ActorText = fmt.Sprintf("당신은 구덩이에 빠졌습니다!\n당신은 %d점의 피해를 입었습니다.\n", effect.Damage)
		event.RoomText = fmt.Sprintf("\n%s이 구덩이에 빠졌습니다.\r\n", actorName)
	case TrapDart:
		event.ActorText = fmt.Sprintf("당신은 숨겨진 독화살에 맞았습니다!\n당신은 %d점의 피해를 입었습니다.\n", effect.Damage)
		event.RoomText = fmt.Sprintf("\n%s이 숨겨진 독화살에 맞았습니다.\r\n", actorName)
	case TrapBlock:
		event.ActorText = fmt.Sprintf("당신은 커다란 돌에 맞았습니다!\n당신은 %d점의 피해를 입었습니다.\n", effect.Damage)
		event.RoomText = fmt.Sprintf("\n%s 위로 커다란 돌이 떨어졌습니다.\r\n", actorName)
	case TrapMPDam:
		event.ActorText = fmt.Sprintf("당신의 마음이 충격을 받았습니다!\n당신은 %d점의 마력을 잃었습니다.\n당신은 %d점의 피해를 입었습니다.\n", effect.MPDamage, effect.Damage)
		event.RoomText = fmt.Sprintf("\n%s이 강한 충격을 받았습니다.\r\n", actorName)
	case TrapRMSpl:
		event.ActorText = "어두운 기운이 당신을 감쌉니다.\n당신의 주문이 사라집니다.\n"
		event.RoomText = fmt.Sprintf("\n어두운 기운이 %s을 감쌉니다.\r\n", actorName)
	case TrapNaked:
		event.ActorText = "붉은 액체가 당신위로 쏟아집니다.\n으악!!! 당신의 장비가 녹아버립니다.\n"
		event.RoomText = fmt.Sprintf("\n붉은 액체가 %s님위로 쏟아집니다.\r\n", actorName)
	case TrapAlarm:
		event.ActorText = "경보장치가 울립니다!\n근처에 경비원들이 없길 바랍니다.\n"
		event.RoomText = fmt.Sprintf("\n%s이 경보장치를 건드렸습니다!\r\n", actorName)
	default:
		return ArrivalTrapEvent{}, false
	}
	return event, true
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
