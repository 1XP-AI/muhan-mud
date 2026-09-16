package world

import (
	"fmt"
	"reflect"
	"sync/atomic"
)

// These are the raw update_active values from src/mtype.h.  LT_MSCAV shares
// the timer slot with LT_TRACK, as it does in the legacy body.
const (
	npcScavengeAttackTimer = 3  // LT_ATTCK
	npcScavengeTimer       = 4  // LT_MSCAV
	npcScavengeFlag        = 11 // MSCAVE
	npcScavengedFlag       = 18 // MHASSC

	npcScavengePermanentFlag          = 0  // OPERMT
	npcScavengeHiddenFlag             = 1  // OHIDDN
	npcScavengePermanentInventoryFlag = 9  // OPERM2
	npcScavengeNotTakeFlag            = 17 // ONOTAK
	npcScavengeSceneryFlag            = 18 // OSCENE

	npcScavengeRollHigh = 100
	npcScavengeRollWin  = 15
	npcScavengeWait     = 20
)

// npcScavengeApplyToken binds a proposal to the NPC selected during planning
// and makes even a semantically unchanged no-op apply one-shot. It is kept out
// of the wire/result projection: a proposal reconstructed without this token
// must fail closed rather than become replayable.
type npcScavengeApplyToken struct {
	npcID string
	used  uint32
}

func (t *npcScavengeApplyToken) validFor(npcID string) bool {
	return t != nil && t.npcID != "" && t.npcID == npcID && atomic.LoadUint32(&t.used) == 0
}

func (t *npcScavengeApplyToken) consume() bool {
	return t != nil && atomic.CompareAndSwapUint32(&t.used, 0, 1)
}

// NPCScavengeDecision is the source-bound outcome of one active NPC pass.
type NPCScavengeDecision string

const (
	NPCScavengeNotDue         NPCScavengeDecision = "not_due"
	NPCScavengeAttackNotReady NPCScavengeDecision = "attack_not_ready"
	NPCScavengeRollFailed     NPCScavengeDecision = "roll_failed"
	NPCScavengeNoEligibleItem NPCScavengeDecision = "no_eligible_item"
	NPCScavengeTransferred    NPCScavengeDecision = "transferred"
)

// NPCScavengeProposal is a complete-snapshot candidate for the scavenging
// part of update_active. Apply never calls RNG; Roll and Decision are the
// durable replay inputs for a due attempt.
type NPCScavengeProposal struct {
	NPCID  string `json:"npc_id"`
	RoomID int16  `json:"room_id"`
	Now    int32  `json:"now"`
	Due    bool   `json:"due"`
	// AttackReady is the preceding update_active gate. The attack timer is
	// owned by the maintenance reducer and is not rewritten by this slice.
	AttackReady bool `json:"attack_ready"`
	Attempted   bool `json:"attempted"`
	Roll        int  `json:"roll"`
	RollSuccess bool `json:"roll_success"`

	// EligibleItemID is the first unprotected floor root. SelectedItemID is
	// non-empty only when the <=15 roll transfers that root.
	EligibleItemID   string              `json:"eligible_item_id,omitempty"`
	SelectedItemID   string              `json:"selected_item_id,omitempty"`
	ProtectedRootIDs []string            `json:"protected_root_ids,omitempty"`
	Decision         NPCScavengeDecision `json:"decision"`
	TimerBefore      LegacyTimer         `json:"timer_before"`
	TimerAfter       LegacyTimer         `json:"timer_after"`
	Transferred      bool                `json:"transferred"`
	MHASSCChanged    bool                `json:"mhassc_changed"`
	MHASSC           bool                `json:"mhassc"`

	// These slices make canonical order part of the receipt instead of
	// allowing Apply to reconstruct it from a map.
	SourceFloorOrder       []string `json:"source_floor_order"`
	AfterRoomFloorOrder    []string `json:"after_room_floor_order"`
	AfterNPCInventoryOrder []string `json:"after_npc_inventory_order"`

	before     State
	applyToken *npcScavengeApplyToken
}

