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
	npcTalkFlag            = 23 // MTALKS
	npcTalkAggressiveFlag  = 26 // MTLKAG (the source spelling is MTLKAG)
	npcTalkBlessSpell      = 4  // SBLESS / 성현진
	npcTalkProtectionSpell = 5  // SPROTE / 수호진
	npcTalkCurePoisonSpell = 3  // SCUREP / 해독
	npcTalkDiseaseSpell    = 48 // SRMDIS / 치료
	npcTalkBlindSpell      = 49 // SRMBLD / 개안술
	npcTalkBlessFlag       = 0  // PBLESS
	npcTalkProtectionFlag  = 8  // PPROTE
	npcTalkPoisonFlag      = 16 // PPOISN
	npcTalkDiseaseFlag     = 41 // PDISEA
	npcTalkBlindFlag       = 42 // PBLIND
	npcTalkProtectionTimer = 1  // LT_PROTE
	npcTalkBlessTimer      = 2  // LT_BLESS
	npcTalkRoomMagicExtend = 32 // RPMEXT

	maxNPCTalkTextBytes = 1023
)

var (
	// ErrNPCTalkTopicsUnavailable is the migration boundary for command8.c's
	// talk/<name>-<level> files. A topic request for an MTALKS NPC must have an
	// injected, source-backed TalkCatalog; it must never be answered from the
	// legacy no-topic Talk string or from an implicit filesystem lookup.
	ErrNPCTalkTopicsUnavailable = errors.New("canonical NPC talk topics unavailable")
	// ErrNPCTalkActionUnavailable is returned when a catalog record contains a
	// C talk_action side effect whose world reducer has not been admitted yet.
	// ATTACK, ACTION, and the item-giving form of GIVE are handled as
	// deterministic transitions below; CAST remains closed to the admitted
	// canonical spell set.
	ErrNPCTalkActionUnavailable         = errors.New("NPC talk action unavailable")
	ErrNPCTalkTargetAbsent              = errors.New("NPC talk target absent")
	ErrNPCTalkCastSpellUnavailable      = errors.New("NPC talk cast spell unavailable")
	ErrNPCTalkCastRandomUnavailable     = errors.New("NPC talk cast random source unavailable")
	ErrNPCTalkCastProjectionUnavailable = errors.New("NPC talk cast projection requires receipt")
	ErrNPCTalkGiveObjectUnavailable     = errors.New("NPC talk give object unavailable")
	ErrNPCTalkGiveInventoryUnavailable  = errors.New("NPC talk give inventory unavailable")
	ErrNPCTalkGiveAllocatorUnavailable  = errors.New("NPC talk give allocator unavailable")
	ErrNPCTalkGiveRandomUnavailable     = errors.New("NPC talk give random source unavailable")
	ErrNPCTalkGiveProjectionUnavailable = errors.New("NPC talk give projection requires receipt")
)

// NPCTalkEffectOptions contains host-owned values needed by admitted catalog
// side effects. Random draws and freshly allocated item IDs are recorded in
// NPCTalkProposal and are never requested again by ApplyNPCTalk, so a
// retry/replay is deterministic.
type NPCTalkEffectOptions struct {
	Now           int32
	Roll          func(int, int) int
	ObjectCatalog SpawnCatalog
	Allocate      func() (string, error)
}

type npcTalkCastSpec struct {
	Name      string
	Spell     int
	Flag      uint
	Timer     int
	Cost      int16
	ClassGate npcTalkCastClassGate
}

type npcTalkCastClassGate uint8

const (
	npcTalkCastAnyClass npcTalkCastClassGate = iota
	npcTalkCastClericOrInvincible
	npcTalkCastClericPaladinOrInvincible
)

func npcTalkCastClassAllowed(class byte, gate npcTalkCastClassGate) bool {
	if class > maxLegacyClass {
		return false
	}
	switch gate {
	case npcTalkCastAnyClass:
		return true
	case npcTalkCastClericOrInvincible:
		return class == clericClass || class >= invincibleClass
	case npcTalkCastClericPaladinOrInvincible:
		return class == clericClass || class == paladinClass || class >= invincibleClass
	default:
		return false
	}
}

