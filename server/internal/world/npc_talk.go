package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// These are the stable bit positions from src/mtype.h.  The Go NPC body
	// still carries the source flag bytes, so the talk reducer deliberately
	// reads those bytes instead of introducing a second flag authority.
	npcTalkFlag           = 23 // MTALKS
	npcTalkAggressiveFlag = 26 // MTLKAG (the source spelling is MTLKAG)

	maxNPCTalkTextBytes = 1023
)

var (
	// ErrNPCTalkTopicsUnavailable is the migration boundary for command8.c's
	// talk/<name>-<level> files. LegacyMonster has the no-topic talk string,
	// but the canonical NPC model has no TalkTopics field or loader contract.
	// A topic must therefore never be answered from an unproven legacy value.
	ErrNPCTalkTopicsUnavailable = errors.New("canonical NPC talk topics unavailable")
	ErrNPCTalkTargetAbsent      = errors.New("NPC talk target absent")
)

// NPCTalkRoomMessage is one ordered room projection. ExcludeActorID is kept
// per message even though the current bounded slice uses the same exclusion
// for all messages; this leaves the recipient boundary explicit if a later
// source branch broadcasts a message to everyone.
type NPCTalkRoomMessage struct {
	Text           string `json:"text"`
	ExcludeActorID string `json:"exclude_actor_id,omitempty"`
}

// NPCTalkEvent is a deterministic post-commit projection of command8.c:talk.
// RoomText is delivered to room occupants other than ExcludeActorID, while
// ActorText is delivered to the actor. RoomMessages/ActorMessages preserve
// the source broadcast order for transports that do not want to concatenate
// messages. The projection is receipt data, not persisted world state.
type NPCTalkEvent struct {
	RoomID         int16                `json:"room_id"`
	ActorID        string               `json:"actor_id"`
	ActorName      string               `json:"actor_name"`
	NPCID          string               `json:"npc_id"`
	NPCName        string               `json:"npc_name"`
	Topic          string               `json:"topic,omitempty"`
	ExcludeActorID string               `json:"exclude_actor_id,omitempty"`
	RoomText       string               `json:"room_text"`
	ActorText      string               `json:"actor_text"`
	RoomMessages   []NPCTalkRoomMessage `json:"room_messages"`
	ActorMessages  []string             `json:"actor_messages"`
}

// NPCTalkResult is the durable actor response plus the deterministic room
// projection. Target identity is included for audit/replay; it is resolved by
// the world reducer and is never accepted from the terminal as authority.
type NPCTalkResult struct {
	Response   string        `json:"response"`
	Broadcast  bool          `json:"broadcast"`
	TargetID   string        `json:"target_id,omitempty"`
	TargetName string        `json:"target_name,omitempty"`
	Occurrence int           `json:"occurrence,omitempty"`
	Topic      string        `json:"topic,omitempty"`
	EnemyAdded bool          `json:"enemy_added,omitempty"`
	Event      *NPCTalkEvent `json:"event,omitempty"`
}

// NPCTalkProposal is the pure candidate for PlanNPCTalkProposal/ApplyNPCTalk.
// before is a private immutable snapshot guard: it prevents a caller from
// applying a target resolved against a different room/NPC ordering. A missing
// target is represented as a deterministic no-op proposal so the source
// generic "그런 것은 여기 없습니다." response can be receipt/replay safe.
type NPCTalkProposal struct {
	ActorID       string
	RoomID        int16
	Selector      string
	Occurrence    int
	TargetID      string
	TargetName    string
	Topic         string
	Found         bool
	ClearHidden   bool
	AddEnemy      bool
	Response      string
	Broadcast     bool
	ExpectedEvent *NPCTalkEvent

	before State
}

// ValidateNPCTalkName checks a client-selected display-name token before it
// participates in a receipt or rendered event. Exact matching is performed
// case-insensitively, but prefixes and legacy keys are intentionally ignored.
func ValidateNPCTalkName(name string) error {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return fmt.Errorf("invalid NPC talk name")
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return fmt.Errorf("invalid NPC talk name")
		}
	}
	return nil
}

