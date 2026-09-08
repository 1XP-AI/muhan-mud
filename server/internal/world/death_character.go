package world

import "fmt"

// Rules must be derived from authoritative attacker identity and room/family
// state. Self is also AttackerPlayer, including poison/environment deaths.
type DeathCharacterRules struct {
	AttackerPlayer, Self, Survival, WarAllowsLoss bool
}

type DeathCharacterPlan struct {
	Body          LegacyMonster
	Kept, Dropped ItemCollection
}

// PlanDeathCharacter composes the victim's progression, recovery and equipment
// changes. It is NOT an independently committable death: the caller must also
// apply killer timers (including self), enemies/family state, floor ownership,
// departure and room 1008 entry, then commit everything before publishing.
// Timer changes are intentionally left to that multi-actor transition.
func PlanDeathCharacter(body LegacyMonster, items ItemCollection, rules DeathCharacterRules) (DeathCharacterPlan, error) {
	if body.Type != 0 || len(body.Inventory) != 0 {
		return DeathCharacterPlan{}, fmt.Errorf("death requires player with canonical items")
	}
	if err := items.Validate(); err != nil {
		return DeathCharacterPlan{}, err
	}
	next, err := ApplyDeathProgression(body, DeathProgressionRules{
		AttackerPlayer: rules.AttackerPlayer, Self: rules.Self,
		SkillLoss: !rules.Survival && rules.WarAllowsLoss,
	})
	if err != nil {
		return DeathCharacterPlan{}, err
	}
	next.HPCurrent = next.HPMax
	if rules.AttackerPlayer {
		next.MPCurrent = max(next.MPCurrent, next.MPMax/10)
	} else {
		next.MPCurrent = next.MPMax
	}
	next.Flags[2] &^= 1 // PPOISN
	next.Flags[5] &^= 2 // PDISEA
	equipment, err := PlanDeathEquipment(items, DeathEquipmentRules{
		AttackerPlayer: rules.AttackerPlayer, Survival: rules.Survival, WarAllowsLoss: rules.WarAllowsLoss,
	})
	if err != nil {
		return DeathCharacterPlan{}, err
	}
	stats, err := equipment.Kept.CombatStats(next)
	if err != nil {
		return DeathCharacterPlan{}, err
	}
	next.Armor, next.Thaco = byte(stats.Armor), byte(stats.Thaco)
	return DeathCharacterPlan{Body: next, Kept: equipment.Kept, Dropped: equipment.Dropped}, nil
}
