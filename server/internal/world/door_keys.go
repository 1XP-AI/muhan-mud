package world

import (
	"fmt"
	"reflect"
	"strings"
)

// DoorUnlock, DoorLock and DoorPick are the source command6.c actions for
// 풀어, 잠궈 and 따.  They are kept separate from open/close because the C
// handlers have different ordering, key consumption and random/timer rules.
const (
	DoorUnlock = "unlock"
	DoorLock   = "lock"
	DoorPick   = "pick"
)

const (
	doorKeyObjectType = 11 // KEY
	doorLockedFlag    = 2  // XLOCKD
	doorClosedFlag    = 3  // XCLOSD
	doorLockableFlag  = 4  // XLOCKS
	doorClosableFlag  = 5  // XCLOSS
	doorUnpickable    = 6  // XUNPCK
	doorPickTimer     = 6  // LT_PICKL
	doorThiefClass    = 8  // THIEF
	doorInvincible    = 9  // INVINCIBLE
	doorMaxClass      = 12 // DM; source bonus/class tables end here
)

// DoorKeyProposal is an in-memory, snapshot-bound candidate for one of the
// key/lock commands.  It is never accepted from a client or unmarshaled from
// a receipt.  The unexported key reference prevents a caller from selecting a
// different inventory object between planning and application.
type DoorKeyProposal struct {
	ActorID     string
	Action      string
	Target      string
	KeyName     string
	RoomID      int16
	ExitIndex   int
	Expected    LegacyExit
	Now         int32
	Chance      int
	WaitSeconds int32
	Cooldown    bool
	Reveal      bool
	Changed     bool
	Attempt     bool
	Succeeded   bool
	Broadcast   bool
	Response    string

	expectedTimer  LegacyTimer
	expectedHidden bool
	hasKey         bool
	key            doorKeyRef
}

// DoorKeyCommandResult is the durable actor response and the receipt-bound
// projection metadata used by the transport.  A pick attempt has Broadcast
// true even when it fails, because C announces the attempt to the room.
type DoorKeyCommandResult struct {
	Response  string `json:"response"`
	Broadcast bool   `json:"broadcast"`
	Attempt   bool   `json:"attempt,omitempty"`
	Succeeded bool   `json:"succeeded,omitempty"`
	RoomID    int16  `json:"room_id,omitempty"`
	ExitName  string `json:"exit_name,omitempty"`
	Action    string `json:"action,omitempty"`
}

type doorKeyRef struct {
	canonical    bool
	id           string
	index        int
	expectedItem Item
	expectedObj  LegacyObject
}

func doorKeyActionValid(action string) bool {
	return action == DoorUnlock || action == DoorLock || action == DoorPick
}

func doorKeyActor(s State, actorID string) (PlayerState, error) {
	return doorActor(s, actorID)
}

// findDoorKey intentionally admits only a root inventory object.  The legacy
// command's linked-list find_obj also searches objects that have already been
// migrated into the player's object graph; equipped/nested occurrence syntax
// remains a separate parser/data-boundary slice rather than guessing an item
// identity.  Exact case-insensitive names match the current item commands.
func findDoorKey(actor PlayerState, name string) (doorKeyRef, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return doorKeyRef{}, fmt.Errorf("key name required")
	}
	if actor.Items != nil {
		if err := actor.Items.Validate(); err != nil {
			return doorKeyRef{}, err
		}
		id, err := selectInventoryRoot(*actor.Items, name, 1, nil)
		if err != nil {
			return doorKeyRef{}, err
		}
		item, ok := actor.Items.Items[id]
		if !ok {
			return doorKeyRef{}, fmt.Errorf("key item absent")
		}
		return doorKeyRef{canonical: true, id: id, expectedItem: item, expectedObj: item.Object}, nil
	}
	for i, object := range actor.Body.Inventory {
		if strings.EqualFold(strings.TrimSpace(object.Name), name) {
			return doorKeyRef{index: i, expectedObj: object}, nil
		}
	}
	return doorKeyRef{}, fmt.Errorf("key item not found")
}

func (r doorKeyRef) object(actor PlayerState) (LegacyObject, bool) {
	if r.canonical {
		if actor.Items == nil {
			return LegacyObject{}, false
		}
		item, ok := actor.Items.Items[r.id]
		if !ok || !reflect.DeepEqual(item, r.expectedItem) {
			return LegacyObject{}, false
		}
		return item.Object, true
	}
	if r.index < 0 || r.index >= len(actor.Body.Inventory) || !reflect.DeepEqual(actor.Body.Inventory[r.index], r.expectedObj) {
		return LegacyObject{}, false
	}
	return actor.Body.Inventory[r.index], true
}

