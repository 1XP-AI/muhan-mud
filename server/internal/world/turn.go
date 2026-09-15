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

// turn (방혼술) is the bounded command62 slice from src/magic3.c.  The
// numeric values are the legacy state slots and flags; they are intentionally
// kept here instead of being recreated in a network adapter.
const (
	turnPlayerType         = 0
	turnMonsterType        = 1
	turnCooldownTimerIndex = 15 // LT_TURNS
	turnAttackTimerIndex   = 3  // LT_ATTCK
	turnCooldownSeconds    = int32(30)
	turnClericClass        = 3  // CLERIC
	turnPaladinClass       = 6  // PALADIN
	turnInvincibleClass    = 9  // INVINCIBLE
	turnMaxLegacyClass     = 12 // DM
	turnActorHiddenFlag    = 1  // PHIDDN (not cleared by turn)
	turnActorInvisibleFlag = 2  // PINVIS
	turnActorDetectFlag    = 21 // PDINVI
	turnPlayerMaleFlag     = 12 // PMALES/MMALES
	turnNPCPermanentFlag   = 0  // MPERMT
	turnNPCInvisibleFlag   = 2  // MINVIS
	turnNPCUndeadFlag      = 14 // MUNDED
	turnNPCNoHarmFlag      = 24 // MUNKIL
	turnNPCDMInvisibleFlag = 10 // PDMINV/MDINVI caretaker projection
)

// Export source-backed slots and flags for command adapters, expiry/tick
// integration, and differential contract tests.
const (
	TurnPlayerType          = turnPlayerType
	TurnMonsterType         = turnMonsterType
	TurnCooldownTimerIndex  = turnCooldownTimerIndex
	TurnAttackTimerIndex    = turnAttackTimerIndex
	TurnTimerIndex          = turnCooldownTimerIndex
	TurnCooldownTimer       = turnCooldownTimerIndex
	TurnAttackTimer         = turnAttackTimerIndex
	TurnCooldownSeconds     = turnCooldownSeconds
	TurnClericClass         = turnClericClass
	TurnPaladinClass        = turnPaladinClass
	TurnInvincibleClass     = turnInvincibleClass
	TurnMaxLegacyClass      = turnMaxLegacyClass
	TurnActorInvisibleFlag  = turnActorInvisibleFlag
	TurnPlayerInvisibleFlag = turnActorInvisibleFlag
	TurnActorDetectFlag     = turnActorDetectFlag
	TurnPlayerDetectFlag    = turnActorDetectFlag
	TurnPlayerMaleFlag      = turnPlayerMaleFlag
	TurnNPCPermanentFlag    = turnNPCPermanentFlag
	TurnNPCInvisibleFlag    = turnNPCInvisibleFlag
	TurnNPCUndeadFlag       = turnNPCUndeadFlag
	TurnNPCNoHarmFlag       = turnNPCNoHarmFlag
	TurnNPCDMInvisibleFlag  = turnNPCDMInvisibleFlag
)

var (
	// ErrTurnCombatSideEffectPending means that add_enm_crt/add_enm_dmg
	// cannot be represented by the canonical NPC relation in this snapshot.
	ErrTurnCombatSideEffectPending = errors.New("turn combat side effect pending")
	// ErrTurnDeathTransitionPending prevents an HP-only corpse when the
	// existing canonical NPC death reducer cannot complete the full transition.
	ErrTurnDeathTransitionPending = errors.New("turn death transition pending")
	// ErrTurnNPCStateUnresolved means room.NPCIDs cannot be resolved to one
	// canonical NPC identity.
	ErrTurnNPCStateUnresolved = errors.New("turn canonical NPC state unresolved")

	// Descriptive aliases let composite combat code use its existing wording
	// without string matching on an error message.
	ErrTurnCombatPending   = ErrTurnCombatSideEffectPending
	ErrTurnDeathPending    = ErrTurnDeathTransitionPending
	ErrTurnStateUnresolved = ErrTurnNPCStateUnresolved
)

// TurnTargetKind is deliberately closed to canonical NPC identities.  The C
// command starts find_crt at the room monster list and has no player fallback.
type TurnTargetKind string

const (
	TurnTargetNPC TurnTargetKind = "npc"
	TurnNPC       TurnTargetKind = TurnTargetNPC
)

// TurnEvent is a post-commit room projection.  The actor receives
// TurnResult.Response, so the transport excludes ExcludeActorID when it fans
// out Texts.  Replay returns this stored projection but never publishes it a
// second time.
type TurnEvent struct {
	RoomID         int16          `json:"room_id"`
	ExcludeActorID string         `json:"exclude_actor_id"`
	ActorID        string         `json:"actor_id"`
	ActorName      string         `json:"actor_name"`
	TargetID       string         `json:"target_id"`
	TargetKind     TurnTargetKind `json:"target_kind"`
	TargetName     string         `json:"target_name"`
	Texts          []string       `json:"texts"`
	Text           string         `json:"text"`
}

