package world

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Flee is the bounded player branch of src/command7.c:flee.  The legacy
// weapon-drop block is commented out, and its F_ISSET(xp->ext,52) reads past
// the four-byte exit flag array.  Neither is guessed here.  Arrival traps are
// also an explicit fail-closed boundary because check_traps is a separate
// state transition (and can kill a player).
const (
	fleeAttackTimer         = 3  // LT_ATTCK
	fleeSpellTimer          = 9  // LT_SPELL
	fleeFearFlag            = 43 // PFEARS
	fleeHiddenFlag          = 1  // PHIDDN
	fleeInvisibleFlag       = 2  // PINVIS
	fleeDetectInvisibleFlag = 21 // PDINVI
	fleeMaleFlag            = 12 // PMALES
	fleeLevitateFlag        = 25 // PLEVIT
	fleeFlyFlag             = 31 // PFLYSP
	fleeDMInvisibleFlag     = 10 // PDMINV

	fleeClosedExitFlag         = 3  // XCLOSD
	fleeSecretExitFlag         = 0  // XSECRT
	fleeInvisibleExitFlag      = 1  // XINVIS
	fleeClimbExitFlag          = 8  // XCLIMB
	fleeDifficultClimbExitFlag = 10 // XDCLIM
	fleeFlyExitFlag            = 11 // XFLYSP
	fleeFemaleExitFlag         = 12 // XFEMAL
	fleeMaleExitFlag           = 13 // XMALES
	fleeNightOnlyExitFlag      = 16 // XNGHTO
	fleeDayOnlyExitFlag        = 17 // XDAYON
	fleeGuardExitFlag          = 18 // XPGUAR
	fleeNoSeeExitFlag          = 19 // XNOSEE
	fleeNakedExitFlag          = 7  // XNAKED

	fleeRoomOnePlayerFlag   = 14 // RONEPL
	fleeRoomTwoPlayerFlag   = 15 // RTWOPL
	fleeRoomThreePlayerFlag = 16 // RTHREE
	fleePermanentTrackFlag  = 18 // RPTRAK

	fleeGuardMonsterFlag = 38 // MPGUAR
	fleePaladinClass     = 6  // PALADIN
	fleeCaretakerClass   = 10 // CARETAKER
)

// ErrFleeArrivalTrap marks a destination whose check_traps side effects are
// not yet part of this command's atomic reducer.  The whole command is
// rejected; no hidden/track/experience mutation is retained.
var ErrFleeArrivalTrap = fmt.Errorf("flee arrival trap is not implemented")

// FleeProposal is the pure candidate for one player flee request.  next and
// before are private on purpose: callers cannot submit a client-built state
// candidate, while ApplyFlee can still verify that it is applying the exact
// snapshot that was planned.  This also lets room-entry refreshes consume
// injected RNG exactly once during planning and never again during replay.
type FleeProposal struct {
	ActorID           string
	ActorName         string
	SourceRoomID      int16
	DestinationRoomID int16
	ExitIndex         int
	ExitName          string
	Now               int32
	Hour              int
	Chance            int
	WaitSeconds       int32
	Cooldown          bool
	NoCombat          bool
	Moved             bool
	Broadcast         bool
	PaladinPenalty    bool
	ExperienceLoss    int32
	Response          string
	Transfer          TransferProposal

	before State
	next   State
}

// FleeResult is stored in the durable receipt.  The room IDs/name are
// receipt data for post-commit event projection; the actor's response is
// already complete and must not be recomputed on replay.
type FleeResult struct {
	Response          string `json:"response"`
	Broadcast         bool   `json:"broadcast"`
	Moved             bool   `json:"moved"`
	ActorID           string `json:"actor_id"`
	ActorName         string `json:"actor_name"`
	SourceRoomID      int16  `json:"source_room_id"`
	DestinationRoomID int16  `json:"destination_room_id"`
	ExitName          string `json:"exit_name"`
}

// FleeEvent is the committed room projection.  The actor receives the
// durable response itself, so both texts exclude ActorID at transport time.
type FleeEvent struct {
	SourceRoomID      int16
	DestinationRoomID int16
	ExcludeActorID    string
	SourceText        string
	DestinationText   string
}

func validFleeText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\r' || r == '\n' || r == '\x00' {
			return false
		}
	}
	return true
}

