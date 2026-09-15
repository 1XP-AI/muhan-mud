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

// poison_mon (command7.c, global command 98) is kept as a small, state-only
// reducer.  These are the original legacy slots and bits; they are not new
// Go status or lock names.
const (
	poisonAttackTimerIndex     = 3  // LT_ATTCK
	poisonSpellTimerIndex      = 9  // LT_SPELL
	poisonCooldownTimerIndex   = 15 // LT_TURNS
	poisonCooldownSeconds      = int32(15)
	poisonAssassinClass        = 1  // ASSASSIN
	poisonInvincibleClass      = 9  // INVINCIBLE
	poisonCaretakerClass       = 10 // CARETAKER
	poisonMaxLegacyClass       = 12 // DM
	poisonPlayerType           = 0  // PLAYER
	poisonMonsterType          = 1  // MONSTER
	poisonActorInvisibleFlag   = 2  // PINVIS
	poisonActorDetectFlag      = 21 // PDINVI
	poisonPlayerMaleFlag       = 12 // PMALES
	poisonTargetInvisibleFlag  = 2  // MINVIS
	poisonTargetDMInvisible    = 10 // PDMINV on a canonical NPC body
	poisonTargetNoHarmFlag     = 24 // MUNKIL
	poisonTargetPoisonedFlag   = 16 // MPOISN
	poisonTargetResistMagic    = 27 // MRMAGI
	poisonTargetResistBefuddle = 43 // MRBEFD
	poisonTargetFleerFlag      = 10 // MFLEER
	poisonTargetFollowDMFlag   = 46 // MDMFOL
)

// Export source-backed slots and flags for session adapters, tick expiry, and
// contract tests.  The lower-case constants above remain the implementation
// vocabulary so a transport cannot accidentally choose a different slot.
const (
	PoisonAttackTimerIndex      = poisonAttackTimerIndex
	PoisonSpellTimerIndex       = poisonSpellTimerIndex
	PoisonCooldownTimerIndex    = poisonCooldownTimerIndex
	PoisonAttackTimer           = poisonAttackTimerIndex
	PoisonSpellTimer            = poisonSpellTimerIndex
	PoisonTimerIndex            = poisonCooldownTimerIndex
	PoisonCooldownTimer         = poisonCooldownTimerIndex
	PoisonCooldownSeconds       = poisonCooldownSeconds
	PoisonAssassinClass         = poisonAssassinClass
	PoisonInvincibleClass       = poisonInvincibleClass
	PoisonCaretakerClass        = poisonCaretakerClass
	PoisonMaxLegacyClass        = poisonMaxLegacyClass
	PoisonPlayerType            = poisonPlayerType
	PoisonMonsterType           = poisonMonsterType
	PoisonActorInvisibleFlag    = poisonActorInvisibleFlag
	PoisonPlayerInvisibleFlag   = poisonActorInvisibleFlag
	PoisonActorDetectFlag       = poisonActorDetectFlag
	PoisonPlayerDetectFlag      = poisonActorDetectFlag
	PoisonPlayerMaleFlag        = poisonPlayerMaleFlag
	PoisonTargetInvisibleFlag   = poisonTargetInvisibleFlag
	PoisonNPCInvisibleFlag      = poisonTargetInvisibleFlag
	PoisonTargetDMInvisibleFlag = poisonTargetDMInvisible
	PoisonNPCDMInvisibleFlag    = poisonTargetDMInvisible
	PoisonTargetNoHarmFlag      = poisonTargetNoHarmFlag
	PoisonNPCNoHarmFlag         = poisonTargetNoHarmFlag
	PoisonFlag                  = poisonTargetPoisonedFlag
	PoisonNPCPoisonedFlag       = poisonTargetPoisonedFlag
	PoisonTargetPoisonedFlag    = poisonTargetPoisonedFlag
	PoisonNPCResistMagicFlag    = poisonTargetResistMagic
	PoisonNPCResistBefuddleFlag = poisonTargetResistBefuddle
	PoisonNPCFleerFlag          = poisonTargetFleerFlag
	PoisonNPCFollowDMFlag       = poisonTargetFollowDMFlag
)

var (
	// ErrPoisonNPCStateUnresolved means that the command cannot prove the
	// selected identity from the canonical same-room NPC index.
	ErrPoisonNPCStateUnresolved = errors.New("poison canonical NPC state unresolved")
	// ErrPoisonCombatSideEffectPending is returned before the first random
	// draw when enemy/death/flee continuation is not safely composable.
	ErrPoisonCombatSideEffectPending = errors.New("poison combat side effect pending")
	// ErrPoisonDeathTransitionPending prevents a lethal poison damage branch
	// from committing an HP-only corpse.
	ErrPoisonDeathTransitionPending = errors.New("poison lethal death transition pending")

	// Descriptive aliases are kept as the same sentinel so callers can use the
	// wording used by their composite combat reducer without string matching.
	ErrPoisonCombatPending         = ErrPoisonCombatSideEffectPending
	ErrPoisonCheckForFleePending   = ErrPoisonCombatSideEffectPending
	ErrPoisonFleeTransitionPending = ErrPoisonCombatSideEffectPending
	ErrPoisonDeathPending          = ErrPoisonDeathTransitionPending
	ErrPoisonStateUnresolved       = ErrPoisonNPCStateUnresolved
)

// PoisonTargetKind is closed to NPCs: poison_mon starts find_crt at the room
// monster list and has no player fallback.
type PoisonTargetKind string