// TurnResult is the deterministic actor response persisted in the command
// receipt.  Every random value used by the first attempt is retained so a
// retry with the same command ID cannot select another NPC or reroll.
type TurnResult struct {
	Action             string          `json:"action"`
	Response           string          `json:"response"`
	Changed            bool            `json:"changed"`
	Broadcast          bool            `json:"broadcast"`
	NoOp               bool            `json:"no_op,omitempty"`
	Authorized         bool            `json:"authorized"`
	TargetFound        bool            `json:"target_found"`
	Rejected           bool            `json:"rejected,omitempty"`
	NoHarm             bool            `json:"no_harm,omitempty"`
	Cooldown           bool            `json:"cooldown,omitempty"`
	WaitSeconds        int32           `json:"wait_seconds,omitempty"`
	Attempted          bool            `json:"attempted,omitempty"`
	Succeeded          bool            `json:"succeeded,omitempty"`
	Disintegrated      bool            `json:"disintegrated,omitempty"`
	Killed             bool            `json:"killed,omitempty"`
	Chance             int             `json:"chance,omitempty"`
	Roll               int             `json:"roll,omitempty"`
	DisintegrateRoll   int             `json:"disintegrate_roll,omitempty"`
	Damage             int             `json:"damage,omitempty"`
	EnemyDamage        int             `json:"enemy_damage,omitempty"`
	TargetHPBefore     int             `json:"target_hp_before,omitempty"`
	TargetHP           int             `json:"target_hp,omitempty"`
	EnemyAdded         bool            `json:"enemy_added,omitempty"`
	ClearInvisible     bool            `json:"clear_invisible,omitempty"`
	ActorTimerWritten  bool            `json:"actor_timer_written,omitempty"`
	AttackTimerWritten bool            `json:"attack_timer_written,omitempty"`
	CooldownInterval   int32           `json:"cooldown_interval,omitempty"`
	AttackInterval     int32           `json:"attack_interval,omitempty"`
	ActorID            string          `json:"actor_id,omitempty"`
	ActorName          string          `json:"actor_name,omitempty"`
	RoomID             int16           `json:"room_id,omitempty"`
	TargetID           string          `json:"target_id,omitempty"`
	TargetKind         TurnTargetKind  `json:"target_kind,omitempty"`
	TargetName         string          `json:"target_name,omitempty"`
	TargetOccurrence   int             `json:"target_occurrence,omitempty"`
	Death              *NPCDeathResult `json:"death,omitempty"`
	Event              *TurnEvent      `json:"event,omitempty"`
}

// TurnOptions contains host-owned dependencies needed by the reducer.  The
// allocator is only consulted when a successful turn kills an NPC with a
// canonical drop graph; it is never read from the terminal command.
type TurnOptions struct {
	Allocate func() (string, error)
}

// TurnProposal is a snapshot-bound candidate.  before and next are private so
// callers cannot manufacture authority or a death transition by editing a
// JSON-compatible command result. ApplyTurn validates all public projections
// and returns the already-planned next state without another random draw.
type TurnProposal struct {
	ActorID          string
	ActorName        string
	RoomID           int16
	QueryName        string
	TargetID         string
	TargetKind       TurnTargetKind
	TargetName       string
	TargetOccurrence int
	Now              int32

	Chance           int
	Roll             int
	DisintegrateRoll int
	Damage           int
	EnemyDamage      int
	TargetHPBefore   int
	TargetHP         int
	WaitSeconds      int32

	Authorized       bool
	TargetFound      bool
	Rejected         bool
	NoHarm           bool
	NoOp             bool
	Cooldown         bool
	Attempted        bool
	Succeeded        bool
	Disintegrated    bool
	Killed           bool
	EnemyAdded       bool
	DamageApplied    bool
	ClearInvisible   bool
	ActorTimerWrite  bool
	AttackTimerWrite bool
	CooldownInterval int32
	AttackInterval   int32
	Broadcast        bool
	Response         string
	ExpectedEvent    *TurnEvent
	Death            *NPCDeathResult

	before State
	next   State
}

type turnResolution struct {
	id         string
	name       string
	target     LegacyMonster
	occurrence int
}

func validTurnName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func turnActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != turnPlayerType || !validTurnName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online turn actor absent")
	}
	if actor.Body.HPCurrent < 1 {
		return PlayerState{}, RoomState{}, fmt.Errorf("%w: turn actor is dead", ErrTurnDeathTransitionPending)
	}
	if actor.Body.Class > turnMaxLegacyClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("turn actor class outside canonical table")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("turn actor room membership absent")
	}
	return actor, room, nil
}

func turnTargetVisible(actor, target LegacyMonster) bool {
	// find_crt suppresses DM/caretaker PDMINV identities and ordinary invisible
	// monsters unless the actor has PDINVI.
	if target.Class >= turnInvincibleClass+1 && flag(target.Flags[:], turnNPCDMInvisibleFlag) {
		return false
	}
	return !flag(target.Flags[:], turnNPCInvisibleFlag) || flag(actor.Flags[:], turnActorDetectFlag)
}

// turnCreaturePrefixMatch is the local turn boundary for creature.c's EQUAL.
// Each field is an alternative, so a root matching its display name and one or
// more keys still contributes exactly one occurrence to find_crt's counter.
func turnCreaturePrefixMatch(target LegacyMonster, query string) bool {
	if query == "" || !utf8.ValidString(query) {
		return false
	}
	if equalFoldPrefix(target.Name, query) {
		return true
	}
	for _, key := range target.Keys {
		if equalFoldPrefix(key, query) {
			return true
		}
	}
	return false
}

func (s State) selectTurnNPC(actorID, query string, occurrence int) (turnResolution, error) {
	if occurrence < 1 {
		return turnResolution{}, fmt.Errorf("turn target occurrence must be positive")
	}
	if !validTurnName(query) {
		return turnResolution{}, fmt.Errorf("invalid turn target name")
	}
	actor, room, err := turnActor(s, actorID)
	if err != nil {
		return turnResolution{}, err
	}
	if s.NPCs == nil {
		return turnResolution{}, ErrTurnNPCStateUnresolved
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != turnMonsterType || npc.Body.RoomID != room.Resource.ID || !validTurnName(npc.Body.Name) {
			return turnResolution{}, fmt.Errorf("%w: unresolved canonical turn NPC identity", ErrTurnNPCStateUnresolved)
		}
		if !turnCreaturePrefixMatch(npc.Body, query) || !turnTargetVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return turnResolution{id: id, name: npc.Body.Name, target: npc.Body, occurrence: found}, nil
		}
	}
	return turnResolution{}, nil
}

