package world

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const npcTalkGiveEnchantFlag = 21 // ORENCH

func parseNPCTalkGiveObjectID(raw string) (int16, error) {
	if raw == "" {
		return 0, fmt.Errorf("%w: object number is empty", ErrNPCTalkGiveObjectUnavailable)
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%w: object number must be decimal", ErrNPCTalkGiveObjectUnavailable)
		}
	}
	value, err := strconv.ParseInt(raw, 10, 16)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%w: object number outside 1..32767", ErrNPCTalkGiveObjectUnavailable)
	}
	return int16(value), nil
}

func validNPCTalkGiveObject(object LegacyObject) error {
	if object.Name == "" || !utf8.ValidString(object.Name) || strings.TrimSpace(object.Name) != object.Name {
		return fmt.Errorf("object name is invalid")
	}
	for _, r := range object.Name {
		if unicode.IsControl(r) {
			return fmt.Errorf("object name contains control text")
		}
	}
	if object.Weight < 0 {
		return fmt.Errorf("object weight is negative")
	}
	var visit func(LegacyObject, int) error
	visit = func(current LegacyObject, depth int) error {
		if depth > 64 {
			return fmt.Errorf("object tree too deep")
		}
		if current.Name == "" || !utf8.ValidString(current.Name) || strings.TrimSpace(current.Name) != current.Name {
			return fmt.Errorf("object tree name is invalid")
		}
		for _, r := range current.Name {
			if unicode.IsControl(r) {
				return fmt.Errorf("object tree name contains control text")
			}
		}
		if current.Weight < 0 {
			return fmt.Errorf("object tree weight is negative")
		}
		for _, child := range current.Contents {
			if err := visit(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(object, 0)
}

func npcTalkGiveRoll(options *NPCTalkEffectOptions) (value int, err error) {
	if options == nil || options.Roll == nil {
		return 0, ErrNPCTalkGiveRandomUnavailable
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("%w: random source panicked: %v", ErrNPCTalkGiveRandomUnavailable, recovered)
		}
	}()
	value = options.Roll(1, 100)
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("%w: random value outside 1..100", ErrNPCTalkGiveRandomUnavailable)
	}
	return value, nil
}

// npcTalkGiveAdmission performs the source order after load_obj/rand_enchant:
// capacity is checked before the quest duplicate gate. A semantic rejection
// is returned as text so the already-delivered talk response can be committed;
// malformed or unresolved canonical state remains an error.
func npcTalkGiveAdmission(actor PlayerState, object LegacyObject) (quest byte, questXP int32, rejectText string, err error) {
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return 0, 0, "", ErrNPCTalkGiveInventoryUnavailable
	}
	if err := actor.Items.Validate(); err != nil {
		return 0, 0, "", fmt.Errorf("%w: %v", ErrNPCTalkGiveInventoryUnavailable, err)
	}
	rootWeight, err := merchantObjectWeight(object)
	if err != nil {
		return 0, 0, "", fmt.Errorf("%w: %v", ErrNPCTalkGiveObjectUnavailable, err)
	}
	carriedWeight, err := actor.Items.Weight()
	if err != nil {
		return 0, 0, "", err
	}
	capacity, err := actor.Items.CapacityCount()
	if err != nil {
		return 0, 0, "", err
	}
	if carriedWeight < 0 || rootWeight < 0 || int64(carriedWeight)+int64(rootWeight) > int64(maxPlayerWeight(actor.Body)) || capacity > 150 {
		return 0, 0, "당신은 더이상 가질 수 없습니다.\n", nil
	}
	if object.Quest == 0 {
		return 0, 0, "", nil
	}
	index := int(object.Quest) - 1
	if index < 0 || index >= len(actor.Body.Quests)*8 {
		return 0, 0, "", fmt.Errorf("%w: quest %d outside canonical range", ErrNPCTalkGiveObjectUnavailable, object.Quest)
	}
	questXP, ok := npcQuestExperience(object.Quest)
	if !ok {
		return 0, 0, "", fmt.Errorf("%w: quest %d has no experience", ErrNPCTalkGiveObjectUnavailable, object.Quest)
	}
	if flag(actor.Body.Quests[:], uint(index)) {
		return object.Quest, questXP, "당신은 그것을 가질 수 없습니다. 당신은 이미 이 임무를 달성했습니다.\n", nil
	}
	return object.Quest, questXP, "", nil
}