func npcTalkCastSpecFor(name string) (npcTalkCastSpec, error) {
	switch name {
	case "성현진":
		if len(legacyInfoSpellNames) <= npcTalkBlessSpell || legacyInfoSpellNames[npcTalkBlessSpell] != name {
			return npcTalkCastSpec{}, ErrNPCTalkCastSpellUnavailable
		}
		return npcTalkCastSpec{
			Name: name, Spell: npcTalkBlessSpell, Flag: npcTalkBlessFlag, Timer: npcTalkBlessTimer, Cost: 10,
		}, nil
	case "수호진":
		if len(legacyInfoSpellNames) <= npcTalkProtectionSpell || legacyInfoSpellNames[npcTalkProtectionSpell] != name {
			return npcTalkCastSpec{}, ErrNPCTalkCastSpellUnavailable
		}
		return npcTalkCastSpec{
			Name: name, Spell: npcTalkProtectionSpell, Flag: npcTalkProtectionFlag, Timer: npcTalkProtectionTimer, Cost: 10,
		}, nil
	case "해독":
		if len(legacyInfoSpellNames) <= npcTalkCurePoisonSpell || legacyInfoSpellNames[npcTalkCurePoisonSpell] != name {
			return npcTalkCastSpec{}, ErrNPCTalkCastSpellUnavailable
		}
		return npcTalkCastSpec{
			Name: name, Spell: npcTalkCurePoisonSpell, Flag: npcTalkPoisonFlag, Timer: -1, Cost: 6,
		}, nil
	case "치료":
		if len(legacyInfoSpellNames) <= npcTalkDiseaseSpell || legacyInfoSpellNames[npcTalkDiseaseSpell] != name {
			return npcTalkCastSpec{}, ErrNPCTalkCastSpellUnavailable
		}
		return npcTalkCastSpec{
			Name: name, Spell: npcTalkDiseaseSpell, Flag: npcTalkDiseaseFlag, Timer: -1, Cost: 12, ClassGate: npcTalkCastClericOrInvincible,
		}, nil
	case "개안술":
		if len(legacyInfoSpellNames) <= npcTalkBlindSpell || legacyInfoSpellNames[npcTalkBlindSpell] != name {
			return npcTalkCastSpec{}, ErrNPCTalkCastSpellUnavailable
		}
		return npcTalkCastSpec{
			Name: name, Spell: npcTalkBlindSpell, Flag: npcTalkBlindFlag, Timer: -1, Cost: 12, ClassGate: npcTalkCastClericPaladinOrInvincible,
		}, nil
	default:
		// Keep the historical action-level error visible to callers that
		// already fail-closed every non-admitted CAST, while exposing the more
		// specific spell boundary to new callers.
		return npcTalkCastSpec{}, fmt.Errorf("%w: %w: %s", ErrNPCTalkActionUnavailable, ErrNPCTalkCastSpellUnavailable, name)
	}
}

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
	Response         string        `json:"response"`
	Broadcast        bool          `json:"broadcast"`
	TargetID         string        `json:"target_id,omitempty"`
	TargetName       string        `json:"target_name,omitempty"`
	Occurrence       int           `json:"occurrence,omitempty"`
	Topic            string        `json:"topic,omitempty"`
	EnemyAdded       bool          `json:"enemy_added,omitempty"`
	Action           *TalkAction   `json:"action,omitempty"`
	ActionTargetID   string        `json:"action_target_id,omitempty"`
	ActionSuppressed bool          `json:"action_suppressed,omitempty"`
	SpellName        string        `json:"spell_name,omitempty"`
	SpellTargetID    string        `json:"spell_target_id,omitempty"`
	SpellAttempted   bool          `json:"spell_attempted,omitempty"`
	SpellSucceeded   bool          `json:"spell_succeeded,omitempty"`
	SpellFailed      bool          `json:"spell_failed,omitempty"`
	SpellRoll        int           `json:"spell_roll,omitempty"`
	SpellChance      int           `json:"spell_chance,omitempty"`
	SpellInterval    int32         `json:"spell_interval,omitempty"`
	GiveObjectID     int16         `json:"give_object_id,omitempty"`
	GiveItemID       string        `json:"give_item_id,omitempty"`
	GiveItemName     string        `json:"give_item_name,omitempty"`
	GiveAttempted    bool          `json:"give_attempted,omitempty"`
	GiveGranted      bool          `json:"give_granted,omitempty"`
	GiveRejected     bool          `json:"give_rejected,omitempty"`
	GiveQuest        byte          `json:"give_quest,omitempty"`
	GiveQuestXP      int32         `json:"give_quest_xp,omitempty"`
	GiveRoll         int           `json:"give_roll,omitempty"`
	GiveEnchanted    bool          `json:"give_enchanted,omitempty"`
	Event            *NPCTalkEvent `json:"event,omitempty"`
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
	// TopicCatalogFound records that the canonical <name>-<level> file was
	// present for an MTALKS topic request. TopicFound is separate because C
	// responds with a shrug when a loaded file has no exact key.
	TopicCatalogFound bool
	TopicFound        bool
	TopicEntry        TalkTopic
	Action            TalkAction
	ActionTargetID    string
	ActionSuppressed  bool
	CastAction        TalkAction
	CastTargetID      string
	CastSpellName     string
	CastAttempted     bool
	CastSucceeded     bool
	CastFailed        bool
	CastRefused       bool
	CastNow           int32
	CastRoll          int
	CastChance        int
	CastInterval      int32
	GiveObjectID      int16
	GiveItemID        string
	GiveItemName      string
	GiveAttempted     bool
	GiveGranted       bool
	GiveRejected      bool
	GiveQuest         byte
	GiveQuestXP       int32
	GiveRoll          int
	GiveEnchanted     bool
	GiveRejectText    string
	GiveObject        LegacyObject

	before           State
	castNPCBefore    LegacyMonster
	castNPCAfter     LegacyMonster
	castTargetBefore PlayerState
	castTargetAfter  PlayerState
	actionNPCBefore  LegacyMonster
	actionNPCAfter   LegacyMonster
	giveTargetBefore PlayerState
	giveTargetAfter  PlayerState
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

// buildNPCTalkTopicEvent mirrors command8.c's topic branch for an exact
// catalog response. The question is sent only to room observers (the C
// broadcast uses the actor fd as its exclusion), while the NPC response is
// projected to both observers and the actor with the actor-specific wording.
func buildNPCTalkTopicEvent(actor LegacyMonster, actorID, npcID string, npc LegacyMonster, topic, response string, actions ...TalkAction) NPCTalkEvent {
	actorDisplay := actor.Name + "님"
	npcSubject := legacySubjectParticle(npc.Name)
	question := fmt.Sprintf("\n%s이 %s에게 \"%s\"에 관해 물어봅니다.\r\n", actorDisplay, npc.Name, topic)
	roomResponse := fmt.Sprintf("\n%s%s %s에게 \"%s\"라고 이야기합니다.\r\n", npc.Name, npcSubject, actorDisplay, response)
	actorResponse := fmt.Sprintf("\n%s%s 당신에게 \"%s\"라고 이야기합니다.\r\n", npc.Name, npcSubject, response)
	event := NPCTalkEvent{
		RoomID:         actor.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Name,
		NPCID:          npcID,
		NPCName:        npc.Name,
		Topic:          topic,
		ExcludeActorID: actorID,
		RoomText:       question + roomResponse,
		ActorText:      actorResponse,
		RoomMessages: []NPCTalkRoomMessage{
			{Text: question, ExcludeActorID: actorID},
			{Text: roomResponse, ExcludeActorID: actorID},
		},
		ActorMessages: []string{actorResponse},
	}
	if len(actions) != 0 && actions[0].Kind == TalkActionAttack {
		actorDisplayParticle := valueObjectParticle(actor.Name + "님")
		roomAttack := fmt.Sprintf("\n%s%s %s님%s 공격합니다.\n", npc.Name, npcSubject, actor.Name, actorDisplayParticle)
		actorAttack := fmt.Sprintf("\n%s%s 당신을 공격합니다.\n", npc.Name, npcSubject)
		event.RoomText += roomAttack
		event.ActorText += actorAttack
		event.RoomMessages = append(event.RoomMessages, NPCTalkRoomMessage{Text: roomAttack, ExcludeActorID: actorID})
		event.ActorMessages = append(event.ActorMessages, actorAttack)
	}
	return event
}