// TurnChance returns the exact src/magic3.c formula before its 1..100 draw.
// Piety is the fifth legacy stat (Stats[4]); the finite table is checked
// rather than allowing a malformed imported value to index outside it.
func TurnChance(actor, target LegacyMonster) (int, error) {
	if actor.Class > turnMaxLegacyClass {
		return 0, fmt.Errorf("turn actor class outside legacy bonus table")
	}
	if actor.Stats[4] > 63 {
		return 0, fmt.Errorf("turn actor piety outside legacy bonus table")
	}
	chance := (((int(actor.Level)+3)/4)-((int(target.Level)+3)/4))*20 + legacyStatBonus[actor.Stats[4]]*5
	if actor.Class == turnPaladinClass {
		chance += 15
	} else {
		chance += 25
	}
	if chance > 80 {
		chance = 80
	}
	return chance, nil
}

func TurnHitChance(actor, target LegacyMonster) (int, error) { return TurnChance(actor, target) }

func turnWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("turn cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", seconds), nil
}

func turnRandomIn(roll func(int, int) int, low, high int) (value int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("turn random source panicked: %v", recovered)
		}
	}()
	return randomIn(roll, low, high)
}

func turnNoArgumentResponse() string { return "\n누구에게 주문을 거실려고요?\n" }
func turnUnauthorizedResponse() string {
	return "\n불제자와 무사만이 방혼술을 사용할수 있습니다.\n"
}
func turnMissingResponse() string   { return "\n그런 괴물은 존재하지 않습니다.\n" }
func turnNotUndeadResponse() string { return "\n죽은 괴물에게만 사용가능합니다.\n" }

func turnPronoun(target LegacyMonster) string {
	if flag(target.Flags[:], turnPlayerMaleFlag) {
		return "그"
	}
	return "그녀"
}

func turnNoHarmResponse(target LegacyMonster) string {
	return fmt.Sprintf("\n당신은 %s의 혼을 소멸시킬 수 없습니다.\n", turnPronoun(target))
}

func turnRevealResponse() string { return "\n당신의 모습이 나타나기 시작합니다.\n" }
func turnRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s의 모습이 보이기 시작합니다.\n", actor.Name, legacySubjectParticle(actor.Name))
}

func turnFailureResponse(target LegacyMonster) string {
	return fmt.Sprintf("\n부적을 하늘로 날리며 혼을 소환하는 방혼술의 주문을 외쳤습니다.하지만 주문이 튕겨져 나오면서 %s%s 당신의 주술을 견뎌냈습니다.\n", target.Name, legacySubjectParticle(target.Name))
}

func turnFailureRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 부적을 하늘로 날리며 혼을 소환시키는 방혼술의 주문을 외칩니다.\n하지만 주문이 튕겨져 나오면서 %s%s 그의 주술을 견뎌냈습니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name, legacySubjectParticle(target.Name))
}

func turnDisintegrateResponse(target LegacyMonster) string {
	return fmt.Sprintf("\n부적을 하늘로 날리며 혼을 소환시키는 방혼술의 주문을 외칩니다.\n부적이 빙글비글 돌면서 소용돌이를 일으키자 그 자리에서 저승사자가\n올라와 %s의 혼을 소멸시켜 버렸습니다.\n ", target.Name)
}

func turnDisintegrateRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 부적을 하늘로 날리며 혼을 소환시키는 방혼술의 주문을 외칩니다.\n부적이 빙글빙글 돌면서 소용돌이를 일으키자 그 자리에서 저승사자가\n올라와 %s의 혼을소멸시켜 버렸습니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func turnDamageResponse(target LegacyMonster, damage int) string {
	return fmt.Sprintf("\n부적을 하늘로 날리며 혼을 소환시키는 방혼술의 주문을 외칩니다.\n부적이 %s의 몸을 공격하며 %d만큼의 타격을 입혔습니다\n", target.Name, damage)
}

func turnDamageRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 부적을 하늘로 날리며 혼을 소환시키는 방혼술의 주문을 외칩니다.\n부적이 %s의 몸을 공격하며 타격을 입혔습니다\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func turnKillResponse(target LegacyMonster) string {
	return fmt.Sprintf("\n당신은 %s의 혼을 소멸시켰습니다.\n%s의 몸에서 혼이 사라지자 녹아버립니다.\n", target.Name, target.Name)
}

func turnKillRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s %s의 혼을 소멸시켰습니다.\n\n%s의 혼이 사라지자 몸이 녹아버립니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name, target.Name)
}

func turnEvent(actorID string, actor LegacyMonster, target turnResolution, texts []string) *TurnEvent {
	if len(texts) == 0 {
		return nil
	}
	copied := append([]string(nil), texts...)
	return &TurnEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: target.id, TargetKind: TurnTargetNPC,
		TargetName: target.name, Texts: copied, Text: strings.Join(copied, ""),
	}
}