// NPCScavengeResult is the committed, replay-visible projection.
type NPCScavengeResult struct {
	NPCID                  string              `json:"npc_id"`
	RoomID                 int16               `json:"room_id"`
	Now                    int32               `json:"now"`
	Due                    bool                `json:"due"`
	AttackReady            bool                `json:"attack_ready"`
	Attempted              bool                `json:"attempted"`
	Roll                   int                 `json:"roll"`
	RollSuccess            bool                `json:"roll_success"`
	EligibleItemID         string              `json:"eligible_item_id,omitempty"`
	SelectedItemID         string              `json:"selected_item_id,omitempty"`
	ProtectedRootIDs       []string            `json:"protected_root_ids,omitempty"`
	Decision               NPCScavengeDecision `json:"decision"`
	TimerBefore            LegacyTimer         `json:"timer_before"`
	TimerAfter             LegacyTimer         `json:"timer_after"`
	Transferred            bool                `json:"transferred"`
	MHASSCChanged          bool                `json:"mhassc_changed"`
	MHASSC                 bool                `json:"mhassc"`
	SourceFloorOrder       []string            `json:"source_floor_order"`
	AfterRoomFloorOrder    []string            `json:"after_room_floor_order"`
	AfterNPCInventoryOrder []string            `json:"after_npc_inventory_order"`
}

func npcScavengeResult(p NPCScavengeProposal) NPCScavengeResult {
	return NPCScavengeResult{
		NPCID: p.NPCID, RoomID: p.RoomID, Now: p.Now, Due: p.Due,
		AttackReady: p.AttackReady, Attempted: p.Attempted, Roll: p.Roll,
		RollSuccess: p.RollSuccess, EligibleItemID: p.EligibleItemID,
		SelectedItemID:   p.SelectedItemID,
		ProtectedRootIDs: append([]string(nil), p.ProtectedRootIDs...),
		Decision:         p.Decision, TimerBefore: p.TimerBefore, TimerAfter: p.TimerAfter,
		Transferred: p.Transferred, MHASSCChanged: p.MHASSCChanged, MHASSC: p.MHASSC,
		SourceFloorOrder:       append([]string(nil), p.SourceFloorOrder...),
		AfterRoomFloorOrder:    append([]string(nil), p.AfterRoomFloorOrder...),
		AfterNPCInventoryOrder: append([]string(nil), p.AfterNPCInventoryOrder...),
	}
}

func npcScavengeDue(timer LegacyTimer, now int32) bool {
	// C uses t-i > 20. A zero LT slot is the canonical first-attempt marker.
	return timer.LastTime == 0 || int64(now)-int64(timer.LastTime) > npcScavengeWait
}

func npcScavengeAttackIsReady(timer LegacyTimer, now int32) bool {
	return int64(timer.LastTime)+int64(timer.Interval) <= int64(now)
}

func npcScavengeProtected(object LegacyObject) bool {
	return flag(object.Flags[:], npcScavengePermanentFlag) ||
		flag(object.Flags[:], npcScavengeHiddenFlag) ||
		flag(object.Flags[:], npcScavengePermanentInventoryFlag) ||
		flag(object.Flags[:], npcScavengeNotTakeFlag) ||
		flag(object.Flags[:], npcScavengeSceneryFlag)
}

// npcScavengeRoots returns the first eligible canonical floor root and records
// protected roots before it. Descendants are never considered independently.
func npcScavengeRoots(items ItemCollection) (string, []string, error) {
	if err := items.Validate(); err != nil {
		return "", nil, fmt.Errorf("NPC MSCAVE floor items: %w", err)
	}
	for _, id := range items.Inventory {
		item, ok := items.Items[id]
		if !ok || id == "" {
			return "", nil, fmt.Errorf("NPC MSCAVE floor root absent")
		}
		if npcScavengeProtected(item.Object) {
			continue
		}
		protected := make([]string, 0)
		for _, prior := range items.Inventory {
			if prior == id {
				break
			}
			priorItem, ok := items.Items[prior]
			if !ok {
				return "", nil, fmt.Errorf("NPC MSCAVE protected floor root absent")
			}
			if npcScavengeProtected(priorItem.Object) {
				protected = append(protected, prior)
			}
		}
		return id, protected, nil
	}
	protected := make([]string, 0, len(items.Inventory))
	for _, id := range items.Inventory {
		item, ok := items.Items[id]
		if !ok {
			return "", nil, fmt.Errorf("NPC MSCAVE protected floor root absent")
		}
		if npcScavengeProtected(item.Object) {
			protected = append(protected, id)
		}
	}
	return "", protected, nil
}