func (r doorKeyRef) mutateUnlock(actor *PlayerState) bool {
	if r.canonical {
		if actor.Items == nil {
			return false
		}
		item, ok := actor.Items.Items[r.id]
		if !ok || !reflect.DeepEqual(item, r.expectedItem) || item.Object.ShotsCurrent < 1 {
			return false
		}
		item.Object.ShotsCurrent--
		actor.Items.Items[r.id] = item
		return true
	}
	if r.index < 0 || r.index >= len(actor.Body.Inventory) || !reflect.DeepEqual(actor.Body.Inventory[r.index], r.expectedObj) || actor.Body.Inventory[r.index].ShotsCurrent < 1 {
		return false
	}
	actor.Body.Inventory[r.index].ShotsCurrent--
	return true
}

func doorKeyDisplay(object LegacyObject) string {
	return displayItemName(Item{Object: object})
}

func doorKeyResponse(action, target, key string) string {
	if target == "" {
		switch action {
		case DoorUnlock:
			return "무엇을 풀고 싶으세요?\r\n"
		case DoorLock:
			return "무엇을 잠굴려구요?\r\n"
		default:
			return "무엇을 따시려구요?\r\n"
		}
	}
	if action == DoorPick {
		return "그런건 여기 없습니다.\r\n"
	}
	return doorKeyResponse(action, "", key)
}

func doorKeyMissingKeyResponse(action string) string {
	if action == DoorLock {
		return "뭘 가지고 잠구려구요?\r\n"
	}
	return "뭘 가지고 열려구요?\r\n"
}

func doorKeyMissingObjectResponse(action string) string {
	if action == DoorLock {
		return "당신은 그런것을 가지고 있지 않습니다.\r\n"
	}
	return "당신은 그런것을 갖고 있지 않습니다.\r\n"
}

func doorKeyPickChance(player LegacyMonster, exit LegacyExit) (int, error) {
	if player.Class > doorMaxClass {
		return 0, fmt.Errorf("picklock actor class outside legacy table")
	}
	if player.Stats[1] > 63 {
		return 0, fmt.Errorf("picklock actor dexterity outside legacy bonus table")
	}
	levelBand := (int(player.Level) + 3) / 4
	if player.Class == doorThiefClass {
		levelBand *= 10
	} else {
		levelBand *= 5
	}
	chance := levelBand + legacyStatBonus[player.Stats[1]]*2
	if flag(exit.Flags[:], doorUnpickable) {
		chance = 0
	}
	return chance, nil
}

func doorKeyRoll(roll func(int, int) int, chance int) (bool, error) {
	if roll == nil {
		return false, fmt.Errorf("missing picklock random source")
	}
	value, err := func() (value int, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("picklock random source panicked: %v", recovered)
			}
		}()
		return roll(1, 100), nil
	}()
	if err != nil {
		return false, err
	}
	if value < 1 || value > 100 {
		return false, fmt.Errorf("picklock random value outside 1..100")
	}
	return value <= chance, nil
}

