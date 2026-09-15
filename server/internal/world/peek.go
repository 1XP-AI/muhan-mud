package world

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	peekTimerIndex      = 10 // LT_PEEKS
	peekThiefClass      = 8  // THIEF
	peekInvincibleClass = 9  // INVINCIBLE
	peekCaretakerClass  = 10 // CARETAKER
	peekSubDMClass      = 11 // SUB_DM
	peekDMClass         = 12 // DM
	peekDMInvisibleFlag = 10 // PDMINV
	peekInvisibleFlag   = 2  // PINVIS/MINVIS
	peekMaleFlag        = 12 // PMALES
	peekCannotStealFlag = 15 // MUNSTL
	peekTradeFlag       = 37 // MTRADE
	peekPurityFlag      = 36 // MPURIT
)

// PeekProposal is the deterministic bare-target branch of command4.c:peek.
// It contains no client-selected inventory identity. Target selection and the
// visible inventory projection are derived from the committed room snapshot.
type PeekProposal struct {
	ActorID     string
	TargetName  string
	TargetID    string
	TargetKind  string
	RoomID      int16
	Now         int32
	Chance      int
	AlertChance int
	WaitSeconds int32
	Cooldown    bool
	TimerWrite  bool
	Succeeded   bool
	Alert       bool
	Response    string
	TargetText  string
	RoomText    string
}

// PeekResult is stored in the durable receipt. Alert projections are kept with
// the receipt so a lost response cannot reroll or rebroadcast on retry.
type PeekResult struct {
	Response   string `json:"response"`
	TargetID   string `json:"target_id,omitempty"`
	TargetKind string `json:"target_kind,omitempty"`
	Succeeded  bool   `json:"succeeded"`
	Alert      bool   `json:"alert"`
	TargetText string `json:"target_text,omitempty"`
	RoomText   string `json:"room_text,omitempty"`
}