func npcScavengeContext(s State, npcID string, now int32) (NPCState, RoomState, bool, bool, error) {
	if err := s.Validate(); err != nil {
		return NPCState{}, RoomState{}, false, false, err
	}
	if now < 0 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE invalid timestamp")
	}
	if npcID == "" || s.NPCs == nil {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE identity unresolved")
	}
	npc, ok := s.NPCs[npcID]
	if !ok || npc.Body.Type != 1 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE identity unresolved")
	}
	room, ok := s.Rooms[npc.Body.RoomID]
	if !ok || room.Resource.ID != npc.Body.RoomID || len(room.PlayerIDs) == 0 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE occupied room unresolved")
	}
	if !containsString(room.NPCIDs, npcID) || s.ActiveNPCIDs == nil || !containsString(s.ActiveNPCIDs, npcID) {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE active identity unresolved")
	}
	if !flag(npc.Body.Flags[:], npcScavengeFlag) {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC is not MSCAVE")
	}
	if room.Items == nil || len(room.Resource.Objects) != 0 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE canonical room items required")
	}
	if npc.Items == nil || len(npc.Body.Inventory) != 0 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE canonical NPC items required")
	}
	if err := npc.Items.Validate(); err != nil {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE NPC items: %w", err)
	}
	for slot, id := range npc.Items.Ready {
		if id != "" {
			// A ready item is a held/equipped root, never a floor candidate. It
			// may remain in the NPC collection while a floor root is added.
			if _, ok := npc.Items.Items[id]; !ok {
				return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE ready item %d unresolved", slot)
			}
		}
	}
	for i, timer := range npc.Body.Timers {
		if timer.LastTime < 0 || timer.Interval < 0 {
			return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE invalid timer %d", i)
		}
	}
	if npc.Body.Timers[npcScavengeTimer].LastTime < 0 || npc.Body.Timers[npcScavengeAttackTimer].LastTime < 0 {
		return NPCState{}, RoomState{}, false, false, fmt.Errorf("NPC MSCAVE invalid timestamp")
	}
	return npc, room, npcScavengeDue(npc.Body.Timers[npcScavengeTimer], now), npcScavengeAttackIsReady(npc.Body.Timers[npcScavengeAttackTimer], now), nil
}

func npcScavengeSetOrders(p *NPCScavengeProposal, room RoomState, npc NPCState) {
	p.SourceFloorOrder = append([]string(nil), room.Items.Inventory...)
	p.AfterRoomFloorOrder = append([]string(nil), room.Items.Inventory...)
	p.AfterNPCInventoryOrder = append([]string(nil), npc.Items.Inventory...)
}