// appendNPCTalkActionEvent mirrors the action(crt_ptr, &cm) branch used by
// command8.c:talk_action.  The NPC is the action actor, while PLAYER means
// the talking player is the exact target.  We reuse the already closed
// action.c emote table instead of introducing a second social vocabulary.
// Targetless actions are broadcast to the player as well as other room
// occupants because the legacy broadcast excludes only the NPC descriptor.
func appendNPCTalkActionEvent(event *NPCTalkEvent, npc LegacyMonster, target PlayerState, action TalkAction) error {
	if event == nil {
		return fmt.Errorf("NPC talk action event missing")
	}
	if action.Kind != TalkActionAction {
		return fmt.Errorf("invalid NPC talk action kind")
	}
	if action.Target != "" && action.Target != "PLAYER" {
		return fmt.Errorf("%w: unsupported target %q", ErrNPCTalkActionUnavailable, action.Target)
	}
	spec, ok := emoteSpecs[strings.TrimSpace(action.Name)]
	if !ok {
		return fmt.Errorf("%w: unsupported action %q", ErrNPCTalkActionUnavailable, action.Name)
	}
	if !validNPCTalkName(npc.Name) || !validNPCTalkName(target.Body.Name) {
		return fmt.Errorf("%w: invalid action identity", ErrNPCTalkActionUnavailable)
	}
	// action.c's targetless-only branches ignore a supplied PLAYER target;
	// preserve that source behavior while keeping target resolution exact for
	// all actions that actually render a target.
	if action.Target == "PLAYER" && !spec.targetlessOnly {
		targetName := emotePlayerName(target.Body.Name)
		targetRef := emoteTargetRef(target.Body.Name, targetName, spec.particle)
		roomText := emoteRoom(fmt.Sprintf(spec.targetRoom, npc.Name, targetRef))
		actorText := emoteDirect(fmt.Sprintf(spec.targetTarget, npc.Name))
		event.RoomText += roomText
		event.ActorText += actorText
		event.RoomMessages = append(event.RoomMessages, NPCTalkRoomMessage{Text: roomText, ExcludeActorID: event.ActorID})
		event.ActorMessages = append(event.ActorMessages, actorText)
		return nil
	}
	roomText := emoteRoom(fmt.Sprintf(spec.soloRoom, npc.Name))
	event.RoomText += roomText
	// No ExcludeActorID is intentional: broadcast_rom excludes the NPC, not
	// the player who triggered the topic, for a targetless action.
	event.RoomMessages = append(event.RoomMessages, NPCTalkRoomMessage{Text: roomText})
	return nil
}

func npcTalkCastRoll(options *NPCTalkEffectOptions) (value int, err error) {
	if options == nil || options.Roll == nil {
		return 0, ErrNPCTalkCastRandomUnavailable
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("%w: random source panicked: %v", ErrNPCTalkCastRandomUnavailable, recovered)
		}
	}()
	value = options.Roll(1, 100)
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("NPC talk cast random value outside 1..100")
	}
	return value, nil
}

func npcTalkCastInterval(caster LegacyMonster, room RoomState) (int32, error) {
	if caster.Stats[3] > 63 {
		return 0, fmt.Errorf("NPC talk cast intelligence outside legacy table")
	}
	interval := int64(1200) + int64(legacyStatBonus[caster.Stats[3]])*600
	if interval < 300 {
		interval = 300
	}
	if caster.Class == 3 || caster.Class == 6 { // CLERIC or PALADIN
		interval += 60 * int64((int(caster.Level)+3)/4)
	}
	if flag(room.Resource.Flags[:], npcTalkRoomMagicExtend) {
		interval += 800
	}
	if interval < 0 || interval > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("NPC talk cast interval outside int32")
	}
	return int32(interval), nil
}

func npcTalkCastTargetAfter(target PlayerState, spec npcTalkCastSpec, now, interval int32) (PlayerState, error) {
	if spec.Timer < 0 {
		body := target.Body
		body.Flags[spec.Flag/8] &^= 1 << (spec.Flag % 8)
		target.Body = body
		return target, nil
	}
	if target.Items == nil || len(target.Body.Inventory) != 0 {
		return PlayerState{}, fmt.Errorf("%w: canonical target equipment required", ErrNPCTalkCastSpellUnavailable)
	}
	if err := target.Items.Validate(); err != nil {
		return PlayerState{}, fmt.Errorf("%w: target equipment invalid: %v", ErrNPCTalkCastSpellUnavailable, err)
	}
	body := target.Body
	body.Flags[spec.Flag/8] |= 1 << (spec.Flag % 8)
	body.Timers[spec.Timer] = LegacyTimer{LastTime: now, Interval: interval}
	stats, err := target.Items.CombatStats(body)
	if err != nil {
		return PlayerState{}, fmt.Errorf("%w: target combat stats unavailable: %v", ErrNPCTalkCastSpellUnavailable, err)
	}
	if spec.Flag == npcTalkBlessFlag {
		body.Thaco = byte(stats.Thaco)
	} else {
		body.Armor = byte(stats.Armor)
	}
	target.Body = body
	return target, nil
}

func appendNPCTalkCastEvent(event *NPCTalkEvent, npc, target LegacyMonster, spec npcTalkCastSpec, attempted, succeeded, failed, refused bool) {
	if event == nil {
		return
	}
	npcSubject := legacySubjectParticle(npc.Name)
	if refused {
		text := fmt.Sprintf("\n%s%s 당신에게 어떤 주문을 거는것을 거부했습니다.\n", npc.Name, npcSubject)
		event.ActorText += text
		event.ActorMessages = append(event.ActorMessages, text)
		return
	}
	if !attempted {
		text := fmt.Sprintf("\n%s%s 지금은 당신에게 주문을 걸어줄 수 없다고 사과합니다.\n", npc.Name, npcSubject)
		event.ActorText += text
		event.ActorMessages = append(event.ActorMessages, text)
		return
	}
	if failed || !succeeded {
		return
	}
	var roomText, actorText string
	switch spec.Flag {
	case npcTalkBlessFlag:
		roomText = fmt.Sprintf("\n%s%s %s의 머리에 한쪽손을 얹으며 성현진을 \n외웁니다.\n그의 머리에서 삼매광이 뿜어져 나와 성스러운 기운이 몸을\n휘감습니다.\n", npc.Name, npcSubject, target.Name)
		actorText = fmt.Sprintf("\n%s%s 당신의 머리에 한쪽손을 얹으며 성현진을 외웁니다.\n당신의 머리에서 삼매광이 뿜어져 나와 성스러운 기운이 몸을\n휘감습니다.\n", npc.Name, npcSubject)
	case npcTalkPoisonFlag:
		roomText = fmt.Sprintf("\n%s%s %s의 혈도를 짚으면서 해독 주문을 외웁니다.\n그의 손가락 끝으로 검은 독기운이 빠져나오는 것이 보입니다.\n", npc.Name, npcSubject, target.Name)
		actorText = fmt.Sprintf("\n%s%s 당신의 혈도를 짚으면서 해독 주문을 외웁니다.\n당신의 손가락 끝으로 독기운이 빠져나가는 것이 느껴집니다.\n", npc.Name, npcSubject)
	case npcTalkDiseaseFlag:
		roomText = fmt.Sprintf("\n%s%s %s의 혈도를 누르고 내공의 힘을 통해 치료를 시작합니다.\n그의 몸에 막혀 있던 혈이 풀리면서 차츰 활기를 띄기 시작합니다.\n", npc.Name, npcSubject, target.Name)
		actorText = fmt.Sprintf("\n%s%s 당신의 혈도를 누르고 내공의 힘을 통해 치료를 시작합니다.\n당신의 몸에 막혀 있던 혈이 풀리면서 차츰 활기를 띄기 시작합니다.\n", npc.Name, npcSubject)
	case npcTalkBlindFlag:
		roomText = fmt.Sprintf("\n%s%s %s의 이마에 개안부를 붙히고서 개안술 주문을 외웁니다.\n그의 감겼던 눈이 움찔거리다가 갑자기 확 뜹니다.\n", npc.Name, npcSubject, target.Name)
		actorText = fmt.Sprintf("\n%s%s 당신의 이마에 개안부를 붙히고서 주문을 외웁니다.\n감겼던 당신의 눈이 움찔거리다가 갑자기 밝아집니다.\n", npc.Name, npcSubject)
	default:
		roomText = fmt.Sprintf("\n%s%s %s의 몸에 수호인을 그리며 수호진의 주문을 걸었습니다.\n빛의 수호령들이 그의 주위를 둘러싸며 방어의 진을 형성했습니다.\n", npc.Name, npcSubject, target.Name)
		actorText = fmt.Sprintf("\n%s%s 당신의 몸에 수호인을 그리며 주문을 걸었습니다.\n빛의 수호령들이 당신의 주위를 둘러싸며 방어의 진을 형성했습니다.\n", npc.Name, npcSubject)
	}
	event.RoomText += roomText
	event.ActorText += actorText
	event.RoomMessages = append(event.RoomMessages, NPCTalkRoomMessage{Text: roomText, ExcludeActorID: event.ActorID})
	event.ActorMessages = append(event.ActorMessages, actorText)
}