func turnResult(p TurnProposal) TurnResult {
	return TurnResult{
		Action: pActionTurn, Response: p.Response,
		Changed:   p.ClearInvisible || p.ActorTimerWrite || p.AttackTimerWrite || p.EnemyAdded || p.DamageApplied || p.Killed,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Authorized: p.Authorized,
		TargetFound: p.TargetFound, Rejected: p.Rejected, NoHarm: p.NoHarm,
		Cooldown: p.Cooldown, WaitSeconds: p.WaitSeconds, Attempted: p.Attempted,
		Succeeded: p.Succeeded, Disintegrated: p.Disintegrated, Killed: p.Killed,
		Chance: p.Chance, Roll: p.Roll, DisintegrateRoll: p.DisintegrateRoll,
		Damage: p.Damage, EnemyDamage: p.EnemyDamage,
		TargetHPBefore: p.TargetHPBefore, TargetHP: p.TargetHP,
		EnemyAdded: p.EnemyAdded, ClearInvisible: p.ClearInvisible,
		ActorTimerWritten: p.ActorTimerWrite, AttackTimerWritten: p.AttackTimerWrite,
		CooldownInterval: p.CooldownInterval, AttackInterval: p.AttackInterval,
		ActorID: p.ActorID, ActorName: p.ActorName, RoomID: p.RoomID,
		TargetID: p.TargetID, TargetKind: p.TargetKind, TargetName: p.TargetName,
		TargetOccurrence: p.TargetOccurrence, Death: p.Death, Event: p.ExpectedEvent,
	}
}

const pActionTurn = "turn"

func turnEnemy(s State, npcID, actorID string) (bool, error) {
	npc, ok := s.NPCs[npcID]
	if s.NPCs == nil || !ok || npc.Enemies == nil {
		return false, ErrTurnCombatSideEffectPending
	}
	want := EntityRef{Kind: "player", ID: actorID}
	found := false
	for _, enemy := range npc.Enemies {
		if enemy.Damage < 0 {
			return false, fmt.Errorf("%w: negative enemy damage", ErrTurnCombatSideEffectPending)
		}
		if enemy.Target == want {
			if found {
				return false, fmt.Errorf("%w: duplicate enemy relation", ErrTurnCombatSideEffectPending)
			}
			found = true
		}
	}
	return found, nil
}

func turnPotentialDeath(s State, actorID, targetID string, maxDamage int, now int32, allocate func() (string, error)) error {
	if maxDamage < 1 {
		return nil
	}
	candidate := s.clone()
	npc, ok := candidate.NPCs[targetID]
	if !ok || npc.Body.HPCurrent < 1 {
		return fmt.Errorf("%w: target is not alive", ErrTurnDeathTransitionPending)
	}
	want := EntityRef{Kind: "player", ID: actorID}
	found := false
	for i := range npc.Enemies {
		if npc.Enemies[i].Target != want {
			continue
		}
		found = true
		if int64(npc.Enemies[i].Damage)+int64(maxDamage) > math.MaxInt32 {
			return fmt.Errorf("%w: enemy damage overflow", ErrTurnCombatSideEffectPending)
		}
		npc.Enemies[i].Damage += int32(maxDamage)
		break
	}
	if !found {
		npc.Enemies = append(npc.Enemies, NPCEnemy{Target: want, Damage: int32(maxDamage)})
	}
	npc.Body.HPCurrent = 0
	candidate.NPCs[targetID] = npc
	if _, _, err := candidate.PlanNPCDeath(targetID, actorID, now, allocate); err != nil {
		return fmt.Errorf("%w: %v", ErrTurnDeathTransitionPending, err)
	}
	return nil
}

// turnProbeAllocator lets PlanTurn validate the canonical death reducer
// without consuming the host's ID allocator. A lethal turn is planned before
// its random draws so unsupported drops/permanent/summon transitions fail
// closed; only the final candidate may ask the host for real item IDs.
func turnProbeAllocator(s State, targetID string, allocate func() (string, error)) func() (string, error) {
	if allocate == nil {
		return nil
	}
	used := map[string]bool{}
	remember := func(items *ItemCollection) {
		if items == nil {
			return
		}
		for id := range items.Items {
			used[id] = true
		}
	}
	for _, player := range s.Players {
		remember(player.Items)
	}
	for _, npc := range s.NPCs {
		remember(npc.Items)
	}
	for _, room := range s.Rooms {
		remember(room.Items)
	}
	for _, account := range s.BankAccounts {
		remember(account.Items)
	}
	index := 0
	return func() (string, error) {
		for {
			index++
			id := fmt.Sprintf("__turn_probe_%s_%d", targetID, index)
			if id == "" || used[id] {
				continue
			}
			used[id] = true
			return id, nil
		}
	}
}

func turnSetActorAndEnemy(s State, p TurnProposal) (State, error) {
	next := s.clone()
	actor, ok := next.Players[p.ActorID]
	if !ok {
		return State{}, fmt.Errorf("turn actor disappeared")
	}
	if p.ClearInvisible {
		setSettingFlag(&actor.Body, turnActorInvisibleFlag, false)
	}
	if p.ActorTimerWrite {
		actor.Body.Timers[turnCooldownTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: turnCooldownSeconds}
	}
	if p.AttackTimerWrite {
		// C writes only LT_ATTCK.ltime here. Preserve a pre-existing attack
		// interval/misc payload owned by the generic combat/tick reducer.
		actor.Body.Timers[turnAttackTimerIndex].LastTime = p.Now
	}
	next.Players[p.ActorID] = actor
	npc, ok := next.NPCs[p.TargetID]
	if !ok {
		return State{}, fmt.Errorf("turn target disappeared")
	}
	if p.EnemyAdded {
		npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: p.ActorID}, Damage: 0})
	}
	if p.DamageApplied {
		if p.TargetHP < -32768 || p.TargetHP > 32767 {
			return State{}, fmt.Errorf("turn target HP outside legacy range")
		}
		for i := range npc.Enemies {
			if npc.Enemies[i].Target != (EntityRef{Kind: "player", ID: p.ActorID}) {
				continue
			}
			if int64(npc.Enemies[i].Damage)+int64(p.EnemyDamage) > math.MaxInt32 {
				return State{}, fmt.Errorf("%w: enemy damage overflow", ErrTurnCombatSideEffectPending)
			}
			npc.Enemies[i].Damage += int32(p.EnemyDamage)
			break
		}
		npc.Body.HPCurrent = int16(p.TargetHP)
	}
	next.NPCs[p.TargetID] = npc
	return next, nil
}

