package world

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LookAtTargetResult is the actor-facing receipt projection for the bounded
// target form of action.c's "보아" branch.  It deliberately does not include
// a room event in the persisted response: callers derive Event from the
// committed snapshot and suppress it when a receipt is replayed.
//
// This is not the general 조사/look command. Exact target names admit
// canonical same-room floor roots, visible exits and the existing player/NPC
// branch. The bounded prefix/occurrence form is limited to canonical
// player/NPC identities because that is the authority used by action.c's
// find_crt; objects/exits remain exact-only in this slice.
type LookAtTargetResult struct {
	Response   string
	Broadcast  bool
	TargetID   string
	TargetKind string
	TargetName string
	Event      *LookAtTargetEvent
}

func validLookAtName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return false
		}
	}
	return true
}

func (s State) selectRoomObjectTarget(actor LegacyMonster, room RoomState, targetName string) (lookAtTarget, bool, error) {
	if room.Items == nil {
		// A legacy linked list is not an identity source. Only report an
		// unresolved boundary when the requested name would otherwise match;
		// unrelated canonical exits remain independently inspectable.
		for _, object := range room.Resource.Objects {
			if object.Name == targetName {
				return lookAtTarget{}, false, fmt.Errorf("%w: legacy room object root", ErrCanonicalRoomObjectUnavailable)
			}
		}
		return lookAtTarget{}, false, nil
	}
	selected, err := SelectCanonicalRoomObjectRoot(room.Items, targetName, flag(actor.Flags[:], playerDetectInvisibleFlag))
	if err != nil {
		if errors.Is(err, ErrCanonicalRoomObjectNotFound) {
			return lookAtTarget{}, false, nil
		}
		return lookAtTarget{}, false, err
	}
	return lookAtTarget{Kind: "object", ID: selected.ID, Name: selected.Object.Name, Description: selected.Object.Description}, true, nil
}

func selectExactLookAtExit(actor LegacyMonster, room LegacyRoom, targetName string) (lookAtTarget, bool, error) {
	if !validLookAtName(targetName) {
		return lookAtTarget{}, false, fmt.Errorf("invalid look-at target")
	}
	selected := -1
	for index, exit := range room.Exits {
		if !validLookAtName(exit.Name) {
			return lookAtTarget{}, false, fmt.Errorf("unresolved canonical look-at exit")
		}
		if exit.Name != targetName || flag(exit.Flags[:], 19) || (flag(exit.Flags[:], 1) && !flag(actor.Flags[:], playerDetectInvisibleFlag)) {
			continue
		}
		if selected >= 0 {
			return lookAtTarget{}, false, fmt.Errorf("ambiguous canonical look-at exit")
		}
		selected = index
	}
	if selected < 0 {
		return lookAtTarget{}, false, nil
	}
	exit := room.Exits[selected]
	return lookAtTarget{Kind: "exit", ID: searchExitID(room.ID, selected), Name: exit.Name}, true, nil
}

// LookAtTargetProposal is the pure candidate for ApplyLookAtTarget.  The
// target identity is resolved from the committed snapshot; callers must not
// replace it with a client-supplied name or ID between planning and apply.
type LookAtTargetProposal struct {
	ActorID     string
	RoomID      int16
	TargetID    string
	TargetKind  string
	TargetName  string
	Response    string
	Broadcast   bool
	ClearHidden bool
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
	Kind        string
	ID          string
	Name        string
	Description string
	Body        LegacyMonster
}

const (
	// CARETAKER and PDMINV are the source values used by find_crt's initial
	// skip.  Keep these local until the wider class/visibility model is
	// admitted as a shared Go contract.
	lookAtCaretakerClass = 10
)

// PlanLookAtTargetProposal ports the exact explicit-target action.c "보아"
// branch. Prefix/occurrence callers use
// PlanLookAtTargetProposalWithOccurrence.
//
// C clears PHIDDN before checking PSILNC (action.c:58-63), so even a silent
// actor gets a candidate with its hidden bit cleared. Target lookup is then
// the source's NPC-first, player-second room traversal, followed only by
// canonical floor roots and exact visible exits. A missing, ambiguous or
// otherwise unresolved target fails closed rather than silently falling back
// to targetless action output.
func (s State) PlanLookAtTargetProposal(actorID, targetName string) (LookAtTargetProposal, error) {
	return s.planLookAtTargetProposal(actorID, targetName, 0)
}

// PlanLookAtTargetProposalWithOccurrence admits the legacy find_crt shape:
// targetName is a name/key prefix and occurrence is one-based. NPC room order
// precedes player room order, matching action.c. Occurrence zero is reserved
// for the exact bounded form and is accepted here only for compatibility with
// the wrapper above; positive occurrences never inspect floor objects/exits.
func (s State) PlanLookAtTargetProposalWithOccurrence(actorID, targetName string, occurrence int) (LookAtTargetProposal, error) {
	return s.planLookAtTargetProposal(actorID, targetName, occurrence)
}

