package world

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// These values are the source numbers in src/mtype.h.  They stay explicit
	// here because the canonical model still stores the legacy flag/timer
	// layout while the migration is in progress.
	stealTimerIndex        = 5  // LT_STEAL
	stealPlayerKillTimer   = 11 // LT_PLYKL
	stealCooldownSeconds   = int32(5)
	stealThiefClass        = 8  // THIEF
	stealInvincibleClass   = 9  // INVINCIBLE
	stealCaretakerClass    = 10 // CARETAKER
	stealSubDMClass        = 11 // SUB_DM
	stealDMClass           = 12 // DM
	stealRoomNoPlayerKill  = 11 // RNOKIL
	stealPlayerHidden      = 1  // PHIDDN
	stealPlayerInvisible   = 2  // PINVIS
	stealPlayerDMInvisible = 10 // PDMINV
	stealPlayerDetect      = 21 // PDINVI
	stealPlayerChaos       = 28 // PCHAOS
	stealPlayerBlind       = 42 // PBLIND
	stealPlayerMale        = 12 // PMALES
	stealNPCNoSteal        = 15 // MUNSTL
	stealNPCNoHarm         = 24 // MUNKIL
	stealObjectInvisible   = 2  // OINVIS
	stealObjectOneWay      = 50 // ONEWEV
)

const (
	// Exported aliases are useful to the session/transport boundary and make
	// the persisted legacy index visible without exposing mutable internals.
	StealTimerIndex      = stealTimerIndex
	StealCooldownSeconds = stealCooldownSeconds
)

var (
	ErrStealTargetUnavailable   = errors.New("steal target unavailable")
	ErrStealInventoryUnresolved = errors.New("steal inventory migration is unresolved")
)

// StealTargetKind is intentionally a closed set.  A client supplies a
// display name, while the reducer resolves the corresponding canonical ID.
type StealTargetKind string

const (
	StealTargetNPC    StealTargetKind = "npc"
	StealTargetPlayer StealTargetKind = "player"
)

// StealEvent is a post-commit room/target projection.  Texts preserve the C
// ordering: an optional invisibility reveal is followed by the failed steal
// attempt.  TargetText is sent only to a player target by a later transport.
// The actor is excluded from room fan-out because the receipt owns its text.
type StealEvent struct {
	RoomID         int16           `json:"room_id"`
	ExcludeActorID string          `json:"exclude_actor_id"`
	ActorID        string          `json:"actor_id"`
	ActorName      string          `json:"actor_name"`
	TargetID       string          `json:"target_id"`
	TargetKind     StealTargetKind `json:"target_kind"`
	TargetName     string          `json:"target_name"`
	ItemID         string          `json:"item_id"`
	ItemName       string          `json:"item_name"`
	Texts          []string        `json:"texts"`
	RevealText     string          `json:"reveal_text,omitempty"`
	TargetText     string          `json:"target_text,omitempty"`
}

// StealResult is the durable actor response and all data required to publish
// a failed-attempt/reveal event without rerunning target lookup or RNG.
type StealResult struct {
	Action             string          `json:"action"`
	Response           string          `json:"response"`
	Changed            bool            `json:"changed"`
	Broadcast          bool            `json:"broadcast"`
	Cooldown           bool            `json:"cooldown,omitempty"`
	WaitSeconds        int32           `json:"wait_seconds,omitempty"`
	Succeeded          bool            `json:"succeeded,omitempty"`
	Attempted          bool            `json:"attempted,omitempty"`
	Chance             int             `json:"chance,omitempty"`
	Roll               int             `json:"roll,omitempty"`
	ActorID            string          `json:"actor_id"`
	TargetID           string          `json:"target_id,omitempty"`
	TargetKind         StealTargetKind `json:"target_kind,omitempty"`
	TargetName         string          `json:"target_name,omitempty"`
	ItemID             string          `json:"item_id,omitempty"`
	ItemName           string          `json:"item_name,omitempty"`
	EnemyAdded         bool            `json:"enemy_added,omitempty"`
	PlayerKillRoll     int             `json:"player_kill_roll,omitempty"`
	PlayerKillInterval int32           `json:"player_kill_interval,omitempty"`
	Event              *StealEvent     `json:"event,omitempty"`
}