func appendNPCTalkGiveEvent(event *NPCTalkEvent, npc, actor LegacyMonster, object LegacyObject, granted bool, rejectText string, questXP int32) {
	if event == nil {
		return
	}
	if !granted {
		if rejectText != "" {
			event.ActorText += rejectText
			event.ActorMessages = append(event.ActorMessages, rejectText)
		}
		return
	}
	if questXP > 0 {
		questText := "임무 달성!  그것을 버리지 마십시요.\n"
		experienceText := fmt.Sprintf("당신은 경험치 %d를 얻었습니다.\n", questXP)
		event.ActorText += questText + experienceText
		event.ActorMessages = append(event.ActorMessages, questText, experienceText)
	}
	npcSubject := legacySubjectParticle(npc.Name)
	objectParticle := valueObjectParticle(object.Name)
	actorText := fmt.Sprintf("\n%s%s 당신에게 %s%s 줍니다\n", npc.Name, npcSubject, object.Name, objectParticle)
	roomText := fmt.Sprintf("\n%s%s %s에게 %s%s 줍니다.\n", npc.Name, npcSubject, actor.Name, object.Name, objectParticle)
	event.ActorText += actorText
	event.ActorMessages = append(event.ActorMessages, actorText)
	event.RoomText += roomText
	event.RoomMessages = append(event.RoomMessages, NPCTalkRoomMessage{Text: roomText, ExcludeActorID: event.ActorID})
}

func (s State) planNPCTalkGive(proposal *NPCTalkProposal, actor PlayerState, npc NPCState, options *NPCTalkEffectOptions) error {
	if proposal == nil || proposal.TopicEntry.Action.Kind != TalkActionGive {
		return nil
	}
	action := proposal.TopicEntry.Action
	if action.Target != "" {
		return fmt.Errorf("%w: GIVE target %q is unsupported", ErrNPCTalkActionUnavailable, action.Target)
	}
	if options == nil || options.ObjectCatalog == nil {
		return fmt.Errorf("%w: %w", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveObjectUnavailable)
	}
	objectID, err := parseNPCTalkGiveObjectID(action.Name)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNPCTalkActionUnavailable, err)
	}
	object, err := options.ObjectCatalog.Object(objectID)
	if err != nil {
		return fmt.Errorf("%w: %w: %v", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveObjectUnavailable, err)
	}
	if err := validNPCTalkGiveObject(object); err != nil {
		return fmt.Errorf("%w: %w: %v", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveObjectUnavailable, err)
	}
	proposal.GiveObjectID = objectID
	proposal.GiveAttempted = true
	proposal.giveTargetBefore = actor
	proposal.giveTargetAfter = actor
	if flag(object.Flags[:], npcTalkGiveEnchantFlag) {
		roll, err := npcTalkGiveRoll(options)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNPCTalkActionUnavailable, err)
		}
		object, err = EnchantObject(object, func(int, int) int { return roll })
		if err != nil {
			return fmt.Errorf("%w: %w: %v", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveObjectUnavailable, err)
		}
		proposal.GiveRoll = roll
		proposal.GiveEnchanted = true
	}
	proposal.GiveObject = object
	quest, questXP, rejectText, err := npcTalkGiveAdmission(actor, object)
	if err != nil {
		return err
	}
	proposal.GiveQuest, proposal.GiveQuestXP, proposal.GiveRejectText = quest, questXP, rejectText
	if rejectText != "" {
		proposal.GiveRejected = true
		return nil
	}
	if options.Allocate == nil {
		return fmt.Errorf("%w: %w", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveAllocatorUnavailable)
	}
	used := merchantItemIDs(s)
	uniqueAllocate := func() (string, error) {
		id, err := options.Allocate()
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("empty NPC talk give item ID")
		}
		if _, exists := used[id]; exists {
			return "", fmt.Errorf("duplicate NPC talk give item ID")
		}
		used[id] = struct{}{}
		return id, nil
	}
	reward, err := ImportItems([]LegacyObject{object}, uniqueAllocate)
	if err != nil {
		return fmt.Errorf("%w: %w: %v", ErrNPCTalkActionUnavailable, ErrNPCTalkGiveAllocatorUnavailable, err)
	}
	plan, err := TransferItemRoots(reward, *actor.Items, reward.Inventory)
	if err != nil {
		return err
	}
	proposal.GiveItemID = reward.Inventory[0]
	proposal.GiveItemName = object.Name
	proposal.GiveGranted = true
	proposal.giveTargetAfter.Items = &plan.Destination
	if quest != 0 {
		index := uint(quest - 1)
		proposal.giveTargetAfter.Body.Quests[index/8] |= 1 << (index % 8)
		if int64(proposal.giveTargetAfter.Body.Experience)+int64(questXP) > int64(^uint32(0)>>1) {
			return fmt.Errorf("NPC talk give quest experience overflow")
		}
		proposal.giveTargetAfter.Body.Experience += questXP
		if err := addUnassignedProficiency(&proposal.giveTargetAfter.Body, questXP); err != nil {
			return err
		}
	}
	return nil
}