func fleeWaitResponse(seconds int32) string {
	if seconds == 1 {
		return "1초만 기다리세요.\r\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds)
}

func fleeRoll(roll func(int, int) int, low, high int) (int, error) {
	if roll == nil {
		return 0, fmt.Errorf("missing flee random source")
	}
	var value int
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				value = low - 1
			}
		}()
		value = roll(low, high)
	}()
	if value < low || value > high {
		return 0, fmt.Errorf("flee random value outside %d..%d", low, high)
	}
	return value, nil
}

func fleeDexBonus(body LegacyMonster) (int, error) {
	if body.Class > 12 || body.Stats[1] > 63 {
		return 0, fmt.Errorf("flee actor has undefined legacy class or dexterity")
	}
	return legacyStatBonus[body.Stats[1]], nil
}

func fleeWeight(player PlayerState) (int, error) {
	if player.Items != nil {
		return player.Items.Weight()
	}
	// A zero legacy inventory is unambiguous.  Non-empty legacy object graphs
	// have no canonical ownership IDs and cannot safely be used by an atomic
	// player command; reject only when an XNAKED candidate needs the value.
	if len(player.Body.Inventory) != 0 {
		return 0, fmt.Errorf("flee weight requires canonical player items")
	}
	return 0, nil
}

func fleeVisibleCount(players []RoomPlayerView) int {
	count := 0
	for _, player := range players {
		if !flag(player.Flags[:], fleeDMInvisibleFlag) {
			count++
		}
	}
	return count
}

// fleeGuarded reports the XPGUAR branch using the canonical ordered NPC
// identities when available. Legacy room monsters remain readable for this
// pure predicate, but an unresolved canonical room never gets silently
// treated as peaceful.
func (s State) fleeGuarded(room RoomState, actor LegacyMonster) (bool, error) {
	if flag(actor.Flags[:], fleeInvisibleFlag) || actor.Class >= fleeCaretakerClass {
		return false, nil
	}
	if s.NPCs == nil {
		for _, monster := range room.Resource.Monsters {
			if flag(monster.Flags[:], fleeGuardMonsterFlag) {
				return true, nil
			}
		}
		return false, nil
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return false, fmt.Errorf("unresolved flee guard identity")
		}
		if flag(npc.Body.Flags[:], fleeGuardMonsterFlag) {
			return true, nil
		}
	}
	return false, nil
}

// fleeCombatState mirrors ply_is_attacking: the player must be present in an
// NPC's enemy list with a non-negative damage value. A room with legacy
// monsters but no imported enemy identities is unresolved and fails closed.
func (s State) fleeCombatState(actorID string, room RoomState) (bool, error) {
	if s.NPCs == nil {
		if len(room.Resource.Monsters) != 0 {
			return false, fmt.Errorf("flee enemy relations unresolved")
		}
		return false, nil
	}
	fighting, _, err := s.NPCMovementRelations(actorID)
	return fighting, err
}

func fleeDestinationMessage(destination LegacyRoom, visitor DestinationVisitor, occupants []RoomPlayerView) string {
	level := int(visitor.Level)
	if int8(destination.LowLevel) > int8(visitor.Level) {
		return "어떤 힘에 의해 다시 되돌아 왔습니다.\r\n"
	}
	if destination.HighLevel != 0 && level > int(int8(destination.HighLevel)) {
		return "어떤 힘에 의해 다시 되돌아 왔습니다.\r\n"
	}
	count := fleeVisibleCount(occupants)
	if (flag(destination.Flags[:], fleeRoomOnePlayerFlag) && count > 0) ||
		(flag(destination.Flags[:], fleeRoomTwoPlayerFlag) && count > 1) ||
		(flag(destination.Flags[:], fleeRoomThreePlayerFlag) && count > 2) {
		return "도망갈려는 방의 정원이 가득 찼습니다!\r\n"
	}
	return ""
}

// planFleeMovement is called only after the exit loop has selected one
// authoritative exit. It intentionally does not apply family/property rules:
// command7.c:flee checks only destination low/high level and RONEPL/RTWOPL/
// RTHREE capacity before the add_ply_rom path.
func planFleeMovement(in MovementInput, selected int) (MovementProposal, error) {
	if selected < 0 || selected >= len(in.Source.Exits) || in.Destination == nil {
		return MovementProposal{}, fmt.Errorf("flee destination absent")
	}
	exit := in.Source.Exits[selected]
	if exit.Destination != in.Destination.ID || in.Source.ID == in.Destination.ID {
		return MovementProposal{}, fmt.Errorf("flee exit destination mismatch")
	}
	r := MovementProposal{
		RoomID:       in.Source.ID,
		HP:           in.Traversal.HP,
		Hidden:       false,
		Track:        in.Source.Track,
		BlockerIndex: -1,
		Traversal:    TraversalResult{HP: in.Traversal.HP, GuardIndex: -1},
	}
	if !flag(in.Source.Flags[:], fleePermanentTrackFlag) {
		r.Track = exit.Name
	}
	if message := fleeDestinationMessage(*in.Destination, in.Visitor, in.Occupants); message != "" {
		r.Messages = append(r.Messages, message)
		return r, nil
	}
	r.RoomID = in.Destination.ID
	r.Moved = true
	return r, nil
}