// PlanNPCScavenge plans one active MSCAVE attempt in an occupied canonical
// room. The injected roll is called exactly once only after the LT_MSCAV and
// LT_ATTCK gates pass; no global clock, RNG, or mutable state is consulted.
func (s State) PlanNPCScavenge(npcID string, now int32, roll func(int, int) int) (NPCScavengeProposal, error) {
	npc, room, due, attackReady, err := npcScavengeContext(s, npcID, now)
	if err != nil {
		return NPCScavengeProposal{}, err
	}
	proposal := NPCScavengeProposal{
		NPCID: npcID, RoomID: npc.Body.RoomID, Now: now,
		Due: due, AttackReady: attackReady, TimerBefore: npc.Body.Timers[npcScavengeTimer],
		TimerAfter: npc.Body.Timers[npcScavengeTimer], MHASSC: flag(npc.Body.Flags[:], npcScavengedFlag),
		before: s.clone(), applyToken: &npcScavengeApplyToken{npcID: npcID},
	}
	npcScavengeSetOrders(&proposal, room, npc)
	if !due {
		proposal.Decision = NPCScavengeNotDue
		return proposal, nil
	}
	if !attackReady {
		proposal.Decision = NPCScavengeAttackNotReady
		return proposal, nil
	}
	if roll == nil {
		return NPCScavengeProposal{}, fmt.Errorf("NPC MSCAVE requires RNG")
	}
	value, err := randomIn(roll, 1, npcScavengeRollHigh)
	if err != nil {
		return NPCScavengeProposal{}, err
	}
	proposal.Attempted = true
	proposal.Roll = value
	proposal.RollSuccess = value <= npcScavengeRollWin
	eligible, protected, err := npcScavengeRoots(*room.Items)
	if err != nil {
		return NPCScavengeProposal{}, err
	}
	proposal.EligibleItemID = eligible
	proposal.ProtectedRootIDs = append([]string(nil), protected...)
	proposal.TimerAfter.LastTime = now
	proposal.AfterRoomFloorOrder = append([]string(nil), room.Items.Inventory...)
	proposal.AfterNPCInventoryOrder = append([]string(nil), npc.Items.Inventory...)
	if !proposal.RollSuccess {
		proposal.Decision = NPCScavengeRollFailed
	} else if eligible == "" {
		proposal.Decision = NPCScavengeNoEligibleItem
	} else {
		proposal.Decision = NPCScavengeTransferred
		proposal.SelectedItemID = eligible
		proposal.Transferred = true
		transfer, transferErr := TransferItemRoots(*room.Items, *npc.Items, []string{eligible})
		if transferErr != nil {
			return NPCScavengeProposal{}, fmt.Errorf("NPC MSCAVE transfer: %w", transferErr)
		}
		proposal.AfterRoomFloorOrder = append([]string(nil), transfer.Source.Inventory...)
		proposal.AfterNPCInventoryOrder = append([]string(nil), transfer.Destination.Inventory...)
		proposal.MHASSCChanged = !proposal.MHASSC
		proposal.MHASSC = true
	}
	return proposal, nil
}

func npcScavengeProposalError(message string) (State, NPCScavengeResult, error) {
	return State{}, NPCScavengeResult{}, fmt.Errorf("NPC MSCAVE %s", message)
}