// buildNPCTalkTopicMissEvent is the loaded-file/no-exact-key branch from
// command8.c. It deliberately remains a response projection rather than an
// action: the caller may add the existing MTLKAG enemy edge atomically.
func buildNPCTalkTopicMissEvent(actor LegacyMonster, actorID, npcID string, npc LegacyMonster, topic string) NPCTalkEvent {
	actorDisplay := actor.Name + "님"
	npcSubject := legacySubjectParticle(npc.Name)
	question := fmt.Sprintf("\n%s이 %s에게 \"%s\"에 관해 물어봅니다.\r\n", actorDisplay, npc.Name, topic)
	shrug := fmt.Sprintf("\n%s%s 어깨를 으쓱 거립니다.\r\n", npc.Name, npcSubject)
	return NPCTalkEvent{
		RoomID:         actor.RoomID,
		ActorID:        actorID,
		ActorName:      actor.Name,
		NPCID:          npcID,
		NPCName:        npc.Name,
		Topic:          topic,
		ExcludeActorID: actorID,
		RoomText:       question + shrug,
		ActorText:      shrug,
		RoomMessages: []NPCTalkRoomMessage{
			{Text: question, ExcludeActorID: actorID},
			{Text: shrug, ExcludeActorID: actorID},
		},
		ActorMessages: []string{shrug},
	}
}

func lookupNPCTalkTopic(catalog TalkCatalog, npc LegacyMonster, key string) (TalkTopic, bool, bool, error) {
	file, fileFound, err := catalog.Lookup(npc.Name, int(npc.Level))
	if err != nil {
		return TalkTopic{}, false, false, err
	}
	if !fileFound {
		return TalkTopic{}, false, false, nil
	}
	topic, found := file.Topic(key)
	return topic, found, true, nil
}

func unpackNPCTalkCatalog(catalogs []TalkCatalog) (*TalkCatalog, error) {
	if len(catalogs) > 1 {
		return nil, fmt.Errorf("NPC talk accepts at most one catalog")
	}
	if len(catalogs) == 0 {
		return nil, nil
	}
	catalog := catalogs[0]
	return &catalog, nil
}

func (s State) planNPCTalkCast(proposal *NPCTalkProposal, actor PlayerState, npc NPCState, room RoomState, options *NPCTalkEffectOptions) error {
	if proposal == nil || proposal.TopicEntry.Action.Kind != TalkActionCast {
		return nil
	}
	action := proposal.TopicEntry.Action
	if action.Target != "" && action.Target != "PLAYER" {
		return fmt.Errorf("%w: unsupported target %q", ErrNPCTalkCastSpellUnavailable, action.Target)
	}
	spec, err := npcTalkCastSpecFor(action.Name)
	if err != nil {
		return err
	}
	proposal.CastAction = action
	proposal.CastTargetID = proposal.ActorID
	proposal.CastSpellName = spec.Name
	proposal.castNPCBefore = npc.Body
	proposal.castNPCAfter = npc.Body
	proposal.castTargetBefore = actor
	proposal.castTargetAfter = actor
	if npcEnemyContains(npc.Enemies, EntityRef{Kind: "player", ID: proposal.ActorID}) {
		// talk_action refuses a non-offensive spell once the NPC already has the
		// player on its enemy list. The topic response is still a committed talk
		// event, and only the actor receives this refusal text.
		proposal.CastRefused = true
		return nil
	}
	if int16(npc.Body.MPCurrent) < spec.Cost || !flag(npc.Body.Spells[:], uint(spec.Spell)) || !npcTalkCastClassAllowed(npc.Body.Class, spec.ClassGate) {
		// Admitted spells print the generic apology when the NPC's MP or spell
		// bit gate leaves mpcur unchanged after talk_action.
		return nil
	}
	if _, err := npcTalkSpellChance(npc.Body); err != nil {
		return err
	}
	interval := int32(0)
	if spec.Timer >= 0 {
		interval, err = npcTalkCastInterval(npc.Body, room)
		if err != nil {
			return err
		}
	}
	if options == nil || options.Roll == nil {
		return ErrNPCTalkCastRandomUnavailable
	}
	if spec.Timer >= 0 {
		proposal.CastNow = options.Now
	}
	// Validate the full target/equipment boundary before consuming a random
	// draw. A cast that cannot recompute canonical combat stats must not produce
	// an otherwise unreplayable attempt.
	preview, err := npcTalkCastTargetAfter(actor, spec, options.Now, interval)
	if err != nil {
		return err
	}
	roll, err := npcTalkCastRoll(options)
	if err != nil {
		return err
	}
	chance, err := npcTalkSpellChance(npc.Body)
	if err != nil {
		return err
	}
	proposal.CastAttempted = true
	proposal.CastRoll = roll
	proposal.CastChance = chance
	proposal.CastInterval = interval
	proposal.castNPCAfter.MPCurrent -= spec.Cost
	if roll > chance {
		proposal.CastFailed = true
		return nil
	}
	proposal.CastSucceeded = true
	proposal.castTargetAfter = preview
	return nil
}