// StealProposal is a state-bound candidate.  before is deliberately private:
// it is a stale-snapshot fence, never a wire or client-controlled field.
// RNG is consumed only by PlanSteal; ApplySteal validates the recorded draw.
type StealProposal struct {
	ActorID             string
	RoomID              int16
	ItemName            string
	TargetName          string
	TargetID            string
	TargetKind          StealTargetKind
	TargetCanonicalName string
	ItemID              string
	CanonicalItemName   string
	Now                 int32
	Chance              int
	Roll                int
	Interval            int32
	WaitSeconds         int32
	PlayerKillRoll      int
	PlayerKillInterval  int32
	Cooldown            bool
	NoOp                bool
	ClearHidden         bool
	ClearInvisible      bool
	Reveal              bool
	TimerWrite          bool
	Attempted           bool
	Succeeded           bool
	EnemyAdded          bool
	Broadcast           bool
	Response            string
	ExpectedEvent       *StealEvent

	before State
}

type stealResolution struct {
	status     string
	targetID   string
	targetKind StealTargetKind
	target     LegacyMonster
	itemID     string
	item       Item
	chance     int
}

const (
	stealReady           = "ready"
	stealNoTarget        = "no-target"
	stealNoHarm          = "no-harm"
	stealAlreadyFighting = "already-fighting"
	stealSafeRoom        = "safe-room"
	stealActorLawful     = "actor-lawful"
	stealTargetLawful    = "target-lawful"
	stealBlind           = "blind"
	stealNoItem          = "no-item"
)

func validStealName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.ContainsAny(name, " \t\r\n") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func stealActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if !ok || actorID == "" || !actor.Online || actor.Body.Type != 0 || !validStealName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online steal actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("steal actor room membership absent")
	}
	return actor, room, nil
}

func stealVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt skips PDMINV on caretaker+ targets, and ordinary
	// MINVIS targets require the actor's PDINVI bit.
	if target.Class >= stealCaretakerClass && flag(target.Flags[:], stealPlayerDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], stealPlayerInvisible) || flag(actor.Flags[:], stealPlayerDetect)
}

func selectStealTarget(s State, actorID, name string) (string, StealTargetKind, LegacyMonster, bool, error) {
	actor, room, err := stealActor(s, actorID)
	if err != nil {
		return "", "", LegacyMonster{}, false, err
	}
	if !validStealName(name) {
		return "", "", LegacyMonster{}, false, fmt.Errorf("invalid steal target name")
	}
	if s.NPCs == nil && (len(room.NPCIDs) != 0 || len(room.Resource.Monsters) != 0) {
		return "", "", LegacyMonster{}, false, fmt.Errorf("%w: canonical NPC identities required", ErrStealTargetUnavailable)
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validStealName(npc.Body.Name) {
			return "", "", LegacyMonster{}, false, fmt.Errorf("unresolved canonical steal NPC identity")
		}
		if strings.EqualFold(npc.Body.Name, name) && stealVisible(actor.Body, npc.Body) {
			return id, StealTargetNPC, npc.Body, true, nil
		}
	}
	// command6.c deliberately blocks the player fallback while blind.  A
	// visible NPC above still wins, after which the later blind gate applies.
	if flag(actor.Body.Flags[:], stealPlayerBlind) {
		return "", "", LegacyMonster{}, false, nil
	}
	for _, id := range room.PlayerIDs {
		target, ok := s.Players[id]
		if id == "" || !ok || !target.Online || target.Body.Type != 0 || target.Body.RoomID != room.Resource.ID || !validStealName(target.Body.Name) {
			return "", "", LegacyMonster{}, false, fmt.Errorf("unresolved canonical steal player identity")
		}
		if id == actorID || !strings.EqualFold(target.Body.Name, name) || !stealVisible(actor.Body, target.Body) {
			continue
		}
		return id, StealTargetPlayer, target.Body, true, nil
	}
	return "", "", LegacyMonster{}, false, nil
}

func stealPronoun(target LegacyMonster) string {
	if flag(target.Flags[:], stealPlayerMale) {
		return "그"
	}
	return "그녀"
}