func (s State) planDoorKeyLock(actorID, action, target, keyName string, now int32) (DoorKeyProposal, error) {
	actor, err := doorKeyActor(s, actorID)
	if err != nil {
		return DoorKeyProposal{}, err
	}
	p := DoorKeyProposal{ActorID: actorID, Action: action, Target: target, KeyName: keyName, RoomID: actor.Body.RoomID, ExitIndex: -1, Now: now}
	if target == "" {
		p.Response = doorKeyResponse(action, target, keyName)
		return p, nil
	}
	room := s.Rooms[actor.Body.RoomID]
	index := SelectExit(room.Resource.Exits, target, 1, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if index < 0 {
		p.Response = doorKeyResponse(action, target, keyName)
		return p, nil
	}
	p.ExitIndex = index
	p.Expected = room.Resource.Exits[index]
	exit := p.Expected
	if action == DoorUnlock {
		if !flag(exit.Flags[:], doorLockedFlag) {
			p.Response = "그것은 잠궈져 있지 않습니다.\r\n"
			return p, nil
		}
	} else {
		if flag(exit.Flags[:], doorLockedFlag) {
			p.Response = "그것은 이미 잠궈져 있습니다.\r\n"
			return p, nil
		}
	}
	if keyName == "" {
		p.Response = doorKeyMissingKeyResponse(action)
		return p, nil
	}
	key, keyErr := findDoorKey(actor, keyName)
	if keyErr != nil {
		p.Response = doorKeyMissingObjectResponse(action)
		return p, nil
	}
	p.hasKey, p.key = true, key
	object, ok := key.object(actor)
	if !ok {
		return DoorKeyProposal{}, fmt.Errorf("key changed while planning")
	}
	if object.Type != doorKeyObjectType {
		if action == DoorLock {
			p.Response = fmt.Sprintf("%s 열쇠가 아닙니다.\r\n", doorKeyDisplay(object))
		} else {
			p.Response = "그것은 열쇠가 아닙니다.\r\n"
		}
		return p, nil
	}
	if action == DoorLock && !flag(exit.Flags[:], doorLockableFlag) {
		p.Response = "당신은 그것을 잠굴수 없습니다.\r\n"
		return p, nil
	}
	if action == DoorLock && !flag(exit.Flags[:], doorClosedFlag) {
		p.Response = "먼저 문을 닫아야 될것 같군요.\r\n"
		return p, nil
	}
	if object.ShotsCurrent < 1 {
		if action == DoorLock {
			p.Response = fmt.Sprintf("%s 부서져 버렸습니다.\r\n", doorKeyDisplay(object))
		} else {
			p.Response = fmt.Sprintf("%s 부숴져 버렸습니다.\r\n", doorKeyDisplay(object))
		}
		return p, nil
	}
	if object.DiceCount != int16(exit.Key) {
		p.Response = "열쇠가 맞지 않습니다.\r\n"
		return p, nil
	}
	p.Reveal, p.Changed = true, true
	p.Broadcast = true
	if action == DoorUnlock {
		p.Response = object.UseOutput
		if p.Response == "" {
			p.Response = "## 찰칵 ##\r\n"
		}
	} else {
		p.Response = "## 찰칵 ##\r\n"
	}
	return p, nil
}

func (s State) planDoorPickState(actorID, target string, now int32) (DoorKeyProposal, error) {
	actor, err := doorKeyActor(s, actorID)
	if err != nil {
		return DoorKeyProposal{}, err
	}
	p := DoorKeyProposal{ActorID: actorID, Action: DoorPick, Target: target, RoomID: actor.Body.RoomID, ExitIndex: -1, Now: now}
	if actor.Body.Class != doorThiefClass && actor.Body.Class < doorInvincible {
		p.Response = "도둑만 자물쇠를 딸 수 있습니다.\r\n"
		return p, nil
	}
	if target == "" {
		p.Response = "무엇을 따시려구요?\r\n"
		return p, nil
	}
	room := s.Rooms[actor.Body.RoomID]
	index := SelectExit(room.Resource.Exits, target, 1, flag(actor.Body.Flags[:], playerDetectInvisibleFlag))
	if index < 0 {
		p.Response = "그런건 여기 없습니다.\r\n"
		return p, nil
	}
	p.ExitIndex, p.Expected = index, room.Resource.Exits[index]
	if flag(actor.Body.Flags[:], playerBlindFlag) {
		p.Response = "당신은 눈이 멀어 있어 딸 수 없습니다.\r\n"
		return p, nil
	}
	if !flag(p.Expected.Flags[:], doorLockedFlag) {
		p.Response = "그것은 잠궈져 있지 않습니다.\r\n"
		return p, nil
	}
	p.Reveal = true
	p.expectedHidden = flag(actor.Body.Flags[:], playerHiddenStateFlag)
	p.expectedTimer = actor.Body.Timers[doorPickTimer]
	if p.expectedTimer.LastTime < 0 || p.expectedTimer.Interval < 0 {
		return DoorKeyProposal{}, fmt.Errorf("picklock timer outside legacy range")
	}
	deadline := int64(p.expectedTimer.LastTime) + int64(p.expectedTimer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait < 1 || wait > int64(^uint32(0)>>1) {
			return DoorKeyProposal{}, fmt.Errorf("picklock cooldown overflow")
		}
		p.Cooldown, p.WaitSeconds = true, int32(wait)
		p.Response = hideWaitResponse(p.WaitSeconds)
		return p, nil
	}
	chance, err := doorKeyPickChance(actor.Body, p.Expected)
	if err != nil {
		return DoorKeyProposal{}, err
	}
	p.Chance = chance
	return p, nil
}

// PlanDoorKey follows command6.c's check ordering.  In particular, picklock
// consumes its random roll only after all authorization/visibility/cooldown
// checks, while unlock/lock never consume randomness.
func (s State) PlanDoorKey(actorID, action, target, keyName string, now int32, roll func(int, int) int) (DoorKeyProposal, error) {
	if err := s.Validate(); err != nil {
		return DoorKeyProposal{}, err
	}
	if !doorKeyActionValid(action) || now < 0 || strings.TrimSpace(target) != target || strings.TrimSpace(keyName) != keyName {
		return DoorKeyProposal{}, fmt.Errorf("invalid door key command")
	}
	if action != DoorPick {
		return s.planDoorKeyLock(actorID, action, target, keyName, now)
	}
	if keyName != "" {
		return DoorKeyProposal{}, fmt.Errorf("picklock does not accept a key")
	}
	p, err := s.planDoorPickState(actorID, target, now)
	if err != nil || p.Response != "" || p.Cooldown {
		return p, err
	}
	succeeded, err := doorKeyRoll(roll, p.Chance)
	if err != nil {
		return DoorKeyProposal{}, err
	}
	p.Attempt, p.Broadcast, p.Succeeded = true, true, succeeded
	if succeeded {
		p.Response = "당신은 문을 따는데 성공했습니다.\r\n"
	} else {
		p.Response = "실패하였습니다!\r\n"
	}
	return p, nil
}

func (s State) applyDoorKeyLock(proposal DoorKeyProposal) (State, DoorKeyCommandResult, error) {
	current, err := s.planDoorKeyLock(proposal.ActorID, proposal.Action, proposal.Target, proposal.KeyName, proposal.Now)
	if err != nil {
		return State{}, DoorKeyCommandResult{}, err
	}
	if current.ExitIndex != proposal.ExitIndex || current.Expected != proposal.Expected || current.Response != proposal.Response || current.Changed != proposal.Changed || current.Broadcast != proposal.Broadcast || current.Reveal != proposal.Reveal || current.hasKey != proposal.hasKey {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale door key proposal")
	}
	if current.hasKey && (current.key.canonical != proposal.key.canonical || current.key.id != proposal.key.id || current.key.index != proposal.key.index || !reflect.DeepEqual(current.key.expectedItem, proposal.key.expectedItem) || !reflect.DeepEqual(current.key.expectedObj, proposal.key.expectedObj)) {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale door key identity")
	}
	if !proposal.Changed {
		return s, DoorKeyCommandResult{Response: proposal.Response}, nil
	}
	next := s.clone()
	actor := next.Players[proposal.ActorID]
	room := next.Rooms[proposal.RoomID]
	room.Resource.Exits[proposal.ExitIndex] = proposal.Expected
	if proposal.Action == DoorUnlock {
		room.Resource.Exits[proposal.ExitIndex].Flags[0] &^= 1 << doorLockedFlag
		room.Resource.Exits[proposal.ExitIndex].LastTime = proposal.Now
		if !proposal.key.mutateUnlock(&actor) {
			return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale door unlock key")
		}
	} else {
		room.Resource.Exits[proposal.ExitIndex].Flags[0] |= 1 << doorLockedFlag
	}
	setSettingFlag(&actor.Body, playerHiddenStateFlag, false)
	next.Rooms[proposal.RoomID] = room
	next.Players[proposal.ActorID] = actor
	if err := next.Validate(); err != nil {
		return State{}, DoorKeyCommandResult{}, err
	}
	return next, DoorKeyCommandResult{Response: proposal.Response, Broadcast: true, RoomID: proposal.RoomID, ExitName: proposal.Expected.Name, Action: proposal.Action}, nil
}

func (s State) applyDoorPick(proposal DoorKeyProposal) (State, DoorKeyCommandResult, error) {
	current, err := s.planDoorPickState(proposal.ActorID, proposal.Target, proposal.Now)
	if err != nil {
		return State{}, DoorKeyCommandResult{}, err
	}
	if current.ExitIndex != proposal.ExitIndex || current.Expected != proposal.Expected || current.Response != "" && current.Response != proposal.Response {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale picklock proposal")
	}
	if current.Response != "" && !current.Cooldown {
		if proposal.Attempt || proposal.Broadcast || proposal.Succeeded || proposal.Reveal || proposal.Response != current.Response {
			return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale picklock response")
		}
		return s, DoorKeyCommandResult{Response: proposal.Response}, nil
	}
	if current.Cooldown != proposal.Cooldown || current.Chance != proposal.Chance || current.WaitSeconds != proposal.WaitSeconds || current.Reveal != proposal.Reveal || current.expectedTimer != proposal.expectedTimer || current.expectedHidden != proposal.expectedHidden {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale picklock timing")
	}
	if proposal.Cooldown {
		if proposal.Attempt || proposal.Broadcast || proposal.Succeeded || proposal.Response != current.Response {
			return State{}, DoorKeyCommandResult{}, fmt.Errorf("stale picklock cooldown")
		}
		next := s.clone()
		actor := next.Players[proposal.ActorID]
		setSettingFlag(&actor.Body, playerHiddenStateFlag, false)
		next.Players[proposal.ActorID] = actor
		if err := next.Validate(); err != nil {
			return State{}, DoorKeyCommandResult{}, err
		}
		return next, DoorKeyCommandResult{Response: proposal.Response}, nil
	}
	if !proposal.Attempt || !proposal.Broadcast || proposal.Response == "" {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("invalid picklock outcome")
	}
	want := "실패하였습니다!\r\n"
	if proposal.Succeeded {
		want = "당신은 문을 따는데 성공했습니다.\r\n"
	}
	if proposal.Response != want {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("picklock response mismatch")
	}
	next := s.clone()
	actor := next.Players[proposal.ActorID]
	setSettingFlag(&actor.Body, playerHiddenStateFlag, false)
	actor.Body.Timers[doorPickTimer] = LegacyTimer{LastTime: proposal.Now, Interval: 10}
	room := next.Rooms[proposal.RoomID]
	if proposal.Succeeded {
		room.Resource.Exits[proposal.ExitIndex].Flags[0] &^= 1 << doorLockedFlag
	}
	next.Players[proposal.ActorID] = actor
	next.Rooms[proposal.RoomID] = room
	if err := next.Validate(); err != nil {
		return State{}, DoorKeyCommandResult{}, err
	}
	return next, DoorKeyCommandResult{Response: proposal.Response, Broadcast: true, Attempt: true, Succeeded: proposal.Succeeded, RoomID: proposal.RoomID, ExitName: proposal.Expected.Name, Action: DoorPick}, nil
}

// ApplyDoorKey rechecks the snapshot-bound exit, inventory identity, timer and
// all source ordering before committing. No random source is consulted here;
// the roll outcome is already part of the proposed receipt candidate.
func (s State) ApplyDoorKey(proposal DoorKeyProposal) (State, DoorKeyCommandResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, DoorKeyCommandResult{}, err
	}
	if proposal.ActorID == "" || !doorKeyActionValid(proposal.Action) || proposal.Now < 0 || strings.TrimSpace(proposal.Target) != proposal.Target || strings.TrimSpace(proposal.KeyName) != proposal.KeyName || proposal.Response == "" {
		return State{}, DoorKeyCommandResult{}, fmt.Errorf("invalid door key proposal")
	}
	actor, err := doorKeyActor(s, proposal.ActorID)
	if err != nil || actor.Body.RoomID != proposal.RoomID {
		if err == nil {
			err = fmt.Errorf("door key actor room changed")
		}
		return State{}, DoorKeyCommandResult{}, err
	}
	if proposal.Action == DoorPick {
		return s.applyDoorPick(proposal)
	}
	return s.applyDoorKeyLock(proposal)
}