const (
	PoisonTargetNPC PoisonTargetKind = "npc"
	PoisonNPC       PoisonTargetKind = PoisonTargetNPC
)

// PoisonEvent is the ordered room projection owned by a command receipt. The
// actor receives PoisonResult.Response, so room fan-out excludes ActorID.
type PoisonEvent struct {
	RoomID         int16            `json:"room_id"`
	ExcludeActorID string           `json:"exclude_actor_id"`
	ActorID        string           `json:"actor_id"`
	ActorName      string           `json:"actor_name"`
	TargetID       string           `json:"target_id"`
	TargetKind     PoisonTargetKind `json:"target_kind"`
	TargetName     string           `json:"target_name"`
	Texts          []string         `json:"texts"`
	Text           string           `json:"text,omitempty"`
}

// PoisonResult is the durable actor response and complete deterministic
// projection for replay. All random draws are retained; replay never asks a
// caller for another roll.
type PoisonResult struct {
	Action             string           `json:"action"`
	Response           string           `json:"response"`
	Changed            bool             `json:"changed"`
	Broadcast          bool             `json:"broadcast"`
	NoOp               bool             `json:"no_op,omitempty"`
	Authorized         bool             `json:"authorized"`
	TargetFound        bool             `json:"target_found"`
	Cooldown           bool             `json:"cooldown,omitempty"`
	Rejected           bool             `json:"rejected,omitempty"`
	WaitSeconds        int32            `json:"wait_seconds,omitempty"`
	Attempted          bool             `json:"attempted,omitempty"`
	ChanceSucceeded    bool             `json:"chance_succeeded,omitempty"`
	Hit                bool             `json:"hit,omitempty"`
	Succeeded          bool             `json:"succeeded,omitempty"`
	Poisoned           bool             `json:"poisoned,omitempty"`
	DamageApplied      bool             `json:"damage_applied,omitempty"`
	CombatApplied      bool             `json:"combat_applied,omitempty"`
	CombatDeferred     bool             `json:"combat_deferred,omitempty"`
	EnemyAdded         bool             `json:"enemy_added,omitempty"`
	ClearInvisible     bool             `json:"clear_invisible,omitempty"`
	PoisonFlagChanged  bool             `json:"poison_flag_changed,omitempty"`
	ActorTimerWritten  bool             `json:"actor_timer_written,omitempty"`
	SpellTimerWritten  bool             `json:"spell_timer_written,omitempty"`
	StatusTimerWritten bool             `json:"status_timer_written,omitempty"`
	Chance             int              `json:"chance,omitempty"`
	Roll               int              `json:"roll,omitempty"`
	ChanceRoll         int              `json:"chance_roll,omitempty"`
	DamageChanceRoll   int              `json:"damage_chance_roll,omitempty"`
	DamageRoll         int              `json:"damage_roll,omitempty"`
	DurationRoll1      int              `json:"duration_roll_1,omitempty"`
	DurationRoll2      int              `json:"duration_roll_2,omitempty"`
	Damage             int              `json:"damage,omitempty"`
	EnemyDamage        int              `json:"enemy_damage,omitempty"`
	TargetHPBefore     int              `json:"target_hp_before,omitempty"`
	TargetHP           int              `json:"target_hp,omitempty"`
	AttackInterval     int32            `json:"attack_interval,omitempty"`
	CooldownInterval   int32            `json:"cooldown_interval,omitempty"`
	SpellInterval      int32            `json:"spell_interval,omitempty"`
	ActorID            string           `json:"actor_id"`
	ActorName          string           `json:"actor_name,omitempty"`
	RoomID             int16            `json:"room_id,omitempty"`
	TargetID           string           `json:"target_id,omitempty"`
	TargetKind         PoisonTargetKind `json:"target_kind,omitempty"`
	TargetName         string           `json:"target_name,omitempty"`
	TargetOccurrence   int              `json:"target_occurrence,omitempty"`
	Event              *PoisonEvent     `json:"event,omitempty"`
}

// PoisonProposal is a state-bound candidate. before is private so a client
// cannot submit an authorization or identity snapshot of its own. ApplyPoison
// checks this exact snapshot and revalidates all deterministic projections.
type PoisonProposal struct {
	ActorID          string
	ActorName        string
	RoomID           int16
	QueryName        string
	TargetID         string
	TargetKind       PoisonTargetKind
	TargetName       string
	TargetOccurrence int
	Now              int32

	Chance           int
	Roll             int
	ChanceRoll       int
	DamageChanceRoll int
	DamageRoll       int
	DurationRoll1    int
	DurationRoll2    int
	Damage           int
	EnemyDamage      int
	TargetHPBefore   int
	TargetHP         int
	AttackInterval   int32
	CooldownInterval int32
	SpellInterval    int32
	WaitSeconds      int32

	Authorized        bool
	TargetFound       bool
	Cooldown          bool
	Rejected          bool
	NoOp              bool
	Attempted         bool
	ChanceSucceeded   bool
	Hit               bool
	Succeeded         bool
	Poisoned          bool
	DamageApplied     bool
	CombatApplied     bool
	CombatDeferred    bool
	EnemyAdded        bool
	ClearInvisible    bool
	PoisonFlagChanged bool
	ActorTimerWrite   bool
	SpellTimerWrite   bool
	StatusTimerWrite  bool
	Broadcast         bool
	Response          string
	ExpectedEvent     *PoisonEvent

	before State
}