func (s State) planLookAtTargetProposal(actorID, targetName string, occurrence int) (LookAtTargetProposal, error) {
	if err := s.Validate(); err != nil {
		return LookAtTargetProposal{}, err
	}
	if occurrence < 0 {
		return LookAtTargetProposal{}, fmt.Errorf("invalid look-at occurrence")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return LookAtTargetProposal{}, fmt.Errorf("online look-at actor absent")
	}
	if actor.Body.Type != 0 || !validLookAtName(actor.Body.Name) {
		return LookAtTargetProposal{}, fmt.Errorf("online look-at actor absent")
	}
	if _, ok := s.Rooms[actor.Body.RoomID]; !ok {
		return LookAtTargetProposal{}, fmt.Errorf("look-at actor room absent")
	}
	if actorID == "" || targetName == "" || !utf8.ValidString(targetName) {
		return LookAtTargetProposal{}, fmt.Errorf("invalid look-at actor or target")
	}
	targetName = strings.TrimSpace(targetName)
	if targetName == "" || strings.IndexFunc(targetName, unicode.IsControl) >= 0 {
		return LookAtTargetProposal{}, fmt.Errorf("invalid look-at target")
	}
	// action.c performs this write before the silence guard.  In particular,
	// do not move it below target resolution: PHIDDN ordering is part of the
	// durable command contract for this bounded slice. Planning does not mutate
	// the input; selection uses a candidate body with the bit cleared.
	actor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	if flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return LookAtTargetProposal{ActorID: actorID, RoomID: actor.Body.RoomID, TargetName: targetName, Response: "한마디도 할수 없습니다!\r\n", ClearHidden: true}, nil
	}

	selectionState := s.clone()
	selectionState.Players[actorID] = actor
	target, err := selectionState.selectLookAtTarget(actorID, targetName, occurrence)
	if err != nil {
		return LookAtTargetProposal{}, err
	}
	_, ok, err = selectionState.RoomLookAtTargetEvent(actorID, target.Kind, target.ID)
	if err != nil {
		return LookAtTargetProposal{}, err
	}
	if !ok {
		return LookAtTargetProposal{}, fmt.Errorf("look-at target event unavailable")
	}
	response := lookAtActorResponse(actor.Body, target)
	return LookAtTargetProposal{
		ActorID:     actorID,
		RoomID:      actor.Body.RoomID,
		TargetID:    target.ID,
		TargetKind:  target.Kind,
		TargetName:  target.Name,
		Response:    response,
		Broadcast:   true,
		ClearHidden: true,
	}, nil
}

// PlanLookAtTarget retains the original reducer-shaped API for existing
// callers while making proposal/apply explicit for durable command handlers.
func (s State) PlanLookAtTarget(actorID, targetName string) (State, LookAtTargetResult, error) {
	proposal, err := s.PlanLookAtTargetProposal(actorID, targetName)
	if err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	return s.ApplyLookAtTarget(proposal)
}