func stealItemRoot(items *ItemCollection, actor LegacyMonster, name string) (string, Item, bool, error) {
	if items == nil {
		return "", Item{}, false, fmt.Errorf("%w: target item collection is absent", ErrStealInventoryUnresolved)
	}
	if err := items.Validate(); err != nil {
		return "", Item{}, false, fmt.Errorf("%w: invalid target item collection: %v", ErrStealInventoryUnresolved, err)
	}
	detect := flag(actor.Flags[:], stealPlayerDetect)
	for _, id := range items.Inventory {
		item, ok := items.Items[id]
		if !ok || id == "" || !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		if flag(item.Object.Flags[:], stealObjectInvisible) && !detect {
			continue
		}
		return id, item, true, nil
	}
	return "", Item{}, false, nil
}

func stealTreeProtected(items ItemCollection, root string) (bool, error) {
	seen := map[string]bool{}
	var visit func(string) (bool, error)
	visit = func(id string) (bool, error) {
		if id == "" || seen[id] {
			return false, fmt.Errorf("cyclic or empty steal subtree")
		}
		item, ok := items.Items[id]
		if !ok {
			return false, fmt.Errorf("steal subtree item absent")
		}
		seen[id] = true
		if item.Object.Quest != 0 || flag(item.Object.Flags[:], stealObjectOneWay) {
			return true, nil
		}
		for _, child := range item.Contents {
			protected, err := visit(child)
			if err != nil || protected {
				return protected, err
			}
		}
		return false, nil
	}
	return visit(root)
}

func stealLevelBand(level byte) int { return (int(level) + 3) / 4 }

func stealChance(actor, target LegacyMonster, item Item, subtreeProtected bool) (int, error) {
	if actor.Class > stealDMClass || target.Class > stealDMClass {
		return 0, fmt.Errorf("steal class outside canonical legacy table")
	}
	if actor.Stats[1] > 63 {
		return 0, fmt.Errorf("steal dexterity outside legacy bonus table")
	}
	chance := 0
	if actor.Class == stealThiefClass {
		chance = 4 * stealLevelBand(actor.Level)
	} else {
		chance = 3 * stealLevelBand(actor.Level)
	}
	chance += legacyStatBonus[actor.Stats[1]] * 3
	if target.Level > actor.Level {
		chance -= 15 * (stealLevelBand(target.Level) - stealLevelBand(actor.Level))
	}
	if item.Object.Quest != 0 {
		chance = 0
	}
	if chance > 65 {
		chance = 65
	}
	if item.Object.Quest != 0 || flag(target.Flags[:], stealNPCNoSteal) {
		chance = 0
	}
	if actor.Class == stealDMClass {
		chance = 100
	}
	if target.Class == stealDMClass {
		chance = 0
	}
	// Source order matters: a top-level ONEWEV item remains protected even
	// from a DM; nested quest/ONEWEV content is protected for non-DMs below.
	if flag(item.Object.Flags[:], stealObjectOneWay) {
		chance = 0
	}
	if (item.Object.Quest != 0 || subtreeProtected) && actor.Class < stealDMClass {
		chance = 0
	}
	return chance, nil
}

func stealRoll(roll func(int, int) int) (int, error) {
	if roll == nil {
		return 0, fmt.Errorf("missing steal random source")
	}
	return randomIn(roll, 1, 100)
}

func stealWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("steal cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds), nil
}

func stealResolutionResponse(res stealResolution) string {
	switch res.status {
	case stealNoTarget:
		return "그런건 여기 없습니다.\r\n"
	case stealNoHarm:
		return fmt.Sprintf("당신은 %s를 해칠수 없습니다.\r\n", stealPronoun(res.target))
	case stealAlreadyFighting:
		return fmt.Sprintf("%s는 싸우는 중이 아닙니다.\r\n", stealPronoun(res.target))
	case stealSafeRoom:
		return "이 방에서는 훔칠 수 없습니다.\r\n"
	case stealActorLawful:
		return "당신은 선해서 훔칠 수 없습니다.\r\n"
	case stealTargetLawful:
		return "그 사용자는 선해서 보호받고 있습니다.\r\n"
	case stealBlind:
		return "당신은 눈이 멀어 훔칠 수 없습니다.\r\n"
	case stealNoItem:
		return fmt.Sprintf("%s는 그런 물건을 갖고 있지 않습니다.\r\n", stealPronoun(res.target))
	default:
		return ""
	}
}