type PoisonCandidate = PoisonProposal
type PoisonOutcome = PoisonResult

type poisonResolution struct {
	id         string
	name       string
	target     LegacyMonster
	occurrence int
}

func validPoisonBodyName(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

func validPoisonSelector(name string) bool {
	if !validPoisonBodyName(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func poisonActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != poisonPlayerType || !validPoisonBodyName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online poison actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("poison actor room membership absent")
	}
	return actor, room, nil
}

func poisonTargetVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt first hides PDMINV caretaker/DM identities, then
	// requires PDINVI for an ordinary MINVIS identity.
	if target.Class >= poisonCaretakerClass && flag(target.Flags[:], poisonTargetDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], poisonTargetInvisibleFlag) || flag(actor.Flags[:], poisonActorDetectFlag)
}

func poisonTargetMatches(target LegacyMonster, query string) bool {
	// The bounded command deliberately narrows EQUAL's legacy prefix/key
	// matcher to an exact, case-insensitive display-name identity. This follows
	// the Go command boundary convention while still preventing a key or prefix
	// from selecting a different canonical NPC in the same room.
	return strings.EqualFold(target.Name, query)
}

func selectPoisonNPC(s State, actorID, query string, occurrence int) (poisonResolution, error) {
	if occurrence < 1 {
		return poisonResolution{}, fmt.Errorf("poison target occurrence must be positive")
	}
	if !validPoisonSelector(query) {
		return poisonResolution{}, fmt.Errorf("invalid poison target name")
	}
	actor, room, err := poisonActor(s, actorID)
	if err != nil {
		return poisonResolution{}, err
	}
	if s.NPCs == nil {
		return poisonResolution{}, ErrPoisonNPCStateUnresolved
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != poisonMonsterType || npc.Body.RoomID != room.Resource.ID || !validPoisonBodyName(npc.Body.Name) {
			return poisonResolution{}, fmt.Errorf("unresolved canonical poison NPC identity")
		}
		if !poisonTargetMatches(npc.Body, query) || !poisonTargetVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return poisonResolution{id: id, name: npc.Body.Name, target: npc.Body, occurrence: found}, nil
		}
	}
	return poisonResolution{}, nil
}

// PoisonChance returns the exact source formula before the 1..100 draw.
func PoisonChance(actor, target LegacyMonster) (int, error) {
	if actor.Stats[1] > 63 {
		return 0, fmt.Errorf("poison actor dexterity outside legacy bonus table")
	}
	chance := (((int(actor.Level)+3)/4)-((int(target.Level)+3)/4))*20 + legacyStatBonus[actor.Stats[1]]*6
	if chance > 80 {
		chance = 80
	}
	return chance, nil
}

func PoisonHitChance(actor, target LegacyMonster) (int, error) { return PoisonChance(actor, target) }

func poisonWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("poison cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", seconds), nil
}

func poisonRandomIn(roll func(int, int) int, low, high int) (value int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("poison random source panicked: %v", recovered)
		}
	}()
	return randomIn(roll, low, high)
}

func poisonDuration(actor, target LegacyMonster, first, second int) (int32, error) {
	if first < 1 || first > 6 || second < 1 || second > 6 {
		return 0, fmt.Errorf("poison duration roll outside 1..6")
	}
	if actor.Stats[1] > 63 {
		return 0, fmt.Errorf("poison actor dexterity outside legacy bonus table")
	}
	duration := legacyStatBonus[actor.Stats[1]]*2 + first + second
	if flag(target.Flags[:], poisonTargetResistMagic) || flag(target.Flags[:], poisonTargetResistBefuddle) {
		duration = 3
	} else if duration < 5 {
		duration = 5
	}
	if duration > 10 {
		duration = 10
	}
	return int32(duration), nil
}

func poisonRevealResponse() string {
	return "\n당신의 모습이 나타나기 시작합니다.\n"
}

func poisonRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s의 모습이 보이기 시작합니다.\n", actor.Name, legacySubjectParticle(actor.Name))
}

func poisonNoArgumentResponse() string {
	return "\n누구를 중독시키시려고요?\n"
}

func poisonUnauthorizedResponse() string {
	return "\n자객만이 가능합니다.\n"
}

func poisonNoTargetResponse() string {
	return "\n그런 괴물은 존재하지 않습니다.\n"
}

func poisonNoHarmResponse(target LegacyMonster) string {
	pronoun := "그녀"
	if flag(target.Flags[:], poisonPlayerMaleFlag) {
		pronoun = "그"
	}
	return fmt.Sprintf("\n당신은 %s를 중독시킬수 없습니다.\n", pronoun)
}

func poisonAttemptResponse() string {
	return "\n당신은 적에게 독을 뿌렸습니다.\n"
}

func poisonMissResponse() string {
	return "그러나 적이 살짝 피해 실패했습니다.\n"
}

func poisonSuccessResponse(target LegacyMonster) string {
	return fmt.Sprintf("%s의 몸이 중독되었습니다.\n ", target.Name)
}

func poisonDamageResponse(target LegacyMonster, damage int) string {
	return fmt.Sprintf("%s%s 중독을  시켜서 %d의 피해를 입혔습니다.\n", target.Name, valueObjectParticle(target.Name), damage)
}

func poisonAttemptRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 적에게 독을 뿌렸습니다.\n", actor.Name, legacySubjectParticle(actor.Name))
}

