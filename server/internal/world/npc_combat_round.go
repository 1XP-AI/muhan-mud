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
	NPCID                  string
	PlayerID               string
	RoomID                 int16
	Hit                    bool
	Critical               bool
	Poisoned               bool
	Diseased               bool
	Blinded                bool
	BreathTriggered        bool
	BreathType             NPCCombatBreathType
	BreathRoll             int
	BreathDiceCount        int
	BreathDiceSides        int
	BreathDicePlus         int
	BreathResisted         bool
	BreathPoisoned         bool
	EnergyDrainTriggered   bool
	EnergyRoll             int
	EnergyBand             int
	EnergyDiceCount        int
	EnergyDiceSides        int
	EnergyDicePlus         int
	EnergyDrain            int
	ExperienceBefore       int32
	ExperienceAfter        int32
	ProficiencyBefore      [5]int32
	ProficiencyAfter       [5]int32
	RealmBefore            [4]int32
	RealmAfter             [4]int32
	DissolveSucceeded      bool
	Dissolved              bool
	DissolveProtected      bool
	DissolveRoll           int
	DissolveSelectionRoll  int
	DissolveCandidateCount int
	// DissolveReadySlot is the zero-based canonical Ready index (0..19).
	DissolveReadySlot              int
	DissolveItemID                 string
	DissolveItemName               string
	Damage                         int
	PlayerHPBefore                 int
	PlayerHPAfter                  int
	before                         State
	next                           State
	expectedHit                    bool
	expectedCritical               bool
	expectedPoisoned               bool
	expectedDiseased               bool
	expectedBlinded                bool
	expectedBreathTriggered        bool
	expectedBreathType             NPCCombatBreathType
	expectedBreathRoll             int
	expectedBreathDiceCount        int
	expectedBreathDiceSides        int
	expectedBreathDicePlus         int
	expectedBreathResisted         bool
	expectedBreathPoisoned         bool
	expectedEnergyDrainTriggered   bool
	expectedEnergyRoll             int
	expectedEnergyBand             int
	expectedEnergyDiceCount        int
	expectedEnergyDiceSides        int
	expectedEnergyDicePlus         int
	expectedEnergyDrain            int
	expectedExperienceBefore       int32
	expectedExperienceAfter        int32
	expectedProficiencyBefore      [5]int32
	expectedProficiencyAfter       [5]int32
	expectedRealmBefore            [4]int32
	expectedRealmAfter             [4]int32
	expectedDamage                 int
	expectedDissolveSucceeded      bool
	expectedDissolved              bool
	expectedDissolveProtected      bool
	expectedDissolveRoll           int
	expectedDissolveSelectionRoll  int
	expectedDissolveCandidateCount int
	expectedDissolveReadySlot      int
	expectedDissolveItemID         string
	expectedDissolveItemName       string
}

// NPCCombatRoundResult is the committed projection. TargetHP is retained as
// an explicit alias for transport/event code that uses target-oriented naming.
type NPCCombatRoundResult struct {
	NPCID                  string
	PlayerID               string
	RoomID                 int16
	Hit                    bool
	Critical               bool
	Poisoned               bool
	Diseased               bool
	Blinded                bool
	BreathTriggered        bool
	BreathType             NPCCombatBreathType
	BreathRoll             int
	BreathDiceCount        int
	BreathDiceSides        int
	BreathDicePlus         int
	BreathResisted         bool
	BreathPoisoned         bool
	EnergyDrainTriggered   bool
	EnergyRoll             int
	EnergyBand             int
	EnergyDiceCount        int
	EnergyDiceSides        int
	EnergyDicePlus         int
	EnergyDrain            int
	ExperienceBefore       int32
	ExperienceAfter        int32
	ProficiencyBefore      [5]int32
	ProficiencyAfter       [5]int32
	RealmBefore            [4]int32
	RealmAfter             [4]int32
	DissolveSucceeded      bool
	Dissolved              bool
	DissolveProtected      bool
	DissolveRoll           int
	DissolveSelectionRoll  int
	DissolveCandidateCount int
	DissolveReadySlot      int
	DissolveItemID         string
	DissolveItemName       string
	Damage                 int
	PlayerHP               int
	TargetHP               int
	Killed                 bool
}

// NPCCombatBreathType is the two-bit MBRWP1/MBRWP2 value from mtype.h. The
// zero value is fire (00), so BreathTriggered must be checked before reading
// the type. Keeping the raw source mapping avoids relabelling the legacy
// branches whose output text and flag comments use different terminology.
type NPCCombatBreathType byte

const (
	NPCCombatBreathFire NPCCombatBreathType = iota // MBRWP1=0, MBRWP2=0
	NPCCombatBreathCold                            // MBRWP1=0, MBRWP2=1
	NPCCombatBreathGas                             // MBRWP1=1, MBRWP2=0
	NPCCombatBreathAcid                            // MBRWP1=1, MBRWP2=1
)