func (s State) planNPCTalkAction(proposal *NPCTalkProposal, actor PlayerState, npc NPCState) error {
	if proposal == nil || proposal.TopicEntry.Action.Kind != TalkActionAction {
		return nil
	}
	action := proposal.TopicEntry.Action
	if action.Target != "" && action.Target != "PLAYER" {
		return fmt.Errorf("%w: unsupported target %q", ErrNPCTalkActionUnavailable, action.Target)
	}
	if _, ok := emoteSpecs[strings.TrimSpace(action.Name)]; !ok {
		return fmt.Errorf("%w: unsupported action %q", ErrNPCTalkActionUnavailable, action.Name)
	}
	if !validNPCTalkName(actor.Body.Name) || !validNPCTalkName(npc.Body.Name) {
		return fmt.Errorf("%w: invalid action identity", ErrNPCTalkActionUnavailable)
	}
	proposal.Action = action
	proposal.ActionTargetID = ""
	if action.Target == "PLAYER" {
		proposal.ActionTargetID = proposal.ActorID
	}
	proposal.ActionSuppressed = flag(npc.Body.Flags[:], playerSilentStateFlag)
	proposal.actionNPCBefore = npc.Body
	proposal.actionNPCAfter = npc.Body
	// action.c clears PHIDDN on the NPC before checking PSILNC.  Keep that
	// ordering in the proposal even when a silent NPC produces no projection.
	proposal.actionNPCAfter.Flags[npcHiddenFlag/8] &^= 1 << (npcHiddenFlag % 8)
	return nil
}

func validateNPCTalkActionProposal(proposal NPCTalkProposal, actor PlayerState, npc NPCState) error {
	action := proposal.TopicEntry.Action
	if action.Kind != TalkActionAction || proposal.Action != action {
		return fmt.Errorf("invalid NPC talk action proposal")
	}
	if action.Target != "" && action.Target != "PLAYER" {
		return fmt.Errorf("%w: unsupported target %q", ErrNPCTalkActionUnavailable, action.Target)
	}
	if _, ok := emoteSpecs[strings.TrimSpace(action.Name)]; !ok {
		return fmt.Errorf("%w: unsupported action %q", ErrNPCTalkActionUnavailable, action.Name)
	}
	if action.Target == "PLAYER" {
		if proposal.ActionTargetID != proposal.ActorID {
			return fmt.Errorf("invalid NPC talk action target proposal")
		}
	} else if proposal.ActionTargetID != "" {
		return fmt.Errorf("invalid NPC talk action target proposal")
	}
	if !reflect.DeepEqual(proposal.actionNPCBefore, npc.Body) {
		return fmt.Errorf("NPC talk action target changed")
	}
	if proposal.ActionSuppressed != flag(npc.Body.Flags[:], playerSilentStateFlag) {
		return fmt.Errorf("NPC talk action silence changed")
	}
	expected := npc.Body
	expected.Flags[npcHiddenFlag/8] &^= 1 << (npcHiddenFlag % 8)
	if !reflect.DeepEqual(proposal.actionNPCAfter, expected) {
		return fmt.Errorf("NPC talk action state projection changed")
	}
	if !validNPCTalkName(actor.Body.Name) {
		return fmt.Errorf("%w: invalid action target identity", ErrNPCTalkActionUnavailable)
	}
	return nil
}

func npcTalkSpellChance(body LegacyMonster) (int, error) {
	// spell_fail in magic8.c uses the same class/INT table as readscroll and
	// consumes one 1..100 draw even for the DM/default success branch.
	return readScrollSpellChance(body)
}

func validateNPCTalkCastProposal(proposal NPCTalkProposal, actor PlayerState, npc NPCState, room RoomState) (npcTalkCastSpec, error) {
	action := proposal.TopicEntry.Action
	if action.Kind != TalkActionCast || proposal.CastAction != action || proposal.CastTargetID != proposal.ActorID {
		return npcTalkCastSpec{}, fmt.Errorf("invalid NPC talk cast action proposal")
	}
	if action.Target != "" && action.Target != "PLAYER" {
		return npcTalkCastSpec{}, fmt.Errorf("%w: unsupported target %q", ErrNPCTalkCastSpellUnavailable, action.Target)
	}
	spec, err := npcTalkCastSpecFor(action.Name)
	if err != nil {
		return npcTalkCastSpec{}, err
	}
	if proposal.CastSpellName != spec.Name || !reflect.DeepEqual(proposal.castNPCBefore, npc.Body) || !reflect.DeepEqual(proposal.castTargetBefore, actor) {
		return npcTalkCastSpec{}, fmt.Errorf("NPC talk cast target changed")
	}
	if proposal.CastRefused {
		if !npcEnemyContains(npc.Enemies, EntityRef{Kind: "player", ID: proposal.ActorID}) || proposal.CastAttempted || proposal.CastSucceeded || proposal.CastFailed || proposal.CastRoll != 0 || proposal.CastChance != 0 || proposal.CastInterval != 0 || proposal.CastNow != 0 || !reflect.DeepEqual(proposal.castNPCAfter, npc.Body) || !reflect.DeepEqual(proposal.castTargetAfter, actor) {
			return npcTalkCastSpec{}, fmt.Errorf("invalid NPC talk cast refusal proposal")
		}
		return spec, nil
	}
	if npcEnemyContains(npc.Enemies, EntityRef{Kind: "player", ID: proposal.ActorID}) {
		return npcTalkCastSpec{}, fmt.Errorf("NPC talk cast refusal changed")
	}
	known := flag(npc.Body.Spells[:], uint(spec.Spell))
	if !proposal.CastAttempted {
		if (int16(npc.Body.MPCurrent) >= spec.Cost && known && npcTalkCastClassAllowed(npc.Body.Class, spec.ClassGate)) || proposal.CastSucceeded || proposal.CastFailed || proposal.CastRoll != 0 || proposal.CastChance != 0 || proposal.CastInterval != 0 || proposal.CastNow != 0 || !reflect.DeepEqual(proposal.castNPCAfter, npc.Body) || !reflect.DeepEqual(proposal.castTargetAfter, actor) {
			return npcTalkCastSpec{}, fmt.Errorf("invalid NPC talk cast no-attempt proposal")
		}
		return spec, nil
	}
	if !known || int16(npc.Body.MPCurrent) < spec.Cost || !npcTalkCastClassAllowed(npc.Body.Class, spec.ClassGate) || proposal.CastRoll < 1 || proposal.CastRoll > 100 {
		return npcTalkCastSpec{}, fmt.Errorf("NPC talk cast gate changed")
	}
	chance, err := npcTalkSpellChance(npc.Body)
	if err != nil {
		return npcTalkCastSpec{}, err
	}
	interval := int32(0)
	if spec.Timer >= 0 {
		interval, err = npcTalkCastInterval(npc.Body, room)
		if err != nil {
			return npcTalkCastSpec{}, err
		}
	}
	if proposal.CastChance != chance || proposal.CastInterval != interval || proposal.CastSucceeded == proposal.CastFailed || proposal.CastSucceeded != (proposal.CastRoll <= chance) || proposal.CastFailed != (proposal.CastRoll > chance) {
		return npcTalkCastSpec{}, fmt.Errorf("NPC talk cast outcome changed")
	}
	expectedNPC := npc.Body
	expectedNPC.MPCurrent -= spec.Cost
	expectedTarget := actor
	if proposal.CastSucceeded {
		expectedTarget, err = npcTalkCastTargetAfter(actor, spec, proposal.CastNow, interval)
		if err != nil {
			return npcTalkCastSpec{}, err
		}
	}
	if !reflect.DeepEqual(proposal.castNPCAfter, expectedNPC) || !reflect.DeepEqual(proposal.castTargetAfter, expectedTarget) {
		return npcTalkCastSpec{}, fmt.Errorf("NPC talk cast state projection changed")
	}
	return spec, nil
}

