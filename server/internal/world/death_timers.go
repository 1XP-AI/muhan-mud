package world

import "fmt"

type DeathTimerPlan struct {
	Victim, Attacker LegacyTimer
	// Caller must remove victim from attacker's authoritative enemy relations.
	RemoveEnemy bool
}

// PlanDeathTimers ports die's LT_PLYKL update, including pointer aliasing when
// attacker==victim. Inputs are slot 11, not whole timer arrays. Other slots and
// Misc remain unchanged. Context must be derived by the server, not the client.
func PlanDeathTimers(victim, attacker LegacyTimer, attackerPlayer, self, survival bool, now int32, roll func(int, int) int) (DeathTimerPlan, error) {
	if self && !attackerPlayer {
		return DeathTimerPlan{}, fmt.Errorf("invalid self death")
	}
	victim.LastTime, victim.Interval = 0, 0
	if self {
		attacker = victim
	}
	removeEnemy := !attackerPlayer || survival
	if !removeEnemy {
		if roll == nil {
			return DeathTimerPlan{}, fmt.Errorf("missing death timer RNG")
		}
		days := roll(7, 14)
		if days < 7 || days > 14 {
			return DeathTimerPlan{}, fmt.Errorf("invalid death timer RNG")
		}
		attacker.LastTime, attacker.Interval = now, int32(days*86400)
	}
	if self {
		victim = attacker
	}
	return DeathTimerPlan{Victim: victim, Attacker: attacker, RemoveEnemy: removeEnemy}, nil
}
