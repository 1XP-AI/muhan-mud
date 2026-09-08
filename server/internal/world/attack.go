package world

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNPCDeathTransitionPending = errors.New("lethal NPC transition pending")

// NPCMeleeAttackOptions carries host-owned context needed when a lethal swing
// crosses into the durable NPC death reducer. RNG and item identity allocation
// remain outside the client payload.
type NPCMeleeAttackOptions struct {
	Now      int32
	Allocate func() (string, error)
}

// NPCMeleeAttackResult is the committed portion of command5's player→monster
// attack. A lethal swing includes the NPC death transition in the same state
// candidate; it never leaves a half-dead identity in the world.
type NPCMeleeAttackResult struct {
	TargetID, TargetName string
	WeaponName           string
	Hit, Critical        bool
	Damage, TargetHP     int
	SwingCount           int
	Killed               bool
	ExperienceAward      int32
	QuestExperienceAward int32
	WeaponBroken         bool
	WeaponDropped        bool
	ProficiencyAward     int32
}

// SelectNPCInRoom resolves a display name against the authoritative ordered
// room identities. The client never supplies an NPC ID; exact matching avoids
// name-prefix or map-order ambiguity until the legacy occurrence parser is
// ported.
func (s State) SelectNPCInRoom(actorID, name string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || s.NPCs == nil {
		return "", fmt.Errorf("canonical NPC attack context required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("attack target required")
	}
	for _, id := range s.Rooms[actor.Body.RoomID].NPCIDs {
		if strings.EqualFold(s.NPCs[id].Body.Name, name) {
			return id, nil
		}
	}
	return "", fmt.Errorf("NPC target absent")
}

func readyObject(items *ItemCollection, slot int) (*LegacyObject, error) {
	if items == nil || slot < 0 || slot >= len(items.Ready) {
		return nil, nil
	}
	id := items.Ready[slot]
	if id == "" {
		return nil, nil
	}
	item, ok := items.Items[id]
	if !ok {
		return nil, fmt.Errorf("ready item absent")
	}
	object := item.Object
	return &object, nil
}

// moveReadyWeaponToInventory applies add_obj_crt's canonical ordering. The
// legacy routine leaves OWEARS/OWHELD bits untouched when a weapon is dropped
// or found broken at the start of a swing, so ownership, not a display bit, is
// authoritative here and the flags are preserved for differential tests.
func moveReadyWeaponToInventory(player *PlayerState, weaponID string) error {
	if player == nil || player.Items == nil || weaponID == "" || player.Items.Ready[19] != weaponID {
		return fmt.Errorf("ready weapon absent")
	}
	player.Items.Ready[19] = ""
	if err := insertInventoryRoot(player.Items, weaponID); err != nil {
		return err
	}
	return refreshEquipmentStats(player)
}

// destroyReadyWeapon is the ID-graph equivalent of free_obj. Every child is
// released with its root; no detached item identity may remain in storage.
func destroyReadyWeapon(items *ItemCollection, weaponID string) error {
	if items == nil || weaponID == "" || items.Ready[19] != weaponID {
		return fmt.Errorf("ready weapon absent")
	}
	stack := []string{weaponID}
	seen := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic weapon subtree")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("weapon subtree item absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	items.Ready[19] = ""
	return nil
}

func meleeDice(body LegacyMonster, weapon *LegacyObject, roll func(int, int) int) (int, error) {
	count, sides, plus := int(body.DiceCount), int(body.DiceSides), int(body.DicePlus)
	if weapon != nil {
		count, sides, plus = int(weapon.DiceCount), int(weapon.DiceSides), int(weapon.DicePlus)
	}
	if count < 1 || sides < 1 {
		return 0, fmt.Errorf("invalid melee dice")
	}
	damage := plus
	for i := 0; i < count; i++ {
		n, err := randomIn(roll, 1, sides)
		if err != nil {
			return 0, err
		}
		damage += n
	}
	return damage, nil
}

func criticalDivisor(class byte) int {
	switch class {
	case 1, 2, 9, 10, 11, 12:
		return 20
	case 7, 6:
		return 25
	case 8, 3:
		return 30
	default:
		return 40
	}
}

// PlanNPCMeleeAttack ports the first durable player→monster attack slice from
// command5.c: authoritative target lookup is done by room identity, the C
// THAC0/armor hit gate and damage dice are replay-safe, hidden/invisible state
// is cleared on attack, and the NPC records the attacker as an enemy. It does
// not claim full player combat, flee, summon, or full combat-tick semantics
// yet; basic lethal NPC removal, PUPDMG multi-swing, and command5 weapon
// break/drop durability behavior are included.
func (s State) PlanNPCMeleeAttack(actorID, targetID string, roll func(int, int) int) (State, NPCMeleeAttackResult, error) {
	return s.PlanNPCMeleeAttackWithOptions(actorID, targetID, roll, NPCMeleeAttackOptions{})
}

const playerPowerDamageFlag = 59 // PUPDMG

func npcAttackSwingCount(body LegacyMonster, roll func(int, int) int) (int, error) {
	count := 1
	if !flag(body.Flags[:], playerPowerDamageFlag) {
		return count, nil
	}
	if (body.Class == 9 && body.Level > 100) || body.Class > 9 {
		burst, err := randomIn(roll, 0, 3)
		if err != nil {
			return 0, err
		}
		if (int(body.Level)-97)/10+burst > 2 {
			count++
		}
	}
	if body.Class > 9 {
		bonus, err := randomIn(roll, 1, 4)
		if err != nil {
			return 0, err
		}
		if bonus == 1 {
			count++
		}
	}
	return count, nil
}

func mergeNPCMeleeAttackResult(total *NPCMeleeAttackResult, swing NPCMeleeAttackResult) {
	if total.SwingCount == 0 {
		*total = swing
		total.SwingCount = 1
		return
	}
	if total.WeaponName == "" {
		total.WeaponName = swing.WeaponName
	}
	total.SwingCount++
	total.Hit = total.Hit || swing.Hit
	total.Critical = total.Critical || swing.Critical
	total.Damage += swing.Damage
	total.TargetHP = swing.TargetHP
	total.Killed = total.Killed || swing.Killed
	total.ExperienceAward += swing.ExperienceAward
	total.QuestExperienceAward += swing.QuestExperienceAward
	total.WeaponBroken = total.WeaponBroken || swing.WeaponBroken
	total.WeaponDropped = total.WeaponDropped || swing.WeaponDropped
	total.ProficiencyAward += swing.ProficiencyAward
}

// PlanNPCMeleeAttackWithOptions is the host-aware form used by the online
// command loop. The compatibility wrapper above keeps pure combat tests that
// do not need drop allocation unchanged. PUPDMG's extra swings are planned as
// one receipt, and a lethal/depleted-weapon swing stops the C-style loop.
func (s State) PlanNPCMeleeAttackWithOptions(actorID, targetID string, roll func(int, int) int, options NPCMeleeAttackOptions) (State, NPCMeleeAttackResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Items == nil {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("online equipped attacker required")
	}
	target, ok := s.NPCs[targetID]
	if s.NPCs == nil || !ok || target.Body.RoomID != actor.Body.RoomID || target.Body.HPCurrent < 1 {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC attack target absent")
	}
	if target.Enemies == nil {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC enemy relations unresolved")
	}
	if actor.Body.Class == 0 || flag(actor.Body.Flags[:], 42) {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("attacker cannot attack")
	}
	swings, err := npcAttackSwingCount(actor.Body, roll)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	next := s
	var total NPCMeleeAttackResult
	for i := 0; i < swings; i++ {
		candidate, result, swingErr := next.planNPCMeleeAttackSwing(actorID, targetID, roll, options)
		if swingErr != nil {
			return State{}, NPCMeleeAttackResult{}, swingErr
		}
		mergeNPCMeleeAttackResult(&total, result)
		next = candidate
		if result.Killed || result.WeaponBroken {
			break
		}
	}
	return next, total, nil
}

// planNPCMeleeAttackSwing performs exactly one command5 loop iteration.
func (s State) planNPCMeleeAttackSwing(actorID, targetID string, roll func(int, int) int, options NPCMeleeAttackOptions) (State, NPCMeleeAttackResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Items == nil {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("online equipped attacker required")
	}
	target, ok := s.NPCs[targetID]
	if s.NPCs == nil || !ok || target.Body.RoomID != actor.Body.RoomID || target.Body.HPCurrent < 1 {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC attack target absent")
	}
	if target.Enemies == nil {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC enemy relations unresolved")
	}
	if actor.Body.Class == 0 || flag(actor.Body.Flags[:], 42) {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("attacker cannot attack")
	}
	weapon, err := readyObject(actor.Items, 19)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	held, err := readyObject(actor.Items, 17)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	combat, err := actor.Items.CombatStats(actor.Body)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	threshold := int(combat.Thaco) - int(int8(target.Body.Armor))/8
	if flag(actor.Body.Flags[:], 43) {
		threshold += 2
	}
	if flag(actor.Body.Flags[:], 42) {
		threshold += 5
	}
	next := s.clone()
	result := NPCMeleeAttackResult{TargetID: targetID, TargetName: target.Body.Name, TargetHP: int(target.Body.HPCurrent)}
	if weapon != nil {
		result.WeaponName = weapon.Name
	}
	attacker := next.Players[actorID]
	// command5 reveals both hidden and invisible attackers before the swing.
	attacker.Body.Flags[0] &^= 1 << 1
	attacker.Body.Flags[0] &^= 1 << 2
	next.Players[actorID] = attacker
	target = next.NPCs[targetID]
	knownEnemy := false
	for _, enemy := range target.Enemies {
		if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
			knownEnemy = true
			break
		}
	}
	if !knownEnemy {
		target.Enemies = append(target.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: actorID}, Damage: 0})
	}
	// command5 checks a depleted wielded weapon before consuming a hit roll.
	// The hostility relation and reveal above are still part of this command.
	if weapon != nil && weapon.ShotsCurrent < 1 {
		if err := moveReadyWeaponToInventory(&attacker, actor.Items.Ready[19]); err != nil {
			return State{}, NPCMeleeAttackResult{}, err
		}
		result.WeaponBroken = true
		next.Players[actorID] = attacker
		next.NPCs[targetID] = target
		if err := next.Validate(); err != nil {
			return State{}, NPCMeleeAttackResult{}, err
		}
		return next, result, nil
	}
	hitRoll, err := randomIn(roll, 1, 30)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	result.Hit = hitRoll >= threshold
	if !result.Hit {
		next.NPCs[targetID] = target
		if err := next.Validate(); err != nil {
			return State{}, NPCMeleeAttackResult{}, err
		}
		return next, result, nil
	}
	var damage int
	if actor.Body.Class == 3 || actor.Body.Class == 5 {
		// C cleric/mage damage excludes weapon proficiency and held-item bonus.
		damage, err = meleeDice(actor.Body, weapon, roll)
		if err != nil {
			return State{}, NPCMeleeAttackResult{}, err
		}
	} else {
		damage, err = meleeDice(actor.Body, weapon, roll)
		if err != nil {
			return State{}, NPCMeleeAttackResult{}, err
		}
		if weapon != nil {
			index := int(weapon.Type)
			if index > 4 {
				index = 2
			}
			proficiency, proficiencyErr := WeaponProficiency(actor.Body.Class, actor.Body.Proficiency[index])
			if proficiencyErr != nil {
				return State{}, NPCMeleeAttackResult{}, proficiencyErr
			}
			damage += proficiency / 10
			if held != nil && held.Type < 5 {
				extra, extraErr := meleeDice(actor.Body, held, roll)
				if extraErr != nil {
					return State{}, NPCMeleeAttackResult{}, extraErr
				}
				damage += extra / 10
			}
		}
	}
	if target.Body.Class >= 12 {
		damage = 0
	}
	if damage < 1 {
		damage = 1
	}
	mod := 0
	index := 2
	if weapon != nil && weapon.Type <= 4 {
		index = int(weapon.Type)
	}
	proficiency, proficiencyErr := WeaponProficiency(actor.Body.Class, actor.Body.Proficiency[index])
	if proficiencyErr != nil {
		return State{}, NPCMeleeAttackResult{}, proficiencyErr
	}
	mod = proficiency / criticalDivisor(actor.Body.Class)
	criticalRoll, err := randomIn(roll, 1, 100)
	if err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	if criticalRoll <= mod || (weapon != nil && flag(weapon.Flags[:], 42)) {
		result.Critical = true
		multiplier, multiplierErr := randomIn(roll, 3, 6)
		if multiplierErr != nil {
			return State{}, NPCMeleeAttackResult{}, multiplierErr
		}
		damage *= multiplier
	}
	if actor.Body.Class == 6 {
		if actor.Body.Alignment < 0 {
			damage /= 2
		} else if actor.Body.Alignment > 250 {
			bonus, bonusErr := randomIn(roll, 1, 3)
			if bonusErr != nil {
				return State{}, NPCMeleeAttackResult{}, bonusErr
			}
			damage += bonus
		}
	}
	if damage < 1 {
		damage = 1
	}
	weaponReady := weapon != nil
	if result.Critical && weapon != nil && !flag(weapon.Flags[:], objectNeverShattersFlag) {
		shatter := flag(weapon.Flags[:], objectAlwaysCriticalFlag)
		if !shatter {
			shatterRoll, shatterErr := randomIn(roll, 1, 100)
			if shatterErr != nil {
				return State{}, NPCMeleeAttackResult{}, shatterErr
			}
			shatter = shatterRoll < 3
		}
		if shatter && !flag(weapon.Flags[:], objectOneWevFlag) {
			if err := destroyReadyWeapon(attacker.Items, actor.Items.Ready[19]); err != nil {
				return State{}, NPCMeleeAttackResult{}, err
			}
			if err := refreshEquipmentStats(&attacker); err != nil {
				return State{}, NPCMeleeAttackResult{}, err
			}
			result.WeaponBroken = true
			weaponReady = false
		}
	} else if !result.Critical && weapon != nil && !flag(weapon.Flags[:], objectCursedFlag) {
		dropRoll, dropErr := randomIn(roll, 1, 100)
		if dropErr != nil {
			return State{}, NPCMeleeAttackResult{}, dropErr
		}
		if dropRoll <= 5-proficiency/criticalDivisor(actor.Body.Class) {
			if err := moveReadyWeaponToInventory(&attacker, actor.Items.Ready[19]); err != nil {
				return State{}, NPCMeleeAttackResult{}, err
			}
			result.WeaponDropped = true
			weaponReady = false
			damage = 0
		}
	}
	// C decrements shots after the break/drop branch. A weapon which becomes
	// empty during this swing remains equipped until the next command.
	if weaponReady && weapon != nil {
		shotRoll, shotErr := randomIn(roll, 0, 3)
		if shotErr != nil {
			return State{}, NPCMeleeAttackResult{}, shotErr
		}
		if shotRoll == 0 {
			weaponID := actor.Items.Ready[19]
			item, ok := attacker.Items.Items[weaponID]
			if !ok || item.Object.ShotsCurrent == -32768 {
				return State{}, NPCMeleeAttackResult{}, fmt.Errorf("weapon shot count underflow")
			}
			item.Object.ShotsCurrent--
			attacker.Items.Items[weaponID] = item
		}
	}
	previousHP := int(target.Body.HPCurrent)
	if int64(target.Body.HPCurrent)-int64(damage) < -32768 {
		return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC HP underflow")
	}
	target.Body.HPCurrent -= int16(damage)
	result.Damage = damage
	result.TargetHP = int(target.Body.HPCurrent)
	dealt := min(previousHP, damage)
	if weaponReady && weapon != nil && target.Body.Type != 0 && dealt > 0 && target.Body.Experience > 0 {
		index := int(weapon.Type)
		if index > 4 {
			index = 4
		}
		hpMax := int64(target.Body.HPMax)
		if hpMax < 1 {
			hpMax = 1
		}
		award64 := int64(dealt) * int64(target.Body.Experience) / hpMax
		if award64 > int64(target.Body.Experience) {
			award64 = int64(target.Body.Experience)
		}
		if award64 > 0 {
			if int64(attacker.Body.Proficiency[index])+award64 > int64(^uint32(0)>>1) {
				return State{}, NPCMeleeAttackResult{}, fmt.Errorf("proficiency overflow")
			}
			attacker.Body.Proficiency[index] += int32(award64)
			result.ProficiencyAward = int32(award64)
		}
	}
	for i, enemy := range target.Enemies {
		if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
			if int64(enemy.Damage)+int64(dealt) > int64(^uint32(0)>>1) {
				return State{}, NPCMeleeAttackResult{}, fmt.Errorf("NPC enemy damage overflow")
			}
			enemy.Damage += int32(dealt)
			target.Enemies[i] = enemy
			break
		}
	}
	if target.Body.HPCurrent < 1 {
		next.Players[actorID] = attacker
		next.NPCs[targetID] = target
		dead, death, deathErr := next.PlanNPCDeath(targetID, actorID, options.Now, options.Allocate)
		if deathErr != nil {
			return State{}, NPCMeleeAttackResult{}, fmt.Errorf("%w: %v", ErrNPCDeathTransitionPending, deathErr)
		}
		result.Killed = true
		result.ExperienceAward = death.ExperienceAward
		result.QuestExperienceAward = death.QuestExperienceAward
		result.TargetHP = 0
		return dead, result, nil
	}
	next.Players[actorID] = attacker
	next.NPCs[targetID] = target
	if err := next.Validate(); err != nil {
		return State{}, NPCMeleeAttackResult{}, err
	}
	return next, result, nil
}