func validateNPCTalkGiveProposal(proposal NPCTalkProposal, actor PlayerState, npc NPCState) error {
	if proposal.TopicEntry.Action.Kind != TalkActionGive || proposal.TopicEntry.Action.Target != "" || !proposal.GiveAttempted || proposal.GiveObjectID < 1 || proposal.GiveObject.Name == "" {
		return fmt.Errorf("invalid NPC talk give proposal")
	}
	if err := validNPCTalkGiveObject(proposal.GiveObject); err != nil || proposal.giveTargetBefore.Items == nil || !reflect.DeepEqual(proposal.giveTargetBefore, actor) {
		return fmt.Errorf("NPC talk give target changed")
	}
	if proposal.GiveEnchanted {
		if proposal.GiveRoll < 1 || proposal.GiveRoll > 100 {
			return fmt.Errorf("invalid NPC talk give enchant roll")
		}
	} else if proposal.GiveRoll != 0 {
		return fmt.Errorf("invalid NPC talk give random state")
	}
	quest, questXP, rejectText, err := npcTalkGiveAdmission(actor, proposal.GiveObject)
	if err != nil {
		return err
	}
	if quest != proposal.GiveQuest || questXP != proposal.GiveQuestXP || rejectText != proposal.GiveRejectText {
		return fmt.Errorf("NPC talk give admission changed")
	}
	if proposal.GiveRejected {
		if proposal.GiveGranted || proposal.GiveItemID != "" || proposal.GiveItemName != "" || rejectText == "" || !reflect.DeepEqual(proposal.giveTargetAfter, actor) {
			return fmt.Errorf("invalid NPC talk give rejection proposal")
		}
		return nil
	}
	if !proposal.GiveGranted || proposal.GiveItemID == "" || proposal.GiveItemName != proposal.GiveObject.Name || rejectText != "" || proposal.giveTargetAfter.Items == nil {
		return fmt.Errorf("invalid NPC talk give grant proposal")
	}
	if err := proposal.giveTargetAfter.Items.Validate(); err != nil {
		return fmt.Errorf("invalid NPC talk give target inventory: %v", err)
	}
	item, ok := proposal.giveTargetAfter.Items.Items[proposal.GiveItemID]
	rootObject := proposal.GiveObject
	rootObject.Contents = nil
	if !ok || !reflect.DeepEqual(item.Object, rootObject) || !containsString(proposal.giveTargetAfter.Items.Inventory, proposal.GiveItemID) {
		return fmt.Errorf("NPC talk give item projection changed")
	}
	if proposal.GiveQuest == 0 {
		if !reflect.DeepEqual(proposal.giveTargetAfter.Body, actor.Body) {
			return fmt.Errorf("NPC talk give body changed")
		}
	} else {
		expected := actor.Body
		index := uint(proposal.GiveQuest - 1)
		expected.Quests[index/8] |= 1 << (index % 8)
		if int64(expected.Experience)+int64(proposal.GiveQuestXP) > int64(^uint32(0)>>1) {
			return fmt.Errorf("NPC talk give quest experience overflow")
		}
		expected.Experience += proposal.GiveQuestXP
		if err := addUnassignedProficiency(&expected, proposal.GiveQuestXP); err != nil {
			return err
		}
		if !reflect.DeepEqual(proposal.giveTargetAfter.Body, expected) {
			return fmt.Errorf("NPC talk give quest projection changed")
		}
	}
	_ = npc
	return nil
}