func turnTarget(s State, p TurnProposal) (turnResolution, error) {
	if p.TargetID == "" || p.TargetName == "" || p.TargetKind != TurnTargetNPC {
		return turnResolution{}, nil
	}
	res, err := s.selectTurnNPC(p.ActorID, p.QueryName, p.TargetOccurrence)
	if err != nil {
		return turnResolution{}, err
	}
	if res.id != p.TargetID || res.name != p.TargetName || res.occurrence != p.TargetOccurrence || res.target.HPCurrent < 1 {
		return turnResolution{}, fmt.Errorf("turn target identity changed")
	}
	return res, nil
}

func turnBuildProjection(p *TurnProposal, actor LegacyMonster, target turnResolution) error {
	if p.ClearInvisible {
		p.Response += turnRevealResponse()
	}
	texts := make([]string, 0, 3)
	if p.ClearInvisible {
		texts = append(texts, turnRevealRoom(actor))
	}
	switch {
	case !p.Succeeded:
		p.Response += turnFailureResponse(target.target)
		texts = append(texts, turnFailureRoom(actor, target.target))
	case p.Disintegrated:
		p.Response += turnDisintegrateResponse(target.target)
		texts = append(texts, turnDisintegrateRoom(actor, target.target))
	default:
		p.Response += turnDamageResponse(target.target, p.Damage)
		texts = append(texts, turnDamageRoom(actor, target.target))
		if p.Killed {
			p.Response += turnKillResponse(target.target)
			texts = append(texts, turnKillRoom(actor, target.target))
		}
	}
	p.ExpectedEvent = turnEvent(p.ActorID, actor, target, texts)
	p.Broadcast = p.ExpectedEvent != nil
	return nil
}

// PlanTurn is the default one-based exact-name target form.
func (s State) PlanTurn(actorID, targetName string, now int32, roll func(int, int) int, allocate func() (string, error)) (TurnProposal, error) {
	return s.PlanTurnWithOccurrence(actorID, targetName, 1, now, roll, TurnOptions{Allocate: allocate})
}

// PlanTurnWithOptions preserves C's source order while keeping host-owned
// allocation separate from terminal input. The full death reducer is
// preflighted before the first random draw whenever a successful branch could
// be lethal; unsupported death/flee/enemy continuations therefore fail closed.
func (s State) PlanTurnWithOptions(actorID, targetName string, now int32, roll func(int, int) int, options TurnOptions) (TurnProposal, error) {
	return s.PlanTurnWithOccurrence(actorID, targetName, 1, now, roll, options)
}