// ApplyNPCScavenge commits a proposal only against the exact planning
// snapshot. It rechecks every derived decision and canonical order without
// invoking RNG, then clones and mutates the two owning collections together.
func (s State) ApplyNPCScavenge(proposal NPCScavengeProposal) (State, NPCScavengeResult, error) {
	if proposal.before.Version == 0 || !proposal.applyToken.validFor(proposal.NPCID) || !reflect.DeepEqual(s, proposal.before) {
		return npcScavengeProposalError("stale or replayed proposal")
	}
	npc, room, due, attackReady, err := npcScavengeContext(s, proposal.NPCID, proposal.Now)
	if err != nil {
		return State{}, NPCScavengeResult{}, err
	}
	if proposal.RoomID != npc.Body.RoomID || proposal.Due != due || proposal.AttackReady != attackReady ||
		!reflect.DeepEqual(proposal.SourceFloorOrder, room.Items.Inventory) ||
		proposal.TimerBefore != npc.Body.Timers[npcScavengeTimer] {
		return npcScavengeProposalError("snapshot identity or order mismatch")
	}
	if !proposal.Due || !proposal.AttackReady {
		wantDecision := NPCScavengeNotDue
		if proposal.Due {
			wantDecision = NPCScavengeAttackNotReady
		}
		if proposal.Attempted || proposal.Roll != 0 || proposal.RollSuccess || proposal.EligibleItemID != "" ||
			proposal.SelectedItemID != "" || proposal.Transferred || proposal.MHASSCChanged || proposal.Decision != wantDecision ||
			len(proposal.ProtectedRootIDs) != 0 ||
			proposal.TimerAfter != proposal.TimerBefore || !reflect.DeepEqual(proposal.AfterRoomFloorOrder, room.Items.Inventory) ||
			!reflect.DeepEqual(proposal.AfterNPCInventoryOrder, npc.Items.Inventory) || proposal.MHASSC != flag(npc.Body.Flags[:], npcScavengedFlag) {
			return npcScavengeProposalError("invalid no-op decision")
		}
		next := s.clone()
		if !proposal.applyToken.consume() {
			return npcScavengeProposalError("stale or replayed proposal")
		}
		return next, npcScavengeResult(proposal), nil
	}
	if !proposal.Attempted || proposal.Roll < 1 || proposal.Roll > npcScavengeRollHigh || proposal.RollSuccess != (proposal.Roll <= npcScavengeRollWin) {
		return npcScavengeProposalError("invalid roll")
	}
	eligible, protected, err := npcScavengeRoots(*room.Items)
	if err != nil {
		return State{}, NPCScavengeResult{}, err
	}
	if proposal.EligibleItemID != eligible || !reflect.DeepEqual(proposal.ProtectedRootIDs, protected) {
		return npcScavengeProposalError("protected item decision mismatch")
	}
	wantTimer := proposal.TimerBefore
	wantTimer.LastTime = proposal.Now
	if proposal.TimerAfter != wantTimer {
		return npcScavengeProposalError("timer decision mismatch")
	}
	wantSelected := ""
	wantDecision := NPCScavengeNoEligibleItem
	wantTransferred := false
	if !proposal.RollSuccess {
		wantDecision = NPCScavengeRollFailed
	} else if eligible != "" {
		wantSelected = eligible
		wantDecision = NPCScavengeTransferred
		wantTransferred = true
	}
	wantChanged := wantTransferred && !flag(npc.Body.Flags[:], npcScavengedFlag)
	wantMHASSC := flag(npc.Body.Flags[:], npcScavengedFlag) || wantTransferred
	if proposal.SelectedItemID != wantSelected || proposal.Decision != wantDecision || proposal.Transferred != wantTransferred ||
		proposal.MHASSCChanged != wantChanged || proposal.MHASSC != wantMHASSC {
		return npcScavengeProposalError("outcome decision mismatch")
	}

	next := s.clone()
	nextNPC := next.NPCs[proposal.NPCID]
	nextNPC.Body.Timers[npcScavengeTimer] = wantTimer
	nextRoom := next.Rooms[proposal.RoomID]
	if wantTransferred {
		transfer, transferErr := TransferItemRoots(*nextRoom.Items, *nextNPC.Items, []string{wantSelected})
		if transferErr != nil {
			return State{}, NPCScavengeResult{}, fmt.Errorf("NPC MSCAVE transfer: %w", transferErr)
		}
		nextRoom.Items = &transfer.Source
		nextNPC.Items = &transfer.Destination
		nextNPC.Body.Flags[npcScavengedFlag/8] |= 1 << (npcScavengedFlag % 8)
	}
	next.Rooms[proposal.RoomID] = nextRoom
	next.NPCs[proposal.NPCID] = nextNPC
	if !reflect.DeepEqual(proposal.AfterRoomFloorOrder, nextRoom.Items.Inventory) ||
		!reflect.DeepEqual(proposal.AfterNPCInventoryOrder, nextNPC.Items.Inventory) {
		return npcScavengeProposalError("canonical order mismatch")
	}
	if err := next.Validate(); err != nil {
		return State{}, NPCScavengeResult{}, fmt.Errorf("NPC MSCAVE result invalid: %w", err)
	}
	if !proposal.applyToken.consume() {
		return npcScavengeProposalError("stale or replayed proposal")
	}
	return next, npcScavengeResult(proposal), nil
}