func stealAttemptResponse(res stealResolution) string {
	if res.targetKind == StealTargetNPC {
		return "실패하였습니다.\r\n그가 당신을 공격합니다.\r\n"
	}
	return "실패하였습니다.\r\n"
}

func stealRoomTexts(actor LegacyMonster, reveal bool, res stealResolution, failed bool) []string {
	texts := make([]string, 0, 2)
	if reveal {
		texts = append(texts, fmt.Sprintf("\n%s%s 모습이 서서히 드러납니다.\r\n", actor.Name, legacySubjectParticle(actor.Name)))
	}
	if failed && res.targetID != "" {
		texts = append(texts, fmt.Sprintf("\n%s%s %s에게서 물건을 훔치려고 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), res.target.Name))
	}
	return texts
}

func stealEvent(actorID string, actor LegacyMonster, res stealResolution, reveal, failed bool) *StealEvent {
	texts := stealRoomTexts(actor, reveal, res, failed)
	if len(texts) == 0 {
		return nil
	}
	event := &StealEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: res.targetID, TargetKind: res.targetKind,
		TargetName: res.target.Name, ItemID: res.itemID, ItemName: res.item.Object.Name,
		Texts: texts,
	}
	if reveal && len(texts) > 0 {
		event.RevealText = texts[0]
	}
	if failed && res.targetKind == StealTargetPlayer && res.itemID != "" {
		event.TargetText = fmt.Sprintf("\n%s%s 당신에게서 %s 훔치려고 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), res.item.Object.Name)
	}
	return event
}

func addStealEnemy(npc *NPCState, actorID string) error {
	if npc == nil || npc.Enemies == nil {
		return fmt.Errorf("steal NPC enemy relations unresolved")
	}
	for _, enemy := range npc.Enemies {
		if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
			return fmt.Errorf("steal NPC already hostile")
		}
	}
	npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: actorID}, Damage: 0})
	return nil
}

func (s State) resolveSteal(actorID, itemName, targetName string) (stealResolution, error) {
	actor, room, err := stealActor(s, actorID)
	if err != nil {
		return stealResolution{}, err
	}
	targetID, targetKind, target, found, err := selectStealTarget(s, actorID, targetName)
	if err != nil {
		return stealResolution{}, err
	}
	if !found {
		return stealResolution{status: stealNoTarget}, nil
	}
	res := stealResolution{targetID: targetID, targetKind: targetKind, target: target}
	if targetKind == StealTargetNPC {
		npc := s.NPCs[targetID]
		if npc.Enemies == nil {
			return stealResolution{}, fmt.Errorf("steal NPC enemy relations unresolved")
		}
		for _, enemy := range npc.Enemies {
			if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
				res.status = stealAlreadyFighting
				return res, nil
			}
		}
		if flag(target.Flags[:], stealNPCNoHarm) {
			res.status = stealNoHarm
			return res, nil
		}
	} else {
		if flag(room.Resource.Flags[:], stealRoomNoPlayerKill) {
			res.status = stealSafeRoom
			return res, nil
		}
		if !flag(actor.Body.Flags[:], stealPlayerChaos) && actor.Body.Class < stealSubDMClass {
			res.status = stealActorLawful
			return res, nil
		}
		if !flag(target.Flags[:], stealPlayerChaos) && actor.Body.Class < stealSubDMClass {
			res.status = stealTargetLawful
			return res, nil
		}
	}
	if flag(actor.Body.Flags[:], stealPlayerBlind) {
		res.status = stealBlind
		return res, nil
	}
	if !validStealName(itemName) {
		return stealResolution{}, fmt.Errorf("invalid steal item name")
	}
	var items *ItemCollection
	if targetKind == StealTargetNPC {
		npc := s.NPCs[targetID]
		if npc.Items == nil || len(npc.Body.Inventory) != 0 {
			return stealResolution{}, fmt.Errorf("%w: canonical NPC inventory required", ErrStealInventoryUnresolved)
		}
		items = npc.Items
	} else {
		player := s.Players[targetID]
		if player.Items == nil || len(player.Body.Inventory) != 0 {
			return stealResolution{}, fmt.Errorf("%w: canonical player inventory required", ErrStealInventoryUnresolved)
		}
		items = player.Items
	}
	itemID, item, present, err := stealItemRoot(items, actor.Body, itemName)
	if err != nil {
		return stealResolution{}, err
	}
	if !present {
		res.status = stealNoItem
		return res, nil
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return stealResolution{}, fmt.Errorf("%w: canonical actor inventory required", ErrStealInventoryUnresolved)
	}
	protected, err := stealTreeProtected(*items, itemID)
	if err != nil {
		return stealResolution{}, err
	}
	chance, err := stealChance(actor.Body, target, item, protected)
	if err != nil {
		return stealResolution{}, err
	}
	res.status, res.itemID, res.item, res.chance = stealReady, itemID, item, chance
	return res, nil
}