func (s State) PlanTurnWithOccurrence(actorID, targetName string, occurrence int, now int32, roll func(int, int) int, options TurnOptions) (TurnProposal, error) {
	zero := TurnProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 || occurrence < 1 {
		return zero, fmt.Errorf("invalid turn actor, occurrence, or clock")
	}
	actor, room, err := turnActor(s, actorID)
	if err != nil {
		return zero, err
	}
	p := TurnProposal{ActorID: actorID, ActorName: actor.Body.Name, RoomID: room.Resource.ID, QueryName: targetName, TargetOccurrence: occurrence, Now: now, TargetKind: TurnTargetNPC, before: s.clone(), next: s.clone()}
	if targetName == "" {
		p.NoOp, p.TargetKind, p.Response = true, "", turnNoArgumentResponse()
		return p, nil
	}
	if !validTurnName(targetName) {
		return zero, fmt.Errorf("invalid turn target name")
	}
	if actor.Body.Class > turnMaxLegacyClass {
		return zero, fmt.Errorf("turn actor class outside legacy table")
	}
	if actor.Body.Class != turnClericClass && actor.Body.Class != turnPaladinClass && actor.Body.Class < turnInvincibleClass {
		p.NoOp, p.TargetKind, p.Response = true, "", turnUnauthorizedResponse()
		return p, nil
	}
	p.Authorized = true
	resolution, err := s.selectTurnNPC(actorID, targetName, occurrence)
	if err != nil {
		return zero, err
	}
	if resolution.id == "" {
		p.NoOp, p.TargetFound, p.TargetKind, p.Response = true, false, "", turnMissingResponse()
		return p, nil
	}
	p.TargetFound, p.TargetID, p.TargetName = true, resolution.id, resolution.name
	if resolution.target.HPCurrent < 1 {
		return zero, fmt.Errorf("%w: turn target is already dead", ErrTurnDeathTransitionPending)
	}
	if !flag(resolution.target.Flags[:], turnNPCUndeadFlag) {
		p.NoOp, p.Rejected, p.Response = true, true, turnNotUndeadResponse()
		return p, nil
	}
	p.ClearInvisible = flag(actor.Body.Flags[:], turnActorInvisibleFlag)
	timer := actor.Body.Timers[turnCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("turn cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait > math.MaxInt32 {
			return zero, fmt.Errorf("turn cooldown overflow")
		}
		p.NoOp, p.Cooldown, p.WaitSeconds = true, true, int32(wait)
		p.Response, err = turnWaitResponse(p.WaitSeconds)
		if err != nil {
			return zero, err
		}
		if p.ClearInvisible {
			p.Response = turnRevealResponse() + p.Response
			eventText := turnRevealRoom(actor.Body)
			p.ExpectedEvent = turnEvent(actorID, actor.Body, resolution, []string{eventText})
			p.Broadcast = true
			p.next = s.clone()
			nextActor := p.next.Players[actorID]
			setSettingFlag(&nextActor.Body, turnActorInvisibleFlag, false)
			p.next.Players[actorID] = nextActor
		} else {
			p.next = s.clone()
		}
		return p, nil
	}
	if flag(resolution.target.Flags[:], turnNPCNoHarmFlag) {
		p.NoOp, p.Rejected, p.NoHarm = true, true, true
		p.Response = turnNoHarmResponse(resolution.target)
		if p.ClearInvisible {
			p.Response = turnRevealResponse() + p.Response
			p.ExpectedEvent = turnEvent(actorID, actor.Body, resolution, []string{turnRevealRoom(actor.Body)})
			p.Broadcast = true
			p.next = s.clone()
			nextActor := p.next.Players[actorID]
			setSettingFlag(&nextActor.Body, turnActorInvisibleFlag, false)
			p.next.Players[actorID] = nextActor
		}
		return p, nil
	}
	foundEnemy, err := turnEnemy(s, resolution.id, actorID)
	if err != nil {
		return zero, fmt.Errorf("%w: target=%s", err, resolution.id)
	}
	p.EnemyAdded = !foundEnemy
	p.Chance, err = TurnChance(actor.Body, resolution.target)
	if err != nil {
		return zero, err
	}
	// If chance can succeed, disintegration is a possible branch. Preflight
	// the canonical death reducer before drawing so a bad drop/permanent/
	// summon/death relation never consumes RNG or reaches a receipt.
	if p.Chance >= 1 {
		if err := turnPotentialDeath(s, actorID, resolution.id, int(resolution.target.HPCurrent), now, turnProbeAllocator(s, resolution.id, options.Allocate)); err != nil {
			return zero, err
		}
	}
	p.ActorTimerWrite, p.AttackTimerWrite = true, true
	p.CooldownInterval, p.AttackInterval = turnCooldownSeconds, turnCooldownSeconds
	p.Attempted = true
	p.Roll, err = turnRandomIn(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	p.Succeeded = p.Roll <= p.Chance
	if p.Succeeded {
		p.DisintegrateRoll, err = turnRandomIn(roll, 1, 100)
		if err != nil {
			return zero, err
		}
		threshold := 90 - legacyStatBonus[actor.Body.Stats[4]]
		p.Disintegrated = p.DisintegrateRoll > threshold
		p.TargetHPBefore = int(resolution.target.HPCurrent)
		if p.Disintegrated {
			p.Damage, p.EnemyDamage, p.TargetHP, p.Killed = p.TargetHPBefore, p.TargetHPBefore, 0, true
			p.DamageApplied = true
		} else {
			p.Damage = p.TargetHPBefore / 2
			if p.Damage < 1 {
				p.Damage = 1
			}
			p.EnemyDamage = min(p.TargetHPBefore, p.Damage)
			p.TargetHP = p.TargetHPBefore - p.Damage
			p.DamageApplied, p.Killed = true, p.TargetHP < 1
		}
	}
	if err := turnBuildProjection(&p, actor.Body, resolution); err != nil {
		return zero, err
	}
	candidate, err := turnSetActorAndEnemy(s, p)
	if err != nil {
		return zero, err
	}
	if p.Killed {
		deadNPC := candidate.NPCs[resolution.id]
		deadNPC.Body.HPCurrent = 0
		candidate.NPCs[resolution.id] = deadNPC
		dead, death, deathErr := candidate.PlanNPCDeath(resolution.id, actorID, now, options.Allocate)
		if deathErr != nil {
			return zero, fmt.Errorf("%w: %v", ErrTurnDeathTransitionPending, deathErr)
		}
		p.Death = &death
		candidate = dead
	}
	if err := candidate.Validate(); err != nil {
		return zero, err
	}
	p.next = candidate
	return p, nil
}

func turnValidateNoOp(s State, p TurnProposal, actor PlayerState, resolution turnResolution) error {
	if !p.NoOp || p.Attempted || p.Succeeded || p.Roll != 0 || p.DisintegrateRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.ActorTimerWrite || p.AttackTimerWrite || p.Killed || p.Death != nil {
		return fmt.Errorf("invalid turn no-op proposal")
	}
	if p.TargetID == "" {
		if p.TargetFound || p.Cooldown || p.Rejected || p.NoHarm || p.ClearInvisible || p.ExpectedEvent != nil || p.Broadcast || p.DamageApplied || p.EnemyAdded || p.ActorTimerWrite || p.AttackTimerWrite || p.WaitSeconds != 0 || p.TargetKind != "" || p.TargetName != "" {
			return fmt.Errorf("invalid turn targetless no-op projection")
		}
		want := turnNoArgumentResponse()
		authorized := actor.Body.Class == turnClericClass || actor.Body.Class == turnPaladinClass || actor.Body.Class >= turnInvincibleClass
		if p.QueryName == "" {
			if p.Authorized {
				return fmt.Errorf("turn missing target authorization changed")
			}
		} else if authorized {
			if !p.Authorized {
				return fmt.Errorf("turn missing target authorization changed")
			}
			want = turnMissingResponse()
		} else {
			if p.Authorized {
				return fmt.Errorf("turn unauthorized target authorization changed")
			}
			want = turnUnauthorizedResponse()
		}
		if p.Response != want {
			return fmt.Errorf("turn targetless response changed")
		}
		return nil
	}
	if resolution.id != p.TargetID || !p.TargetFound || p.TargetKind != TurnTargetNPC || resolution.target.HPCurrent < 1 {
		return fmt.Errorf("turn no-op target changed")
	}
	reveal := flag(actor.Body.Flags[:], turnActorInvisibleFlag)
	if p.ClearInvisible != reveal {
		return fmt.Errorf("turn no-op invisibility changed")
	}
	if p.Cooldown {
		if p.Rejected || p.NoHarm || p.WaitSeconds < 1 || p.ClearInvisible != reveal {
			return fmt.Errorf("invalid turn cooldown proposal")
		}
		timer := actor.Body.Timers[turnCooldownTimerIndex]
		if int64(p.Now) >= int64(timer.LastTime)+int64(timer.Interval) {
			return fmt.Errorf("turn cooldown elapsed")
		}
		wait, err := turnWaitResponse(p.WaitSeconds)
		prefix := ""
		if reveal {
			prefix = turnRevealResponse()
		}
		wantEvent := (*TurnEvent)(nil)
		if reveal {
			wantEvent = turnEvent(p.ActorID, actor.Body, resolution, []string{turnRevealRoom(actor.Body)})
		}
		if err != nil || p.Response != prefix+wait || p.Broadcast != (wantEvent != nil) || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) {
			return fmt.Errorf("turn cooldown response changed")
		}
		return nil
	}
	if p.NoHarm {
		if !p.Rejected || !flag(resolution.target.Flags[:], turnNPCNoHarmFlag) {
			return fmt.Errorf("invalid turn no-harm proposal")
		}
		want := turnNoHarmResponse(resolution.target)
		if reveal {
			want = turnRevealResponse() + want
		}
		wantEvent := (*TurnEvent)(nil)
		if reveal {
			wantEvent = turnEvent(p.ActorID, actor.Body, resolution, []string{turnRevealRoom(actor.Body)})
		}
		if p.Response != want || p.Broadcast != (wantEvent != nil) || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) {
			return fmt.Errorf("turn no-harm projection changed")
		}
		return nil
	}
	if p.Rejected {
		if p.Response != turnNotUndeadResponse() || flag(resolution.target.Flags[:], turnNPCUndeadFlag) || p.Broadcast || p.ExpectedEvent != nil {
			return fmt.Errorf("turn undead rejection changed")
		}
		return nil
	}
	return fmt.Errorf("invalid turn no-op branch")
}