const (
	npcCombatBreatherFlag       uint = 19 // MBRETH
	npcCombatBreathWeapon1Flag  uint = 28 // MBRWP1
	npcCombatBreathWeapon2Flag  uint = 29 // MBRWP2
	npcCombatPoisonerFlag       uint = 13 // MPOISS
	npcCombatVictimPoisonedFlag uint = 16 // PPOISN
	npcCombatVictimResistFire   uint = 30 // PRFIRE
	npcCombatVictimResistCold   uint = 36 // PRCOLD
	npcCombatDiseaserFlag       uint = 34 // MDISEA
	npcCombatVictimDiseasedFlag uint = 41 // PDISEA
	npcCombatBlinderFlag        uint = 45 // MBLNDR
	npcCombatVictimBlindedFlag  uint = 42 // PBLIND
	npcCombatDissolverFlag      uint = 35 // MDISIT
	npcCombatEnergyDrainFlag    uint = 30 // MENEDR
	npcCombatBefuddledFlag      uint = 51 // MBEFUD
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

func npcCombatValidateProgression(body LegacyMonster) error {
	const maxLegacyInt32 = int64(^uint32(0) >> 1)
	total := int64(0)
	if body.Experience < 0 {
		return fmt.Errorf("NPC combat target has negative experience")
	}
	for _, value := range body.Proficiency {
		if value < 0 {
			return fmt.Errorf("NPC combat target has negative weapon proficiency")
		}
		total += int64(value)
	}
	for _, value := range body.Realm {
		if value < 0 {
			return fmt.Errorf("NPC combat target has negative realm proficiency")
		}
		total += int64(value)
	}
	if total > maxLegacyInt32 {
		return fmt.Errorf("NPC combat target proficiency total overflow")
	}
	return nil
}

// npcCombatEnergyDamage mirrors dice((level+3)/4, 5, band*5). The source
// evaluates this only after the MENEDR hit gate and before mdice ordinary
// melee. A wide accumulator rejects impossible future inputs rather than
// allowing an int conversion to wrap the receipt.
func npcCombatEnergyDamage(level byte, roll func(int, int) int) (band, count, sides, plus, damage int, err error) {
	band = (int(level) + 3) / 4
	count, sides, plus = band, 5, band*5
	if band < 0 || count < 0 || sides < 1 || plus < 0 {
		return 0, 0, 0, 0, 0, fmt.Errorf("invalid NPC combat energy dice")
	}
	value := int64(plus)
	const maxLegacyInt32 = int64(^uint32(0) >> 1)
	if value > maxLegacyInt32 {
		return 0, 0, 0, 0, 0, fmt.Errorf("NPC combat energy dice overflow")
	}
	for i := 0; i < count; i++ {
		n, randomErr := randomIn(roll, 1, sides)
		if randomErr != nil {
			return 0, 0, 0, 0, 0, randomErr
		}
		value += int64(n)
		if value > maxLegacyInt32 {
			return 0, 0, 0, 0, 0, fmt.Errorf("NPC combat energy dice overflow")
		}
	}
	return band, count, sides, plus, int(value), nil
}

// lowerNPCCombatProficiency is the private MENEDR port of
// src/command10.c:lower_prof. Its order is part of the source contract:
// five weapon proficiencies, then four magical realms, followed by the
// max-weapon-proficiency floor. It intentionally remains local to this NPC
// combat slice instead of becoming a second shared progression authority.
func lowerNPCCombatProficiency(body *LegacyMonster, loss int32) error {
	if body == nil || loss < 0 {
		return fmt.Errorf("invalid NPC combat proficiency loss")
	}
	const maxLegacyInt32 = int64(^uint32(0) >> 1)
	var total int64
	for _, value := range body.Proficiency {
		if value < 0 {
			return fmt.Errorf("negative NPC combat weapon proficiency")
		}
		total += int64(value)
	}
	for _, value := range body.Realm {
		if value < 0 {
			return fmt.Errorf("negative NPC combat realm proficiency")
		}
		total += int64(value)
	}
	if total > maxLegacyInt32 {
		return fmt.Errorf("NPC combat proficiency total overflow")
	}
	profloss := int64(loss)
	if profloss > total {
		profloss = total
	}
	below := 0
	for profloss > 9 && below < 9 {
		below = 0
		for n := 0; n < 9; n++ {
			share := profloss / int64(9-n)
			if share > maxLegacyInt32 {
				return fmt.Errorf("NPC combat proficiency loss overflow")
			}
			if n < 5 {
				body.Proficiency[n] -= int32(share)
				// Keep the two C expressions separate: the second division sees
				// the reduced proficiency-loss total.
				profloss -= profloss / int64(9-n)
				if body.Proficiency[n] < 0 {
					below++
					profloss -= int64(body.Proficiency[n])
					body.Proficiency[n] = 0
				}
			} else {
				index := n - 5
				body.Realm[index] -= int32(share)
				profloss -= profloss / int64(9-n)
				if body.Realm[index] < 0 {
					below++
					profloss -= int64(body.Realm[index])
					body.Realm[index] = 0
				}
			}
		}
	}
	maxIndex := 0
	for n := 1; n < 5; n++ {
		if body.Proficiency[n] > body.Proficiency[maxIndex] {
			maxIndex = n
		}
	}
	if body.Proficiency[maxIndex] < 1024 {
		body.Proficiency[maxIndex] = 1024
	}
	return nil
}

// npcCombatBreathSpec ports the MBRWP1/MBRWP2 branch at update.c:419-454.
// The type is the raw two-bit source value: 00 fire, 01 cold, 10 gas, and 11
// acid. The source's 10 branch prints a spit message, but its damage and flag
// selection are still the 10 branch; keeping the raw value prevents a text
// label from changing combat semantics.
func npcCombatBreathSpec(npc, player LegacyMonster) (NPCCombatBreathType, int, int, int, bool, bool) {
	weapon1 := flag(npc.Flags[:], npcCombatBreathWeapon1Flag)
	weapon2 := flag(npc.Flags[:], npcCombatBreathWeapon2Flag)
	band := (int(npc.Level) + 3) / 4
	sides, plus := 4, 0
	resisted := false
	poisoned := false
	var breathType NPCCombatBreathType

	switch {
	case weapon1 && weapon2:
		breathType = NPCCombatBreathAcid
		sides, plus, poisoned = 2, 1, true
	case weapon1:
		breathType = NPCCombatBreathGas
		sides = 3
	case weapon2:
		breathType = NPCCombatBreathCold
		resisted = flag(player.Flags[:], npcCombatVictimResistCold)
		if resisted {
			sides = 2
		}
	default:
		breathType = NPCCombatBreathFire
		resisted = flag(player.Flags[:], npcCombatVictimResistFire)
		if resisted {
			sides = 2
		}
	}
	return breathType, band, sides, plus, resisted, poisoned
}

// npcCombatBreathDamage mirrors misc.c:dice: it consumes exactly one mrand
// draw per level-band die and adds the branch-specific plus value afterwards.
func npcCombatBreathDamage(count, sides, plus int, roll func(int, int) int) (int, error) {
	if count < 0 || sides < 1 {
		return 0, fmt.Errorf("invalid NPC combat breath dice")
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

// npcCombatReadySlots mirrors dissolve_item's checklist construction. The
// canonical ItemCollection has already been validated by State.Validate, but
// the explicit checks keep this helper safe if it is reused at another
// proposal boundary.
func npcCombatReadySlots(items *ItemCollection) ([]int, error) {
	if items == nil {
		return nil, fmt.Errorf("NPC combat MDISIT requires canonical player items")
	}
	if err := items.Validate(); err != nil {
		return nil, fmt.Errorf("NPC combat MDISIT player items: %w", err)
	}
	slots := make([]int, 0, len(items.Ready))
	for slot, id := range items.Ready {
		if id == "" {
			continue
		}
		if _, ok := items.Items[id]; !ok {
			return nil, fmt.Errorf("NPC combat MDISIT ready item absent")
		}
		slots = append(slots, slot)
	}
	return slots, nil
}

// npcCombatDeleteReadyRoot is the canonical ID-graph equivalent of
// dissolve_item's free_obj(root). A ready root owns its complete subtree;
// deleting only the root would leave orphan IDs and make the next snapshot
// invalid.
func npcCombatDeleteReadyRoot(items *ItemCollection, slot int) error {
	if items == nil || slot < 0 || slot >= len(items.Ready) {
		return fmt.Errorf("NPC combat MDISIT ready slot absent")
	}
	root := items.Ready[slot]
	if root == "" {
		return fmt.Errorf("NPC combat MDISIT ready root absent")
	}
	stack := []string{root}
	seen := make(map[string]bool)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == "" || seen[id] {
			return fmt.Errorf("NPC combat MDISIT item subtree is cyclic")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("NPC combat MDISIT item subtree absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	items.Ready[slot] = ""
	return nil
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
	if err := npcCombatValidateProgression(player.Body); err != nil {
		return NPCCombatRoundProposal{}, err
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
	// The legacy dissolve_item path can fall back to raw creature pointers, but
	// canonical Go combat may only mutate an ID-owned ItemCollection. Reject an
	// unresolved player inventory before consuming attack randomness.
	if flag(npc.Body.Flags[:], npcCombatDissolverFlag) && player.Items == nil {
		return NPCCombatRoundProposal{}, fmt.Errorf("NPC combat MDISIT requires canonical player items")
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
	nextPlayer := next.Players[playerID]
	damage := 0
	breathTriggered := false
	breathType := NPCCombatBreathFire
	breathRoll := 0
	breathDiceCount := 0
	breathDiceSides := 0
	breathDicePlus := 0
	breathResisted := false
	breathPoisoned := false
	energyDrainTriggered := false
	energyRoll := 0
	energyBand := 0
	energyDiceCount := 0
	energyDiceSides := 0
	energyDicePlus := 0
	energyDrain := 0
	if flag(npc.Body.Flags[:], npcCombatBreatherFlag) {
		breathRoll, err = randomIn(roll, 1, 30)
		if err != nil {
			return NPCCombatRoundProposal{}, err
		}
		if breathRoll < 5 {
			breathTriggered = true
			breathType, breathDiceCount, breathDiceSides, breathDicePlus, breathResisted, breathPoisoned = npcCombatBreathSpec(npc.Body, player.Body)
			damage, err = npcCombatBreathDamage(breathDiceCount, breathDiceSides, breathDicePlus, roll)
			if err != nil {
				return NPCCombatRoundProposal{}, err
			}
			if breathPoisoned {
				nextPlayer.Body.Flags[npcCombatVictimPoisonedFlag/8] |= 1 << (npcCombatVictimPoisonedFlag % 8)
			}
		}
	}
	if !breathTriggered {
		if flag(npc.Body.Flags[:], npcCombatEnergyDrainFlag) {
			energyRoll, err = randomIn(roll, 1, 100)
			if err != nil {
				return NPCCombatRoundProposal{}, err
			}
			if energyRoll < 10 {
				energyDrainTriggered = true
				var energyRaw int
				var energyErr error
				energyBand, energyDiceCount, energyDiceSides, energyDicePlus, energyRaw, energyErr = npcCombatEnergyDamage(npc.Body.Level, roll)
				if energyErr != nil {
					return NPCCombatRoundProposal{}, energyErr
				}
				energyDrain = energyRaw
				if energyDrain > int(player.Body.Experience) {
					energyDrain = int(player.Body.Experience)
				}
				nextPlayer.Body.Experience = player.Body.Experience - int32(energyDrain)
				if err := lowerNPCCombatProficiency(&nextPlayer.Body, int32(energyDrain)); err != nil {
					return NPCCombatRoundProposal{}, err
				}
			}
		}
		damage, err = npcCombatDamage(npc.Body, player.Body, roll)
		if err != nil {
			return NPCCombatRoundProposal{}, err
		}
	}
	// src/update.c:471-480 attenuates both ordinary and breath damage after
	// damage is determined and before HP or any post-hit effect is applied.
	if flag(npc.Body.Flags[:], npcCombatBefuddledFlag) {
		damage /= 3
	}
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
	diseased := false
	if flag(npc.Body.Flags[:], npcCombatDiseaserFlag) {
		diseaseRoll, diseaseErr := randomIn(roll, 1, 100)
		if diseaseErr != nil {
			return NPCCombatRoundProposal{}, diseaseErr
		}
		diseased = diseaseRoll <= 10
		if diseased {
			nextPlayer.Body.Flags[npcCombatVictimDiseasedFlag/8] |= 1 << (npcCombatVictimDiseasedFlag % 8)
		}
	}
	blinded := false
	if flag(npc.Body.Flags[:], npcCombatBlinderFlag) {
		blindRoll, blindErr := randomIn(roll, 1, 100)
		if blindErr != nil {
			return NPCCombatRoundProposal{}, blindErr
		}
		blinded = blindRoll <= 10
		if blinded {
			nextPlayer.Body.Flags[npcCombatVictimBlindedFlag/8] |= 1 << (npcCombatVictimBlindedFlag % 8)
		}
	}
	dissolveSucceeded := false
	dissolved := false
	dissolveProtected := false
	dissolveRoll := 0
	dissolveSelectionRoll := 0
	dissolveCandidateCount := 0
	dissolveReadySlot := 0
	dissolveItemID := ""
	dissolveItemName := ""
	// src/update.c:507-509 performs this check after the poison/disease/blind
	// checks. dissolve_item's checklist is ported below from command10.c:442-480.
	if flag(npc.Body.Flags[:], npcCombatDissolverFlag) {
		var dissolveErr error
		dissolveRoll, dissolveErr = randomIn(roll, 1, 100)
		if dissolveErr != nil {
			return NPCCombatRoundProposal{}, dissolveErr
		}
		if dissolveRoll <= 15 {
			dissolveSucceeded = true
			slots, slotsErr := npcCombatReadySlots(player.Items)
			if slotsErr != nil {
				return NPCCombatRoundProposal{}, slotsErr
			}
			dissolveCandidateCount = len(slots)
			if dissolveCandidateCount > 0 {
				dissolveSelectionRoll, dissolveErr = randomIn(roll, 0, dissolveCandidateCount-1)
				if dissolveErr != nil {
					return NPCCombatRoundProposal{}, dissolveErr
				}
				dissolveReadySlot = slots[dissolveSelectionRoll]
				dissolveItemID = player.Items.Ready[dissolveReadySlot]
				selected := player.Items.Items[dissolveItemID]
				dissolveItemName = selected.Object.Name
				dissolveProtected = flag(selected.Object.Flags[:], objectOneWevFlag)
				if !dissolveProtected {
					if err := npcCombatDeleteReadyRoot(nextPlayer.Items, dissolveReadySlot); err != nil {
						return NPCCombatRoundProposal{}, err
					}
					if err := refreshEquipmentStats(&nextPlayer); err != nil {
						return NPCCombatRoundProposal{}, err
					}
					dissolved = true
				}
			}
		}
	}
	next.Players[playerID] = nextPlayer
	proposal.next = next
	proposal.Hit = true
	proposal.expectedHit = true
	proposal.Poisoned = poisoned
	proposal.expectedPoisoned = poisoned
	proposal.Diseased = diseased
	proposal.expectedDiseased = diseased
	proposal.Blinded = blinded
	proposal.expectedBlinded = blinded
	proposal.BreathTriggered = breathTriggered
	proposal.expectedBreathTriggered = breathTriggered
	proposal.BreathType = breathType
	proposal.expectedBreathType = breathType
	proposal.BreathRoll = breathRoll
	proposal.expectedBreathRoll = breathRoll
	proposal.BreathDiceCount = breathDiceCount
	proposal.expectedBreathDiceCount = breathDiceCount
	proposal.BreathDiceSides = breathDiceSides
	proposal.expectedBreathDiceSides = breathDiceSides
	proposal.BreathDicePlus = breathDicePlus
	proposal.expectedBreathDicePlus = breathDicePlus
	proposal.BreathResisted = breathResisted
	proposal.expectedBreathResisted = breathResisted
	proposal.BreathPoisoned = breathPoisoned
	proposal.expectedBreathPoisoned = breathPoisoned
	proposal.EnergyDrainTriggered = energyDrainTriggered
	proposal.expectedEnergyDrainTriggered = energyDrainTriggered
	proposal.EnergyRoll = energyRoll
	proposal.expectedEnergyRoll = energyRoll
	proposal.EnergyBand = energyBand
	proposal.expectedEnergyBand = energyBand
	proposal.EnergyDiceCount = energyDiceCount
	proposal.expectedEnergyDiceCount = energyDiceCount
	proposal.EnergyDiceSides = energyDiceSides
	proposal.expectedEnergyDiceSides = energyDiceSides
	proposal.EnergyDicePlus = energyDicePlus
	proposal.expectedEnergyDicePlus = energyDicePlus
	proposal.EnergyDrain = energyDrain
	proposal.expectedEnergyDrain = energyDrain
	proposal.ExperienceBefore = player.Body.Experience
	proposal.expectedExperienceBefore = player.Body.Experience
	proposal.ExperienceAfter = nextPlayer.Body.Experience
	proposal.expectedExperienceAfter = nextPlayer.Body.Experience
	proposal.ProficiencyBefore = player.Body.Proficiency
	proposal.expectedProficiencyBefore = player.Body.Proficiency
	proposal.ProficiencyAfter = nextPlayer.Body.Proficiency
	proposal.expectedProficiencyAfter = nextPlayer.Body.Proficiency
	proposal.RealmBefore = player.Body.Realm
	proposal.expectedRealmBefore = player.Body.Realm
	proposal.RealmAfter = nextPlayer.Body.Realm
	proposal.expectedRealmAfter = nextPlayer.Body.Realm
	proposal.DissolveSucceeded = dissolveSucceeded
	proposal.expectedDissolveSucceeded = dissolveSucceeded
	proposal.Dissolved = dissolved
	proposal.expectedDissolved = dissolved
	proposal.DissolveProtected = dissolveProtected
	proposal.expectedDissolveProtected = dissolveProtected
	proposal.DissolveRoll = dissolveRoll
	proposal.expectedDissolveRoll = dissolveRoll
	proposal.DissolveSelectionRoll = dissolveSelectionRoll
	proposal.expectedDissolveSelectionRoll = dissolveSelectionRoll
	proposal.DissolveCandidateCount = dissolveCandidateCount
	proposal.expectedDissolveCandidateCount = dissolveCandidateCount
	proposal.DissolveReadySlot = dissolveReadySlot
	proposal.expectedDissolveReadySlot = dissolveReadySlot
	proposal.DissolveItemID = dissolveItemID
	proposal.expectedDissolveItemID = dissolveItemID
	proposal.DissolveItemName = dissolveItemName
	proposal.expectedDissolveItemName = dissolveItemName
	proposal.Damage = damage
	proposal.expectedDamage = damage
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
	if err := npcCombatValidateProgression(player.Body); err != nil {
		return State{}, NPCCombatRoundResult{}, err
	}
	if proposal.Hit != proposal.expectedHit || proposal.Critical != proposal.expectedCritical || proposal.Damage != proposal.expectedDamage || (!proposal.Hit && proposal.Damage != 0) || (proposal.Critical && !proposal.Hit) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat outcome")
	}
	if proposal.Poisoned != proposal.expectedPoisoned || (proposal.Poisoned && (!proposal.Hit || !flag(npc.Body.Flags[:], npcCombatPoisonerFlag))) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat poison outcome")
	}
	if proposal.Diseased != proposal.expectedDiseased || (proposal.Diseased && (!proposal.Hit || !flag(npc.Body.Flags[:], npcCombatDiseaserFlag))) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat disease outcome")
	}
	if proposal.Blinded != proposal.expectedBlinded || (proposal.Blinded && (!proposal.Hit || !flag(npc.Body.Flags[:], npcCombatBlinderFlag))) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat blind outcome")
	}
	if proposal.EnergyDrainTriggered != proposal.expectedEnergyDrainTriggered ||
		proposal.EnergyRoll != proposal.expectedEnergyRoll ||
		proposal.EnergyBand != proposal.expectedEnergyBand ||
		proposal.EnergyDiceCount != proposal.expectedEnergyDiceCount ||
		proposal.EnergyDiceSides != proposal.expectedEnergyDiceSides ||
		proposal.EnergyDicePlus != proposal.expectedEnergyDicePlus ||
		proposal.EnergyDrain != proposal.expectedEnergyDrain ||
		proposal.ExperienceBefore != proposal.expectedExperienceBefore ||
		proposal.ExperienceAfter != proposal.expectedExperienceAfter ||
		proposal.ProficiencyBefore != proposal.expectedProficiencyBefore ||
		proposal.ProficiencyAfter != proposal.expectedProficiencyAfter ||
		proposal.RealmBefore != proposal.expectedRealmBefore ||
		proposal.RealmAfter != proposal.expectedRealmAfter {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy outcome")
	}
	if !proposal.Hit {
		if proposal.EnergyDrainTriggered || proposal.EnergyRoll != 0 || proposal.EnergyBand != 0 || proposal.EnergyDiceCount != 0 || proposal.EnergyDiceSides != 0 || proposal.EnergyDicePlus != 0 || proposal.EnergyDrain != 0 || proposal.ExperienceBefore != 0 || proposal.ExperienceAfter != 0 || proposal.ProficiencyBefore != [5]int32{} || proposal.ProficiencyAfter != [5]int32{} || proposal.RealmBefore != [4]int32{} || proposal.RealmAfter != [4]int32{} {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy on miss")
		}
	} else {
		if proposal.ExperienceBefore != player.Body.Experience || proposal.ProficiencyBefore != player.Body.Proficiency || proposal.RealmBefore != player.Body.Realm {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat progression before")
		}
		progressed := player.Body
		if flag(npc.Body.Flags[:], npcCombatEnergyDrainFlag) && !proposal.BreathTriggered {
			if proposal.EnergyRoll < 1 || proposal.EnergyRoll > 100 || proposal.EnergyDrainTriggered != (proposal.EnergyRoll < 10) {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy roll")
			}
			if proposal.EnergyDrainTriggered {
				wantBand := (int(npc.Body.Level) + 3) / 4
				if proposal.EnergyBand != wantBand || proposal.EnergyDiceCount != wantBand || proposal.EnergyDiceSides != 5 || proposal.EnergyDicePlus != wantBand*5 || proposal.EnergyDrain < 0 || int64(proposal.EnergyDrain) > int64(player.Body.Experience) {
					return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy dice")
				}
				progressed.Experience -= int32(proposal.EnergyDrain)
				if err := lowerNPCCombatProficiency(&progressed, int32(proposal.EnergyDrain)); err != nil {
					return State{}, NPCCombatRoundResult{}, err
				}
			} else if proposal.EnergyBand != 0 || proposal.EnergyDiceCount != 0 || proposal.EnergyDiceSides != 0 || proposal.EnergyDicePlus != 0 || proposal.EnergyDrain != 0 {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy miss")
			}
		} else if proposal.EnergyDrainTriggered || proposal.EnergyRoll != 0 || proposal.EnergyBand != 0 || proposal.EnergyDiceCount != 0 || proposal.EnergyDiceSides != 0 || proposal.EnergyDicePlus != 0 || proposal.EnergyDrain != 0 {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat energy capability")
		}
		if proposal.ExperienceAfter != progressed.Experience || proposal.ProficiencyAfter != progressed.Proficiency || proposal.RealmAfter != progressed.Realm {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat progression after")
		}
	}
	if proposal.BreathTriggered != proposal.expectedBreathTriggered ||
		proposal.BreathType != proposal.expectedBreathType ||
		proposal.BreathRoll != proposal.expectedBreathRoll ||
		proposal.BreathDiceCount != proposal.expectedBreathDiceCount ||
		proposal.BreathDiceSides != proposal.expectedBreathDiceSides ||
		proposal.BreathDicePlus != proposal.expectedBreathDicePlus ||
		proposal.BreathResisted != proposal.expectedBreathResisted ||
		proposal.BreathPoisoned != proposal.expectedBreathPoisoned {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath outcome")
	}
	if !proposal.Hit {
		if proposal.BreathTriggered || proposal.BreathType != NPCCombatBreathFire || proposal.BreathRoll != 0 || proposal.BreathDiceCount != 0 || proposal.BreathDiceSides != 0 || proposal.BreathDicePlus != 0 || proposal.BreathResisted || proposal.BreathPoisoned {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath on miss")
		}
	} else if flag(npc.Body.Flags[:], npcCombatBreatherFlag) {
		if proposal.BreathRoll < 1 || proposal.BreathRoll > 30 || proposal.BreathTriggered != (proposal.BreathRoll < 5) {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath trigger")
		}
		if !proposal.BreathTriggered {
			if proposal.BreathType != NPCCombatBreathFire || proposal.BreathDiceCount != 0 || proposal.BreathDiceSides != 0 || proposal.BreathDicePlus != 0 || proposal.BreathResisted || proposal.BreathPoisoned {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath miss")
			}
		} else {
			wantType, wantCount, wantSides, wantPlus, wantResisted, wantPoisoned := npcCombatBreathSpec(npc.Body, player.Body)
			if proposal.BreathType != wantType || proposal.BreathDiceCount != wantCount || proposal.BreathDiceSides != wantSides || proposal.BreathDicePlus != wantPlus || proposal.BreathResisted != wantResisted || proposal.BreathPoisoned != wantPoisoned {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath branch")
			}
		}
	} else if proposal.BreathTriggered || proposal.BreathType != NPCCombatBreathFire || proposal.BreathRoll != 0 || proposal.BreathDiceCount != 0 || proposal.BreathDiceSides != 0 || proposal.BreathDicePlus != 0 || proposal.BreathResisted || proposal.BreathPoisoned {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat breath capability")
	}
	if proposal.DissolveSucceeded != proposal.expectedDissolveSucceeded ||
		proposal.Dissolved != proposal.expectedDissolved ||
		proposal.DissolveProtected != proposal.expectedDissolveProtected ||
		proposal.DissolveRoll != proposal.expectedDissolveRoll ||
		proposal.DissolveSelectionRoll != proposal.expectedDissolveSelectionRoll ||
		proposal.DissolveCandidateCount != proposal.expectedDissolveCandidateCount ||
		proposal.DissolveReadySlot != proposal.expectedDissolveReadySlot ||
		proposal.DissolveItemID != proposal.expectedDissolveItemID ||
		proposal.DissolveItemName != proposal.expectedDissolveItemName {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve outcome")
	}
	if proposal.Dissolved && (!proposal.DissolveSucceeded || proposal.DissolveProtected) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve protection")
	}
	if proposal.DissolveProtected && (!proposal.DissolveSucceeded || proposal.DissolveItemID == "") {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat protected dissolve")
	}
	if !proposal.Hit && (proposal.DissolveSucceeded || proposal.DissolveRoll != 0 || proposal.DissolveSelectionRoll != 0 || proposal.DissolveCandidateCount != 0 || proposal.DissolveReadySlot != 0 || proposal.DissolveItemID != "" || proposal.DissolveItemName != "" || proposal.DissolveProtected || proposal.Dissolved) {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve on miss")
	}
	if proposal.Hit && flag(npc.Body.Flags[:], npcCombatDissolverFlag) {
		if player.Items == nil {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("NPC combat MDISIT requires canonical player items")
		}
		slots, err := npcCombatReadySlots(player.Items)
		if err != nil {
			return State{}, NPCCombatRoundResult{}, err
		}
		if proposal.DissolveRoll < 1 || proposal.DissolveRoll > 100 || proposal.DissolveSucceeded != (proposal.DissolveRoll <= 15) {
			return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve roll")
		}
		if !proposal.DissolveSucceeded {
			if proposal.DissolveSelectionRoll != 0 || proposal.DissolveCandidateCount != 0 || proposal.DissolveReadySlot != 0 || proposal.DissolveItemID != "" || proposal.DissolveItemName != "" || proposal.DissolveProtected || proposal.Dissolved {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve miss")
			}
		} else {
			if proposal.DissolveCandidateCount != len(slots) {
				return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve candidates")
			}
			if len(slots) == 0 {
				if proposal.DissolveSelectionRoll != 0 || proposal.DissolveReadySlot != 0 || proposal.DissolveItemID != "" || proposal.DissolveItemName != "" || proposal.DissolveProtected || proposal.Dissolved {
					return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve empty selection")
				}
			} else {
				if proposal.DissolveSelectionRoll < 0 || proposal.DissolveSelectionRoll >= len(slots) || proposal.DissolveReadySlot != slots[proposal.DissolveSelectionRoll] {
					return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve selection")
				}
				selectedID := player.Items.Ready[proposal.DissolveReadySlot]
				selected, ok := player.Items.Items[selectedID]
				if !ok || selectedID != proposal.DissolveItemID || selected.Object.Name != proposal.DissolveItemName || proposal.DissolveProtected != flag(selected.Object.Flags[:], objectOneWevFlag) || proposal.Dissolved == proposal.DissolveProtected {
					return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve item")
				}
			}
		}
	} else if proposal.DissolveSucceeded || proposal.Dissolved || proposal.DissolveProtected || proposal.DissolveRoll != 0 || proposal.DissolveSelectionRoll != 0 || proposal.DissolveCandidateCount != 0 || proposal.DissolveReadySlot != 0 || proposal.DissolveItemID != "" || proposal.DissolveItemName != "" {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat dissolve capability")
	}
	nextPlayer, nextOK := proposal.next.Players[proposal.PlayerID]
	if !nextOK || int(nextPlayer.Body.HPCurrent) != proposal.PlayerHPAfter {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat candidate")
	}
	wantPoisoned := flag(player.Body.Flags[:], npcCombatVictimPoisonedFlag) || proposal.Poisoned || proposal.BreathPoisoned
	if flag(nextPlayer.Body.Flags[:], npcCombatVictimPoisonedFlag) != wantPoisoned {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat poison candidate")
	}
	wantDiseased := flag(player.Body.Flags[:], npcCombatVictimDiseasedFlag) || proposal.Diseased
	if flag(nextPlayer.Body.Flags[:], npcCombatVictimDiseasedFlag) != wantDiseased {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat disease candidate")
	}
	wantBlinded := flag(player.Body.Flags[:], npcCombatVictimBlindedFlag) || proposal.Blinded
	if flag(nextPlayer.Body.Flags[:], npcCombatVictimBlindedFlag) != wantBlinded {
		return State{}, NPCCombatRoundResult{}, fmt.Errorf("tampered NPC combat blind candidate")
	}
	expected := s.clone()
	expectedPlayer := expected.Players[proposal.PlayerID]
	expectedPlayer.Body.HPCurrent = int16(proposal.PlayerHPAfter)
	expectedPlayer.Body.Experience = proposal.ExperienceAfter
	expectedPlayer.Body.Proficiency = proposal.ProficiencyAfter
	expectedPlayer.Body.Realm = proposal.RealmAfter
	if proposal.Poisoned || proposal.BreathPoisoned {
		expectedPlayer.Body.Flags[npcCombatVictimPoisonedFlag/8] |= 1 << (npcCombatVictimPoisonedFlag % 8)
	}
	if proposal.Diseased {
		expectedPlayer.Body.Flags[npcCombatVictimDiseasedFlag/8] |= 1 << (npcCombatVictimDiseasedFlag % 8)
	}
	if proposal.Blinded {
		expectedPlayer.Body.Flags[npcCombatVictimBlindedFlag/8] |= 1 << (npcCombatVictimBlindedFlag % 8)
	}
	if proposal.Dissolved {
		if err := npcCombatDeleteReadyRoot(expectedPlayer.Items, proposal.DissolveReadySlot); err != nil {
			return State{}, NPCCombatRoundResult{}, err
		}
		if err := refreshEquipmentStats(&expectedPlayer); err != nil {
			return State{}, NPCCombatRoundResult{}, err
		}
	}
	expected.Players[proposal.PlayerID] = expectedPlayer
	if !reflect.DeepEqual(proposal.next, expected) {
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
		Hit: proposal.Hit, Critical: proposal.Critical, Poisoned: proposal.Poisoned,
		Diseased: proposal.Diseased, Blinded: proposal.Blinded,
		BreathTriggered: proposal.BreathTriggered, BreathType: proposal.BreathType,
		BreathRoll: proposal.BreathRoll, BreathDiceCount: proposal.BreathDiceCount,
		BreathDiceSides: proposal.BreathDiceSides, BreathDicePlus: proposal.BreathDicePlus,
		BreathResisted: proposal.BreathResisted, BreathPoisoned: proposal.BreathPoisoned,
		EnergyDrainTriggered: proposal.EnergyDrainTriggered, EnergyRoll: proposal.EnergyRoll,
		EnergyBand: proposal.EnergyBand, EnergyDiceCount: proposal.EnergyDiceCount,
		EnergyDiceSides: proposal.EnergyDiceSides, EnergyDicePlus: proposal.EnergyDicePlus,
		EnergyDrain: proposal.EnergyDrain, ExperienceBefore: proposal.ExperienceBefore,
		ExperienceAfter: proposal.ExperienceAfter, ProficiencyBefore: proposal.ProficiencyBefore,
		ProficiencyAfter: proposal.ProficiencyAfter, RealmBefore: proposal.RealmBefore,
		RealmAfter:        proposal.RealmAfter,
		DissolveSucceeded: proposal.DissolveSucceeded, Dissolved: proposal.Dissolved,
		DissolveProtected: proposal.DissolveProtected, DissolveRoll: proposal.DissolveRoll,
		DissolveSelectionRoll:  proposal.DissolveSelectionRoll,
		DissolveCandidateCount: proposal.DissolveCandidateCount,
		DissolveReadySlot:      proposal.DissolveReadySlot, DissolveItemID: proposal.DissolveItemID,
		DissolveItemName: proposal.DissolveItemName, Damage: proposal.Damage,
		PlayerHP: proposal.PlayerHPAfter, TargetHP: proposal.PlayerHPAfter, Killed: false,
	}
	return proposal.next.clone(), result, nil
}