func poisonMissRoom(target LegacyMonster) string {
	return fmt.Sprintf("그러나  %s%s 살짝 피했습니다.\n", target.Name, legacySubjectParticle(target.Name))
}

func poisonSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 적에게 독을 뿌렸습니다.\n%s의 몸이 중독되었습니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func poisonDamageRoom(actor, target LegacyMonster, damage int) string {
	return fmt.Sprintf("%s%s %s%s 중독을  시켜서 %d의 피해를 입혔습니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name, valueObjectParticle(target.Name), damage)
}

func poisonEvent(actorID string, actor LegacyMonster, target poisonResolution, texts []string) *PoisonEvent {
	if len(texts) == 0 {
		return nil
	}
	copied := append([]string(nil), texts...)
	return &PoisonEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: target.id, TargetKind: PoisonTargetNPC,
		TargetName: target.name, Texts: copied, Text: strings.Join(copied, ""),
	}
}

func poisonProposalResult(p PoisonProposal) PoisonResult {
	return PoisonResult{
		Action: "poison", Response: p.Response,
		Changed:   p.ClearInvisible || p.ActorTimerWrite || p.SpellTimerWrite || p.EnemyAdded || p.DamageApplied || p.PoisonFlagChanged,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Authorized: p.Authorized,
		TargetFound: p.TargetFound, Cooldown: p.Cooldown, Rejected: p.Rejected,
		WaitSeconds: p.WaitSeconds, Attempted: p.Attempted, ChanceSucceeded: p.ChanceSucceeded,
		Hit: p.Hit, Succeeded: p.Succeeded, Poisoned: p.Poisoned,
		DamageApplied: p.DamageApplied, CombatApplied: p.CombatApplied,
		CombatDeferred: p.CombatDeferred, EnemyAdded: p.EnemyAdded,
		ClearInvisible: p.ClearInvisible, PoisonFlagChanged: p.PoisonFlagChanged,
		ActorTimerWritten: p.ActorTimerWrite, SpellTimerWritten: p.SpellTimerWrite,
		StatusTimerWritten: p.StatusTimerWrite, Chance: p.Chance, Roll: p.Roll,
		ChanceRoll: p.ChanceRoll, DamageChanceRoll: p.DamageChanceRoll,
		DamageRoll: p.DamageRoll, DurationRoll1: p.DurationRoll1, DurationRoll2: p.DurationRoll2,
		Damage: p.Damage, EnemyDamage: p.EnemyDamage, TargetHPBefore: p.TargetHPBefore,
		TargetHP: p.TargetHP, AttackInterval: p.AttackInterval,
		CooldownInterval: p.CooldownInterval, SpellInterval: p.SpellInterval,
		ActorID: p.ActorID, ActorName: p.ActorName, RoomID: p.RoomID,
		TargetID: p.TargetID, TargetKind: p.TargetKind, TargetName: p.TargetName,
		TargetOccurrence: p.TargetOccurrence, Event: p.ExpectedEvent,
	}
}

func poisonPotentialCombat(target LegacyMonster, chance int) error {
	// A chance below 10 can never enter mrand(10,100)<=chance, so no damage,
	// death, or flee continuation can follow the first successful hit.
	if chance < 10 {
		return nil
	}
	if target.HPCurrent < 1 {
		return fmt.Errorf("%w: target is already dead", ErrPoisonDeathTransitionPending)
	}
	damage := int(target.HPMax) / 3
	if damage < 1 {
		return nil
	}
	post := int(target.HPCurrent) - damage
	if post < 1 {
		return fmt.Errorf("%w: target=%s", ErrPoisonDeathTransitionPending, target.Name)
	}
	if flag(target.Flags[:], poisonTargetFleerFlag) && !flag(target.Flags[:], poisonTargetFollowDMFlag) && post <= int(target.HPMax)/5 {
		return fmt.Errorf("%w: target flee transition=%s", ErrPoisonCombatSideEffectPending, target.Name)
	}
	return nil
}

// poisonPotentialEnemyUpdate checks the only remaining combat mutation in
// this bounded reducer. It runs before the first draw so an enemy damage
// counter that cannot represent the source update does not consume RNG or
// reach receipt admission.
func poisonPotentialEnemyUpdate(npc NPCState, actorID string, target LegacyMonster, chance int) error {
	if chance < 10 {
		return nil
	}
	damage := int(target.HPMax) / 3
	if damage < 1 {
		return nil
	}
	post := int(target.HPCurrent) - damage
	if post < 1 {
		// poisonPotentialCombat owns the typed death error. Keep this helper
		// side-effect-only so callers can preserve that more specific error.
		return nil
	}
	delta := min(post, damage)
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Target == want && int64(enemy.Damage)+int64(delta) > math.MaxInt32 {
			return fmt.Errorf("%w: enemy damage overflow", ErrPoisonCombatSideEffectPending)
		}
	}
	return nil
}

func poisonEnemyContains(npc NPCState, actorID string) (bool, error) {
	if npc.Enemies == nil {
		return false, ErrPoisonCombatSideEffectPending
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Damage < 0 {
			return false, fmt.Errorf("%w: negative enemy damage", ErrPoisonCombatSideEffectPending)
		}
		if enemy.Target == want {
			return true, nil
		}
	}
	return false, nil
}

// PlanPoison is the default one-based target occurrence form.
func (s State) PlanPoison(actorID, targetName string, now int32, roll func(int, int) int) (PoisonProposal, error) {
	return s.PlanPoisonWithOccurrence(actorID, targetName, 1, now, roll)
}