// lowerFleeProficiency is a direct, bounded port of command10.c:lower_prof.
// It is kept local to this slice because the broader progression reducer is
// not yet the authority for experience-loss side effects.
func lowerFleeProficiency(body *LegacyMonster, loss int32) error {
	if body == nil || loss < 0 {
		return fmt.Errorf("invalid flee proficiency loss")
	}
	total := int64(0)
	for _, value := range body.Proficiency {
		if value < 0 {
			return fmt.Errorf("negative flee weapon proficiency")
		}
		total += int64(value)
	}
	for _, value := range body.Realm {
		if value < 0 {
			return fmt.Errorf("negative flee magic proficiency")
		}
		total += int64(value)
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
			if share > int64(^uint32(0)>>1) {
				return fmt.Errorf("flee proficiency loss outside legacy range")
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

func applyFleePenalty(s State, proposal FleeProposal) (State, error) {
	next := s.clone()
	if !proposal.PaladinPenalty {
		return next, nil
	}
	actor, ok := next.Players[proposal.ActorID]
	if !ok || actor.Body.Class != fleePaladinClass || actor.Body.Level <= 20 || actor.Body.Experience < 0 || proposal.ExperienceLoss < 0 || proposal.ExperienceLoss > actor.Body.Experience {
		return State{}, fmt.Errorf("stale flee paladin penalty")
	}
	actor.Body.Experience -= proposal.ExperienceLoss
	if err := lowerFleeProficiency(&actor.Body, proposal.ExperienceLoss); err != nil {
		return State{}, err
	}
	next.Players[proposal.ActorID] = actor
	return next, nil
}

// PlanFlee implements the source's player flee eligibility and selected-exit
// movement. Roll order is the source order: one chance roll per eligible exit,
// and no random source call on cooldown, no-combat, or no-exit responses.
func (s State) PlanFlee(actorID string, now int32, hour int, roll func(int, int) int, catalog SpawnCatalog, allocate func() (string, error)) (FleeProposal, error) {
	if err := s.Validate(); err != nil {
		return FleeProposal{}, err
	}
	if actorID == "" || now < 0 || hour < 0 || hour > 23 {
		return FleeProposal{}, fmt.Errorf("invalid flee actor or clock")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validFleeText(actor.Body.Name) {
		return FleeProposal{}, fmt.Errorf("online flee actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return FleeProposal{}, fmt.Errorf("flee actor room absent")
	}
	// command7.c compares the two stored ltime values directly. Their
	// intervals are used by other commands but are not part of flee's gate.
	deadline := int64(actor.Body.Timers[fleeAttackTimer].LastTime)
	spellDeadline := int64(actor.Body.Timers[fleeSpellTimer].LastTime)
	if spellDeadline > deadline {
		deadline = spellDeadline
	}
	proposal := FleeProposal{ActorID: actorID, ActorName: actor.Body.Name, SourceRoomID: actor.Body.RoomID, Now: now, Hour: hour, before: s.clone()}
	if int64(now) < deadline && !flag(actor.Body.Flags[:], fleeFearFlag) {
		wait := deadline - int64(now)
		if wait < 1 || wait > int64(^uint32(0)>>1) {
			return FleeProposal{}, fmt.Errorf("flee cooldown overflow")
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response = fleeWaitResponse(proposal.WaitSeconds)
		proposal.next = s.clone()
		return proposal, nil
	}

	dexBonus, err := fleeDexBonus(actor.Body)
	if err != nil {
		return FleeProposal{}, err
	}
	chance := 65 + dexBonus*5
	proposal.Chance = chance
	weightKnown := actor.Items != nil || len(actor.Body.Inventory) == 0
	var weight int
	if weightKnown && actor.Items != nil {
		weight, err = fleeWeight(actor)
		if err != nil {
			return FleeProposal{}, err
		}
	}
	guardFound := false // C's found variable is intentionally sticky across exits.
	selected := -1
	attackingKnown := false
	attacking := false
	for index, exit := range room.Resource.Exits {
		if !validFleeText(exit.Name) {
			return FleeProposal{}, fmt.Errorf("invalid flee exit name")
		}
		if flag(exit.Flags[:], fleeClosedExitFlag) {
			continue
		}
		if (flag(exit.Flags[:], fleeClimbExitFlag) || flag(exit.Flags[:], fleeDifficultClimbExitFlag)) && !flag(actor.Body.Flags[:], fleeLevitateFlag) {
			continue
		}
		if flag(exit.Flags[:], fleeNakedExitFlag) {
			if !weightKnown {
				return FleeProposal{}, fmt.Errorf("flee weight requires canonical player items")
			}
			if weight != 0 {
				continue
			}
		}
		if flag(exit.Flags[:], fleeFemaleExitFlag) && flag(actor.Body.Flags[:], fleeMaleFlag) {
			continue
		}
		if flag(exit.Flags[:], fleeMaleExitFlag) && !flag(actor.Body.Flags[:], fleeMaleFlag) {
			continue
		}
		if flag(exit.Flags[:], fleeFlyExitFlag) && !flag(actor.Body.Flags[:], fleeFlyFlag) {
			continue
		}
		if !flag(actor.Body.Flags[:], fleeDetectInvisibleFlag) && flag(exit.Flags[:], fleeInvisibleExitFlag) {
			continue
		}
		if flag(exit.Flags[:], fleeSecretExitFlag) || flag(exit.Flags[:], fleeNoSeeExitFlag) {
			continue
		}
		if flag(exit.Flags[:], fleeNightOnlyExitFlag) && hour > 6 && hour < 20 {
			continue
		}
		if flag(exit.Flags[:], fleeDayOnlyExitFlag) && (hour < 6 || hour > 20) {
			continue
		}
		if flag(exit.Flags[:], fleeGuardExitFlag) {
			guard, guardErr := s.fleeGuarded(room, actor.Body)
			if guardErr != nil {
				return FleeProposal{}, guardErr
			}
			if guard {
				guardFound = true
			}
			if guardFound {
				continue
			}
		}
		if !attackingKnown {
			attacking, err = s.fleeCombatState(actorID, room)
			if err != nil {
				return FleeProposal{}, err
			}
			attackingKnown = true
		}
		if !attacking {
			proposal.NoCombat = true
			proposal.Response = "누구에게서 도망가시려구요?\r\n"
			proposal.next = s.clone()
			return proposal, nil
		}
		value, rollErr := fleeRoll(roll, 1, 100)
		if rollErr != nil {
			return FleeProposal{}, rollErr
		}
		if value < chance {
			selected = index
			break
		}
	}
	if selected < 0 {
		proposal.Response = "\n당신은 겁에 질려 다리가 떨어지지 않습니다!"
		proposal.next = s.clone()
		return proposal, nil
	}
	exit := room.Resource.Exits[selected]
	destinationRoom, ok := s.Rooms[exit.Destination]
	if !ok {
		return FleeProposal{}, fmt.Errorf("flee destination room absent")
	}
	if destinationRoom.Resource.Trap != 0 || destinationRoom.Resource.TrapExit != 0 {
		return FleeProposal{}, ErrFleeArrivalTrap
	}
	projectedDestination, err := s.ProjectRoom(exit.Destination)
	if err != nil {
		return FleeProposal{}, err
	}
	input := TransferInput{
		ActorID:  actorID,
		Movement: MovementInput{Destination: &projectedDestination, Prefix: exit.Name, Now: int64(now), Traversal: TraversalInput{HP: int(actor.Body.HPCurrent)}, Visitor: DestinationVisitor{Class: actor.Body.Class, Level: actor.Body.Level}},
		View:     SceneOptions{ViewOptions: ViewOptions{Hour: hour}, ViewerID: actorID},
	}
	next, transfer, err := s.transferWithPlanner(input, catalog, roll, allocate, func(m MovementInput, _ func(int, int) int) (MovementProposal, error) {
		return planFleeMovement(m, selected)
	})
	if err != nil {
		return FleeProposal{}, err
	}
	proposal.Transfer = transfer
	proposal.ExitIndex = selected
	proposal.ExitName = exit.Name
	proposal.DestinationRoomID = exit.Destination
	proposal.Moved = transfer.Movement.Moved
	proposal.Broadcast = true
	proposal.Response = "\n당신은 줄행랑을 칩니다."
	if flag(actor.Body.Flags[:], fleeFearFlag) {
		proposal.Response += "\n당신은 겁에 질린듯 얼굴이 창백하게 변해 도망을 갑니다!"
	}
	if actor.Body.Class == fleePaladinClass && actor.Body.Level > 20 {
		if actor.Body.Experience < 0 {
			return FleeProposal{}, fmt.Errorf("negative flee experience")
		}
		loss := int32(((int(actor.Body.Level) + 3) / 4) * 10)
		if int64(loss) > int64(actor.Body.Experience) {
			loss = actor.Body.Experience
		}
		proposal.PaladinPenalty = true
		proposal.ExperienceLoss = loss
		proposal.Response += fmt.Sprintf("당신은 도망을 간 댓가로 %d 만큼의 경험치를 잃었습니다.\r\n", loss)
	}
	proposal.Response += strings.Join(transfer.Movement.Messages, "")
	if transfer.Movement.Moved {
		proposal.Response += "\r\n"
	}
	withPenalty, err := applyFleePenalty(next, proposal)
	if err != nil {
		return FleeProposal{}, err
	}
	if transfer.Movement.Moved {
		if scene, sceneErr := withPenalty.CurrentScene(actorID, hour); sceneErr != nil {
			return FleeProposal{}, sceneErr
		} else {
			proposal.Response += scene
		}
	}
	proposal.next = withPenalty
	return proposal, nil
}

// ApplyFlee commits a previously planned candidate and rejects stale/tampered
// proposals atomically. It performs no random calls or room refreshes.
func (s State) ApplyFlee(proposal FleeProposal) (State, FleeResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, FleeResult{}, err
	}
	if proposal.ActorID == "" || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, FleeResult{}, fmt.Errorf("stale flee proposal")
	}
	if err := proposal.next.Validate(); err != nil {
		return State{}, FleeResult{}, err
	}
	result := FleeResult{Response: proposal.Response, Broadcast: proposal.Broadcast, Moved: proposal.Moved, ActorID: proposal.ActorID, ActorName: proposal.ActorName, SourceRoomID: proposal.SourceRoomID, DestinationRoomID: proposal.DestinationRoomID, ExitName: proposal.ExitName}
	return proposal.next, result, nil
}

// RoomFleeEvent validates the receipt-bound movement and derives ordered room
// messages. The legacy handler broadcasts the destination arrival both after
// a successful add_ply_rom and after low/high/capacity denial, so a selected
// exit always has both room projections.
func (s State) RoomFleeEvent(result FleeResult) (FleeEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return FleeEvent{}, false, err
	}
	if !result.Broadcast || result.ActorID == "" || !validFleeText(result.ActorName) || !validFleeText(result.ExitName) {
		return FleeEvent{}, false, nil
	}
	actor, ok := s.Players[result.ActorID]
	if !ok || !actor.Online || actor.Body.Name != result.ActorName {
		return FleeEvent{}, false, fmt.Errorf("flee event actor absent")
	}
	if _, ok := s.Rooms[result.SourceRoomID]; !ok {
		return FleeEvent{}, false, fmt.Errorf("flee event source room absent")
	}
	if _, ok := s.Rooms[result.DestinationRoomID]; !ok {
		return FleeEvent{}, false, fmt.Errorf("flee event destination room absent")
	}
	if result.SourceRoomID == result.DestinationRoomID && result.Moved {
		return FleeEvent{}, false, fmt.Errorf("flee event source equals destination")
	}
	if result.Moved && actor.Body.RoomID != result.DestinationRoomID {
		return FleeEvent{}, false, fmt.Errorf("flee event destination changed")
	}
	if !result.Moved && actor.Body.RoomID != result.SourceRoomID {
		return FleeEvent{}, false, fmt.Errorf("flee event source changed")
	}
	event := FleeEvent{
		SourceRoomID:      result.SourceRoomID,
		DestinationRoomID: result.DestinationRoomID,
		ExcludeActorID:    result.ActorID,
		SourceText:        fmt.Sprintf("\n%s님이 %s쪽으로 도망을 갑니다.\r\n", result.ActorName, result.ExitName),
	}
	event.DestinationText = fmt.Sprintf("\n%s님이 도착하였습니다.\r\n", result.ActorName)
	return event, true, nil
}