func (s State) PlanSteal(actorID, itemName, targetName string, now int32, roll func(int, int) int) (StealProposal, error) {
	if err := s.Validate(); err != nil {
		return StealProposal{}, err
	}
	if actorID == "" || now < 0 || !validStealName(itemName) || !validStealName(targetName) {
		return StealProposal{}, fmt.Errorf("invalid steal actor, item, target, or clock")
	}
	actor, room, err := stealActor(s, actorID)
	if err != nil {
		return StealProposal{}, err
	}
	proposal := StealProposal{ActorID: actorID, RoomID: room.Resource.ID, ItemName: itemName, TargetName: targetName, Now: now, Interval: stealCooldownSeconds, before: s.clone()}
	if actor.Body.Class > stealDMClass {
		return StealProposal{}, fmt.Errorf("steal actor class outside canonical legacy table")
	}
	if actor.Body.Class != stealThiefClass && actor.Body.Class < stealInvincibleClass {
		proposal.NoOp = true
		proposal.Response = "도둑만 훔칠수 있습니다.\r\n"
		return proposal, nil
	}
	timer := actor.Body.Timers[stealTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return StealProposal{}, fmt.Errorf("steal timer outside legacy range")
	}
	proposal.ClearHidden = true
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait < 1 || wait > math.MaxInt32 {
			return StealProposal{}, fmt.Errorf("steal cooldown overflow")
		}
		proposal.Cooldown, proposal.WaitSeconds = true, int32(wait)
		proposal.Response, err = stealWaitResponse(proposal.WaitSeconds)
		if err != nil {
			return StealProposal{}, err
		}
		return proposal, nil
	}
	proposal.ClearInvisible = true
	proposal.Reveal = flag(actor.Body.Flags[:], stealPlayerInvisible)
	proposal.TimerWrite = true
	resolution, err := s.resolveSteal(actorID, itemName, targetName)
	if err != nil {
		return StealProposal{}, err
	}
	proposal.TargetID, proposal.TargetKind, proposal.TargetCanonicalName = resolution.targetID, resolution.targetKind, resolution.target.Name
	proposal.ItemID, proposal.CanonicalItemName, proposal.Chance = resolution.itemID, resolution.item.Object.Name, resolution.chance
	if resolution.status != stealReady {
		proposal.Response = stealResolutionResponse(resolution)
		proposal.ExpectedEvent = stealEvent(actorID, actor.Body, resolution, proposal.Reveal, false)
		if proposal.Reveal {
			proposal.Response = "당신의 모습이 서서히 드러납니다.\r\n" + proposal.Response
		}
		proposal.Broadcast = proposal.ExpectedEvent != nil
		return proposal, nil
	}
	value, err := stealRoll(roll)
	if err != nil {
		return StealProposal{}, err
	}
	proposal.Roll, proposal.Attempted, proposal.Succeeded = value, true, value <= resolution.chance
	if proposal.Succeeded {
		proposal.Response = "훔쳤습니다.\r\n"
		proposal.ExpectedEvent = stealEvent(actorID, actor.Body, resolution, proposal.Reveal, false)
		if resolution.targetKind == StealTargetPlayer {
			killRoll, rollErr := randomIn(roll, 7, 10)
			if rollErr != nil {
				return StealProposal{}, rollErr
			}
			proposal.PlayerKillRoll = killRoll
			proposal.PlayerKillInterval = int32(int64(killRoll) * 86400)
		}
	} else {
		proposal.Response = stealAttemptResponse(resolution)
		proposal.EnemyAdded = resolution.targetKind == StealTargetNPC
		proposal.ExpectedEvent = stealEvent(actorID, actor.Body, resolution, proposal.Reveal, true)
	}
	if proposal.Reveal {
		proposal.Response = "당신의 모습이 서서히 드러납니다.\r\n" + proposal.Response
	}
	proposal.Broadcast = proposal.ExpectedEvent != nil
	return proposal, nil
}

