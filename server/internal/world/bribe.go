package world

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// npcBribePermanentFlag is MPERMT from src/mtype.h.  The legacy flag
	// number is 0; MTRADE is 37 and must not be confused with permanence.
	npcBribePermanentFlag = npcPermanentFlag
	npcBribeThresholdBase = int64(895)
)

// BribeEvent is the deterministic room projection of command9.c:bribe. The
// actor receives Result.Response; the room projection excludes that actor so
// a transport cannot deliver the same command twice.
type BribeEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	NPCID          string `json:"npc_id"`
	NPCName        string `json:"npc_name"`
	Amount         int32  `json:"amount"`
	Stayed         bool   `json:"stayed"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

// BribeResult is the durable actor response and audit projection.  A missing
// target is a successful no-op receipt, matching other command reducers;
// malformed amounts, stale state, and insufficient gold fail before commit.
type BribeResult struct {
	Action        string      `json:"action"`
	RoomID        int16       `json:"room_id"`
	NPCID         string      `json:"npc_id,omitempty"`
	NPCName       string      `json:"npc_name"`
	NPCOccurrence int         `json:"npc_occurrence"`
	Amount        int32       `json:"amount,omitempty"`
	GoldBefore    int64       `json:"gold_before,omitempty"`
	GoldAfter     int64       `json:"gold_after,omitempty"`
	Stayed        bool        `json:"stayed,omitempty"`
	Broadcast     bool        `json:"broadcast,omitempty"`
	Response      string      `json:"response"`
	Event         *BribeEvent `json:"event,omitempty"`
}

// BribeProposal is a state-bound candidate.  The complete source snapshot is
// retained privately so ApplyBribe rejects any plan reused against a changed
// world, including changes to ordered room or active-NPC lists.
type BribeProposal struct {
	ActorID       string
	RoomID        int16
	NPCID         string
	NPCName       string
	NPCOccurrence int
	Amount        int32
	Found         bool
	Rejected      bool
	Stayed        bool
	Response      string
	ExpectedEvent *BribeEvent

	before State
}

// ParseBribeAmount admits the exact C command amount token: decimal digits
// followed by the Korean currency suffix.  Signs, separators, whitespace,
// overflow, zero, and trailing data are rejected.
func ParseBribeAmount(token string) (int32, error) {
	if token == "" || !strings.HasSuffix(token, "냥") {
		return 0, fmt.Errorf("bribe amount must end in 냥")
	}
	digits := strings.TrimSuffix(token, "냥")
	if digits == "" {
		return 0, fmt.Errorf("bribe amount is empty")
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid bribe amount")
		}
	}
	value, err := strconv.ParseInt(digits, 10, 32)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("bribe amount outside positive int32 range")
	}
	return int32(value), nil
}

func bribeNPCVisible(actor, npc LegacyMonster) bool {
	// This is the same gate as creature.c:find_crt: caretaker DM-invisible
	// creatures are skipped, and ordinary MINVIS creatures require PDINVI.
	if npc.Class >= lookAtCaretakerClass && flag(npc.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return !flag(npc.Flags[:], npcInvisibleFlag) || flag(actor.Flags[:], playerDetectFlag)
}

func validBribeName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func selectBribeNPC(s State, actorID, name string, occurrence int) (string, bool, error) {
	if occurrence < 1 {
		return "", false, fmt.Errorf("bribe NPC occurrence must be positive")
	}
	if !validBribeName(name) {
		return "", false, fmt.Errorf("invalid bribe NPC name")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validBribeName(actor.Body.Name) {
		return "", false, fmt.Errorf("online bribe actor absent")
	}
	if s.NPCs == nil {
		return "", false, fmt.Errorf("canonical bribe NPC state required")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return "", false, fmt.Errorf("bribe actor room absent")
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if id == "" || !exists || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validBribeName(npc.Body.Name) {
			return "", false, fmt.Errorf("unresolved canonical bribe NPC identity")
		}
		if !strings.EqualFold(npc.Body.Name, name) || !bribeNPCVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return id, true, nil
		}
	}
	return "", false, nil
}

func bribeResponse(npcName string, amount int32, stayed bool) string {
	_ = amount // amount is part of the result/event, not the legacy actor text.
	particle := legacySubjectParticle(npcName)
	if stayed {
		return fmt.Sprintf("%s%s 뇌물을 슬쩍 받았습니다만 꿈쩍도 하지 않습니다.\r\n", npcName, particle)
	}
	return fmt.Sprintf("%s%s 뇌물을 받자 미소를 지으며 어딘가로 갑니다.\r\n", npcName, particle)
}

func bribeRoomEvent(actor LegacyMonster, actorID, npcID string, npc LegacyMonster, amount int32, stayed bool) BribeEvent {
	actorParticle := legacySubjectParticle(actor.Name)
	return BribeEvent{
		RoomID:         actor.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Name,
		NPCID:          npcID,
		NPCName:        npc.Name,
		Amount:         amount,
		Stayed:         stayed,
		ExcludeActorID: actorID,
		Text:           fmt.Sprintf("\n%s%s %s에게 뇌물을 줍니다.\r\n", actor.Name, actorParticle, npc.Name),
	}
}

func bribeNoTargetResult(roomID int16, name string, occurrence int, amount int32) BribeResult {
	return BribeResult{
		Action:        "bribe-rejected",
		RoomID:        roomID,
		NPCName:       name,
		NPCOccurrence: occurrence,
		Amount:        amount,
		Response:      "그런 것은 여기 없습니다.\r\n",
	}
}

func removeNPCEnemyReferences(next *State, targetID string) {
	for id, npc := range next.NPCs {
		if id == targetID || npc.Enemies == nil {
			continue
		}
		kept := make([]NPCEnemy, 0, len(npc.Enemies))
		for _, enemy := range npc.Enemies {
			if enemy.Target.Kind == "npc" && enemy.Target.ID == targetID {
				continue
			}
			kept = append(kept, enemy)
		}
		npc.Enemies = kept
		next.NPCs[id] = npc
	}
}

// PlanBribe validates the canonical same-room target and prepares the exact
// debit/stay-or-leave decision. It never mutates the source snapshot.
func (s State) PlanBribe(actorID, npcName string, npcOccurrence int, amount int32) (BribeProposal, error) {
	if err := s.Validate(); err != nil {
		return BribeProposal{}, err
	}
	if amount < 1 {
		return BribeProposal{}, fmt.Errorf("bribe amount must be positive")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Gold < 0 {
		return BribeProposal{}, fmt.Errorf("online bribe actor or gold absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return BribeProposal{}, fmt.Errorf("bribe actor room absent")
	}
	proposal := BribeProposal{
		ActorID:       actorID,
		RoomID:        room.Resource.ID,
		NPCName:       npcName,
		NPCOccurrence: npcOccurrence,
		Amount:        amount,
		before:        s.clone(),
	}
	targetID, found, err := selectBribeNPC(s, actorID, npcName, npcOccurrence)
	if err != nil {
		return BribeProposal{}, err
	}
	proposal.Found = found
	if !found {
		proposal.Rejected = true
		proposal.Response = bribeNoTargetResult(room.Resource.ID, npcName, npcOccurrence, amount).Response
		return proposal, nil
	}
	npc := s.NPCs[targetID]
	if npc.Body.Gold < 0 {
		return BribeProposal{}, fmt.Errorf("negative NPC gold")
	}
	if amount > actor.Body.Gold {
		return BribeProposal{}, fmt.Errorf("insufficient bribe gold")
	}
	threshold := ((int64(npc.Body.Level) + 3) / 4) * npcBribeThresholdBase
	stayed := int64(amount) < threshold || flag(npc.Body.Flags[:], npcBribePermanentFlag)
	if !stayed && s.ActiveNPCIDs == nil {
		return BribeProposal{}, fmt.Errorf("active NPC order unresolved")
	}
	proposal.NPCID = targetID
	proposal.NPCName = npc.Body.Name
	proposal.Stayed = stayed
	proposal.Response = bribeResponse(npc.Body.Name, amount, stayed)
	event := bribeRoomEvent(actor.Body, actorID, targetID, npc.Body, amount, stayed)
	proposal.ExpectedEvent = &event
	return proposal, nil
}

// ApplyBribe commits the prepared candidate only against its exact source
// snapshot. Debit and NPC credit/removal happen in one cloned transition.
func (s State) ApplyBribe(proposal BribeProposal) (State, BribeResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, BribeResult{}, err
	}
	room, roomExists := s.Rooms[proposal.RoomID]
	if proposal.ActorID == "" || !roomExists || room.Resource.ID != proposal.RoomID || proposal.NPCOccurrence < 1 || proposal.Amount < 1 || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, BribeResult{}, fmt.Errorf("stale or invalid bribe proposal")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.RoomID != proposal.RoomID || actor.Body.Gold < 0 {
		return State{}, BribeResult{}, fmt.Errorf("bribe actor changed")
	}
	if proposal.Rejected || !proposal.Found {
		if proposal.NPCID != "" || proposal.ExpectedEvent != nil || proposal.Stayed || proposal.Amount < 1 {
			return State{}, BribeResult{}, fmt.Errorf("invalid rejected bribe proposal")
		}
		result := bribeNoTargetResult(proposal.RoomID, proposal.NPCName, proposal.NPCOccurrence, proposal.Amount)
		return s.clone(), result, nil
	}
	if proposal.NPCID == "" || proposal.NPCName == "" || proposal.ExpectedEvent == nil {
		return State{}, BribeResult{}, fmt.Errorf("invalid successful bribe proposal")
	}
	if !containsString(room.NPCIDs, proposal.NPCID) {
		return State{}, BribeResult{}, fmt.Errorf("bribe target room membership changed")
	}
	npc, ok := s.NPCs[proposal.NPCID]
	if !ok || npc.Body.Type != 1 || npc.Body.RoomID != proposal.RoomID || npc.Body.Name != proposal.NPCName {
		return State{}, BribeResult{}, fmt.Errorf("bribe target changed")
	}
	if proposal.Amount > actor.Body.Gold || npc.Body.Gold < 0 {
		return State{}, BribeResult{}, fmt.Errorf("bribe gold changed")
	}
	threshold := ((int64(npc.Body.Level) + 3) / 4) * npcBribeThresholdBase
	stayed := int64(proposal.Amount) < threshold || flag(npc.Body.Flags[:], npcBribePermanentFlag)
	if stayed != proposal.Stayed {
		return State{}, BribeResult{}, fmt.Errorf("bribe outcome changed")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	goldBefore := int64(nextActor.Body.Gold)
	nextActor.Body.Gold -= proposal.Amount
	next.Players[proposal.ActorID] = nextActor
	if proposal.Stayed {
		nextNPC := next.NPCs[proposal.NPCID]
		if int64(nextNPC.Body.Gold) > int64(^uint32(0)>>1)-int64(proposal.Amount) {
			return State{}, BribeResult{}, fmt.Errorf("NPC bribe gold overflow")
		}
		nextNPC.Body.Gold += proposal.Amount
		next.NPCs[proposal.NPCID] = nextNPC
	} else {
		room := next.Rooms[proposal.RoomID]
		room.NPCIDs, _ = removeNPCID(room.NPCIDs, proposal.NPCID)
		next.Rooms[proposal.RoomID] = room
		if next.ActiveNPCIDs != nil {
			next.ActiveNPCIDs, _ = removeNPCID(next.ActiveNPCIDs, proposal.NPCID)
		}
		if follower := next.NPCs[proposal.NPCID].FollowingPlayerID; follower != "" {
			leader, exists := next.Players[follower]
			if !exists {
				return State{}, BribeResult{}, fmt.Errorf("bribe follower leader absent")
			}
			leader.NPCFollowerIDs, _ = removeNPCID(leader.NPCFollowerIDs, proposal.NPCID)
			leader.FollowerRefs = removeNPCFollowerRef(leader.FollowerRefs, proposal.NPCID)
			next.Players[follower] = leader
		}
		removeNPCEnemyReferences(&next, proposal.NPCID)
		delete(next.NPCs, proposal.NPCID)
	}
	if err := next.Validate(); err != nil {
		return State{}, BribeResult{}, err
	}
	result := BribeResult{
		Action:        "bribe",
		RoomID:        proposal.RoomID,
		NPCID:         proposal.NPCID,
		NPCName:       proposal.NPCName,
		NPCOccurrence: proposal.NPCOccurrence,
		Amount:        proposal.Amount,
		GoldBefore:    goldBefore,
		GoldAfter:     int64(nextActor.Body.Gold),
		Stayed:        proposal.Stayed,
		Broadcast:     true,
		Response:      proposal.Response,
		Event:         proposal.ExpectedEvent,
	}
	return next, result, nil
}

// BribeNPCByName is the one-call reducer boundary used by session commands.
func (s State) BribeNPCByName(actorID, npcName string, npcOccurrence int, amount int32) (State, BribeResult, error) {
	proposal, err := s.PlanBribe(actorID, npcName, npcOccurrence, amount)
	if err != nil {
		return State{}, BribeResult{}, err
	}
	return s.ApplyBribe(proposal)
}

// PlanNPCBribe is a descriptive compatibility alias for callers that name the
// reducer after its canonical NPC target.
func (s State) PlanNPCBribe(actorID, npcName string, npcOccurrence int, amount int32) (BribeProposal, error) {
	return s.PlanBribe(actorID, npcName, npcOccurrence, amount)
}