// ApplyTurn commits only the exact candidate produced by PlanTurn.  It never
// invokes RNG and fails closed when a caller presents a stale/tampered state.
func (s State) ApplyTurn(p TurnProposal) (State, TurnResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, TurnResult{}, err
	}
	if p.ActorID == "" || p.before.Version != 1 || p.Response == "" || !reflect.DeepEqual(s, p.before) {
		return State{}, TurnResult{}, fmt.Errorf("stale or invalid turn proposal")
	}
	actor, room, err := turnActor(s, p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID || actor.Body.Name != p.ActorName {
		return State{}, TurnResult{}, fmt.Errorf("turn actor changed")
	}
	result := turnResult(p)
	if p.TargetID == "" {
		if err := turnValidateNoOp(s, p, actor, turnResolution{}); err != nil {
			return State{}, TurnResult{}, err
		}
		if !reflect.DeepEqual(p.next, s) {
			return State{}, TurnResult{}, fmt.Errorf("turn targetless state changed")
		}
		return s.clone(), result, nil
	}
	resolution, err := turnTarget(s, p)
	if err != nil {
		return State{}, TurnResult{}, err
	}
	if resolution.id == "" {
		return State{}, TurnResult{}, fmt.Errorf("turn target disappeared")
	}
	if p.NoOp {
		if err := turnValidateNoOp(s, p, actor, resolution); err != nil {
			return State{}, TurnResult{}, err
		}
		expected := s.clone()
		if p.ClearInvisible {
			nextActor := expected.Players[p.ActorID]
			setSettingFlag(&nextActor.Body, turnActorInvisibleFlag, false)
			expected.Players[p.ActorID] = nextActor
		}
		if !reflect.DeepEqual(p.next, expected) {
			return State{}, TurnResult{}, fmt.Errorf("turn no-op state projection changed")
		}
		result.Changed = p.ClearInvisible
		result.ClearInvisible = p.ClearInvisible
		result.Broadcast = p.ExpectedEvent != nil
		result.Event = p.ExpectedEvent
		return expected, result, nil
	}

	if p.TargetKind != TurnTargetNPC || p.TargetName != resolution.name || p.TargetID != resolution.id || !p.Authorized || p.Cooldown || !p.Attempted || !p.ActorTimerWrite || !p.AttackTimerWrite || p.CooldownInterval != turnCooldownSeconds || p.AttackInterval != turnCooldownSeconds || p.WaitSeconds != 0 || p.Roll < 1 || p.Roll > 100 {
		return State{}, TurnResult{}, fmt.Errorf("invalid turn attempt proposal")
	}
	if !flag(resolution.target.Flags[:], turnNPCUndeadFlag) || flag(resolution.target.Flags[:], turnNPCNoHarmFlag) {
		return State{}, TurnResult{}, fmt.Errorf("turn target authorization changed")
	}
	if _, err := turnEnemy(s, p.TargetID, p.ActorID); err != nil {
		return State{}, TurnResult{}, err
	}
	chance, err := TurnChance(actor.Body, resolution.target)
	if err != nil || chance != p.Chance {
		if err != nil {
			return State{}, TurnResult{}, err
		}
		return State{}, TurnResult{}, fmt.Errorf("turn chance changed")
	}
	if p.Succeeded != (p.Roll <= p.Chance) {
		return State{}, TurnResult{}, fmt.Errorf("turn chance projection changed")
	}
	if !p.Succeeded {
		if p.DisintegrateRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 0 || p.TargetHP != 0 || p.DamageApplied || p.Disintegrated || p.Killed || p.Death != nil {
			return State{}, TurnResult{}, fmt.Errorf("invalid turn miss projection")
		}
	} else {
		if p.DisintegrateRoll < 1 || p.DisintegrateRoll > 100 || p.TargetHPBefore != int(resolution.target.HPCurrent) || p.Damage < 1 || !p.DamageApplied || p.EnemyDamage < 1 {
			return State{}, TurnResult{}, fmt.Errorf("invalid turn success projection")
		}
		wantDisintegrated := p.DisintegrateRoll > 90-legacyStatBonus[actor.Body.Stats[4]]
		if p.Disintegrated != wantDisintegrated {
			return State{}, TurnResult{}, fmt.Errorf("turn disintegration projection changed")
		}
		if p.Disintegrated {
			if p.Damage != p.TargetHPBefore || p.EnemyDamage != p.TargetHPBefore || p.TargetHP != 0 || !p.Killed || p.Death == nil {
				return State{}, TurnResult{}, fmt.Errorf("invalid turn disintegration projection")
			}
		} else {
			wantDamage := p.TargetHPBefore / 2
			if wantDamage < 1 {
				wantDamage = 1
			}
			if p.Damage != wantDamage || p.EnemyDamage != min(p.TargetHPBefore, wantDamage) || p.TargetHP != p.TargetHPBefore-p.Damage || p.Killed != (p.TargetHP < 1) {
				return State{}, TurnResult{}, fmt.Errorf("turn damage projection changed")
			}
			if p.Killed != (p.Death != nil) {
				return State{}, TurnResult{}, fmt.Errorf("turn death projection changed")
			}
		}
	}
	if p.ClearInvisible != flag(actor.Body.Flags[:], turnActorInvisibleFlag) {
		return State{}, TurnResult{}, fmt.Errorf("turn invisibility projection changed")
	}
	found, enemyErr := turnEnemy(s, p.TargetID, p.ActorID)
	if enemyErr != nil {
		return State{}, TurnResult{}, enemyErr
	}
	if p.EnemyAdded != !found {
		return State{}, TurnResult{}, fmt.Errorf("turn enemy projection changed")
	}
	wantResponse := p.Response
	check := p
	check.Response = ""
	// Rebuild the response/event in a copy to compare all source-visible text.
	check.Response = ""
	if p.ClearInvisible {
		check.Response = turnRevealResponse()
	}
	texts := make([]string, 0, 3)
	if p.ClearInvisible {
		texts = append(texts, turnRevealRoom(actor.Body))
	}
	if !p.Succeeded {
		check.Response += turnFailureResponse(resolution.target)
		texts = append(texts, turnFailureRoom(actor.Body, resolution.target))
	} else if p.Disintegrated {
		check.Response += turnDisintegrateResponse(resolution.target)
		texts = append(texts, turnDisintegrateRoom(actor.Body, resolution.target))
	} else {
		check.Response += turnDamageResponse(resolution.target, p.Damage)
		texts = append(texts, turnDamageRoom(actor.Body, resolution.target))
		if p.Killed {
			check.Response += turnKillResponse(resolution.target)
			texts = append(texts, turnKillRoom(actor.Body, resolution.target))
		}
	}
	wantEvent := turnEvent(p.ActorID, actor.Body, resolution, texts)
	if wantResponse != check.Response || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
		return State{}, TurnResult{}, fmt.Errorf("turn output projection changed")
	}

	base, err := turnSetActorAndEnemy(s, p)
	if err != nil {
		return State{}, TurnResult{}, err
	}
	if !p.Killed {
		if !reflect.DeepEqual(p.next, base) {
			return State{}, TurnResult{}, fmt.Errorf("turn state projection changed")
		}
		result.Changed = true
		result.Broadcast = wantEvent != nil
		result.Event = wantEvent
		result.ClearInvisible, result.ActorTimerWritten, result.AttackTimerWritten = p.ClearInvisible, true, true
		return base, result, nil
	}
	// PlanTurn already composed the canonical death candidate. ApplyTurn has no
	// allocator on purpose; it verifies the private candidate shape and returns
	// it only after the exact source-visible projection above matches.
	if p.next.Version != 1 || p.Death == nil || p.Death.TargetID != p.TargetID {
		return State{}, TurnResult{}, fmt.Errorf("invalid turn death candidate")
	}
	if _, exists := p.next.NPCs[p.TargetID]; exists || containsString(p.next.Rooms[p.RoomID].NPCIDs, p.TargetID) {
		return State{}, TurnResult{}, fmt.Errorf("turn death candidate retained NPC")
	}
	if err := p.next.Validate(); err != nil {
		return State{}, TurnResult{}, err
	}
	result.Changed = true
	result.Broadcast = wantEvent != nil
	result.Event = wantEvent
	result.ClearInvisible, result.ActorTimerWritten, result.AttackTimerWritten = p.ClearInvisible, true, true
	return p.next.clone(), result, nil
}

// TurnByName is the pure compatibility spelling used by non-session callers.
func (s State) TurnByName(actorID, targetName string, now int32, roll func(int, int) int, allocate func() (string, error)) (State, TurnResult, error) {
	p, err := s.PlanTurn(actorID, targetName, now, roll, allocate)
	if err != nil {
		return State{}, TurnResult{}, err
	}
	return s.ApplyTurn(p)
}

// Turn is a concise alias for TurnByName.
func (s State) Turn(actorID, targetName string, now int32, roll func(int, int) int, allocate func() (string, error)) (State, TurnResult, error) {
	return s.TurnByName(actorID, targetName, now, roll, allocate)
}