func (s State) ApplySteal(proposal StealProposal) (State, StealResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, StealResult{}, err
	}
	if proposal.ActorID == "" || proposal.Now < 0 || proposal.Interval != stealCooldownSeconds || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, StealResult{}, fmt.Errorf("stale or invalid steal proposal")
	}
	if _, ok := s.Rooms[proposal.RoomID]; !ok {
		return State{}, StealResult{}, fmt.Errorf("steal proposal room absent")
	}
	actor, room, err := stealActor(s, proposal.ActorID)
	if err != nil || room.Resource.ID != proposal.RoomID {
		return State{}, StealResult{}, fmt.Errorf("steal actor changed")
	}
	result := StealResult{Action: "steal", Response: proposal.Response, Changed: proposal.ClearHidden || proposal.TimerWrite, Broadcast: proposal.Broadcast, Cooldown: proposal.Cooldown, WaitSeconds: proposal.WaitSeconds, Succeeded: proposal.Succeeded, Attempted: proposal.Attempted, Chance: proposal.Chance, Roll: proposal.Roll, ActorID: proposal.ActorID, TargetID: proposal.TargetID, TargetKind: proposal.TargetKind, TargetName: proposal.TargetCanonicalName, ItemID: proposal.ItemID, ItemName: proposal.CanonicalItemName, EnemyAdded: proposal.EnemyAdded, PlayerKillRoll: proposal.PlayerKillRoll, PlayerKillInterval: proposal.PlayerKillInterval, Event: proposal.ExpectedEvent}
	if proposal.NoOp {
		if actor.Body.Class == stealThiefClass || actor.Body.Class < stealInvincibleClass || proposal.ClearHidden || proposal.ClearInvisible || proposal.Cooldown || proposal.TimerWrite || proposal.Attempted || proposal.Succeeded || proposal.Roll != 0 || proposal.ExpectedEvent != nil {
			return State{}, StealResult{}, fmt.Errorf("stale steal authorization proposal")
		}
		return s.clone(), result, nil
	}
	if actor.Body.Class != stealThiefClass && actor.Body.Class < stealInvincibleClass {
		return State{}, StealResult{}, fmt.Errorf("steal authorization changed")
	}
	if actor.Body.Class > stealDMClass || !proposal.ClearHidden || (proposal.Cooldown && proposal.ClearInvisible) || (!proposal.Cooldown && !proposal.ClearInvisible) {
		return State{}, StealResult{}, fmt.Errorf("invalid steal authorization proposal")
	}
	timer := actor.Body.Timers[stealTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, StealResult{}, fmt.Errorf("steal timer changed")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	setSettingFlag(&nextActor.Body, stealPlayerHidden, false)
	if proposal.Cooldown {
		if proposal.ClearInvisible || proposal.TimerWrite || proposal.Attempted || proposal.Succeeded || proposal.Roll != 0 || proposal.TargetID != "" || proposal.ExpectedEvent != nil || int64(proposal.Now) >= deadline {
			return State{}, StealResult{}, fmt.Errorf("invalid steal cooldown proposal")
		}
		wait := deadline - int64(proposal.Now)
		response, responseErr := stealWaitResponse(int32(wait))
		if responseErr != nil || proposal.WaitSeconds != int32(wait) || response != proposal.Response {
			return State{}, StealResult{}, fmt.Errorf("stale steal cooldown proposal")
		}
		next.Players[proposal.ActorID] = nextActor
		result.Changed = flag(actor.Body.Flags[:], stealPlayerHidden)
		result.Broadcast = false
		result.Event = nil
		return next, result, nil
	}
	if int64(proposal.Now) < deadline || !proposal.TimerWrite || proposal.WaitSeconds != 0 {
		return State{}, StealResult{}, fmt.Errorf("steal cooldown changed")
	}
	if proposal.Reveal != flag(actor.Body.Flags[:], stealPlayerInvisible) {
		return State{}, StealResult{}, fmt.Errorf("steal invisibility proposal changed")
	}
	if proposal.Reveal {
		setSettingFlag(&nextActor.Body, stealPlayerInvisible, false)
	}
	// Publish the premovement visibility changes into the working snapshot
	// before a successful item transfer re-reads the actor from next.Players.
	next.Players[proposal.ActorID] = nextActor
	resolution, err := s.resolveSteal(proposal.ActorID, proposal.ItemName, proposal.TargetName)
	if err != nil {
		return State{}, StealResult{}, err
	}
	if resolution.targetID != proposal.TargetID || resolution.targetKind != proposal.TargetKind || resolution.target.Name != proposal.TargetCanonicalName || resolution.itemID != proposal.ItemID || resolution.item.Object.Name != proposal.CanonicalItemName || resolution.chance != proposal.Chance {
		return State{}, StealResult{}, fmt.Errorf("stale steal target or item identity")
	}
	if resolution.status != stealReady {
		if proposal.Attempted || proposal.Succeeded || proposal.Roll != 0 || proposal.EnemyAdded || proposal.Chance != 0 || proposal.ItemID != "" {
			return State{}, StealResult{}, fmt.Errorf("invalid rejected steal proposal")
		}
		wantResponse := stealResolutionResponse(resolution)
		if proposal.Reveal {
			wantResponse = "당신의 모습이 서서히 드러납니다.\r\n" + wantResponse
		}
		if proposal.Response != wantResponse {
			return State{}, StealResult{}, fmt.Errorf("stale steal rejection response")
		}
		wantEvent := stealEvent(proposal.ActorID, actor.Body, resolution, proposal.Reveal, false)
		if !reflect.DeepEqual(wantEvent, proposal.ExpectedEvent) || proposal.Broadcast != (wantEvent != nil) {
			return State{}, StealResult{}, fmt.Errorf("stale steal rejection event")
		}
		next.Players[proposal.ActorID] = nextActor
		nextActor.Body.Timers[stealTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: stealCooldownSeconds}
		next.Players[proposal.ActorID] = nextActor
		result.Response = wantResponse
		if err := next.Validate(); err != nil {
			return State{}, StealResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}
	if !proposal.Attempted || proposal.Roll < 1 || proposal.Roll > 100 || proposal.Succeeded != (proposal.Roll <= resolution.chance) {
		return State{}, StealResult{}, fmt.Errorf("invalid steal random outcome")
	}
	if resolution.targetKind == StealTargetPlayer && proposal.Succeeded {
		if proposal.PlayerKillRoll < 7 || proposal.PlayerKillRoll > 10 || proposal.PlayerKillInterval != int32(int64(proposal.PlayerKillRoll)*86400) {
			return State{}, StealResult{}, fmt.Errorf("invalid player steal cooldown outcome")
		}
	} else if proposal.PlayerKillRoll != 0 || proposal.PlayerKillInterval != 0 {
		return State{}, StealResult{}, fmt.Errorf("unexpected player steal cooldown outcome")
	}
	if proposal.Succeeded {
		if proposal.EnemyAdded {
			return State{}, StealResult{}, fmt.Errorf("invalid successful steal proposal")
		}
		wantResponse := "훔쳤습니다.\r\n"
		if proposal.Reveal {
			wantResponse = "당신의 모습이 서서히 드러납니다.\r\n" + wantResponse
		}
		wantEvent := stealEvent(proposal.ActorID, actor.Body, resolution, proposal.Reveal, false)
		if proposal.Response != wantResponse || !reflect.DeepEqual(wantEvent, proposal.ExpectedEvent) || proposal.Broadcast != (wantEvent != nil) {
			return State{}, StealResult{}, fmt.Errorf("stale successful steal projection")
		}
		if resolution.targetKind == StealTargetPlayer {
			if s.Players[resolution.targetID].Items == nil {
				return State{}, StealResult{}, fmt.Errorf("target player inventory disappeared")
			}
		}
		plan, transferErr := stealTransfer(next, proposal, resolution)
		if transferErr != nil {
			return State{}, StealResult{}, transferErr
		}
		next = plan
		nextActor = next.Players[proposal.ActorID]
		if resolution.targetKind == StealTargetPlayer {
			target := next.Players[resolution.targetID]
			target.Body.Timers[stealPlayerKillTimer] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.PlayerKillInterval}
			next.Players[resolution.targetID] = target
		}
	} else {
		if proposal.Response != stealAttemptResponse(resolution) && proposal.Response != "당신의 모습이 서서히 드러납니다.\r\n"+stealAttemptResponse(resolution) {
			return State{}, StealResult{}, fmt.Errorf("stale failed steal response")
		}
		wantEvent := stealEvent(proposal.ActorID, actor.Body, resolution, proposal.Reveal, true)
		if !reflect.DeepEqual(wantEvent, proposal.ExpectedEvent) || proposal.Broadcast != (wantEvent != nil) || proposal.EnemyAdded != (resolution.targetKind == StealTargetNPC) {
			return State{}, StealResult{}, fmt.Errorf("stale failed steal event")
		}
		if resolution.targetKind == StealTargetNPC {
			npc := next.NPCs[resolution.targetID]
			if err := addStealEnemy(&npc, proposal.ActorID); err != nil {
				return State{}, StealResult{}, err
			}
			next.NPCs[resolution.targetID] = npc
		}
	}
	nextActor = next.Players[proposal.ActorID]
	nextActor.Body.Timers[stealTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: stealCooldownSeconds}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, StealResult{}, err
	}
	result.Changed = true
	return next, result, nil
}