// ApplyLookAtTarget atomically applies a proposal against the same canonical
// identities and visibility gates used during planning. It clears PHIDDN even
// for the silent no-op branch, but never applies a partial target event.
func (s State) ApplyLookAtTarget(proposal LookAtTargetProposal) (State, LookAtTargetResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	if proposal.ActorID == "" || proposal.Response == "" || !proposal.ClearHidden {
		return State{}, LookAtTargetResult{}, fmt.Errorf("invalid look-at proposal")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.RoomID != proposal.RoomID || !validLookAtName(actor.Body.Name) {
		return State{}, LookAtTargetResult{}, fmt.Errorf("look-at actor changed")
	}
	if proposal.Broadcast {
		if proposal.TargetID == "" || proposal.TargetKind == "" || proposal.TargetName == "" {
			return State{}, LookAtTargetResult{}, fmt.Errorf("invalid look-at target proposal")
		}
		if flag(actor.Body.Flags[:], playerSilentStateFlag) {
			return State{}, LookAtTargetResult{}, fmt.Errorf("look-at silence changed")
		}
		target, err := s.lookAtTargetByID(proposal.ActorID, proposal.TargetKind, proposal.TargetID)
		if err != nil || target.Name != proposal.TargetName {
			return State{}, LookAtTargetResult{}, fmt.Errorf("look-at target changed")
		}
		event, ok, err := s.RoomLookAtTargetEvent(proposal.ActorID, proposal.TargetKind, proposal.TargetID)
		if err != nil || !ok {
			return State{}, LookAtTargetResult{}, fmt.Errorf("look-at event changed")
		}
		if proposal.Response != lookAtActorResponse(actor.Body, target) || event.TargetName != proposal.TargetName {
			return State{}, LookAtTargetResult{}, fmt.Errorf("look-at projection changed")
		}
		next := s.clone()
		nextActor := next.Players[proposal.ActorID]
		nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, LookAtTargetResult{}, err
		}
		return next, LookAtTargetResult{Response: proposal.Response, Broadcast: true, TargetID: proposal.TargetID, TargetKind: proposal.TargetKind, TargetName: proposal.TargetName, Event: &event}, nil
	}
	if proposal.TargetID != "" || proposal.TargetKind != "" || proposal.Broadcast || proposal.Response != "한마디도 할수 없습니다!\r\n" || !flag(actor.Body.Flags[:], playerSilentStateFlag) {
		return State{}, LookAtTargetResult{}, fmt.Errorf("invalid look-at silent proposal")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, LookAtTargetResult{}, err
	}
	return next, LookAtTargetResult{Response: proposal.Response}, nil
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
	targetName := lookAtRenderedTargetName(actor.Body, target)
	targetRef := targetName + lookAtObjectParticle(targetName)
	event := LookAtTargetEvent{
		RoomID:         actor.Body.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Body.Name,
		TargetID:       target.ID,
		TargetKind:     target.Kind,
		TargetName:     target.Name,
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
	return s.lookAtTargetEventForNameOccurrence(actorID, targetName, 0)
}

// LookAtTargetEventForNameOccurrence re-resolves the same bounded target form
// after a committed receipt so transport can derive the room projection from
// canonical state without trusting a client-supplied ID. It is read-only and
// never consumes randomness.
func (s State) LookAtTargetEventForNameOccurrence(actorID, targetName string, occurrence int) (LookAtTargetEvent, bool, error) {
	return s.lookAtTargetEventForNameOccurrence(actorID, targetName, occurrence)
}

func (s State) lookAtTargetEventForNameOccurrence(actorID, targetName string, occurrence int) (LookAtTargetEvent, bool, error) {
	target, err := s.selectLookAtTarget(actorID, targetName, occurrence)
	if err != nil {
		return LookAtTargetEvent{}, false, err
	}
	return s.RoomLookAtTargetEvent(actorID, target.Kind, target.ID)
}

// selectLookAtTarget preserves find_crt's canonical traversal order: NPCs
// precede players in the room. With occurrence > 0 it applies C's EQUAL
// semantics (name or any key prefix) and one-based occurrence to that
// creature traversal. Canonical room object roots and exits are considered
// only for the exact form; no prefix/occurrence interpretation is inferred
// for those extensions.
func (s State) selectLookAtTarget(actorID, targetName string, occurrence int) (lookAtTarget, error) {
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
	if occurrence < 0 {
		return lookAtTarget{}, fmt.Errorf("invalid look-at occurrence")
	}
	if occurrence > 0 {
		return s.selectLookAtCreatureOccurrence(actorID, room, targetName, occurrence)
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
		return lookAtTarget{Kind: "npc", ID: id, Name: npc.Body.Name, Body: npc.Body}, nil
	}
	for _, id := range room.PlayerIDs {
		player, exists := s.Players[id]
		if !exists || id == "" || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("unresolved player look-at identity")
		}
		if id == actorID || !strings.EqualFold(player.Body.Name, targetName) || !lookAtVisible(actor.Body, player.Body) {
			continue
		}
		return lookAtTarget{Kind: "player", ID: id, Name: player.Body.Name, Body: player.Body}, nil
	}
	if object, ok, err := s.selectRoomObjectTarget(actor.Body, room, targetName); err != nil {
		return lookAtTarget{}, err
	} else if ok {
		return object, nil
	}
	if exit, ok, err := selectExactLookAtExit(actor.Body, room.Resource, targetName); err != nil {
		return lookAtTarget{}, err
	} else if ok {
		return exit, nil
	}
	return lookAtTarget{}, fmt.Errorf("look-at target absent")
}