// PlanPoisonWithOccurrence follows command7.c in source order: authorization,
// canonical visible same-room lookup, stealth reveal, cooldown, MUNKIL, enemy
// edge/timers, chance, poison, optional damage, and duration. Unsupported
// death/flee continuations fail before any draw and therefore before a receipt.
func (s State) PlanPoisonWithOccurrence(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (PoisonProposal, error) {
	zero := PoisonProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 || occurrence < 1 {
		return zero, fmt.Errorf("invalid poison actor, occurrence, or clock")
	}
	actor, room, err := poisonActor(s, actorID)
	if err != nil {
		return zero, err
	}
	if actor.Body.Class > poisonMaxLegacyClass {
		return zero, fmt.Errorf("poison actor class outside legacy table")
	}
	p := PoisonProposal{
		ActorID: actorID, ActorName: actor.Body.Name, RoomID: room.Resource.ID,
		QueryName: targetName, TargetOccurrence: occurrence, Now: now,
		TargetKind: PoisonTargetNPC, before: s.clone(),
	}
	if actor.Body.Class != poisonAssassinClass && actor.Body.Class < poisonInvincibleClass {
		p.NoOp, p.Response = true, poisonUnauthorizedResponse()
		p.TargetKind = ""
		return p, nil
	}
	p.Authorized = true
	if targetName == "" {
		p.NoOp, p.TargetKind, p.Response = true, "", poisonNoArgumentResponse()
		return p, nil
	}
	if !validPoisonSelector(targetName) {
		return zero, fmt.Errorf("invalid poison target name")
	}
	resolution, err := selectPoisonNPC(s, actorID, targetName, occurrence)
	if err != nil {
		return zero, err
	}
	if resolution.id == "" {
		p.NoOp, p.TargetFound, p.TargetKind, p.Response = true, false, "", poisonNoTargetResponse()
		return p, nil
	}
	p.TargetFound, p.TargetID, p.TargetKind, p.TargetName = true, resolution.id, PoisonTargetNPC, resolution.name
	p.ClearInvisible = flag(actor.Body.Flags[:], poisonActorInvisibleFlag)
	roomTexts := make([]string, 0, 3)
	if p.ClearInvisible {
		p.Response += poisonRevealResponse()
		roomTexts = append(roomTexts, poisonRevealRoom(actor.Body))
	}

	// C reveals before reading LT_TURNS, so a cooldown candidate can still
	// clear PINVIS and publish its ordered reveal event.
	timer := actor.Body.Timers[poisonCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("poison cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait > math.MaxInt32 {
			return zero, fmt.Errorf("poison cooldown overflow")
		}
		p.NoOp, p.Cooldown, p.WaitSeconds = true, true, int32(wait)
		waitText, waitErr := poisonWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return zero, waitErr
		}
		p.Response += waitText
		p.ExpectedEvent = poisonEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	if flag(resolution.target.Flags[:], poisonTargetNoHarmFlag) {
		p.NoOp, p.Rejected = true, true
		p.Response += poisonNoHarmResponse(resolution.target)
		p.ExpectedEvent = poisonEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	npc := s.NPCs[resolution.id]
	hasEnemy, err := poisonEnemyContains(npc, actorID)
	if err != nil {
		return zero, fmt.Errorf("%w: target=%s", err, resolution.id)
	}
	p.EnemyAdded = !hasEnemy
	p.AttackInterval, p.CooldownInterval = poisonCooldownSeconds, poisonCooldownSeconds
	p.Chance, err = PoisonChance(actor.Body, resolution.target)
	if err != nil {
		return zero, err
	}
	if err := poisonPotentialCombat(resolution.target, p.Chance); err != nil {
		return zero, err
	}
	if err := poisonPotentialEnemyUpdate(npc, actorID, resolution.target, p.Chance); err != nil {
		return zero, err
	}

	// add_enm_crt and both actor timers precede the first random draw in C.
	p.ActorTimerWrite = true
	p.Attempted = true
	p.ChanceRoll, err = poisonRandomIn(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	p.Roll, p.ChanceSucceeded, p.Hit = p.ChanceRoll, p.ChanceRoll <= p.Chance, p.ChanceRoll <= p.Chance
	if !p.ChanceSucceeded {
		p.Response += poisonAttemptResponse() + poisonMissResponse()
		roomTexts = append(roomTexts, poisonAttemptRoom(actor.Body, resolution.target), poisonMissRoom(resolution.target))
		p.ExpectedEvent = poisonEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	p.Succeeded, p.Poisoned = true, true
	p.PoisonFlagChanged = !flag(resolution.target.Flags[:], poisonTargetPoisonedFlag)
	p.Response += poisonAttemptResponse() + poisonSuccessResponse(resolution.target)
	roomTexts = append(roomTexts, poisonSuccessRoom(actor.Body, resolution.target))
	p.DamageChanceRoll, err = poisonRandomIn(roll, 10, 100)
	if err != nil {
		return zero, err
	}
	p.DamageRoll = p.DamageChanceRoll
	if p.DamageChanceRoll <= p.Chance {
		p.Damage = int(resolution.target.HPMax) / 3
		p.TargetHPBefore = int(resolution.target.HPCurrent)
		p.TargetHP = p.TargetHPBefore - p.Damage
		if p.Damage > 0 {
			p.DamageApplied = true
			p.EnemyDamage = min(p.TargetHP, p.Damage)
			p.Response += poisonDamageResponse(resolution.target, p.Damage)
			roomTexts = append(roomTexts, poisonDamageRoom(actor.Body, resolution.target, p.Damage))
		}
	}
	// dice(2,6,0) is evaluated even when MRMAGI/MRBEFD later forces 3.
	p.DurationRoll1, err = poisonRandomIn(roll, 1, 6)
	if err != nil {
		return zero, err
	}
	p.DurationRoll2, err = poisonRandomIn(roll, 1, 6)
	if err != nil {
		return zero, err
	}
	p.SpellInterval, err = poisonDuration(actor.Body, resolution.target, p.DurationRoll1, p.DurationRoll2)
	if err != nil {
		return zero, err
	}
	p.SpellTimerWrite = true
	p.ExpectedEvent = poisonEvent(actorID, actor.Body, resolution, roomTexts)
	p.Broadcast = p.ExpectedEvent != nil
	return p, nil
}

// poisonProposalRandomEmpty detects candidates that carry no untrusted draw.
func poisonProposalRandomEmpty(p PoisonProposal) bool {
	return p.Chance == 0 && p.Roll == 0 && p.ChanceRoll == 0 && p.DamageChanceRoll == 0 && p.DamageRoll == 0 &&
		p.DurationRoll1 == 0 && p.DurationRoll2 == 0 && !p.Attempted && !p.ChanceSucceeded && !p.Hit && !p.Succeeded &&
		p.Damage == 0 && p.EnemyDamage == 0
}

// ApplyPoison validates and atomically applies a proposal planned from this
// exact state. It performs no random calls.
func (s State) ApplyPoison(p PoisonProposal) (State, PoisonResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, PoisonResult{}, err
	}
	if p.ActorID == "" || p.Now < 0 || p.before.Version != 1 || p.Response == "" || !reflect.DeepEqual(s, p.before) {
		return State{}, PoisonResult{}, fmt.Errorf("stale or invalid poison proposal")
	}
	actor, room, err := poisonActor(s, p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID || actor.Body.Name != p.ActorName {
		return State{}, PoisonResult{}, fmt.Errorf("poison actor changed")
	}
	if actor.Body.Class > poisonMaxLegacyClass {
		return State{}, PoisonResult{}, fmt.Errorf("poison actor class outside legacy table")
	}
	authorized := actor.Body.Class == poisonAssassinClass || actor.Body.Class >= poisonInvincibleClass
	if p.Authorized != authorized {
		return State{}, PoisonResult{}, fmt.Errorf("poison authorization changed")
	}
	result := poisonProposalResult(p)

	if !authorized {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ExpectedEvent != nil || p.Broadcast || p.Response != poisonUnauthorizedResponse() {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison authorization proposal")
		}
		return s.clone(), result, nil
	}
	if p.TargetOccurrence < 1 {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison target occurrence")
	}
	if p.QueryName == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.WaitSeconds != 0 || p.ExpectedEvent != nil || p.Broadcast || p.Response != poisonNoArgumentResponse() {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison missing-target proposal")
		}
		return s.clone(), result, nil
	}
	if !validPoisonSelector(p.QueryName) {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison target selector")
	}
	resolution, err := selectPoisonNPC(s, p.ActorID, p.QueryName, p.TargetOccurrence)
	if err != nil {
		return State{}, PoisonResult{}, err
	}
	if resolution.id == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.WaitSeconds != 0 || p.ExpectedEvent != nil || p.Broadcast || p.Response != poisonNoTargetResponse() {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison absent-target proposal")
		}
		return s.clone(), result, nil
	}
	if !p.TargetFound || p.TargetID != resolution.id || p.TargetKind != PoisonTargetNPC || p.TargetName != resolution.name {
		return State{}, PoisonResult{}, fmt.Errorf("poison target identity changed")
	}
	reveal := flag(actor.Body.Flags[:], poisonActorInvisibleFlag)
	if p.ClearInvisible != reveal {
		return State{}, PoisonResult{}, fmt.Errorf("poison invisibility proposal changed")
	}
	if p.CombatDeferred {
		return State{}, PoisonResult{}, ErrPoisonCombatSideEffectPending
	}
	if p.CombatApplied {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison combat projection")
	}

	roomTexts := make([]string, 0, 3)
	responsePrefix := ""
	if reveal {
		responsePrefix = poisonRevealResponse()
		roomTexts = append(roomTexts, poisonRevealRoom(actor.Body))
	}
	timer := actor.Body.Timers[poisonCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, PoisonResult{}, fmt.Errorf("poison cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if p.Cooldown {
		if !p.NoOp || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ChanceRoll != 0 || p.DamageChanceRoll != 0 || p.DurationRoll1 != 0 || p.DurationRoll2 != 0 || p.ActorTimerWrite || p.SpellTimerWrite || p.EnemyAdded || int64(p.Now) >= deadline {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison cooldown proposal")
		}
		wait := deadline - int64(p.Now)
		if wait < 1 || wait > math.MaxInt32 || p.WaitSeconds != int32(wait) {
			return State{}, PoisonResult{}, fmt.Errorf("poison cooldown changed")
		}
		waitText, waitErr := poisonWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return State{}, PoisonResult{}, waitErr
		}
		wantResponse := responsePrefix + waitText
		wantEvent := poisonEvent(p.ActorID, actor.Body, resolution, roomTexts)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, PoisonResult{}, fmt.Errorf("stale poison cooldown projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, poisonActorInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = wantResponse, reveal, wantEvent
		result.Broadcast, result.Cooldown, result.TargetFound = wantEvent != nil, true, true
		result.ClearInvisible = reveal
		if err := next.Validate(); err != nil {
			return State{}, PoisonResult{}, err
		}
		return next, result, nil
	}
	if p.Rejected {
		if !p.NoOp || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ChanceRoll != 0 || p.DamageChanceRoll != 0 || p.DurationRoll1 != 0 || p.DurationRoll2 != 0 || p.ActorTimerWrite || p.SpellTimerWrite || p.EnemyAdded || int64(p.Now) < deadline || !flag(resolution.target.Flags[:], poisonTargetNoHarmFlag) {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison protected-target proposal")
		}
		wantResponse := responsePrefix + poisonNoHarmResponse(resolution.target)
		wantEvent := poisonEvent(p.ActorID, actor.Body, resolution, roomTexts)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, PoisonResult{}, fmt.Errorf("stale poison protected-target projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, poisonActorInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = wantResponse, reveal, wantEvent
		result.Broadcast, result.Rejected, result.TargetFound = wantEvent != nil, true, true
		result.ClearInvisible = reveal
		if err := next.Validate(); err != nil {
			return State{}, PoisonResult{}, err
		}
		return next, result, nil
	}

	// Every attempt must have the source's fixed 15-second actor timer and a
	// nonnil canonical enemy list before an RNG-bearing candidate is admitted.
	if !p.Attempted || !p.ActorTimerWrite || p.AttackInterval != poisonCooldownSeconds || p.CooldownInterval != poisonCooldownSeconds || p.Now < 0 || p.Roll != p.ChanceRoll || p.ChanceRoll < 1 || p.ChanceRoll > 100 {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison attempt proposal")
	}
	if p.TargetHPBefore < 0 || p.TargetHP < 0 || p.Damage < 0 || p.EnemyDamage < 0 {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison numeric projection")
	}
	npc := s.NPCs[resolution.id]
	hasEnemy, err := poisonEnemyContains(npc, p.ActorID)
	if err != nil {
		return State{}, PoisonResult{}, fmt.Errorf("%w: target=%s", err, resolution.id)
	}
	if p.EnemyAdded == hasEnemy {
		return State{}, PoisonResult{}, fmt.Errorf("poison enemy projection changed")
	}
	chance, err := PoisonChance(actor.Body, resolution.target)
	if err != nil || chance != p.Chance {
		if err != nil {
			return State{}, PoisonResult{}, err
		}
		return State{}, PoisonResult{}, fmt.Errorf("poison chance changed")
	}
	if err := poisonPotentialCombat(resolution.target, chance); err != nil {
		return State{}, PoisonResult{}, err
	}
	if err := poisonPotentialEnemyUpdate(npc, p.ActorID, resolution.target, chance); err != nil {
		return State{}, PoisonResult{}, err
	}
	if p.ChanceSucceeded != (p.ChanceRoll <= chance) || p.Hit != p.ChanceSucceeded {
		return State{}, PoisonResult{}, fmt.Errorf("poison chance projection changed")
	}
	if !p.ChanceSucceeded {
		if p.Succeeded || p.Poisoned || p.DamageApplied || p.PoisonFlagChanged || p.SpellTimerWrite || p.DamageChanceRoll != 0 || p.DamageRoll != 0 || p.DurationRoll1 != 0 || p.DurationRoll2 != 0 || p.SpellInterval != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 0 || p.TargetHP != 0 {
			return State{}, PoisonResult{}, fmt.Errorf("invalid poison miss projection")
		}
		wantResponse := responsePrefix + poisonAttemptResponse() + poisonMissResponse()
		wantTexts := append(roomTexts, poisonAttemptRoom(actor.Body, resolution.target), poisonMissRoom(resolution.target))
		wantEvent := poisonEvent(p.ActorID, actor.Body, resolution, wantTexts)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, PoisonResult{}, fmt.Errorf("stale poison miss projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, poisonActorInvisibleFlag, false)
		}
		nextActor.Body.Timers[poisonCooldownTimerIndex].LastTime = p.Now
		nextActor.Body.Timers[poisonCooldownTimerIndex].Interval = poisonCooldownSeconds
		nextActor.Body.Timers[poisonAttackTimerIndex].LastTime = p.Now
		next.Players[p.ActorID] = nextActor
		if p.EnemyAdded {
			nextNPC := next.NPCs[resolution.id]
			nextNPC.Enemies = append(nextNPC.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: p.ActorID}, Damage: 0})
			next.NPCs[resolution.id] = nextNPC
		}
		result.Response, result.Changed, result.Event = wantResponse, true, wantEvent
		result.Broadcast, result.TargetFound = wantEvent != nil, true
		result.ClearInvisible, result.ActorTimerWritten = reveal, true
		result.CooldownInterval, result.AttackInterval = poisonCooldownSeconds, poisonCooldownSeconds
		if err := next.Validate(); err != nil {
			return State{}, PoisonResult{}, err
		}
		return next, result, nil
	}
	if !p.Succeeded || !p.Poisoned || p.DamageChanceRoll < 10 || p.DamageChanceRoll > 100 || p.DamageRoll != p.DamageChanceRoll || p.DurationRoll1 < 1 || p.DurationRoll1 > 6 || p.DurationRoll2 < 1 || p.DurationRoll2 > 6 || !p.SpellTimerWrite {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison success projection")
	}
	if p.DamageChanceRoll <= chance {
		wantDamage := int(resolution.target.HPMax) / 3
		wantBefore := int(resolution.target.HPCurrent)
		wantAfter := wantBefore - wantDamage
		wantEnemyDamage := min(wantAfter, wantDamage)
		if p.Damage != wantDamage || p.TargetHPBefore != wantBefore || p.TargetHP != wantAfter || p.EnemyDamage != wantEnemyDamage || p.DamageApplied != (wantDamage > 0) {
			return State{}, PoisonResult{}, fmt.Errorf("poison damage projection changed")
		}
	} else if p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 0 || p.TargetHP != 0 || p.DamageApplied {
		return State{}, PoisonResult{}, fmt.Errorf("invalid poison resisted-damage projection")
	}
	wantDuration, err := poisonDuration(actor.Body, resolution.target, p.DurationRoll1, p.DurationRoll2)
	if err != nil || wantDuration != p.SpellInterval {
		if err != nil {
			return State{}, PoisonResult{}, err
		}
		return State{}, PoisonResult{}, fmt.Errorf("poison duration changed")
	}
	wantPoisonChanged := !flag(resolution.target.Flags[:], poisonTargetPoisonedFlag)
	if p.PoisonFlagChanged != wantPoisonChanged {
		return State{}, PoisonResult{}, fmt.Errorf("poison flag projection changed")
	}
	wantResponse := responsePrefix + poisonAttemptResponse() + poisonSuccessResponse(resolution.target)
	wantTexts := append(roomTexts, poisonSuccessRoom(actor.Body, resolution.target))
	if p.DamageChanceRoll <= chance && p.Damage > 0 {
		wantResponse += poisonDamageResponse(resolution.target, p.Damage)
		wantTexts = append(wantTexts, poisonDamageRoom(actor.Body, resolution.target, p.Damage))
	}
	wantEvent := poisonEvent(p.ActorID, actor.Body, resolution, wantTexts)
	if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
		return State{}, PoisonResult{}, fmt.Errorf("stale poison success projection")
	}

	next := s.clone()
	nextActor := next.Players[p.ActorID]
	if reveal {
		setSettingFlag(&nextActor.Body, poisonActorInvisibleFlag, false)
	}
	nextActor.Body.Timers[poisonCooldownTimerIndex].LastTime = p.Now
	nextActor.Body.Timers[poisonCooldownTimerIndex].Interval = poisonCooldownSeconds
	nextActor.Body.Timers[poisonAttackTimerIndex].LastTime = p.Now
	next.Players[p.ActorID] = nextActor
	nextNPC := next.NPCs[resolution.id]
	if p.EnemyAdded {
		nextNPC.Enemies = append(nextNPC.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: p.ActorID}, Damage: 0})
	}
	if p.DamageApplied {
		if p.TargetHP < -32768 || p.TargetHP > 32767 {
			return State{}, PoisonResult{}, fmt.Errorf("poison target HP outside legacy range")
		}
		nextNPC.Body.HPCurrent = int16(p.TargetHP)
		for i := range nextNPC.Enemies {
			if nextNPC.Enemies[i].Target == (EntityRef{Kind: "player", ID: p.ActorID}) {
				if int64(nextNPC.Enemies[i].Damage)+int64(p.EnemyDamage) > math.MaxInt32 {
					return State{}, PoisonResult{}, fmt.Errorf("poison enemy damage overflow")
				}
				nextNPC.Enemies[i].Damage += int32(p.EnemyDamage)
				break
			}
		}
	}
	if p.PoisonFlagChanged {
		setSettingFlag(&nextNPC.Body, poisonTargetPoisonedFlag, true)
	}
	if p.SpellTimerWrite {
		nextNPC.Body.Timers[poisonSpellTimerIndex].LastTime = p.Now
		nextNPC.Body.Timers[poisonSpellTimerIndex].Interval = p.SpellInterval
	}
	next.NPCs[resolution.id] = nextNPC
	result.Response, result.Changed, result.Event = wantResponse, true, wantEvent
	result.Broadcast, result.TargetFound = wantEvent != nil, true
	result.ClearInvisible, result.ActorTimerWritten = reveal, true
	result.SpellTimerWritten, result.SpellInterval = true, p.SpellInterval
	result.CooldownInterval, result.AttackInterval = poisonCooldownSeconds, poisonCooldownSeconds
	if err := next.Validate(); err != nil {
		return State{}, PoisonResult{}, err
	}
	return next, result, nil
}

// Poison is the convenient plan/apply form for callers that do not retain a
// candidate. A failed plan returns no state or receipt candidate.
func (s State) Poison(actorID, targetName string, now int32, roll func(int, int) int) (State, PoisonResult, error) {
	p, err := s.PlanPoison(actorID, targetName, now, roll)
	if err != nil {
		return State{}, PoisonResult{}, err
	}
	return s.ApplyPoison(p)
}

func (s State) PoisonByName(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (State, PoisonResult, error) {
	p, err := s.PlanPoisonWithOccurrence(actorID, targetName, occurrence, now, roll)
	if err != nil {
		return State{}, PoisonResult{}, err
	}
	return s.ApplyPoison(p)
}
