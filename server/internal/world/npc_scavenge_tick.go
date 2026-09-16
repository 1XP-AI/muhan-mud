package world

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync/atomic"
)

// NPCScavengeTickAction is the per-NPC projection retained in a scavenging
// phase receipt. The one-NPC reducer owns the actual MSCAVE decision; this
// alias keeps the phase projection source-shaped without introducing a second
// transfer model.
type NPCScavengeTickAction = NPCScavengeResult

// NPCScavengeEvent is the post-commit projection of update.c:335-337. C uses
// broadcast_rom(-1, ...), so ExcludeActorID is intentionally empty and every
// online player in the committed room is a fan-out candidate. Item/NPC IDs,
// rather than display names, are the transport identity fence.
type NPCScavengeEvent struct {
	RoomID         int16  `json:"room_id"`
	NPCID          string `json:"npc_id"`
	NPCName        string `json:"npc_name"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	ExcludeActorID string `json:"exclude_actor_id,omitempty"`
	Text           string `json:"text"`
}

// NPCScavengeTickSummary is the durable projection of one ordered active-list
// pass. Actions contain only active MSCAVE NPCs in their original active-list
// order; ActiveNPCIDs records the complete source order, including NPCs that
// are not scavengers or are in an unoccupied room.
type NPCScavengeTickSummary struct {
	Now          int32                   `json:"now"`
	Actions      []NPCScavengeTickAction `json:"actions,omitempty"`
	Events       []NPCScavengeEvent      `json:"events,omitempty"`
	ActiveNPCIDs []string                `json:"active_npc_ids"`
	Changed      bool                    `json:"changed"`
	NoOp         bool                    `json:"no_op,omitempty"`
}

// NPCScavengeTickResult is a descriptive name for the committed phase
// response. Keeping it as an alias avoids two result projections drifting.
type NPCScavengeTickResult = NPCScavengeTickSummary

// NPCScavengeSummary is retained as a short descriptive alias for transport
// callers that name the response after its summary role.
type NPCScavengeSummary = NPCScavengeTickSummary

// NPCScavengeTickProposal composes the already-authoritative one-NPC
// PlanNPCScavenge/ApplyNPCScavenge reducer. Actions are child proposals in
// exact active-list order. before and applyToken are private so a serialized
// result or a caller-created partial proposal cannot bypass the snapshot and
// one-shot apply fences.
type NPCScavengeTickProposal struct {
	Now          int32
	Actions      []NPCScavengeProposal
	ActiveNPCIDs []string

	before     State
	plannedNow int32
	applyToken *npcScavengeTickApplyToken
}

// NPCScavengePhaseProposal is a descriptive alias for callers that call this
// bounded operation a phase rather than a tick.
type NPCScavengePhaseProposal = NPCScavengeTickProposal

type npcScavengeTickApplyToken struct {
	used uint32
}

func cloneNPCScavengeTickState(s State) (State, error) {
	// State.clone is intentionally lightweight but collapses known-empty
	// slices to nil. The MSCAVE receipt carries canonical order, so use the
	// strict JSON round trip here to preserve nil-vs-empty ownership markers in
	// the private snapshot fence without changing the shared clone helper.
	raw, err := json.Marshal(s)
	if err != nil {
		return State{}, err
	}
	return DecodeState(raw)
}

func (t *npcScavengeTickApplyToken) valid() bool {
	return t != nil && atomic.LoadUint32(&t.used) == 0
}

func (t *npcScavengeTickApplyToken) consume() bool {
	return t != nil && atomic.CompareAndSwapUint32(&t.used, 0, 1)
}

// NPCScavengeRoomText preserves the update.c format string used by the
// successful MSCAVE broadcast. It deliberately has no CRLF suffix because
// the legacy broadcast formatter adds the room-line framing at its boundary,
// matching the other NPC tick projections.
func NPCScavengeRoomText(npcName, itemName string) string {
	return fmt.Sprintf("\n%s%s %s%s 줍습니다.", npcName, legacySubjectParticle(npcName), itemName, valueObjectParticle(itemName))
}

// npcScavengeTickEligibleIDs validates the complete source active order and
// returns only the MSCAVE NPCs for which the one-NPC reducer can run. C
// removes an NPC from active traversal before it reaches MSCAVE when its room
// is empty; this unintegrated phase records the order and leaves that removal
// to the maintenance/scheduler slice. Occupied active NPCs with unresolved
// enemy relations fail closed before any RNG is consumed, even when they are
// not scavengers, because the same update_active relation graph is shared by
// the following AI branches.
func npcScavengeTickEligibleIDs(s State) ([]string, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.NPCs == nil {
		return nil, fmt.Errorf("NPC MSCAVE tick requires canonical NPC state")
	}
	if s.ActiveNPCIDs == nil {
		return nil, fmt.Errorf("NPC MSCAVE tick active order unresolved")
	}
	eligible := make([]string, 0, len(s.ActiveNPCIDs))
	for _, npcID := range s.ActiveNPCIDs {
		npc, ok := s.NPCs[npcID]
		if !ok || npcID == "" || npc.Body.Type != 1 {
			return nil, fmt.Errorf("NPC MSCAVE active identity unresolved for %q", npcID)
		}
		room, ok := s.Rooms[npc.Body.RoomID]
		if !ok || !containsString(room.NPCIDs, npcID) {
			return nil, fmt.Errorf("NPC MSCAVE active room identity unresolved for %q", npcID)
		}
		if len(room.PlayerIDs) == 0 {
			continue
		}
		if npc.Enemies == nil {
			return nil, fmt.Errorf("NPC MSCAVE active enemy relations unresolved for %q", npcID)
		}
		if flag(npc.Body.Flags[:], npcScavengeFlag) {
			eligible = append(eligible, npcID)
		}
	}
	return eligible, nil
}

func cloneNPCScavengeProposalForTick(proposal NPCScavengeProposal) NPCScavengeProposal {
	// Planning the next active NPC needs a shadow Apply. Give that shadow a
	// fresh token so the child proposal retained by the durable parent remains
	// unused and can be applied atomically later.
	proposal.applyToken = &npcScavengeApplyToken{npcID: proposal.NPCID}
	return proposal
}

func cloneNPCScavengeIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	cloned := make([]string, len(ids))
	copy(cloned, ids)
	return cloned
}

func normalizeNPCScavengeTickChild(proposal NPCScavengeProposal) NPCScavengeProposal {
	// The merged one-NPC reducer records an empty protected-root traversal as
	// nil, while its Apply-side canonical walk returns a known empty slice.
	// Normalize that equivalent source result at this adapter boundary so a
	// floor whose first root is immediately eligible remains composable. A
	// non-due/attack-blocked child must keep nil: it did not inspect roots.
	if proposal.Attempted && proposal.ProtectedRootIDs == nil {
		proposal.ProtectedRootIDs = []string{}
	}
	// The same reducer copies canonical empty order slices through append(nil,
	// values...), which loses the known-empty distinction. Bind the child order
	// fields to the exact planning snapshot here; Apply then compares the
	// source order without inventing or dropping an empty-list marker.
	if room, ok := proposal.before.Rooms[proposal.RoomID]; ok && room.Items != nil {
		proposal.SourceFloorOrder = cloneNPCScavengeIDs(room.Items.Inventory)
		if !proposal.Transferred {
			proposal.AfterRoomFloorOrder = cloneNPCScavengeIDs(room.Items.Inventory)
		}
	}
	if npc, ok := proposal.before.NPCs[proposal.NPCID]; ok && npc.Items != nil && !proposal.Transferred {
		proposal.AfterNPCInventoryOrder = cloneNPCScavengeIDs(npc.Items.Inventory)
	}
	return proposal
}

func preserveNPCScavengeTickNoTransferOrders(before, after State, result NPCScavengeResult) State {
	if result.Transferred {
		return after
	}
	roomBefore, roomBeforeOK := before.Rooms[result.RoomID]
	roomAfter, roomAfterOK := after.Rooms[result.RoomID]
	if roomBeforeOK && roomAfterOK && roomBefore.Items != nil && roomAfter.Items != nil && roomBefore.Items.Inventory != nil && len(roomBefore.Items.Inventory) == 0 {
		roomAfter.Items.Inventory = []string{}
		after.Rooms[result.RoomID] = roomAfter
	}
	npcBefore, npcBeforeOK := before.NPCs[result.NPCID]
	npcAfter, npcAfterOK := after.NPCs[result.NPCID]
	if npcBeforeOK && npcAfterOK && npcBefore.Items != nil && npcAfter.Items != nil && npcBefore.Items.Inventory != nil && len(npcBefore.Items.Inventory) == 0 {
		npcAfter.Items.Inventory = []string{}
		after.NPCs[result.NPCID] = npcAfter
	}
	return after
}

func validateNPCScavengeTickChildOrders(state State, proposal NPCScavengeProposal) error {
	room, ok := state.Rooms[proposal.RoomID]
	if !ok || room.Items == nil {
		return fmt.Errorf("NPC MSCAVE child room order unavailable")
	}
	if !reflect.DeepEqual(proposal.SourceFloorOrder, room.Items.Inventory) {
		return fmt.Errorf("NPC MSCAVE child source order mismatch")
	}
	npc, ok := state.NPCs[proposal.NPCID]
	if !ok || npc.Items == nil {
		return fmt.Errorf("NPC MSCAVE child NPC order unavailable")
	}
	if !proposal.Transferred {
		if !reflect.DeepEqual(proposal.AfterRoomFloorOrder, room.Items.Inventory) || !reflect.DeepEqual(proposal.AfterNPCInventoryOrder, npc.Items.Inventory) {
			return fmt.Errorf("NPC MSCAVE child result order mismatch")
		}
		return nil
	}
	if proposal.SelectedItemID == "" {
		return fmt.Errorf("NPC MSCAVE child selected root unavailable")
	}
	transfer, err := TransferItemRoots(*room.Items, *npc.Items, []string{proposal.SelectedItemID})
	if err != nil {
		return fmt.Errorf("NPC MSCAVE child transfer order: %w", err)
	}
	if !reflect.DeepEqual(proposal.AfterRoomFloorOrder, transfer.Source.Inventory) || !reflect.DeepEqual(proposal.AfterNPCInventoryOrder, transfer.Destination.Inventory) {
		return fmt.Errorf("NPC MSCAVE child transfer order mismatch")
	}
	return nil
}

// PlanNPCScavengeTick plans one source-ordered MSCAVE pass. Every eligible
// active NPC is planned exactly once; PlanNPCScavenge itself decides whether
// its LT_MSCAV gate is due and therefore whether the injected RNG is called.
// Shadow application lets a later NPC observe an earlier successful transfer
// without changing the authoritative input state or consuming its durable
// child token.
func (s State) PlanNPCScavengeTick(now int32, roll func(int, int) int) (NPCScavengeTickProposal, error) {
	if now < 0 {
		return NPCScavengeTickProposal{}, fmt.Errorf("NPC MSCAVE tick invalid timestamp")
	}
	eligible, err := npcScavengeTickEligibleIDs(s)
	if err != nil {
		return NPCScavengeTickProposal{}, err
	}
	// Resolve every canonical room/item context before the first roll. A
	// malformed later NPC must not allow an earlier NPC to consume randomness
	// before the whole command is known to be admissible.
	for _, npcID := range eligible {
		if _, _, _, _, err := npcScavengeContext(s, npcID, now); err != nil {
			return NPCScavengeTickProposal{}, err
		}
	}
	before, err := cloneNPCScavengeTickState(s)
	if err != nil {
		return NPCScavengeTickProposal{}, fmt.Errorf("NPC MSCAVE tick snapshot: %w", err)
	}
	proposal := NPCScavengeTickProposal{
		Now:          now,
		ActiveNPCIDs: cloneNPCScavengeIDs(s.ActiveNPCIDs),
		before:       before,
		plannedNow:   now,
		applyToken:   &npcScavengeTickApplyToken{},
	}
	staged := s
	for _, npcID := range eligible {
		child, err := staged.PlanNPCScavenge(npcID, now, roll)
		if err != nil {
			return NPCScavengeTickProposal{}, err
		}
		childBefore, err := cloneNPCScavengeTickState(staged)
		if err != nil {
			return NPCScavengeTickProposal{}, fmt.Errorf("NPC MSCAVE tick child snapshot %q: %w", npcID, err)
		}
		child.before = childBefore
		child = normalizeNPCScavengeTickChild(child)
		proposal.Actions = append(proposal.Actions, child)
		// ApplyNPCScavenge's shared State.clone intentionally normalizes some
		// empty slices to nil. Run the shadow against that exact reducer input;
		// the retained child still carries the strict canonical snapshot above.
		reducerState := staged.clone()
		reducerChild := child
		reducerChild.before = reducerState
		reducerChild = normalizeNPCScavengeTickChild(reducerChild)
		shadow := cloneNPCScavengeProposalForTick(reducerChild)
		candidate, shadowResult, err := reducerState.ApplyNPCScavenge(shadow)
		if err != nil {
			return NPCScavengeTickProposal{}, fmt.Errorf("NPC MSCAVE tick shadow apply %q: %w", npcID, err)
		}
		staged = preserveNPCScavengeTickNoTransferOrders(staged, candidate, shadowResult)
	}
	return proposal, nil
}

// PlanNPCScavengePhase is a descriptive alias for the unintegrated phase
// seam. It does not add scheduler behavior.
func (s State) PlanNPCScavengePhase(now int32, roll func(int, int) int) (NPCScavengeTickProposal, error) {
	return s.PlanNPCScavengeTick(now, roll)
}

func npcScavengeTickEvent(after State, action NPCScavengeResult) (NPCScavengeEvent, error) {
	if !action.Transferred || action.SelectedItemID == "" {
		return NPCScavengeEvent{}, nil
	}
	npc, ok := after.NPCs[action.NPCID]
	if !ok || npc.Body.Type != 1 || npc.Items == nil {
		return NPCScavengeEvent{}, fmt.Errorf("NPC MSCAVE event NPC identity unresolved")
	}
	item, ok := npc.Items.Items[action.SelectedItemID]
	if !ok || !containsString(npc.Items.Inventory, action.SelectedItemID) {
		return NPCScavengeEvent{}, fmt.Errorf("NPC MSCAVE event item identity unresolved")
	}
	return NPCScavengeEvent{
		RoomID: action.RoomID, NPCID: action.NPCID, NPCName: npc.Body.Name,
		ItemID: action.SelectedItemID, ItemName: item.Object.Name,
		Text: NPCScavengeRoomText(npc.Body.Name, item.Object.Name),
	}, nil
}

// ApplyNPCScavengeTick applies every child proposal against one cloned
// candidate. Child Apply performs the reducer's identity/order/timer checks;
// this wrapper validates all children with shadow tokens first, then consumes
// the real parent/child tokens only after the complete pass and event
// projection are known to be valid. Thus a stale, tampered, or partial pass
// cannot publish a partial room/NPC transfer.
func (s State) ApplyNPCScavengeTick(proposal NPCScavengeTickProposal) (State, NPCScavengeTickSummary, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCScavengeTickSummary{}, err
	}
	if proposal.before.Version == 0 || proposal.Now < 0 || proposal.Now != proposal.plannedNow || !reflect.DeepEqual(s, proposal.before) {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or invalid NPC MSCAVE tick proposal")
	}
	if !proposal.applyToken.valid() || !reflect.DeepEqual(proposal.ActiveNPCIDs, s.ActiveNPCIDs) {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or tampered NPC MSCAVE tick token/order")
	}
	eligible, err := npcScavengeTickEligibleIDs(s)
	if err != nil {
		return State{}, NPCScavengeTickSummary{}, err
	}
	if len(proposal.Actions) != len(eligible) {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("NPC MSCAVE tick action count mismatch")
	}

	staged := s
	results := make([]NPCScavengeResult, 0, len(proposal.Actions))
	for i, child := range proposal.Actions {
		if child.NPCID != eligible[i] {
			return State{}, NPCScavengeTickSummary{}, fmt.Errorf("NPC MSCAVE tick active order mismatch")
		}
		canonicalBefore, cloneErr := cloneNPCScavengeTickState(staged)
		if cloneErr != nil || !reflect.DeepEqual(child.before, canonicalBefore) {
			return State{}, NPCScavengeTickSummary{}, fmt.Errorf("NPC MSCAVE tick child snapshot mismatch")
		}
		if orderErr := validateNPCScavengeTickChildOrders(staged, child); orderErr != nil {
			return State{}, NPCScavengeTickSummary{}, orderErr
		}
		// Use the reducer's own clone shape for the child Apply, then restore
		// known-empty non-transfer order markers on the candidate.
		reducerState := staged.clone()
		reducerChild := child
		reducerChild.before = reducerState
		reducerChild = normalizeNPCScavengeTickChild(reducerChild)
		shadow := cloneNPCScavengeProposalForTick(reducerChild)
		candidate, result, applyErr := reducerState.ApplyNPCScavenge(shadow)
		if applyErr != nil {
			return State{}, NPCScavengeTickSummary{}, fmt.Errorf("NPC MSCAVE tick action %q: %w", child.NPCID, applyErr)
		}
		staged = preserveNPCScavengeTickNoTransferOrders(staged, candidate, result)
		results = append(results, result)
	}
	if err := staged.Validate(); err != nil {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("NPC MSCAVE tick result invalid: %w", err)
	}

	summary := NPCScavengeTickSummary{
		Now:          proposal.Now,
		Actions:      append([]NPCScavengeTickAction(nil), results...),
		ActiveNPCIDs: cloneNPCScavengeIDs(staged.ActiveNPCIDs),
	}
	for _, result := range results {
		// A due attempt writes LT_MSCAV even when the source timestamp is
		// already equal to now (for example the canonical zero-time marker at
		// time zero). Preserve that write in the summary rather than deriving
		// it only from value inequality.
		if (result.Due && result.Attempted) || result.TimerBefore != result.TimerAfter || result.Transferred {
			summary.Changed = true
		}
		if result.Transferred {
			event, eventErr := npcScavengeTickEvent(staged, result)
			if eventErr != nil {
				return State{}, NPCScavengeTickSummary{}, eventErr
			}
			summary.Events = append(summary.Events, event)
		}
	}
	summary.NoOp = !summary.Changed

	// Check every original token before consuming any of them. A caller that
	// tried to apply a child independently, or raced another parent apply, is
	// rejected without mutating the candidate state.
	if !proposal.applyToken.valid() {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or replayed NPC MSCAVE tick proposal")
	}
	for _, child := range proposal.Actions {
		if !child.applyToken.validFor(child.NPCID) {
			return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or replayed NPC MSCAVE child proposal")
		}
	}
	if !proposal.applyToken.consume() {
		return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or replayed NPC MSCAVE tick proposal")
	}
	for _, child := range proposal.Actions {
		if !child.applyToken.consume() {
			return State{}, NPCScavengeTickSummary{}, fmt.Errorf("stale or replayed NPC MSCAVE child proposal")
		}
	}
	return staged, summary, nil
}

// ApplyNPCScavengePhase is a descriptive alias for the unintegrated phase
// seam. It does not add scheduler behavior.
func (s State) ApplyNPCScavengePhase(proposal NPCScavengeTickProposal) (State, NPCScavengeTickSummary, error) {
	return s.ApplyNPCScavengeTick(proposal)
}