func (s State) selectLookAtCreatureOccurrence(actorID string, room RoomState, targetName string, occurrence int) (lookAtTarget, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online {
		return lookAtTarget{}, fmt.Errorf("online look-at actor absent")
	}
	if targetName == "" || !utf8.ValidString(targetName) {
		return lookAtTarget{}, fmt.Errorf("invalid look-at target")
	}
	// C's find_crt scans first_mon before first_ply. The canonical ID slices
	// retain those source orders; map iteration is deliberately not involved.
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || id == "" || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("unresolved NPC look-at identity")
		}
		if id == actorID || !legacyCreaturePrefixMatch(npc.Body, targetName) || !lookAtVisible(actor.Body, npc.Body) {
			continue
		}
		occurrence--
		if occurrence == 0 {
			return lookAtTarget{Kind: "npc", ID: id, Name: npc.Body.Name, Body: npc.Body}, nil
		}
	}
	for _, id := range room.PlayerIDs {
		player, exists := s.Players[id]
		if !exists || id == "" || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID {
			return lookAtTarget{}, fmt.Errorf("unresolved player look-at identity")
		}
		if id == actorID || !legacyCreaturePrefixMatch(player.Body, targetName) || !lookAtVisible(actor.Body, player.Body) {
			continue
		}
		occurrence--
		if occurrence == 0 {
			return lookAtTarget{Kind: "player", ID: id, Name: player.Body.Name, Body: player.Body}, nil
		}
	}
	return lookAtTarget{}, fmt.Errorf("look-at target occurrence absent")
}

// legacyCreaturePrefixMatch is the Go equivalent of C's EQUAL macro for the
// occurrence form: one input prefix may match the display name or any of the
// three legacy keys. A candidate counts once even when multiple fields match.
func legacyCreaturePrefixMatch(body LegacyMonster, prefix string) bool {
	if prefix == "" || !utf8.ValidString(prefix) {
		return false
	}
	if equalFoldPrefix(body.Name, prefix) {
		return true
	}
	for _, key := range body.Keys {
		if key != "" && equalFoldPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func equalFoldPrefix(value, prefix string) bool {
	if value == "" || !utf8.ValidString(value) || !utf8.ValidString(prefix) {
		return false
	}
	valueRunes, prefixRunes := []rune(value), []rune(prefix)
	if len(prefixRunes) > len(valueRunes) {
		return false
	}
	return strings.EqualFold(string(valueRunes[:len(prefixRunes)]), prefix)
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
		return lookAtTarget{Kind: targetKind, ID: targetID, Name: player.Body.Name, Body: player.Body}, nil
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
		return lookAtTarget{Kind: targetKind, ID: targetID, Name: npc.Body.Name, Body: npc.Body}, nil
	case "object":
		if room.Items == nil || !containsString(room.Items.Inventory, targetID) {
			return lookAtTarget{}, fmt.Errorf("look-at object target is not a canonical room root")
		}
		item, exists := room.Items.Items[targetID]
		if !exists || item.Object.Name == "" || !validLookAtName(item.Object.Name) {
			return lookAtTarget{}, fmt.Errorf("look-at object target absent")
		}
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !flag(actor.Body.Flags[:], playerDetectInvisibleFlag) {
			return lookAtTarget{}, fmt.Errorf("look-at object target is not visible")
		}
		return lookAtTarget{Kind: targetKind, ID: targetID, Name: item.Object.Name, Description: item.Object.Description}, nil
	case "exit":
		index, ok := parseLookAtExitID(targetID, room.Resource.ID)
		if !ok || index < 0 || index >= len(room.Resource.Exits) {
			return lookAtTarget{}, fmt.Errorf("look-at exit target is not in actor room")
		}
		exit := room.Resource.Exits[index]
		if !validLookAtName(exit.Name) || exit.Name == "" {
			return lookAtTarget{}, fmt.Errorf("look-at exit target absent")
		}
		if flag(exit.Flags[:], 19) || (flag(exit.Flags[:], 1) && !flag(actor.Body.Flags[:], playerDetectInvisibleFlag)) {
			return lookAtTarget{}, fmt.Errorf("look-at exit target is not visible")
		}
		return lookAtTarget{Kind: targetKind, ID: targetID, Name: exit.Name}, nil
	default:
		return lookAtTarget{}, fmt.Errorf("unsupported look-at target kind %q", targetKind)
	}
}

func parseLookAtExitID(id string, roomID int16) (int, bool) {
	return parseSearchExitID(id, roomID)
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

func lookAtRenderedTargetName(actor LegacyMonster, target lookAtTarget) string {
	if target.Kind == "player" {
		if flag(target.Body.Flags[:], playerDMInvisibleFlag) || (flag(target.Body.Flags[:], playerInvisibleFlag) && !flag(actor.Flags[:], playerDetectFlag)) {
			return "누군가"
		}
		name := target.Body.Name
		if flag(target.Body.Flags[:], playerInvisibleFlag) {
			name += "(*)"
		}
		return name + "님"
	}
	return target.Name
}

func lookAtActorResponse(actor LegacyMonster, target lookAtTarget) string {
	rendered := lookAtRenderedTargetName(actor, target)
	return fmt.Sprintf("당신은 %s%s 봅니다.\r\n", rendered, lookAtObjectParticle(rendered))
}

func lookAtObjectParticle(rendered string) string {
	if hasFinalHangul(rendered) {
		return "을"
	}
	return "를"
}
