package world

import (
	"fmt"
	"reflect"
)

// NPCCombatRoundProposal is the pure candidate for one ordinary NPC melee
// swing from update_active's NPC->PLAYER path. It intentionally stops before player
// death: the existing player-death reducer has rules that need to be composed
// by the durable scheduler, so a lethal round is rejected rather than saving a
// partially-dead player.
type NPCCombatRoundProposal struct {
	NPCID            string
	PlayerID         string
	RoomID           int16
	Hit              bool
	Critical         bool
	Poisoned         bool
	Damage           int
	PlayerHPBefore   int
	PlayerHPAfter    int
	before           State
	next             State
	expectedHit      bool
	expectedCritical bool
	expectedPoisoned bool
}

// NPCCombatRoundResult is the committed projection. TargetHP is retained as
// an explicit alias for transport/event code that uses target-oriented naming.
type NPCCombatRoundResult struct {
	NPCID    string
	PlayerID string
	RoomID   int16
	Hit      bool
	Critical bool
	Poisoned bool
	Damage   int
	PlayerHP int
	TargetHP int
	Killed   bool
}

const (
	npcCombatPoisonerFlag       uint = 13 // MPOISS
	npcCombatVictimPoisonedFlag uint = 16 // PPOISN
)

func npcCombatContainsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func npcCombatDamage(body LegacyMonster, player LegacyMonster, roll func(int, int) int) (damage int, err error) {
	damage, err = meleeDice(body, nil, roll)
	if err != nil {
		return 0, err
	}
	// update.c subtracts the victim's armor contribution and clamps the
	// ordinary attack to one. Armor is a signed legacy byte; widen first.
	damage -= (70 - int(int8(player.Armor))) / 5
	if damage < 1 {
		damage = 1
	}
	return damage, nil
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
			if relation.Damage < 0 {
				return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat enemy relation unresolved")
			}
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
	proposal := NPCCombatRoundProposal{
		NPCID: npcID, PlayerID: playerID, RoomID: npc.Body.RoomID,
		PlayerHPBefore: int(player.Body.HPCurrent), PlayerHPAfter: int(player.Body.HPCurrent),
		before: s.clone(), next: next,
		expectedHit: false,
	}
	// update_active treats mrand(1,20) >= n as a hit. The NPC's stored THAC0
	// is authoritative; an absent/legacy zero still has the source MAX(1, n).
	threshold := int(int8(npc.Body.Thaco)) - int(int8(player.Body.Armor))/8
	if threshold < 1 {
		threshold = 1
	}
	if n < threshold {
		proposal.Hit = false
		return proposal, nil
	}
	damage, err := npcCombatDamage(npc.Body, player.Body, roll)
	if err != nil {
		return NPCCombatRoundProposal{}, err
	}
	nextPlayer := next.Players[playerID]
	nextPlayer.Body.HPCurrent = int16(int(player.Body.HPCurrent) - damage)
	poisoned := false
	if flag(npc.Body.Flags[:], npcCombatPoisonerFlag) {
		poisonRoll, poisonErr := randomIn(roll, 1, 100)
		if poisonErr != nil {
			return NPCCombatRoundProposal{}, poisonErr
		}
		poisoned = poisonRoll <= 15
		if poisoned {
			nextPlayer.Body.Flags[npcCombatVictimPoisonedFlag/8] |= 1 << (npcCombatVictimPoisonedFlag % 8)
		}
	}
	next.Players[playerID] = nextPlayer
	proposal.next = next
	proposal.Hit = true
	proposal.expectedHit = true
	proposal.Poisoned = poisoned
	proposal.expectedPoisoned = poisoned
	proposal.Damage = damage
	proposal.PlayerHPAfter = int(nextPlayer.Body.HPCurrent)
	if damage >= int(player.Body.HPCurrent) {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat player death continuation pending")
	}
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
	if proposal.Poisoned != proposal.expectedPoisoned || (proposal.Poisoned && (!proposal.Hit || !flag(npc.Body.Flags[:], npcCombatPoisonerFlag))) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat poison outcome")
	}
	nextPlayer, nextOK := proposal.next.Players[proposal.PlayerID]
	if !nextOK || int(nextPlayer.Body.HPCurrent) != proposal.PlayerHPAfter {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat candidate")
	}
	wantPoisoned := flag(player.Body.Flags[:], npcCombatVictimPoisonedFlag) || proposal.Poisoned
	if flag(nextPlayer.Body.Flags[:], npcCombatVictimPoisonedFlag) != wantPoisoned {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat poison candidate")
	}
	if proposal.Damage >= proposal.PlayerHPBefore {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("NPC combat player death continuation pending")
	}
	if err := proposal.next.Validate(); err != nil {
		return State{}, NPCCombatRoundResult{}, err
	}
	result := NPCCombatRoundResult{
		NPCID: proposal.NPCID, PlayerID: proposal.PlayerID, RoomID: proposal.RoomID,
		Hit: proposal.Hit, Critical: proposal.Critical, Poisoned: proposal.Poisoned, Damage: proposal.Damage,
		PlayerHP: proposal.PlayerHPAfter, TargetHP: proposal.PlayerHPAfter,
		Killed: false,
	}
	return proposal.next.clone(), result, nil
}