func validNPCTalkText(text string) bool {
	if !utf8.ValidString(text) || len(text) > maxNPCTalkTextBytes {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return false
		}
	}
	return true
}

func npcTalkVisible(actor LegacyMonster, npc LegacyMonster) bool {
	// find_crt skips caretaker PDMINV creatures and ordinary MINVIS creatures
	// unless the actor has PDINVI. MDINVI is an NPC combat capability, not the
	// player's detection permission, and therefore is not consulted here.
	if npc.Class >= lookAtCaretakerClass && flag(npc.Flags[:], playerDMInvisibleFlag) {
		return false
	}
	return !flag(npc.Flags[:], npcInvisibleFlag) || flag(actor.Flags[:], playerDetectFlag)
}

// selectNPCTalkTarget resolves the canonical same-room NPC list in its stored
// order. A positive occurrence counts only exact display-name matches that
// pass the source visibility gate. It never scans LegacyRoom.Monsters or the
// player list and never uses map iteration.
func (s State) selectNPCTalkTarget(actorID, targetName string, occurrence int) (string, bool, error) {
	if occurrence < 1 {
		return "", false, fmt.Errorf("NPC talk occurrence must be positive")
	}
	if err := ValidateNPCTalkName(targetName); err != nil {
		return "", false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return "", false, fmt.Errorf("online NPC talk actor absent")
	}
	if s.NPCs == nil {
		return "", false, fmt.Errorf("canonical NPC talk state required")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return "", false, fmt.Errorf("NPC talk actor room absent")
	}
	matched := 0
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if id == "" || !exists || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || npc.Body.Name == "" {
			return "", false, fmt.Errorf("unresolved canonical NPC talk identity")
		}
		if !strings.EqualFold(npc.Body.Name, targetName) || !npcTalkVisible(actor.Body, npc.Body) {
			continue
		}
		matched++
		if matched == occurrence {
			return id, true, nil
		}
	}
	return "", false, nil
}

func (s State) npcTalkTargetByID(actorID, targetID string) (LegacyMonster, error) {
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return LegacyMonster{}, fmt.Errorf("online NPC talk actor absent")
	}
	if s.NPCs == nil || targetID == "" {
		return LegacyMonster{}, ErrNPCTalkTargetAbsent
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.NPCIDs, targetID) {
		return LegacyMonster{}, ErrNPCTalkTargetAbsent
	}
	npc, ok := s.NPCs[targetID]
	if !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !npcTalkVisible(actor.Body, npc.Body) {
		return LegacyMonster{}, ErrNPCTalkTargetAbsent
	}
	return npc.Body, nil
}

func npctalkNoTargetResult(occurrence int, topic string) NPCTalkResult {
	return NPCTalkResult{
		Response:   "그런 것은 여기 없습니다.\r\n",
		Occurrence: occurrence,
		Topic:      topic,
	}
}

func npcTalkRoomMessages(actor, npc LegacyMonster, reply string) ([]NPCTalkRoomMessage, []string) {
	actorDisplay := actor.Name + "님"
	npcSubject := legacySubjectParticle(npc.Name)
	npcWith := npcTalkJosa(npc.Name, "과", "와")
	roomMessages := []NPCTalkRoomMessage{
		{
			Text:           fmt.Sprintf("\n%s이 %s%s 이야기를 합니다.\r\n", actorDisplay, npc.Name, npcWith),
			ExcludeActorID: "",
		},
	}
	if reply == "" {
		blank := fmt.Sprintf("\n%s%s 단지 당신을 멍하니 바라봅니다.\r\n", npc.Name, npcSubject)
		roomMessages = append(roomMessages, NPCTalkRoomMessage{Text: blank})
		return roomMessages, []string{blank}
	}
	roomReply := fmt.Sprintf("\n%s%s %s에게 \"%s\"라고 이야기합니다.\r\n", npc.Name, npcSubject, actorDisplay, reply)
	actorReply := fmt.Sprintf("\n%s%s 당신에게 \"%s\"라고 이야기합니다.\r\n", npc.Name, npcSubject, reply)
	roomMessages = append(roomMessages, NPCTalkRoomMessage{Text: roomReply})
	return roomMessages, []string{actorReply}
}

