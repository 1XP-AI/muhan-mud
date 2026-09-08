package world

import (
	"fmt"
	"strings"
)

// LookAtTargetResult is the actor-facing receipt projection for the bounded
// target form of action.c's "보아" branch.  It deliberately does not include
// a room event in the persisted response: callers derive Event from the
// committed snapshot and suppress it when a receipt is replayed.
//
// This is not the general 조사/look command.  Room descriptions, objects,
// occurrence selectors, prefixes and NPC socket delivery remain outside this
// slice.
type LookAtTargetResult struct {
	Response   string
	Broadcast  bool
	TargetID   string
	TargetKind string
	TargetName string
	Event      *LookAtTargetEvent
}

// LookAtTargetEvent is the committed-state projection of action.c OUTj2.
// TargetText is present only for an online player target.  NPCs are canonical
// room occupants but have no proven client recipient in the Go runtime, so an
// NPC target receives only the room projection.
type LookAtTargetEvent struct {
	RoomID          int16
	ActorID         string
	ActorName       string
	TargetID        string
	TargetKind      string
	TargetName      string
	ExcludeActorID  string
	ExcludeTargetID string
	Text            string
	TargetText      string
}

type lookAtTarget struct {
	Kind string
	ID   string
	Body LegacyMonster
}

const (
	// CARETAKER and PDMINV are the source values used by find_crt's initial
	// skip.  Keep these local until the wider class/visibility model is
	// admitted as a shared Go contract.
	lookAtCaretakerClass = 10
)

// PlanLookAtTarget ports only the explicit-target action.c "보아" branch.
//
// C clears PHIDDN before checking PSILNC (action.c:58-63), so even a silent
// actor gets a cloned candidate with its hidden bit cleared.  Target lookup is
// then the source's NPC-first, player-second room traversal, narrowed here to
// exact display names and canonical online/same-room identities.  A missing,
// ambiguous or otherwise unresolved target fails closed rather than silently
// falling back to targetless action output.
func (s State) PlanLookAtTarget(actorID, targetName string) (State, LookAtTargetResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return State{}, LookAtTargetResult{}, fmt.Errorf("online look-at actor absent")
	}

	next := s.clone()
	actor = next.Players[actorID]
	// action.c performs this write before the silence guard.  In particular,
	// do not move it below target resolution: PHIDDN ordering is part of the
	// durable command contract for this bounded slice.
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[actorID] = actor

	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return next, LookAtTargetResult{Response: "한마디도 할수 없습니다!\r\n"}, nil
	}

	target, err := next.selectLookAtTarget(actorID, targetName)
	if err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	event, ok, err := next.RoomLookAtTargetEvent(actorID, target.Kind, target.ID)
	if err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	if !ok {
		return State{}, LookAtTargetResult{}, fmt.Errorf("look-at target event unavailable")
	}
	response := lookAtActorResponse(actor.Body, target.Body, target.Kind)
	result := LookAtTargetResult{
		Response:   response,
		Broadcast:  true,
		TargetID:   target.ID,
		TargetKind: target.Kind,
		TargetName: target.Body.Name,
		Event:      &event,
	}
	return next, result, nil
}

// RoomLookAtTargetEvent derives the room/recipient projection from a
// committed snapshot.  It revalidates canonical identity and visibility so a
// caller cannot publish a stale target after a receipt commit or replay.
func (s State) RoomLookAtTargetEvent(actorID, targetKind, targetID string) (LookAtTargetEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return LookAtTargetEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return LookAtTargetEvent{}, false, fmt.Errorf("online look-at actor absent")
	}
	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return LookAtTargetEvent{}, false, nil
	}
	if targetID == "" {
		return LookAtTargetEvent{}, false, fmt.Errorf("look-at target identity required")
	}
	target, err := s.lookAtTargetByID(actorID, targetKind, targetID)
	if err != nil {
		return LookAtTargetEvent{}, false, err
	}

	actorName := actor.Body.Name + "님"
	targetName := lookAtRenderedTargetName(actor.Body, target.Body, target.Kind)
	targetRef := targetName + lookAtObjectParticle(targetName)
	event := LookAtTargetEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		TargetID:       target.ID,
		TargetKind:     target.Kind,
		TargetName:     target.Body.Name,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s이 %s 봅니다.\r\n", actorName, targetRef),
	}
	if target.Kind == "player" {
		event.ExcludeTargetID = target.ID
		event.TargetText = fmt.Sprintf("\n%s이 당신을 봅니다.\r\n", actorName)
	}
	return event, true, nil
}

