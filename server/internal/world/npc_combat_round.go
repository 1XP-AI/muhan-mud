package world

import (
	"fmt"
	"reflect"
)

const (
	npcCombatHiddenFlag    = 1 // PHIDDN
	npcCombatInvisibleFlag = 2 // PINVIS
)

// NPCCombatRoundProposal is the pure candidate for one ordinary NPC melee
// swing from update_active/attack_crt. It intentionally stops before player
// death: the existing player-death reducer has rules that need to be composed
// by the durable scheduler, so a lethal round is rejected rather than saving a
// partially-dead player.
type NPCCombatRoundProposal struct {
	NPCID            string
	PlayerID         string
	RoomID           int16
	Hit              bool
	Critical         bool
	Damage           int
	PlayerHPBefore   int
	PlayerHPAfter    int
	before           State
	next             State
	expectedHit      bool
	expectedCritical bool
}

// NPCCombatRoundResult is the committed projection. TargetHP is retained as
// an explicit alias for transport/event code that uses target-oriented naming.
type NPCCombatRoundResult struct {
	NPCID    string
	PlayerID string
	RoomID   int16
	Hit      bool
	Critical bool
	Damage   int
	PlayerHP int
	TargetHP int
	Killed   bool
}

func npcCombatContainsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func npcCombatCriticalDivisor(class byte) (int, error) {
	switch class {
	case 2, 4, 9, 10:
		return 20, nil
	case 6, 7:
		return 25, nil
	case 1, 3, 8:
		return 30, nil
	case 5, 11, 12:
		return 40, nil
	default:
		return 0, fmt.Errorf("NPC combat class outside critical table")
	}
}

func npcCombatCriticalChance(body LegacyMonster) (int, error) {
	if len(body.Proficiency) <= 2 {
		return 0, fmt.Errorf("NPC combat proficiency unavailable")
	}
	proficiency, err := WeaponProficiency(body.Class, body.Proficiency[2])
	if err != nil {
		return 0, err
	}
	divisor, err := npcCombatCriticalDivisor(body.Class)
	if err != nil {
		return 0, err
	}
	return proficiency / divisor, nil
}

func npcCombatDamage(body LegacyMonster, player LegacyMonster, roll func(int, int) int) (damage int, critical bool, err error) {
	damage, err = meleeDice(body, nil, roll)
	if err != nil {
		return 0, false, err
	}
	// update.c subtracts the victim's armor contribution and clamps the
	// ordinary attack to one. Armor is a signed legacy byte; widen first.
	damage -= (70 - int(player.Armor)) / 5
	if damage < 1 {
		damage = 1
	}
	chance, err := npcCombatCriticalChance(body)
	if err != nil {
		return 0, false, err
	}
	criticalRoll, err := randomIn(roll, 1, 100)
	if err != nil {
		return 0, false, err
	}
	if criticalRoll <= chance {
		multiplier, err := randomIn(roll, 3, 6)
		if err != nil {
			return 0, false, err
		}
		damage *= multiplier
		critical = true
	}
	return damage, critical, nil
}

