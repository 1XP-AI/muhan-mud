package world

import (
	"fmt"
	"math"
	"reflect"
	"strings"
)

const (
	// The C implementation keeps ply_burn_time outside the character file.
	// The canonical runtime uses the otherwise-unused LT_READS-compatible slot
	// so the cooldown is part of the same durable actor transaction.
	burnTimerIndex  = 12
	burnCooldown    = int64(2)
	burnNoBurnFlag  = 48 // ONOBUN
	burnSubDMClass  = 11 // SUB_DM
	burnJackpotGold = int64(100000)
	burnJackpotExp  = int32(10)
)

const (
	BurnTimerIndex = burnTimerIndex
	BurnCooldown   = burnCooldown
)

// BurnEvent is the deterministic room projection of command2.c:burn. The
// actor receives Result.Response, so the room fan-out excludes that actor.
type BurnEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ItemID         string `json:"item_id"`
	ItemName       string `json:"item_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
	Jackpot        bool   `json:"jackpot,omitempty"`
}

// BurnResult is the typed durable receipt projection. Rejected attempts can
// still clear PHIDDN, just like the legacy command; cooldown is the one branch
// that returns unchanged state before the hidden flag is cleared.
type BurnResult struct {
	Action          string     `json:"action"`
	Response        string     `json:"response"`
	Broadcast       bool       `json:"broadcast"`
	Changed         bool       `json:"changed"`
	Burned          bool       `json:"burned"`
	Cooldown        bool       `json:"cooldown,omitempty"`
	WaitSeconds     int32      `json:"wait_seconds,omitempty"`
	ItemID          string     `json:"item_id,omitempty"`
	ItemName        string     `json:"item_name,omitempty"`
	ItemOccurrence  int        `json:"item_occurrence,omitempty"`
	GoldAward       int32      `json:"gold_award,omitempty"`
	ExperienceAward int32      `json:"experience_award,omitempty"`
	Jackpot         bool       `json:"jackpot,omitempty"`
	JackpotRoll     int        `json:"jackpot_roll,omitempty"`
	RoomID          int16      `json:"room_id,omitempty"`
	ActorID         string     `json:"actor_id,omitempty"`
	ActorName       string     `json:"actor_name,omitempty"`
	Event           *BurnEvent `json:"event,omitempty"`
}

// BurnProposal is a state-bound, replay-safe candidate. The injected RNG is
// consumed only during planning after all source gates pass; ApplyBurn checks
// the stored draw and never calls it again.
type BurnProposal struct {
	ActorID        string
	RoomID         int16
	ItemID         string
	ItemName       string
	ItemOccurrence int
	Now            int32
	JackpotRoll    int
	Jackpot        bool
	Cooldown       bool
	WaitSeconds    int32
	ClearHidden    bool
	Burned         bool
	Broadcast      bool
	Response       string
	ExpectedEvent  *BurnEvent

	expectedActor PlayerState
	expectedTimer LegacyTimer
}

// BurnOptions carries the host-bound clock and optional jackpot RNG. A nil
// Roll intentionally means the optional jackpot branch is disabled; all other
// source behaviour remains deterministic.
type BurnOptions struct {
	Now  int32
	Roll func(int, int) int
}

func burnWaitResponse(seconds int64) (string, error) {
	if seconds < 1 || seconds > math.MaxInt32 {
		return "", fmt.Errorf("burn cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds), nil
}

func burnActor(s State, actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" || actor.Items == nil {
		return PlayerState{}, fmt.Errorf("online player with migrated burn inventory required")
	}
	if actor.Body.Gold < 0 || actor.Body.Experience < 0 {
		return PlayerState{}, fmt.Errorf("burn actor reward range invalid")
	}
	if err := actor.Items.Validate(); err != nil {
		return PlayerState{}, err
	}
	return actor, nil
}

func selectBurnRoot(actor PlayerState, name string, occurrence int) (string, Item, error) {
	if occurrence < 1 {
		return "", Item{}, fmt.Errorf("invalid burn item occurrence")
	}
	if name == "" || strings.TrimSpace(name) != name {
		return "", Item{}, fmt.Errorf("burn item name required")
	}
	found := 0
	detectInvisible := flag(actor.Body.Flags[:], playerDetectInvisibleFlag)
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok || !strings.EqualFold(item.Object.Name, name) || (flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible) {
			continue
		}
		found++
		if found == occurrence {
			return id, item, nil
		}
	}
	return "", Item{}, fmt.Errorf("burn item not found")
}

func burnProtected(actor PlayerState, item Item) (string, bool) {
	if flag(item.Object.Flags[:], burnNoBurnFlag) {
		return "burn-cannot", true
	}
	if actor.Body.Class < burnSubDMClass && item.Object.ShotsCurrent > 0 {
		if item.Object.Quest != 0 {
			return "burn-quest-protected", true
		}
		if flag(item.Object.Flags[:], objectOneWevFlag) {
			return "burn-event-protected", true
		}
	}
	return "", false
}

func burnActorSnapshot(actor PlayerState) PlayerState {
	copy := actor
	if actor.Items != nil {
		items := actor.Items.clone()
		copy.Items = &items
	}
	return copy
}

func burnRootResponse(itemName string, jackpot bool) string {
	response := fmt.Sprintf("당신은 %s을(를) 태웠습니다.\r\n", itemName)
	response += "\n당신은 약간의 상금과 경험을 받았습니다.\r\n"
	if jackpot {
		response += "\n신이 당신의 정성이 갸륵해서 경험치와 돈벼락을 내립니다.\r\n"
	}
	return response
}

func burnRejectedResponse(action string) string {
	switch action {
	case "burn-cannot":
		return "소각할수 없는 아이템입니다.\r\n"
	case "burn-quest-protected":
		return "임무 아이템은 태우지 못합니다.\r\n"
	case "burn-event-protected":
		return "이벤트 아이템은 소각할수 없습니다.\r\n"
	default:
		return "당신은 그런것을 갖고 있지 않습니다.\r\n"
	}
}

func burnEvent(actor LegacyMonster, actorID, itemID, itemName string, jackpot bool) BurnEvent {
	text := fmt.Sprintf("\n%s%s %s을(를) 태웠습니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), itemName)
	if jackpot {
		text += "\n신이 당신의 정성이 갸륵해서 경험치와 돈벼락을 내립니다.\r\n"
	}
	return BurnEvent{
		RoomID: actor.RoomID, ActorID: actorID, ActorName: actor.Name,
		ItemID: itemID, ItemName: itemName, ExcludeActorID: actorID,
		Text: text, Jackpot: jackpot,
	}
}

func burnResult(proposal BurnProposal, actor PlayerState) BurnResult {
	result := BurnResult{
		Action: "burn", Response: proposal.Response, Broadcast: proposal.Broadcast,
		Changed: proposal.Burned || proposal.ClearHidden, Burned: proposal.Burned,
		Cooldown: proposal.Cooldown, WaitSeconds: proposal.WaitSeconds,
		ItemID: proposal.ItemID, ItemName: proposal.ItemName,
		ItemOccurrence: proposal.ItemOccurrence, Jackpot: proposal.Jackpot,
		JackpotRoll: proposal.JackpotRoll, RoomID: proposal.RoomID,
		ActorID: proposal.ActorID, ActorName: actor.Body.Name, Event: proposal.ExpectedEvent,
	}
	if proposal.Cooldown || !proposal.Burned {
		result.Action = "burn-rejected"
	}
	if proposal.Burned {
		result.GoldAward = 4
		result.ExperienceAward = 1
		if proposal.Jackpot {
			result.GoldAward += int32(burnJackpotGold)
			result.ExperienceAward += burnJackpotExp
		}
	}
	return result
}

// PlanBurn ports command2.c:burn against the canonical direct inventory
// roots. Hidden-state clearing follows the source order; cooldown is checked
// first and therefore leaves PHIDDN untouched.
func (s State) PlanBurn(actorID, itemName string, occurrence int, now int32, roll func(int, int) int) (BurnProposal, error) {
	if now < 0 {
		return BurnProposal{}, fmt.Errorf("burn clock must be nonnegative")
	}
	actor, err := burnActor(s, actorID)
	if err != nil {
		return BurnProposal{}, err
	}
	timer := actor.Body.Timers[burnTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return BurnProposal{}, fmt.Errorf("burn timer outside legacy range")
	}
	if int64(timer.LastTime)+int64(timer.Interval) > int64(now) {
		wait := int64(timer.LastTime) + int64(timer.Interval) - int64(now)
		response, err := burnWaitResponse(wait)
		if err != nil {
			return BurnProposal{}, err
		}
		expected := burnActorSnapshot(actor)
		return BurnProposal{
			ActorID: actorID, RoomID: actor.Body.RoomID, ItemOccurrence: occurrence,
			Now: now, Cooldown: true, WaitSeconds: int32(wait), Response: response,
			expectedActor: expected, expectedTimer: timer,
		}, nil
	}

	proposal := BurnProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, ItemOccurrence: occurrence,
		Now: now, ClearHidden: true, expectedActor: burnActorSnapshot(actor), expectedTimer: timer,
	}
	itemID, item, err := selectBurnRoot(actor, itemName, occurrence)
	if err != nil {
		proposal.Response = burnRejectedResponse("")
		return proposal, nil
	}
	proposal.ItemID, proposal.ItemName = itemID, item.Object.Name
	if action, protected := burnProtected(actor, item); protected {
		proposal.Response = burnRejectedResponse(action)
		return proposal, nil
	}
	if roll != nil {
		proposal.JackpotRoll = roll(1, 100)
		if proposal.JackpotRoll < 1 || proposal.JackpotRoll > 100 {
			return BurnProposal{}, fmt.Errorf("burn jackpot roll outside 1..100")
		}
	}
	proposal.Jackpot = roll != nil && (int64(now)+int64(proposal.JackpotRoll))%250 == 9
	awardGold := int64(4)
	awardExp := int64(1)
	if proposal.Jackpot {
		awardGold += burnJackpotGold
		awardExp += int64(burnJackpotExp)
	}
	if int64(actor.Body.Gold) > math.MaxInt32-awardGold || int64(actor.Body.Experience) > math.MaxInt32-awardExp {
		return BurnProposal{}, fmt.Errorf("burn reward overflow")
	}
	proposal.Burned, proposal.Broadcast = true, true
	proposal.Response = burnRootResponse(item.Object.Name, proposal.Jackpot)
	event := burnEvent(actor.Body, actorID, itemID, item.Object.Name, proposal.Jackpot)
	proposal.ExpectedEvent = &event
	return proposal, nil
}

// removeBurnRoot deletes a direct inventory root and its complete canonical
// descendant graph. Nested descendants are not selectable as independent
// burn targets, matching find_obj's direct player-list lookup.
func removeBurnRoot(items *ItemCollection, root string) error {
	if items == nil {
		return fmt.Errorf("burn inventory absent")
	}
	remaining, err := removeInventoryRoot(items.Inventory, root)
	if err != nil {
		return err
	}
	stack := []string{root}
	seen := map[string]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			return fmt.Errorf("cyclic burn subtree")
		}
		seen[id] = true
		item, ok := items.Items[id]
		if !ok {
			return fmt.Errorf("burn item subtree absent")
		}
		stack = append(stack, item.Contents...)
		delete(items.Items, id)
	}
	items.Inventory = remaining
	return nil
}

// ApplyBurn atomically clears the direct root, updates rewards/timer, and
// validates the resulting canonical world. It never invokes the RNG.
func (s State) ApplyBurn(proposal BurnProposal) (State, BurnResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, BurnResult{}, err
	}
	actor, err := burnActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID || proposal.Now < 0 || proposal.ItemOccurrence < 1 || proposal.Response == "" {
		return State{}, BurnResult{}, fmt.Errorf("burn actor or proposal changed")
	}
	if !reflect.DeepEqual(actor, proposal.expectedActor) || actor.Body.Timers[burnTimerIndex] != proposal.expectedTimer {
		return State{}, BurnResult{}, fmt.Errorf("stale burn actor")
	}
	if proposal.Cooldown {
		if proposal.ClearHidden || proposal.Burned || proposal.Broadcast || proposal.Jackpot || proposal.JackpotRoll != 0 || proposal.ItemID != "" || proposal.ItemName != "" {
			return State{}, BurnResult{}, fmt.Errorf("invalid burn cooldown proposal")
		}
		deadline := int64(proposal.expectedTimer.LastTime) + int64(proposal.expectedTimer.Interval)
		wait := deadline - int64(proposal.Now)
		response, err := burnWaitResponse(wait)
		if err != nil || wait != int64(proposal.WaitSeconds) || proposal.Response != response || wait <= 0 {
			return State{}, BurnResult{}, fmt.Errorf("stale burn cooldown proposal")
		}
		return s.clone(), burnResult(proposal, actor), nil
	}
	if !proposal.ClearHidden || proposal.JackpotRoll < 0 || proposal.JackpotRoll > 100 || proposal.Broadcast != proposal.Burned {
		return State{}, BurnResult{}, fmt.Errorf("invalid burn proposal")
	}
	if !proposal.Burned {
		if proposal.Jackpot || proposal.JackpotRoll != 0 || proposal.ExpectedEvent != nil {
			return State{}, BurnResult{}, fmt.Errorf("invalid rejected burn proposal")
		}
		next := s.clone()
		nextActor := next.Players[proposal.ActorID]
		setSettingFlag(&nextActor.Body, playerHiddenStateFlag, false)
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BurnResult{}, err
		}
		return next, burnResult(proposal, nextActor), nil
	}
	if proposal.ItemID == "" || proposal.ItemName == "" || proposal.ExpectedEvent == nil || proposal.JackpotRoll == 0 && proposal.Jackpot {
		return State{}, BurnResult{}, fmt.Errorf("invalid successful burn proposal")
	}
	selectedID, selected, err := selectBurnRoot(actor, proposal.ItemName, proposal.ItemOccurrence)
	if err != nil || selectedID != proposal.ItemID || selected.Object.Name != proposal.ItemName {
		return State{}, BurnResult{}, fmt.Errorf("burn target changed")
	}
	if action, protected := burnProtected(actor, selected); protected {
		return State{}, BurnResult{}, fmt.Errorf("burn protection changed: %s", action)
	}
	wantJackpot := proposal.JackpotRoll > 0 && (int64(proposal.Now)+int64(proposal.JackpotRoll))%250 == 9
	if wantJackpot != proposal.Jackpot {
		return State{}, BurnResult{}, fmt.Errorf("burn jackpot outcome changed")
	}
	wantResponse := burnRootResponse(selected.Object.Name, proposal.Jackpot)
	wantEvent := burnEvent(actor.Body, proposal.ActorID, selectedID, selected.Object.Name, proposal.Jackpot)
	if proposal.Response != wantResponse || !reflect.DeepEqual(*proposal.ExpectedEvent, wantEvent) {
		return State{}, BurnResult{}, fmt.Errorf("burn receipt projection changed")
	}
	awardGold := int64(4)
	awardExp := int64(1)
	if proposal.Jackpot {
		awardGold += burnJackpotGold
		awardExp += int64(burnJackpotExp)
	}
	if int64(actor.Body.Gold) > math.MaxInt32-awardGold || int64(actor.Body.Experience) > math.MaxInt32-awardExp {
		return State{}, BurnResult{}, fmt.Errorf("burn reward overflow")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	if err := removeBurnRoot(nextActor.Items, proposal.ItemID); err != nil {
		return State{}, BurnResult{}, err
	}
	nextActor.Body.Gold += int32(awardGold)
	nextActor.Body.Experience += int32(awardExp)
	nextActor.Body.Timers[burnTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: int32(burnCooldown)}
	setSettingFlag(&nextActor.Body, playerHiddenStateFlag, false)
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, BurnResult{}, err
	}
	return next, burnResult(proposal, nextActor), nil
}

// BurnItemByName is the one-call pure reducer boundary used by session code.
func (s State) BurnItemByName(actorID, itemName string, occurrence int, now int32, roll func(int, int) int) (State, BurnResult, error) {
	proposal, err := s.PlanBurn(actorID, itemName, occurrence, now, roll)
	if err != nil {
		return State{}, BurnResult{}, err
	}
	return s.ApplyBurn(proposal)
}