// LookAtTargetEventForName resolves the exact bounded command target against
// the same committed snapshot used by the reducer, then derives the
// recipient-specific projection. It is intentionally read-only and exists so
// the transport never treats a client-supplied name as an identity.
func (s State) LookAtTargetEventForName(actorID, targetName string) (LookAtTargetEvent, bool, error) {
	target, err := s.selectLookAtTarget(actorID, targetName)
	if err != nil {
		return LookAtTargetEvent{}, false, err
	}
	return s.RoomLookAtTargetEvent(actorID, target.Kind, target.ID)
}

// selectLookAtTarget preserves find_crt's canonical traversal order: NPCs
// precede players in the room.  Unlike EQUAL, this bounded command admits
// only exact display-name matching; no key/prefix/occurrence interpretation
// is inferred from a client line.
func (s State) selectLookAtTarget(actorID, targetName string) (lookAtTarget, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return lookAtTarget{}, fmt.Errorf("online look-at actor absent")
	}
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return lookAtTarget{}, fmt.Errorf("look-at target required")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return lookAtTarget{}, fmt.Errorf("look-at actor room absent")
	}

	// A non-nil NPC map is the admission boundary for canonical NPC identity.
	// Never fall back to LegacyRoom.Monsters: those bodies have no durable
	// identity or online/recipient contract in this slice.
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("unresolved NPC look-at identity")
		}
		if !strings.EqualFold(npc.Body.Name, targetName) || !lookAtVisible(actor.Body, npc.Body) {
			continue
		}
		return lookAtTarget{Kind: "npc", ID: id, Body: npc.Body}, nil
	}
	for _, id := range room.PlayerIDs {
		player, exists := s.Players[id]
		if !exists || id == "" || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("unresolved player look-at identity")
		}
		if id == actorID || !strings.EqualFold(player.Body.Name, targetName) || !lookAtVisible(actor.Body, player.Body) {
			continue
		}
		return lookAtTarget{Kind: "player", ID: id, Body: player.Body}, nil
	}
	return lookAtTarget{}, fmt.Errorf("look-at target absent")
}

func (s State) lookAtTargetByID(actorID, targetKind, targetID string) (lookAtTarget, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return lookAtTarget{}, fmt.Errorf("online look-at actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return lookAtTarget{}, fmt.Errorf("look-at actor room absent")
	}
	switch targetKind {
	case "player":
		if targetID == actorID || !roomContainsPlayer(room, targetID) {
			return lookAtTarget{}, fmt.Errorf("look-at player target is not in actor room")
		}
		player, exists := s.Players[targetID]
		if !exists || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("look-at player target absent")
		}
		if !lookAtVisible(actor.Body, player.Body) {
			return lookAtTarget{}, fmt.Errorf("look-at player target is not visible")
		}
		return lookAtTarget{Kind: targetKind, ID: targetID, Body: player.Body}, nil
	case "npc":
		if !containsString(room.NPCIDs, targetID) || s.NPCs == nil {
			return lookAtTarget{}, fmt.Errorf("look-at NPC target is not in actor room")
		}
		npc, exists := s.NPCs[targetID]
		if !exists || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("look-at NPC target absent")
		}
		if !lookAtVisible(actor.Body, npc.Body) {
			return lookAtTarget{}, fmt.Errorf("look-at NPC target is not visible")
		}
		return lookAtTarget{Kind: targetKind, ID: targetID, Body: npc.Body}, nil
	default:
		return lookAtTarget{}, fmt.Errorf("unsupported look-at target kind %q", targetKind)
	}
}

// lookAtVisible is the source find_crt visibility gate, restricted to the
// player/NPC flags represented by canonical LegacyMonster bodies.  A target
// with a caretaker-class PDMINV identity is skipped exactly as find_crt does;
// ordinary players/NPCs may be resolved through PDINVI for the proven
// invisible-target case.
func lookAtVisible(actor, target LegacyMonster) bool {
	if target.Class >= lookAtCaretakerClass && flag(target.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return flag(actor.Flags[:], playerDetectFlag) || !flag(target.Flags[:], playerInvisibleFlag)
}

func lookAtRenderedTargetName(actor, target LegacyMonster, kind string) string {
	if kind == "player" {
		if flag(target.Flags[:], playerDMInvisibleFlag) || (flag(target.Flags[:], playerInvisibleFlag) && !flag(actor.Flags[:], playerDetectFlag)) {
			return "누군가"
		}
		name := target.Name
		if flag(target.Flags[:], playerInvisibleFlag) {
			name += "(*)"
		}
		return name + "님"
	}
	return target.Name
}

func lookAtActorResponse(actor, target LegacyMonster, kind string) string {
	rendered := lookAtRenderedTargetName(actor, target, kind)
	return fmt.Sprintf("당신은 %s%s 봅니다.\r\n", rendered, lookAtObjectParticle(rendered))
}

func lookAtObjectParticle(rendered string) string {
	if hasFinalHangul(rendered) {
		return "을"
	}
	return "를"
}