func npcTalkJosa(name, withFinal, withoutFinal string) string {
	if hasFinalHangul(name) {
		return withFinal
	}
	return withoutFinal
}

func buildNPCTalkEvent(actor LegacyMonster, actorID, npcID string, npc LegacyMonster, topic string) NPCTalkEvent {
	roomMessages, actorMessages := npcTalkRoomMessages(actor, npc, npc.Talk)
	var roomText, actorText string
	for _, message := range roomMessages {
		roomText += message.Text
	}
	for _, message := range actorMessages {
		actorText += message
	}
	for i := range roomMessages {
		// The actor receives ActorMessages, so room delivery is always excluded
		// from that actor. This also gives the source broadcast(-1) blank-stare
		// branch a single deterministic recipient projection.
		roomMessages[i].ExcludeActorID = actorID
	}
	return NPCTalkEvent{
		RoomID:         actor.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Name,
		NPCID:          npcID,
		NPCName:        npc.Name,
		Topic:          topic,
		ExcludeActorID: actorID,
		RoomText:       roomText,
		ActorText:      actorText,
		RoomMessages:   roomMessages,
		ActorMessages:  actorMessages,
	}
}

// PlanNPCTalkProposal is the source-backed no-topic command8.c:talk boundary.
// With a topic, C would load a per-NPC talk file. Since the canonical Go model
// has no TalkTopics/loader contract, a MTALKS NPC rejects that branch before a
// receipt or state mutation is created. A non-MTALKS NPC follows C's
// `cmnd->num == 2 || !MTALKS` branch and gives its no-topic response.
func (s State) PlanNPCTalkProposal(actorID, targetName string, occurrence int, topic string) (NPCTalkProposal, error) {
	if err := s.Validate(); err != nil {
		return NPCTalkProposal{}, err
	}
	if actorID == "" || occurrence < 1 {
		return NPCTalkProposal{}, fmt.Errorf("invalid NPC talk actor or occurrence")
	}
	if err := ValidateNPCTalkName(targetName); err != nil {
		return NPCTalkProposal{}, err
	}
	if !utf8.ValidString(topic) || strings.TrimSpace(topic) != topic {
		return NPCTalkProposal{}, fmt.Errorf("invalid NPC talk topic")
	}
	for _, r := range topic {
		if unicode.IsControl(r) || r == '\r' || r == '\n' {
			return NPCTalkProposal{}, fmt.Errorf("invalid NPC talk topic")
		}
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || !validNPCTalkName(actor.Body.Name) {
		return NPCTalkProposal{}, fmt.Errorf("online NPC talk actor absent")
	}
	targetID, found, err := s.selectNPCTalkTarget(actorID, targetName, occurrence)
	if err != nil {
		return NPCTalkProposal{}, err
	}
	proposal := NPCTalkProposal{
		ActorID:    actorID,
		RoomID:     actor.Body.RoomID,
		Selector:   targetName,
		Occurrence: occurrence,
		Topic:      topic,
		TargetID:   targetID,
		Found:      found,
		before:     s.clone(),
	}
	if !found {
		result := npctalkNoTargetResult(occurrence, topic)
		proposal.Response = result.Response
		return proposal, nil
	}
	npc := s.NPCs[targetID]
	if !validNPCTalkName(npc.Body.Name) || !validNPCTalkText(npc.Body.Talk) {
		return NPCTalkProposal{}, fmt.Errorf("invalid canonical NPC talk response")
	}
	if topic != "" && flag(npc.Body.Flags[:], npcTalkFlag) {
		return NPCTalkProposal{}, ErrNPCTalkTopicsUnavailable
	}
	proposal.TargetName = npc.Body.Name
	proposal.ClearHidden = true
	proposal.AddEnemy = flag(npc.Body.Flags[:], npcTalkAggressiveFlag)
	if proposal.AddEnemy && npc.Enemies == nil {
		return NPCTalkProposal{}, fmt.Errorf("NPC talk enemy relations unresolved")
	}
	event := buildNPCTalkEvent(actor.Body, actorID, targetID, npc.Body, "")
	proposal.ExpectedEvent = &event
	proposal.Response = event.ActorText
	proposal.Broadcast = true
	return proposal, nil
}

// PlanNPCTalkWithOccurrence is the reducer-shaped convenience API used by
// pure callers that need the committed candidate and durable result together.
func (s State) PlanNPCTalkWithOccurrence(actorID, targetName string, occurrence int, topic string) (State, NPCTalkResult, error) {
	proposal, err := s.PlanNPCTalkProposal(actorID, targetName, occurrence, topic)
	if err != nil {
		return State{}, NPCTalkResult{}, err
	}
	return s.ApplyNPCTalk(proposal)
}

// PlanNPCTalk uses the canonical default occurrence one. It is intentionally
// separate from the parser so server-side callers cannot accidentally treat a
// zero occurrence as a legacy wildcard.
func (s State) PlanNPCTalk(actorID, targetName, topic string) (State, NPCTalkResult, error) {
	return s.PlanNPCTalkWithOccurrence(actorID, targetName, 1, topic)
}

// ApplyNPCTalk atomically applies the proposal against the exact snapshot on
// which it was planned. PHIDDN release and the optional MTALKAG enemy edge are
// committed together; any stale identity, topic gate, or relation error
// rejects the whole candidate without exposing a partial state.
func (s State) ApplyNPCTalk(proposal NPCTalkProposal) (State, NPCTalkResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCTalkResult{}, err
	}
	if proposal.before.Version == 0 || !reflect.DeepEqual(s, proposal.before) {
		return State{}, NPCTalkResult{}, fmt.Errorf("stale NPC talk proposal")
	}
	room, roomOK := s.Rooms[proposal.RoomID]
	if proposal.ActorID == "" || proposal.Occurrence < 1 || !roomOK || room.Resource.ID != proposal.RoomID {
		return State{}, NPCTalkResult{}, fmt.Errorf("invalid NPC talk proposal")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.RoomID != proposal.RoomID || !validNPCTalkName(actor.Body.Name) {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk actor changed")
	}
	if err := ValidateNPCTalkName(proposal.Selector); err != nil {
		return State{}, NPCTalkResult{}, err
	}
	targetID, found, err := s.selectNPCTalkTarget(proposal.ActorID, proposal.Selector, proposal.Occurrence)
	if err != nil {
		return State{}, NPCTalkResult{}, err
	}
	if found != proposal.Found || found != (proposal.TargetID != "") {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk target changed")
	}
	if !found {
		if proposal.Response != "그런 것은 여기 없습니다.\r\n" || proposal.Broadcast || proposal.ClearHidden || proposal.ExpectedEvent != nil || proposal.TargetName != "" {
			return State{}, NPCTalkResult{}, fmt.Errorf("invalid NPC talk no-op proposal")
		}
		return s.clone(), npctalkNoTargetResult(proposal.Occurrence, proposal.Topic), nil
	}
	if targetID != proposal.TargetID {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk target identity changed")
	}
	npc, err := s.npcTalkTargetByID(proposal.ActorID, targetID)
	if err != nil {
		return State{}, NPCTalkResult{}, err
	}
	if npc.Name != proposal.TargetName || !validNPCTalkText(npc.Talk) {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk target changed")
	}
	if proposal.Topic != "" && flag(npc.Flags[:], npcTalkFlag) {
		return State{}, NPCTalkResult{}, ErrNPCTalkTopicsUnavailable
	}
	currentNPC := s.NPCs[targetID]
	addEnemy := flag(npc.Flags[:], npcTalkAggressiveFlag)
	if addEnemy != proposal.AddEnemy || (addEnemy && currentNPC.Enemies == nil) {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk enemy relation changed")
	}
	expected := buildNPCTalkEvent(actor.Body, proposal.ActorID, targetID, npc, "")
	if proposal.ExpectedEvent == nil || !reflect.DeepEqual(*proposal.ExpectedEvent, expected) || proposal.Response != expected.ActorText || !proposal.ClearHidden || !proposal.Broadcast {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk projection changed")
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	next.Players[proposal.ActorID] = nextActor
	if addEnemy {
		nextNPC := next.NPCs[targetID]
		ref := EntityRef{Kind: "player", ID: proposal.ActorID}
		hasEnemy := false
		for _, relation := range nextNPC.Enemies {
			if relation.Target == ref {
				hasEnemy = true
				break
			}
		}
		if !hasEnemy {
			nextNPC.Enemies = append([]NPCEnemy{{Target: ref, Damage: 0}}, nextNPC.Enemies...)
		}
		next.NPCs[targetID] = nextNPC
	}
	if err := next.Validate(); err != nil {
		return State{}, NPCTalkResult{}, err
	}
	result := NPCTalkResult{
		Response:   expected.ActorText,
		Broadcast:  true,
		TargetID:   targetID,
		TargetName: npc.Name,
		Occurrence: proposal.Occurrence,
		Topic:      proposal.Topic,
		EnemyAdded: addEnemy && !npcEnemyContains(currentNPC.Enemies, EntityRef{Kind: "player", ID: proposal.ActorID}),
		Event:      &expected,
	}
	return next, result, nil
}

// ApplyNPCTalkProposal is a descriptive alias for callers that name the
// reducer by its explicit proposal boundary.
func (s State) ApplyNPCTalkProposal(proposal NPCTalkProposal) (State, NPCTalkResult, error) {
	return s.ApplyNPCTalk(proposal)
}

func npcEnemyContains(enemies []NPCEnemy, target EntityRef) bool {
	for _, enemy := range enemies {
		if enemy.Target == target {
			return true
		}
	}
	return false
}

// RoomNPCTalkEvent derives the same event from committed canonical state. It
// is read-only and is intended for transports that publish projections after
// the first receipt commit; replay callers should use the receipt's result.
func (s State) RoomNPCTalkEvent(actorID, targetID, topic string) (NPCTalkEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return NPCTalkEvent{}, false, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return NPCTalkEvent{}, false, fmt.Errorf("online NPC talk actor absent")
	}
	npc, err := s.npcTalkTargetByID(actorID, targetID)
	if err != nil {
		return NPCTalkEvent{}, false, err
	}
	if topic != "" && flag(npc.Flags[:], npcTalkFlag) {
		return NPCTalkEvent{}, false, ErrNPCTalkTopicsUnavailable
	}
	event := buildNPCTalkEvent(actor.Body, actorID, targetID, npc, "")
	return event, true, nil
}

// NPCTalkEventForName resolves the exact positive-occurrence selector against
// committed state and derives a recipient projection without trusting a client
// supplied NPC ID.
func (s State) NPCTalkEventForName(actorID, targetName string, occurrence int, topic string) (NPCTalkEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return NPCTalkEvent{}, false, err
	}
	targetID, found, err := s.selectNPCTalkTarget(actorID, targetName, occurrence)
	if err != nil {
		return NPCTalkEvent{}, false, err
	}
	if !found {
		return NPCTalkEvent{}, false, ErrNPCTalkTargetAbsent
	}
	return s.RoomNPCTalkEvent(actorID, targetID, topic)
}

func validNPCTalkName(name string) bool {
	return ValidateNPCTalkName(name) == nil
}