// PlanNPCCombatRound plans one canonical NPC attack against an exact player
// identity. It consumes randomness only after all identity, room, enemy, and
// cooldown checks pass. A lethal result is rejected until the player-death
// continuation is composed by the scheduler.
func (s State) PlanNPCCombatRound(npcID, playerID string, roll func(int, int) int) (NPCCombatRoundProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCCombatRoundProposal{}, err
	}
	if roll == nil {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat round requires RNG")
	}
	npc, ok := s.NPCs[npcID]
	if s.NPCs == nil || !ok || npc.Body.Type != 1 || npc.Body.HPCurrent < 1 {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat identity absent")
	}
	player, ok := s.Players[playerID]
	if !ok || !player.Online || player.Body.HPCurrent < 1 {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat player absent")
	}
	if player.Body.RoomID != npc.Body.RoomID {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat room mismatch")
	}
	room, ok := s.Rooms[npc.Body.RoomID]
	if !ok || !npcCombatContainsID(room.NPCIDs, npcID) || !npcCombatContainsID(room.PlayerIDs, playerID) {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat membership absent")
	}
	if npc.Enemies == nil {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat enemy relations unresolved")
	}
	enemy := false
	for _, relation := range npc.Enemies {
		if relation.Target == (EntityRef{Kind: "player", ID: playerID}) {
			enemy = true
			break
		}
	}
	if !enemy {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat enemy absent")
	}
	n, err := randomIn(roll, 1, 20)
	if err != nil {
		return NPCCombatRoundProposal{}, err
	}
	next := s.clone()
	nextNPC := next.NPCs[npcID]
	nextPlayer := next.Players[playerID]
	nextNPC.Body.Flags[0] &^= 1 << npcCombatHiddenFlag
	nextNPC.Body.Flags[0] &^= 1 << npcCombatInvisibleFlag
	proposal := NPCCombatRoundProposal{
		NPCID: npcID, PlayerID: playerID, RoomID: npc.Body.RoomID,
		PlayerHPBefore: int(player.Body.HPCurrent), PlayerHPAfter: int(player.Body.HPCurrent),
		before: s.clone(), next: next,
		expectedHit: false,
	}
	// update_active treats mrand(1,20) >= n as a hit. The NPC's stored THAC0
	// is authoritative; an absent/legacy zero still has the source MAX(1, n).
	threshold := int(npc.Body.Thaco) - int(player.Body.Armor)/8
	if threshold < 1 {
		threshold = 1
	}
	if n < threshold {
		proposal.next.NPCs[npcID] = nextNPC
		proposal.Hit = false
		return proposal, nil
	}
	damage, critical, err := npcCombatDamage(npc.Body, player.Body, roll)
	if err != nil {
		return NPCCombatRoundProposal{}, err
	}
	if damage >= int(player.Body.HPCurrent) {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat player death continuation pending")
	}
	nextPlayer.Body.HPCurrent = int16(int(player.Body.HPCurrent) - damage)
	next.NPCs[npcID] = nextNPC
	next.Players[playerID] = nextPlayer
	proposal.next = next
	proposal.Hit = true
	proposal.Critical = critical
	proposal.expectedHit = true
	proposal.expectedCritical = critical
	proposal.Damage = damage
	proposal.PlayerHPAfter = int(nextPlayer.Body.HPCurrent)
	return proposal, nil
}

// ApplyNPCCombatRound accepts only the exact candidate produced by Plan. It
// rejects stale/tampered proposals before exposing a partial state.
func (s State) ApplyNPCCombatRound(proposal NPCCombatRoundProposal) (State, NPCCombatRoundResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCCombatRoundResult{}, err
	}
	if proposal.NPCID == "" || proposal.PlayerID == "" || proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("stale NPC combat round")
	}
	npc, ok := s.NPCs[proposal.NPCID]
	player, playerOK := s.Players[proposal.PlayerID]
	if !ok || !playerOK || npc.Body.RoomID != proposal.RoomID || player.Body.RoomID != proposal.RoomID || proposal.Damage < 0 || proposal.PlayerHPBefore != int(player.Body.HPCurrent) || proposal.PlayerHPAfter != proposal.PlayerHPBefore-proposal.Damage {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat round")
	}
	if proposal.Hit != proposal.expectedHit || proposal.Critical != proposal.expectedCritical || proposal.Hit != (proposal.Damage > 0) || (proposal.Critical && !proposal.Hit) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat outcome")
	}
	nextPlayer, nextOK := proposal.next.Players[proposal.PlayerID]
	if !nextOK || int(nextPlayer.Body.HPCurrent) != proposal.PlayerHPAfter {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat candidate")
	}
	if proposal.Damage >= proposal.PlayerHPBefore {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("NPC combat player death continuation pending")
	}
	if err := proposal.next.Validate(); err != nil {
		return State{}, NPCCombatRoundResult{}, err
	}
	result := NPCCombatRoundResult{
		NPCID: proposal.NPCID, PlayerID: proposal.PlayerID, RoomID: proposal.RoomID,
		Hit: proposal.Hit, Critical: proposal.Critical, Damage: proposal.Damage,
		PlayerHP: proposal.PlayerHPAfter, TargetHP: proposal.PlayerHPAfter,
		Killed: false,
	}
	return proposal.next.clone(), result, nil
}