func validPeekTargetName(name string) bool {
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

func peekRoll(roll func(int, int) int) (int, error) {
	if roll == nil {
		return 0, fmt.Errorf("missing peek random source")
	}
	value := roll(1, 100)
	if value < 1 || value > 100 {
		return 0, fmt.Errorf("peek random value outside 1..100")
	}
	return value, nil
}

func peekVisible(actor LegacyMonster, target LegacyMonster) bool {
	if target.Class >= peekCaretakerClass && flag(target.Flags[:], peekDMInvisibleFlag) {
		return false
	}
	if flag(target.Flags[:], peekInvisibleFlag) && !flag(actor.Flags[:], playerDetectInvisibleFlag) {
		return false
	}
	return true
}

func (s State) selectPeekTarget(actorID, targetName string) (string, string, error) {
	actor, ok := s.Players[actorID]
	if !ok {
		return "", "", fmt.Errorf("peek actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok {
		return "", "", fmt.Errorf("peek room absent")
	}
	// find_crt scans monsters before players. We keep exact names in this
	// bounded slice; prefix/occurrence parsing remains an explicit follow-up.
	for _, id := range room.NPCIDs {
		npc, exists := s.NPCs[id]
		if !exists || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || npc.Body.Name != targetName {
			continue
		}
		if peekVisible(actor.Body, npc.Body) {
			return id, "npc", nil
		}
		return "", "", fmt.Errorf("peek target unavailable")
	}
	for _, id := range room.PlayerIDs {
		player, exists := s.Players[id]
		if !exists || id == actorID || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID || player.Body.Name != targetName {
			continue
		}
		if peekVisible(actor.Body, player.Body) {
			return id, "player", nil
		}
		return "", "", fmt.Errorf("peek target unavailable")
	}
	return "", "", fmt.Errorf("peek target absent")
}

func peekTargetBody(s State, targetID, targetKind string) (LegacyMonster, *ItemCollection, error) {
	switch targetKind {
	case "player":
		target, ok := s.Players[targetID]
		if !ok || !target.Online || target.Body.Type != 0 {
			return LegacyMonster{}, nil, fmt.Errorf("peek player target absent")
		}
		return target.Body, target.Items, nil
	case "npc":
		target, ok := s.NPCs[targetID]
		if !ok || target.Body.Type != 1 {
			return LegacyMonster{}, nil, fmt.Errorf("peek NPC target absent")
		}
		return target.Body, target.Items, nil
	default:
		return LegacyMonster{}, nil, fmt.Errorf("unknown peek target kind")
	}
}

func peekLegacyObjects(objects []LegacyObject, detect bool, out *[]string) {
	for _, object := range objects {
		if !detect && flag(object.Flags[:], objectInvisibleFlag) {
			continue
		}
		*out = append(*out, displayItemName(Item{Object: object}))
	}
}

func peekCanonicalItems(items *ItemCollection, detect bool) ([]string, error) {
	if items == nil {
		return nil, nil
	}
	if err := items.Validate(); err != nil {
		return nil, err
	}
	entries := make([]string, 0, len(items.Inventory))
	for _, id := range items.Inventory {
		item, ok := items.Items[id]
		if !ok {
			return nil, fmt.Errorf("peek inventory item absent")
		}
		if !detect && flag(item.Object.Flags[:], objectInvisibleFlag) {
			continue
		}
		entries = append(entries, displayItemName(item))
	}
	return entries, nil
}

func (s State) peekInventoryResponse(actorID, targetID, targetKind string) (string, error) {
	actor, ok := s.Players[actorID]
	if !ok {
		return "", fmt.Errorf("peek actor absent")
	}
	target, items, err := peekTargetBody(s, targetID, targetKind)
	if err != nil {
		return "", err
	}
	entries, err := peekCanonicalItems(items, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if err != nil {
		return "", err
	}
	if items == nil {
		peekLegacyObjects(target.Inventory, flag(actor.Body.Flags[:], playerDetectInvisibleFlag), &entries)
	}
	pronoun := "그녀"
	if flag(target.Flags[:], peekMaleFlag) {
		pronoun = "그"
	}
	if len(entries) == 0 {
		return fmt.Sprintf("%s는 아무것도 들고 있지 않습니다.\r\n", pronoun), nil
	}
	return fmt.Sprintf("%s의 소지품: %s.\r\n", pronoun, strings.Join(entries, ", ")), nil
}

// PlanPeek ports command4.c:peek's player/NPC inventory branch. It admits
// exact target names and canonical inventory roots; unknown prefix,
// occurrence, object identity and un-migrated inventory are not guessed.
func (s State) PlanPeek(actorID, targetName string, now int32, roll func(int, int) int) (PeekProposal, error) {
	if err := s.Validate(); err != nil {
		return PeekProposal{}, err
	}
	if actorID == "" || now < 0 || !validPeekTargetName(targetName) {
		return PeekProposal{}, fmt.Errorf("invalid peek actor, target, or clock")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return PeekProposal{}, fmt.Errorf("online peek actor absent")
	}
	base := PeekProposal{ActorID: actorID, TargetName: targetName}
	if actor.Body.Class != peekThiefClass && actor.Body.Class < peekInvincibleClass {
		base.Response = "당신 직업으로는 다른사람의 소지품을 볼 수 없습니다.\r\n"
		return base, nil
	}
	if flag(actor.Body.Flags[:], playerBlindFlag) {
		base.Response = "당신은 눈이 멀어 있습니다!\r\n"
		return base, nil
	}
	targetID, targetKind, err := s.selectPeekTarget(actorID, targetName)
	if err != nil {
		base.Response = "그런 사람 없어요!\r\n"
		return base, nil
	}
	target, _, err := peekTargetBody(s, targetID, targetKind)
	if err != nil {
		return PeekProposal{}, err
	}
	roomID := actor.Body.RoomID
	timer := actor.Body.Timers[peekTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return PeekProposal{}, fmt.Errorf("peek timer outside legacy range")
	}
	proposal := PeekProposal{ActorID: actorID, TargetName: targetName, TargetID: targetID, TargetKind: targetKind, RoomID: roomID, Now: now}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait < 1 || wait > int64(^uint32(0)>>1) {
			return PeekProposal{}, fmt.Errorf("peek cooldown overflow")
		}
		proposal.Cooldown = true
		proposal.WaitSeconds = int32(wait)
		proposal.Response = peekWaitResponse(wait)
		return proposal, nil
	}
	proposal.TimerWrite = true
	if flag(target.Flags[:], peekCannotStealFlag) || flag(target.Flags[:], peekTradeFlag) || flag(target.Flags[:], peekPurityFlag) {
		if actor.Body.Class < peekDMClass {
			proposal.Response = "당신은 다른사람의 소지품을 볼 수 없습니다.\n다른사람이 당신보고 도둑이라고 생각할 것입니다.\r\n"
			return proposal, nil
		}
	}
	levelBand := func(level byte) int { return (int(level) + 3) / 4 }
	chance := 25 + levelBand(actor.Body.Level)*10 - levelBand(target.Level)*5
	if chance < 0 {
		chance = 0
	}
	if actor.Body.Class >= peekCaretakerClass {
		chance = 100
	}
	proposal.Chance = chance
	value, err := peekRoll(roll)
	if err != nil {
		return PeekProposal{}, err
	}
	if value > chance {
		proposal.Response = "실패하였습니다!\r\n"
		return proposal, nil
	}
	proposal.Succeeded = true
	alertChance := 15 + levelBand(actor.Body.Level)*5
	if alertChance > 90 {
		alertChance = 90
	}
	proposal.AlertChance = alertChance
	if actor.Body.Class < peekCaretakerClass {
		alertValue, err := peekRoll(roll)
		if err != nil {
			return PeekProposal{}, err
		}
		proposal.Alert = alertValue > alertChance
	}
	proposal.Response, err = s.peekInventoryResponse(actorID, targetID, targetKind)
	if err != nil {
		return PeekProposal{}, err
	}
	if proposal.Alert {
		proposal.TargetText = fmt.Sprintf("%s님이 당신의 소지품을 슬쩍 엿봅니다.\r\n", actor.Body.Name)
		if targetKind == "player" {
			proposal.RoomText = fmt.Sprintf("\n%s님이 %s님의 소지품을 슬쩍 엿봅니다.\r\n", actor.Body.Name, target.Name)
		}
	}
	return proposal, nil
}

func peekWaitResponse(wait int64) string {
	if wait == 1 {
		return "1초만 기다리세요.\r\n"
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", wait)
}

// ApplyPeek validates the target and visible inventory again before committing
// only the legacy LT_PEEKS timer. The response and alert event are receipt
// data; no target inventory mutation occurs in this slice.
func (s State) ApplyPeek(proposal PeekProposal) (State, PeekResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PeekResult{}, err
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Response == "" {
		return State{}, PeekResult{}, fmt.Errorf("invalid peek proposal")
	}
	actor, ok := s.Players[proposal.ActorID]
	if !ok || !actor.Online || actor.Body.Type != 0 {
		return State{}, PeekResult{}, fmt.Errorf("peek actor changed")
	}
	if proposal.TargetID == "" {
		if proposal.Cooldown || proposal.TimerWrite || proposal.Succeeded || proposal.Alert {
			return State{}, PeekResult{}, fmt.Errorf("invalid peek no-op proposal")
		}
		unauthorized := actor.Body.Class != peekThiefClass && actor.Body.Class < peekInvincibleClass
		blind := flag(actor.Body.Flags[:], playerBlindFlag)
		switch {
		case strings.Contains(proposal.Response, "직업") && !unauthorized:
			return State{}, PeekResult{}, fmt.Errorf("peek authorization changed")
		case strings.Contains(proposal.Response, "눈이 멀어") && (!blind || unauthorized):
			return State{}, PeekResult{}, fmt.Errorf("peek blindness changed")
		case strings.Contains(proposal.Response, "그런 사람") && (blind || unauthorized):
			return State{}, PeekResult{}, fmt.Errorf("peek target gate changed")
		}
		if strings.Contains(proposal.Response, "그런 사람") {
			if _, _, err := s.selectPeekTarget(proposal.ActorID, proposal.TargetName); err == nil {
				return State{}, PeekResult{}, fmt.Errorf("peek target appeared")
			}
		}
		return s, PeekResult{Response: proposal.Response}, nil
	}
	if actor.Body.Class != peekThiefClass && actor.Body.Class < peekInvincibleClass {
		return State{}, PeekResult{}, fmt.Errorf("peek authorization changed")
	}
	if flag(actor.Body.Flags[:], playerBlindFlag) {
		return State{}, PeekResult{}, fmt.Errorf("peek blindness changed")
	}
	target, targetKind, err := s.selectPeekTarget(proposal.ActorID, proposal.TargetName)
	if err != nil || target != proposal.TargetID || targetKind != proposal.TargetKind {
		return State{}, PeekResult{}, fmt.Errorf("peek target changed")
	}
	targetBody, _, err := peekTargetBody(s, proposal.TargetID, proposal.TargetKind)
	if err != nil || targetBody.RoomID != proposal.RoomID || actor.Body.RoomID != proposal.RoomID {
		return State{}, PeekResult{}, fmt.Errorf("peek target room changed")
	}
	timer := actor.Body.Timers[peekTimerIndex]
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if proposal.Cooldown {
		if proposal.TimerWrite || proposal.Succeeded || proposal.Alert || int64(proposal.Now) >= deadline || proposal.Response != peekWaitResponse(deadline-int64(proposal.Now)) {
			return State{}, PeekResult{}, fmt.Errorf("stale peek cooldown proposal")
		}
		return s, PeekResult{Response: proposal.Response, TargetID: proposal.TargetID, TargetKind: proposal.TargetKind}, nil
	}
	if int64(proposal.Now) < deadline || !proposal.TimerWrite {
		return State{}, PeekResult{}, fmt.Errorf("peek cooldown changed")
	}
	blocked := flag(targetBody.Flags[:], peekCannotStealFlag) || flag(targetBody.Flags[:], peekTradeFlag) || flag(targetBody.Flags[:], peekPurityFlag)
	if blocked && actor.Body.Class < peekDMClass {
		if proposal.Succeeded || proposal.Response != "당신은 다른사람의 소지품을 볼 수 없습니다.\n다른사람이 당신보고 도둑이라고 생각할 것입니다.\r\n" {
			return State{}, PeekResult{}, fmt.Errorf("peek target restriction changed")
		}
	} else if !blocked || actor.Body.Class >= peekDMClass {
		if strings.Contains(proposal.Response, "다른사람의 소지품") {
			return State{}, PeekResult{}, fmt.Errorf("peek target restriction changed")
		}
		levelBand := func(level byte) int { return (int(level) + 3) / 4 }
		chance := 25 + levelBand(actor.Body.Level)*10 - levelBand(targetBody.Level)*5
		if chance < 0 {
			chance = 0
		}
		if actor.Body.Class >= peekCaretakerClass {
			chance = 100
		}
		if proposal.Chance != chance {
			return State{}, PeekResult{}, fmt.Errorf("stale peek chance proposal")
		}
		alertChance := 15 + levelBand(actor.Body.Level)*5
		if alertChance > 90 {
			alertChance = 90
		}
		if proposal.AlertChance != alertChance && proposal.Succeeded {
			return State{}, PeekResult{}, fmt.Errorf("stale peek alert chance proposal")
		}
	}
	if proposal.Succeeded {
		expected, err := s.peekInventoryResponse(proposal.ActorID, proposal.TargetID, proposal.TargetKind)
		if err != nil || expected != proposal.Response {
			return State{}, PeekResult{}, fmt.Errorf("peek inventory changed")
		}
		if proposal.Alert != (proposal.TargetText != "") || proposal.Alert && proposal.TargetKind != "player" {
			return State{}, PeekResult{}, fmt.Errorf("peek alert target mismatch")
		}
	} else if proposal.Response != "실패하였습니다!\r\n" && !strings.Contains(proposal.Response, "다른사람의 소지품") {
		return State{}, PeekResult{}, fmt.Errorf("peek response mismatch")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Timers[peekTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: 5}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, PeekResult{}, err
	}
	return next, PeekResult{Response: proposal.Response, TargetID: proposal.TargetID, TargetKind: proposal.TargetKind, Succeeded: proposal.Succeeded, Alert: proposal.Alert, TargetText: proposal.TargetText, RoomText: proposal.RoomText}, nil
}