// PlanNPCTalkProposal is the source-backed no-topic command8.c:talk boundary.
// A topic branch receives its immutable TalkCatalog explicitly. A non-MTALKS
// NPC follows C's `cmnd->num == 2 || !MTALKS` branch and gives its existing
// no-topic response even when the caller supplied a topic token.
//
// The variadic form is backwards-compatible with the already connected
// no-topic session path while allowing world callers to inject exactly one
// catalog. PlanNPCTalkProposalWithCatalog is the explicit spelling.
func (s State) PlanNPCTalkProposal(actorID, targetName string, occurrence int, topic string, catalogs ...TalkCatalog) (NPCTalkProposal, error) {
	catalog, err := unpackNPCTalkCatalog(catalogs)
	if err != nil {
		return NPCTalkProposal{}, err
	}
	return s.planNPCTalkProposal(actorID, targetName, occurrence, topic, catalog, nil)
}

func (s State) PlanNPCTalkProposalWithCatalog(actorID, targetName string, occurrence int, topic string, catalog TalkCatalog) (NPCTalkProposal, error) {
	return s.planNPCTalkProposal(actorID, targetName, occurrence, topic, &catalog, nil)
}

// PlanNPCTalkProposalWithEffectOptions is the explicit host-injection form
// for catalog actions that need a clock or deterministic RNG. Plain topic
// callers keep the older API and therefore cannot accidentally execute CAST.
func (s State) PlanNPCTalkProposalWithEffectOptions(actorID, targetName string, occurrence int, topic string, catalog TalkCatalog, options NPCTalkEffectOptions) (NPCTalkProposal, error) {
	return s.planNPCTalkProposal(actorID, targetName, occurrence, topic, &catalog, &options)
}

func (s State) planNPCTalkProposal(actorID, targetName string, occurrence int, topic string, catalog *TalkCatalog, effectOptions *NPCTalkEffectOptions) (NPCTalkProposal, error) {
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
		if catalog == nil {
			return NPCTalkProposal{}, ErrNPCTalkTopicsUnavailable
		}
		entry, found, fileFound, err := lookupNPCTalkTopic(*catalog, npc.Body, topic)
		if err != nil {
			return NPCTalkProposal{}, fmt.Errorf("%w: %v", ErrNPCTalkTopicsUnavailable, err)
		}
		if !fileFound {
			return NPCTalkProposal{}, ErrNPCTalkTopicsUnavailable
		}
		proposal.TopicCatalogFound = true
		proposal.TopicFound = found
		proposal.TopicEntry = entry
		if found && entry.Action.Kind != TalkActionNone && entry.Action.Kind != TalkActionAttack && entry.Action.Kind != TalkActionAction && entry.Action.Kind != TalkActionCast && entry.Action.Kind != TalkActionGive {
			return NPCTalkProposal{}, fmt.Errorf("%w: %s", ErrNPCTalkActionUnavailable, entry.Action.Kind.String())
		}
	}
	proposal.TargetName = npc.Body.Name
	proposal.ClearHidden = true
	proposal.AddEnemy = flag(npc.Body.Flags[:], npcTalkAggressiveFlag) && (!proposal.TopicCatalogFound || !proposal.TopicFound)
	if proposal.TopicCatalogFound && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionAttack {
		// command8.c's talk_action(ATTACK) adds the player to the NPC's enemy
		// list even when the NPC is not globally MTLKAG-aggressive.
		proposal.AddEnemy = true
	}
	if proposal.AddEnemy && npc.Enemies == nil {
		return NPCTalkProposal{}, fmt.Errorf("NPC talk enemy relations unresolved")
	}
	if proposal.TopicCatalogFound && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionCast {
		if err := s.planNPCTalkCast(&proposal, actor, s.NPCs[targetID], s.Rooms[actor.Body.RoomID], effectOptions); err != nil {
			return NPCTalkProposal{}, err
		}
	}
	if proposal.TopicCatalogFound && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionAction {
		if err := s.planNPCTalkAction(&proposal, actor, s.NPCs[targetID]); err != nil {
			return NPCTalkProposal{}, err
		}
	}
	if proposal.TopicCatalogFound && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionGive {
		if err := s.planNPCTalkGive(&proposal, actor, s.NPCs[targetID], effectOptions); err != nil {
			return NPCTalkProposal{}, err
		}
	}
	event := buildNPCTalkEvent(actor.Body, actorID, targetID, npc.Body, "")
	if proposal.TopicCatalogFound {
		if proposal.TopicFound {
			event = buildNPCTalkTopicEvent(actor.Body, actorID, targetID, npc.Body, topic, proposal.TopicEntry.Response, proposal.TopicEntry.Action)
			if proposal.TopicEntry.Action.Kind == TalkActionCast {
				castSpec, specErr := npcTalkCastSpecFor(proposal.CastSpellName)
				if specErr != nil {
					return NPCTalkProposal{}, specErr
				}
				appendNPCTalkCastEvent(&event, npc.Body, actor.Body, castSpec, proposal.CastAttempted, proposal.CastSucceeded, proposal.CastFailed, proposal.CastRefused)
			}
			if proposal.TopicEntry.Action.Kind == TalkActionAction && !proposal.ActionSuppressed {
				if err := appendNPCTalkActionEvent(&event, npc.Body, actor, proposal.Action); err != nil {
					return NPCTalkProposal{}, err
				}
			}
			if proposal.TopicEntry.Action.Kind == TalkActionGive {
				appendNPCTalkGiveEvent(&event, npc.Body, actor.Body, proposal.GiveObject, proposal.GiveGranted, proposal.GiveRejectText, proposal.GiveQuestXP)
			}
		} else {
			event = buildNPCTalkTopicMissEvent(actor.Body, actorID, targetID, npc.Body, topic)
		}
	}
	proposal.ExpectedEvent = &event
	proposal.Response = event.ActorText
	proposal.Broadcast = true
	return proposal, nil
}