func stealTransfer(next State, proposal StealProposal, resolution stealResolution) (State, error) {
	actor := next.Players[proposal.ActorID]
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return State{}, fmt.Errorf("%w: canonical actor inventory required", ErrStealInventoryUnresolved)
	}
	var source, destination ItemCollection
	if resolution.targetKind == StealTargetNPC {
		npc, ok := next.NPCs[resolution.targetID]
		if !ok || npc.Items == nil || len(npc.Body.Inventory) != 0 {
			return State{}, fmt.Errorf("%w: canonical NPC inventory required", ErrStealInventoryUnresolved)
		}
		source, destination = *npc.Items, *actor.Items
	} else {
		target, ok := next.Players[resolution.targetID]
		if !ok || target.Items == nil || len(target.Body.Inventory) != 0 {
			return State{}, fmt.Errorf("%w: canonical player inventory required", ErrStealInventoryUnresolved)
		}
		source, destination = *target.Items, *actor.Items
	}
	transfer, err := TransferItemRoots(source, destination, []string{proposal.ItemID})
	if err != nil {
		return State{}, err
	}
	if resolution.targetKind == StealTargetNPC {
		npc := next.NPCs[resolution.targetID]
		npc.Items = &transfer.Source
		next.NPCs[resolution.targetID] = npc
	} else {
		target := next.Players[resolution.targetID]
		target.Items = &transfer.Source
		next.Players[resolution.targetID] = target
	}
	actor.Items = &transfer.Destination
	next.Players[proposal.ActorID] = actor
	return next, nil
}

// StealByName is the pure one-call reducer boundary for tests and non-session
// callers. The session adapter should prefer Plan/Apply through ExecuteGame.
func (s State) StealByName(actorID, itemName, targetName string, now int32, roll func(int, int) int) (State, StealResult, error) {
	proposal, err := s.PlanSteal(actorID, itemName, targetName, now, roll)
	if err != nil {
		return State{}, StealResult{}, err
	}
	return s.ApplySteal(proposal)
}