// RoomDoorKeyEvents derives only committed room projections. It intentionally
// returns no event for an error, wrong key, cooldown or non-pick failure.
func (s State) RoomDoorKeyEvents(actorID string, result DoorKeyCommandResult) ([]DoorEvent, error) {
	if !result.Broadcast {
		return nil, nil
	}
	actor, err := doorKeyActor(s, actorID)
	if err != nil {
		return nil, err
	}
	if result.RoomID != actor.Body.RoomID || result.ExitName == "" {
		return nil, fmt.Errorf("door key event room mismatch")
	}
	events := []DoorEvent{}
	switch result.Action {
	case DoorUnlock:
		events = append(events, DoorEvent{RoomID: result.RoomID, ExcludeActorID: actorID, Text: fmt.Sprintf("\n%s이 %s쪽 출구를 풀었습니다.\r\n", actor.Body.Name, result.ExitName)})
	case DoorLock:
		events = append(events, DoorEvent{RoomID: result.RoomID, ExcludeActorID: actorID, Text: fmt.Sprintf("\n%s이 %s쪽 출구를 잠궜습니다.\r\n", actor.Body.Name, result.ExitName)})
	case DoorPick:
		if !result.Attempt {
			return nil, fmt.Errorf("picklock broadcast without attempt")
		}
		events = append(events, DoorEvent{RoomID: result.RoomID, ExcludeActorID: actorID, Text: fmt.Sprintf("\n%s이 %s쪽 출구를 따려고 합니다.\r\n", actor.Body.Name, result.ExitName)})
		if result.Succeeded {
			events = append(events, DoorEvent{RoomID: result.RoomID, ExcludeActorID: actorID, Text: "\n" + actor.Body.Name + "이 문을 땄습니다.\r\n"})
		}
	default:
		return nil, fmt.Errorf("unknown door key event action")
	}
	return events, nil
}