// PlanNPCTalkWithOccurrence is the reducer-shaped convenience API used by
// pure callers that need the committed candidate and durable result together.
func (s State) PlanNPCTalkWithOccurrence(actorID, targetName string, occurrence int, topic string, catalogs ...TalkCatalog) (State, NPCTalkResult, error) {
	proposal, err := s.PlanNPCTalkProposal(actorID, targetName, occurrence, topic, catalogs...)
	if err != nil {
		return State{}, NPCTalkResult{}, err
	}
	return s.ApplyNPCTalk(proposal, catalogs...)
}

func (s State) PlanNPCTalkWithOccurrenceAndCatalog(actorID, targetName string, occurrence int, topic string, catalog TalkCatalog) (State, NPCTalkResult, error) {
	return s.PlanNPCTalkWithOccurrence(actorID, targetName, occurrence, topic, catalog)
}

// PlanNPCTalk uses the canonical default occurrence one. It is intentionally
// separate from the parser so server-side callers cannot accidentally treat a
// zero occurrence as a legacy wildcard.
func (s State) PlanNPCTalk(actorID, targetName, topic string, catalogs ...TalkCatalog) (State, NPCTalkResult, error) {
	return s.PlanNPCTalkWithOccurrence(actorID, targetName, 1, topic, catalogs...)
}

func (s State) PlanNPCTalkWithCatalog(actorID, targetName, topic string, catalog TalkCatalog) (State, NPCTalkResult, error) {
	return s.PlanNPCTalk(actorID, targetName, topic, catalog)
}

// ApplyNPCTalk atomically applies the proposal against the exact snapshot on
// which it was planned. PHIDDN release and the optional MTALKAG enemy edge are
// committed together; any stale identity, topic gate, or relation error
// rejects the whole candidate without exposing a partial state.
func (s State) ApplyNPCTalk(proposal NPCTalkProposal, catalogs ...TalkCatalog) (State, NPCTalkResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, NPCTalkResult{}, err
	}
	catalog, err := unpackNPCTalkCatalog(catalogs)
	if err != nil {
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
	mtalksTopic := proposal.Topic != "" && flag(npc.Flags[:], npcTalkFlag)
	if mtalksTopic {
		if !proposal.TopicCatalogFound {
			return State{}, NPCTalkResult{}, ErrNPCTalkTopicsUnavailable
		}
		if proposal.TopicFound {
			if proposal.TopicEntry.Key != proposal.Topic || !validNPCTalkText(proposal.TopicEntry.Response) {
				return State{}, NPCTalkResult{}, fmt.Errorf("invalid NPC talk topic proposal")
			}
			if proposal.TopicEntry.Action.Kind != TalkActionNone && proposal.TopicEntry.Action.Kind != TalkActionAttack && proposal.TopicEntry.Action.Kind != TalkActionAction && proposal.TopicEntry.Action.Kind != TalkActionCast && proposal.TopicEntry.Action.Kind != TalkActionGive {
				return State{}, NPCTalkResult{}, fmt.Errorf("%w: %s", ErrNPCTalkActionUnavailable, proposal.TopicEntry.Action.Kind.String())
			}
		} else if proposal.TopicEntry != (TalkTopic{}) {
			return State{}, NPCTalkResult{}, fmt.Errorf("invalid missing NPC talk topic proposal")
		}
		if catalog != nil {
			entry, found, fileFound, lookupErr := lookupNPCTalkTopic(*catalog, npc, proposal.Topic)
			if lookupErr != nil || !fileFound || found != proposal.TopicFound || (found && entry != proposal.TopicEntry) {
				if lookupErr != nil {
					return State{}, NPCTalkResult{}, fmt.Errorf("%w: %v", ErrNPCTalkTopicsUnavailable, lookupErr)
				}
				return State{}, NPCTalkResult{}, fmt.Errorf("%w: topic catalog changed", ErrNPCTalkTopicsUnavailable)
			}
		}
	} else if proposal.TopicCatalogFound || proposal.TopicFound || proposal.TopicEntry != (TalkTopic{}) {
		return State{}, NPCTalkResult{}, fmt.Errorf("invalid NPC talk topic branch")
	}
	currentNPC := s.NPCs[targetID]
	addEnemy := flag(npc.Flags[:], npcTalkAggressiveFlag)
	if mtalksTopic && proposal.TopicFound {
		addEnemy = proposal.TopicEntry.Action.Kind == TalkActionAttack
	}
	if addEnemy != proposal.AddEnemy || (addEnemy && currentNPC.Enemies == nil) {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk enemy relation changed")
	}
	var castSpec npcTalkCastSpec
	if mtalksTopic && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionCast {
		castSpec, err = validateNPCTalkCastProposal(proposal, actor, currentNPC, room)
		if err != nil {
			return State{}, NPCTalkResult{}, err
		}
	}
	if mtalksTopic && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionAction {
		if err := validateNPCTalkActionProposal(proposal, actor, currentNPC); err != nil {
			return State{}, NPCTalkResult{}, err
		}
	}
	if mtalksTopic && proposal.TopicFound && proposal.TopicEntry.Action.Kind == TalkActionGive {
		if err := validateNPCTalkGiveProposal(proposal, actor, currentNPC); err != nil {
			return State{}, NPCTalkResult{}, err
		}
	}
	expected := buildNPCTalkEvent(actor.Body, proposal.ActorID, targetID, npc, "")
	if mtalksTopic {
		if proposal.TopicFound {
			expected = buildNPCTalkTopicEvent(actor.Body, proposal.ActorID, targetID, npc, proposal.Topic, proposal.TopicEntry.Response, proposal.TopicEntry.Action)
			if proposal.TopicEntry.Action.Kind == TalkActionCast {
				appendNPCTalkCastEvent(&expected, npc, actor.Body, castSpec, proposal.CastAttempted, proposal.CastSucceeded, proposal.CastFailed, proposal.CastRefused)
			}
			if proposal.TopicEntry.Action.Kind == TalkActionAction && !proposal.ActionSuppressed {
				if err := appendNPCTalkActionEvent(&expected, npc, actor, proposal.Action); err != nil {
					return State{}, NPCTalkResult{}, err
				}
			}
			if proposal.TopicEntry.Action.Kind == TalkActionGive {
				appendNPCTalkGiveEvent(&expected, npc, actor.Body, proposal.GiveObject, proposal.GiveGranted, proposal.GiveRejectText, proposal.GiveQuestXP)
			}
		} else {
			expected = buildNPCTalkTopicMissEvent(actor.Body, proposal.ActorID, targetID, npc, proposal.Topic)
		}
	}
	if proposal.ExpectedEvent == nil || !reflect.DeepEqual(*proposal.ExpectedEvent, expected) || proposal.Response != expected.ActorText || !proposal.ClearHidden || !proposal.Broadcast {
		return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk projection changed")
	}

	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	if proposal.TopicEntry.Action.Kind == TalkActionCast && proposal.CastSucceeded {
		// Keep the clone-owned item graph and connection-local player slices;
		// only the spell-mutated body crosses the proposal boundary.
		nextActor.Body = proposal.castTargetAfter.Body
		nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
	}
	next.Players[proposal.ActorID] = nextActor
	if proposal.TopicEntry.Action.Kind == TalkActionCast && (proposal.CastAttempted || proposal.CastRefused) {
		nextNPC := next.NPCs[targetID]
		nextNPC.Body = proposal.castNPCAfter
		next.NPCs[targetID] = nextNPC
	}
	if proposal.TopicEntry.Action.Kind == TalkActionAction {
		nextNPC := next.NPCs[targetID]
		nextNPC.Body = proposal.actionNPCAfter
		next.NPCs[targetID] = nextNPC
	}
	if proposal.TopicEntry.Action.Kind == TalkActionGive && proposal.GiveGranted {
		// Keep the clone-owned connection-local fields from nextActor, while
		// replacing only the item graph and quest/proficiency body values fixed
		// during planning.
		nextActor.Body = proposal.giveTargetAfter.Body
		if proposal.giveTargetAfter.Items == nil {
			return State{}, NPCTalkResult{}, fmt.Errorf("NPC talk give target inventory disappeared")
		}
		nextActor.Body.Flags[playerHiddenStateFlag/8] &^= 1 << (playerHiddenStateFlag % 8)
		items := proposal.giveTargetAfter.Items.clone()
		nextActor.Items = &items
		next.Players[proposal.ActorID] = nextActor
	}
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
	if proposal.TopicEntry.Action.Kind == TalkActionAction {
		action := proposal.Action
		result.Action = &action
		result.ActionTargetID = proposal.ActionTargetID
		result.ActionSuppressed = proposal.ActionSuppressed
	}
	if proposal.TopicEntry.Action.Kind == TalkActionCast {
		action := proposal.CastAction
		result.Action = &action
		result.SpellName = proposal.CastSpellName
		result.SpellTargetID = proposal.CastTargetID
		result.SpellAttempted = proposal.CastAttempted
		result.SpellSucceeded = proposal.CastSucceeded
		result.SpellFailed = proposal.CastFailed
		result.SpellRoll = proposal.CastRoll
		result.SpellChance = proposal.CastChance
		result.SpellInterval = proposal.CastInterval
	}
	if proposal.TopicEntry.Action.Kind == TalkActionGive {
		action := proposal.TopicEntry.Action
		result.Action = &action
		result.GiveObjectID = proposal.GiveObjectID
		result.GiveItemID = proposal.GiveItemID
		result.GiveItemName = proposal.GiveItemName
		result.GiveAttempted = proposal.GiveAttempted
		result.GiveGranted = proposal.GiveGranted
		result.GiveRejected = proposal.GiveRejected
		result.GiveQuest = proposal.GiveQuest
		result.GiveQuestXP = proposal.GiveQuestXP
		result.GiveRoll = proposal.GiveRoll
		result.GiveEnchanted = proposal.GiveEnchanted
	}
	return next, result, nil
}

func (s State) ApplyNPCTalkWithCatalog(proposal NPCTalkProposal, catalog TalkCatalog) (State, NPCTalkResult, error) {
	return s.ApplyNPCTalk(proposal, catalog)
}

// ApplyNPCTalkProposal is a descriptive alias for callers that name the
// reducer by its explicit proposal boundary.
func (s State) ApplyNPCTalkProposal(proposal NPCTalkProposal, catalogs ...TalkCatalog) (State, NPCTalkResult, error) {
	return s.ApplyNPCTalk(proposal, catalogs...)
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
func (s State) RoomNPCTalkEvent(actorID, targetID, topic string, catalogs ...TalkCatalog) (NPCTalkEvent, bool, error) {
	if err := s.Validate(); err != nil {
		return NPCTalkEvent{}, false, err
	}
	catalog, err := unpackNPCTalkCatalog(catalogs)
	if err != nil {
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
		if catalog == nil {
			return NPCTalkEvent{}, false, ErrNPCTalkTopicsUnavailable
		}
		entry, found, fileFound, lookupErr := lookupNPCTalkTopic(*catalog, npc, topic)
		if lookupErr != nil {
			return NPCTalkEvent{}, false, fmt.Errorf("%w: %v", ErrNPCTalkTopicsUnavailable, lookupErr)
		}
		if !fileFound {
			return NPCTalkEvent{}, false, ErrNPCTalkTopicsUnavailable
		}
		if found {
			if entry.Action.Kind == TalkActionGive {
				// The gift graph, allocator IDs, enchant draw, and quest outcome
				// are receipt-bound. A post-state projection cannot recover those
				// facts without replaying an external catalog/RNG dependency.
				return NPCTalkEvent{}, false, ErrNPCTalkGiveProjectionUnavailable
			}
			if entry.Action.Kind == TalkActionCast {
				// CAST includes an RNG outcome and a caster/target mutation that
				// is intentionally carried by the durable receipt. Reconstructing
				// it from the post-state would be ambiguous when the same effect
				// already existed, so callers must use the committed receipt event.
				return NPCTalkEvent{}, false, ErrNPCTalkCastProjectionUnavailable
			}
			if entry.Action.Kind != TalkActionNone && entry.Action.Kind != TalkActionAttack && entry.Action.Kind != TalkActionAction {
				return NPCTalkEvent{}, false, fmt.Errorf("%w: %s", ErrNPCTalkActionUnavailable, entry.Action.Kind.String())
			}
			event := buildNPCTalkTopicEvent(actor.Body, actorID, targetID, npc, topic, entry.Response, entry.Action)
			if entry.Action.Kind == TalkActionAction && !flag(npc.Flags[:], playerSilentStateFlag) {
				if err := appendNPCTalkActionEvent(&event, npc, actor, entry.Action); err != nil {
					return NPCTalkEvent{}, false, err
				}
			}
			return event, true, nil
		}
		return buildNPCTalkTopicMissEvent(actor.Body, actorID, targetID, npc, topic), true, nil
	}
	event := buildNPCTalkEvent(actor.Body, actorID, targetID, npc, "")
	return event, true, nil
}

// NPCTalkEventForName resolves the exact positive-occurrence selector against
// committed state and derives a recipient projection without trusting a client
// supplied NPC ID.
func (s State) NPCTalkEventForName(actorID, targetName string, occurrence int, topic string, catalogs ...TalkCatalog) (NPCTalkEvent, bool, error) {
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
	return s.RoomNPCTalkEvent(actorID, targetID, topic, catalogs...)
}

func validNPCTalkName(name string) bool {
	return ValidateNPCTalkName(name) == nil
}
