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

// These are the legacy slots and bits consumed by src/magic3.c:absorb.  The
// values deliberately remain in the migrated LegacyMonster body; no separate
// Go-only cooldown or status authority is introduced.
const (
	absorbAttackTimerIndex   = 3  // LT_ATTCK
	absorbCooldownTimerIndex = 15 // LT_TURNS
	absorbCooldownSeconds    = int32(30)
	absorbMageClass          = 5  // MAGE
	absorbInvincibleClass    = 9  // INVINCIBLE
	absorbCaretakerClass     = 10 // CARETAKER
	absorbMaxLegacyClass     = 12 // DM
	absorbPlayerType         = 0  // PLAYER
	absorbMonsterType        = 1  // MONSTER
	absorbActorInvisibleFlag = 2  // PINVIS
	absorbActorDetectFlag    = 21 // PDINVI
	absorbTargetDMInvisible  = 10 // PDMINV
	absorbTargetNoHarmFlag   = 24 // MUNKIL
	absorbTargetUndeadFlag   = 14 // MUNDED
	absorbTargetFleerFlag    = 10 // MFLEER
	absorbTargetFollowDMFlag = 46 // MDMFOL
	absorbMaleFlag           = 12 // MMALES/PMales share the legacy bit
	absorbIntelligenceStat   = 3  // creature.stats[INT-1]
	absorbMaxHP              = int16(math.MaxInt16)
	absorbMinHP              = int16(math.MinInt16)
)

// Export only source-confirmed constants so a session/transport adapter can
// describe the receipt without choosing a second set of legacy indexes.
const (
	AbsorbAttackTimerIndex    = absorbAttackTimerIndex
	AbsorbCooldownTimerIndex  = absorbCooldownTimerIndex
	AbsorbAttackTimer         = absorbAttackTimerIndex
	AbsorbCooldownTimer       = absorbCooldownTimerIndex
	AbsorbCooldownSeconds     = absorbCooldownSeconds
	AbsorbMageClass           = absorbMageClass
	AbsorbInvincibleClass     = absorbInvincibleClass
	AbsorbCaretakerClass      = absorbCaretakerClass
	AbsorbMaxLegacyClass      = absorbMaxLegacyClass
	AbsorbPlayerType          = absorbPlayerType
	AbsorbMonsterType         = absorbMonsterType
	AbsorbActorInvisibleFlag  = absorbActorInvisibleFlag
	AbsorbPlayerInvisibleFlag = absorbActorInvisibleFlag
	AbsorbActorDetectFlag     = absorbActorDetectFlag
	AbsorbPlayerDetectFlag    = absorbActorDetectFlag
	AbsorbTargetDMInvisible   = absorbTargetDMInvisible
	AbsorbNPCDMInvisibleFlag  = absorbTargetDMInvisible
	AbsorbTargetNoHarmFlag    = absorbTargetNoHarmFlag
	AbsorbNPCNoHarmFlag       = absorbTargetNoHarmFlag
	AbsorbTargetUndeadFlag    = absorbTargetUndeadFlag
	AbsorbNPCUndeadFlag       = absorbTargetUndeadFlag
	AbsorbTargetFleerFlag     = absorbTargetFleerFlag
	AbsorbNPCFleerFlag        = absorbTargetFleerFlag
	AbsorbTargetFollowDMFlag  = absorbTargetFollowDMFlag
	AbsorbNPCFollowDMFlag     = absorbTargetFollowDMFlag
	AbsorbMaleFlag            = absorbMaleFlag
	AbsorbIntelligenceStat    = absorbIntelligenceStat
)

var (
	// ErrAbsorbNPCStateUnresolved prevents a command from falling back to the
	// legacy room monster slice or selecting an identity by display-name map.
	ErrAbsorbNPCStateUnresolved = errors.New("absorb canonical NPC state unresolved")
	// ErrAbsorbCombatSideEffectPending means add_enm_crt/add_enm_dmg or a
	// possible flee continuation cannot be represented in this atomic reducer.
	ErrAbsorbCombatSideEffectPending = errors.New("absorb combat side effect pending")
	// ErrAbsorbDeathTransitionPending prevents an HP-only corpse.  A later
	// composite combat reducer must include the complete NPC death transition.
	ErrAbsorbDeathTransitionPending = errors.New("absorb lethal death transition pending")
	// ErrAbsorbHPOverflow is distinct because the source stores signed 16-bit
	// hit points; wrapping the actor's healed HP would corrupt the snapshot.
	ErrAbsorbHPOverflow = errors.New("absorb HP outside legacy range")

	// Descriptive aliases let a composite combat owner match the same typed
	// boundary without string matching or introducing duplicate sentinels.
	ErrAbsorbCombatPending   = ErrAbsorbCombatSideEffectPending
	ErrAbsorbFleePending     = ErrAbsorbCombatSideEffectPending
	ErrAbsorbDeathPending    = ErrAbsorbDeathTransitionPending
	ErrAbsorbStateUnresolved = ErrAbsorbNPCStateUnresolved
)

// AbsorbTargetKind is intentionally closed to canonical NPCs.  The original
// routine starts find_crt at first_mon and does not target players.
type AbsorbTargetKind string

const (
	AbsorbTargetNPC AbsorbTargetKind = "npc"
	AbsorbNPC       AbsorbTargetKind = AbsorbTargetNPC
)

// AbsorbEvent is the ordered room projection emitted after a receipt commits.
// The actor receives AbsorbResult.Response, therefore ExcludeActorID prevents
// duplicate output on the originating connection.
type AbsorbEvent struct {
	RoomID         int16            `json:"room_id"`
	ExcludeActorID string           `json:"exclude_actor_id"`
	ActorID        string           `json:"actor_id"`
	ActorName      string           `json:"actor_name"`
	TargetID       string           `json:"target_id"`
	TargetKind     AbsorbTargetKind `json:"target_kind"`
	TargetName     string           `json:"target_name"`
	Texts          []string         `json:"texts"`
	Text           string           `json:"text,omitempty"`
}

// AbsorbResult is the durable actor response and deterministic replay
// projection.  All random input and the source's intentionally uncapped HP
// healing are retained so a retry never reruns lookup or randomness.
type AbsorbResult struct {
	Action            string           `json:"action"`
	Response          string           `json:"response"`
	Changed           bool             `json:"changed"`
	Broadcast         bool             `json:"broadcast"`
	NoOp              bool             `json:"no_op,omitempty"`
	Authorized        bool             `json:"authorized"`
	TargetFound       bool             `json:"target_found"`
	Cooldown          bool             `json:"cooldown,omitempty"`
	Rejected          bool             `json:"rejected,omitempty"`
	Undead            bool             `json:"undead,omitempty"`
	WaitSeconds       int32            `json:"wait_seconds,omitempty"`
	Attempted         bool             `json:"attempted,omitempty"`
	ChanceSucceeded   bool             `json:"chance_succeeded,omitempty"`
	Hit               bool             `json:"hit,omitempty"`
	Succeeded         bool             `json:"succeeded,omitempty"`
	DamageApplied     bool             `json:"damage_applied,omitempty"`
	CombatApplied     bool             `json:"combat_applied,omitempty"`
	CombatDeferred    bool             `json:"combat_deferred,omitempty"`
	EnemyAdded        bool             `json:"enemy_added,omitempty"`
	ClearInvisible    bool             `json:"clear_invisible,omitempty"`
	ActorTimerWritten bool             `json:"actor_timer_written,omitempty"`
	MPChanged         bool             `json:"mp_changed,omitempty"`
	Chance            int              `json:"chance,omitempty"`
	Roll              int              `json:"roll,omitempty"`
	Damage            int              `json:"damage,omitempty"`
	Absorbed          int              `json:"absorbed,omitempty"`
	EnemyDamage       int32            `json:"enemy_damage,omitempty"`
	ActorHPBefore     int16            `json:"actor_hp_before"`
	ActorHP           int16            `json:"actor_hp"`
	MPBefore          int16            `json:"mp_before"`
	MPAfter           int16            `json:"mp_after"`
	TargetHPBefore    int              `json:"target_hp_before,omitempty"`
	TargetHP          int              `json:"target_hp,omitempty"`
	AttackInterval    int32            `json:"attack_interval,omitempty"`
	CooldownInterval  int32            `json:"cooldown_interval,omitempty"`
	ActorID           string           `json:"actor_id"`
	ActorName         string           `json:"actor_name,omitempty"`
	RoomID            int16            `json:"room_id,omitempty"`
	TargetID          string           `json:"target_id,omitempty"`
	TargetKind        AbsorbTargetKind `json:"target_kind,omitempty"`
	TargetName        string           `json:"target_name,omitempty"`
	TargetOccurrence  int              `json:"target_occurrence,omitempty"`
	Event             *AbsorbEvent     `json:"event,omitempty"`
}

// AbsorbProposal is a snapshot-bound candidate.  before is private so a
// caller cannot manufacture a permission or identity snapshot.  ApplyAbsorb
// rechecks every public projection and performs no random calls.
type AbsorbProposal struct {
	ActorID          string
	ActorName        string
	RoomID           int16
	QueryName        string
	TargetID         string
	TargetKind       AbsorbTargetKind
	TargetName       string
	TargetOccurrence int
	Now              int32

	Chance           int
	Roll             int
	Damage           int
	Absorbed         int
	EnemyDamage      int32
	ActorHPBefore    int16
	ActorHP          int16
	MPBefore         int16
	MPAfter          int16
	TargetHPBefore   int
	TargetHP         int
	AttackInterval   int32
	CooldownInterval int32
	WaitSeconds      int32

	Authorized      bool
	TargetFound     bool
	Cooldown        bool
	Rejected        bool
	Undead          bool
	NoOp            bool
	Attempted       bool
	ChanceSucceeded bool
	Hit             bool
	Succeeded       bool
	DamageApplied   bool
	CombatApplied   bool
	CombatDeferred  bool
	EnemyAdded      bool
	ClearInvisible  bool
	ActorTimerWrite bool
	MPChanged       bool
	Broadcast       bool
	Response        string
	ExpectedEvent   *AbsorbEvent

	before State
}

type AbsorbCandidate = AbsorbProposal
type AbsorbOutcome = AbsorbResult

type absorbResolution struct {
	id         string
	name       string
	target     LegacyMonster
	occurrence int
}

func validAbsorbBodyName(name string) bool {
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

func validAbsorbSelector(name string) bool {
	if !validAbsorbBodyName(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func absorbActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != absorbPlayerType || !validAbsorbBodyName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online absorb actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("absorb actor room membership absent")
	}
	return actor, room, nil
}

func absorbTargetVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt hides PDMINV caretaker/DM entries first, then
	// requires PDINVI to see an ordinary MINVIS entry.
	if target.Class >= absorbCaretakerClass && flag(target.Flags[:], absorbTargetDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], absorbTargetDMInvisible-8) || flag(actor.Flags[:], absorbActorDetectFlag)
}

func selectAbsorbNPC(s State, actorID, query string, occurrence int) (absorbResolution, error) {
	if occurrence < 1 {
		return absorbResolution{}, fmt.Errorf("absorb target occurrence must be positive")
	}
	if !validAbsorbSelector(query) {
		return absorbResolution{}, fmt.Errorf("invalid absorb target name")
	}
	actor, room, err := absorbActor(s, actorID)
	if err != nil {
		return absorbResolution{}, err
	}
	if s.NPCs == nil {
		return absorbResolution{}, ErrAbsorbNPCStateUnresolved
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != absorbMonsterType || npc.Body.RoomID != room.Resource.ID || !validAbsorbBodyName(npc.Body.Name) {
			return absorbResolution{}, fmt.Errorf("unresolved canonical absorb NPC identity")
		}
		// The bounded command narrows legacy EQUAL's key/prefix matcher to an
		// exact display identity.  EqualFold retains the existing terminal
		// convention without allowing a different key to select an NPC.
		if !strings.EqualFold(npc.Body.Name, query) || !absorbTargetVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return absorbResolution{id: id, name: npc.Body.Name, target: npc.Body, occurrence: found}, nil
		}
	}
	return absorbResolution{}, nil
}

// AbsorbChance returns the exact source formula before mrand(1,100).  The
// legacy bonus table is finite; an invalid intelligence value is rejected
// instead of indexing outside the migrated state.
func AbsorbChance(actor, target LegacyMonster) (int, error) {
	if actor.Stats[absorbIntelligenceStat] > 63 {
		return 0, fmt.Errorf("absorb actor intelligence outside legacy bonus table")
	}
	chance := (((int(actor.Level)+3)/4)-((int(target.Level)+3)/4))*20 + legacyStatBonus[actor.Stats[absorbIntelligenceStat]]*5
	if chance > 80 {
		chance = 80
	}
	return chance, nil
}

func AbsorbHitChance(actor, target LegacyMonster) (int, error) {
	return AbsorbChance(actor, target)
}

func absorbWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("absorb cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", seconds), nil
}

func absorbRandom(roll func(int, int) int, low, high int) (value int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("absorb random source panicked: %v", recovered)
		}
	}()
	return randomIn(roll, low, high)
}

func absorbDamage(actor LegacyMonster) (int, error) {
	damage := ((int(actor.Level) + 73) / 4) * 10
	if damage < 1 {
		return 0, fmt.Errorf("absorb damage outside legacy range")
	}
	return damage, nil
}

func absorbEnemy(npc NPCState, actorID string) (found bool, damage int32, err error) {
	if npc.Enemies == nil {
		return false, 0, ErrAbsorbCombatSideEffectPending
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Damage < 0 {
			return false, 0, fmt.Errorf("%w: negative enemy damage", ErrAbsorbCombatSideEffectPending)
		}
		if enemy.Target == want {
			return true, enemy.Damage, nil
		}
	}
	return false, 0, nil
}

// absorbPotentialCombat proves that a successful roll can be represented by
// this reducer before consuming randomness.  Lethal death, enemy damage
// overflow, and the source's flee threshold all require a broader atomic
// combat/death transition, so they fail closed.
func absorbPotentialCombat(actor LegacyMonster, target LegacyMonster, npc NPCState, actorID string, chance, damage int) error {
	if chance < 1 {
		return nil // mrand(1,100) can never satisfy a non-positive chance.
	}
	if target.HPCurrent < 1 {
		return fmt.Errorf("%w: target is already dead", ErrAbsorbDeathTransitionPending)
	}
	post := int(target.HPCurrent) - damage
	if post < 1 {
		return fmt.Errorf("%w: target=%s", ErrAbsorbDeathTransitionPending, target.Name)
	}
	if int64(actor.HPCurrent)+int64(max(1, damage)) > int64(absorbMaxHP) || int64(actor.HPCurrent)+int64(max(1, damage)) < int64(absorbMinHP) {
		return ErrAbsorbHPOverflow
	}
	if flag(target.Flags[:], absorbTargetFleerFlag) && !flag(target.Flags[:], absorbTargetFollowDMFlag) && int(target.HPMax) > 0 && post <= int(target.HPMax)/5 {
		return fmt.Errorf("%w: target flee transition=%s", ErrAbsorbCombatSideEffectPending, target.Name)
	}
	found, existing, err := absorbEnemy(npc, actorID)
	if err != nil {
		return err
	}
	if found && int64(existing)+int64(min(int(target.HPCurrent), damage)) > math.MaxInt32 {
		return fmt.Errorf("%w: enemy damage overflow", ErrAbsorbCombatSideEffectPending)
	}
	return nil
}

func absorbRevealResponse() string {
	return "\n당신의 모습이 나타나기 시작합니다.\n"
}

func absorbRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s의 모습이 보이기 시작합니다.\n", actor.Name, legacySubjectParticle(actor.Name))
}

func absorbNoArgumentResponse() string {
	return "\n누구에게 주문을 거실려고요?\n"
}

func absorbUnauthorizedResponse() string {
	return "\n도술사만이 흡성대법을 사용할수 있습니다.\n"
}

func absorbNoTargetResponse() string {
	return "\n그런 괴물은 존재하지 않습니다.\n"
}

func absorbNoHarmResponse(target LegacyMonster) string {
	pronoun := "그녀"
	if flag(target.Flags[:], absorbMaleFlag) {
		pronoun = "그"
	}
	return fmt.Sprintf("\n당신은 %s의 기를 흡수할수 없습니다.\n", pronoun)
}

func absorbMissResponse(target LegacyMonster) string {
	return fmt.Sprintf("\n손을 적에게 뻗으며 기를 흡수하는 흡성대법의 주문을 \n외쳤습니다.하지만 주문이 튕겨져 나오면서 %s가 당신의 주술을 \n견뎌냈습니다.\n", target.Name)
}

func absorbSuccessResponse() string {
	return "\n손을 적에게 뻗으며 기를 흡수하는 흡성대법의 주문을 외칩니다.\n적의 몸에서 기가 흘러나와 손으로\n빨려 들어갑니다.\n "
}

func absorbUndeadResponse() string {
	return "\n윽! 악령의 사악한 기운이 당신에게 흘러듭니다.\n"
}

func absorbMissRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 손을 적에게 뻗으며 흡성대법의 주문을 외칩니다.\n하지만 %s%s 그의 주술을 견뎌냈습니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name, legacySubjectParticle(target.Name))
}

func absorbSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 손을 적에게 뻗으며 흡성대법의 주문을 외칩니다.\n%s의 몸에서 기가 흘러나와 손으로 빨려듭니다.\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func absorbEvent(actorID string, actor LegacyMonster, target absorbResolution, texts []string) *AbsorbEvent {
	if len(texts) == 0 {
		return nil
	}
	copied := append([]string(nil), texts...)
	return &AbsorbEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: target.id, TargetKind: AbsorbTargetNPC,
		TargetName: target.name, Texts: copied, Text: strings.Join(copied, ""),
	}
}

func absorbProposalResult(p AbsorbProposal) AbsorbResult {
	return AbsorbResult{
		Action: "absorb", Response: p.Response,
		Changed:   p.ClearInvisible || p.ActorTimerWrite || p.EnemyAdded || p.DamageApplied || p.MPChanged,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Authorized: p.Authorized,
		TargetFound: p.TargetFound, Cooldown: p.Cooldown, Rejected: p.Rejected,
		Undead: p.Undead, WaitSeconds: p.WaitSeconds, Attempted: p.Attempted,
		ChanceSucceeded: p.ChanceSucceeded, Hit: p.Hit, Succeeded: p.Succeeded,
		DamageApplied: p.DamageApplied, CombatApplied: p.CombatApplied,
		CombatDeferred: p.CombatDeferred, EnemyAdded: p.EnemyAdded,
		ClearInvisible: p.ClearInvisible, ActorTimerWritten: p.ActorTimerWrite,
		MPChanged: p.MPChanged, Chance: p.Chance, Roll: p.Roll,
		Damage: p.Damage, Absorbed: p.Absorbed, EnemyDamage: p.EnemyDamage,
		ActorHPBefore: p.ActorHPBefore, ActorHP: p.ActorHP,
		MPBefore: p.MPBefore, MPAfter: p.MPAfter,
		TargetHPBefore: p.TargetHPBefore, TargetHP: p.TargetHP,
		AttackInterval: p.AttackInterval, CooldownInterval: p.CooldownInterval,
		ActorID: p.ActorID, ActorName: p.ActorName, RoomID: p.RoomID,
		TargetID: p.TargetID, TargetKind: p.TargetKind, TargetName: p.TargetName,
		TargetOccurrence: p.TargetOccurrence, Event: p.ExpectedEvent,
	}
}

// PlanAbsorb is the default one-based target occurrence form.
func (s State) PlanAbsorb(actorID, targetName string, now int32, roll func(int, int) int) (AbsorbProposal, error) {
	return s.PlanAbsorbWithOccurrence(actorID, targetName, 1, now, roll)
}

// PlanAbsorbWithOccurrence follows magic3.c: permission, canonical visible
// NPC lookup, stealth reveal, cooldown, MUNKIL, enemy relation/timers, chance,
// and then the single random draw.  A successful lethal result or an unsafe
// enemy/flee continuation is rejected before that draw.
func (s State) PlanAbsorbWithOccurrence(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (AbsorbProposal, error) {
	zero := AbsorbProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 || occurrence < 1 {
		return zero, fmt.Errorf("invalid absorb actor, occurrence, or clock")
	}
	actor, room, err := absorbActor(s, actorID)
	if err != nil {
		return zero, err
	}
	if actor.Body.Class > absorbMaxLegacyClass {
		return zero, fmt.Errorf("absorb actor class outside legacy table")
	}
	p := AbsorbProposal{
		ActorID: actorID, ActorName: actor.Body.Name, RoomID: room.Resource.ID,
		QueryName: targetName, TargetOccurrence: occurrence, Now: now,
		ActorHPBefore: actor.Body.HPCurrent, ActorHP: actor.Body.HPCurrent,
		MPBefore: actor.Body.MPCurrent, MPAfter: actor.Body.MPCurrent,
		TargetKind: AbsorbTargetNPC, before: s.clone(),
	}
	p.Authorized = actor.Body.Class == absorbMageClass || actor.Body.Class >= absorbInvincibleClass
	// magic3.c checks the command arity before the class gate, so a bare
	// command keeps its original no-argument response even for an unauthorized
	// actor.  The terminal parser normally rejects this form, but world callers
	// must preserve the source ordering too.
	if targetName == "" {
		p.NoOp, p.TargetKind, p.Response = true, "", absorbNoArgumentResponse()
		return p, nil
	}
	if !p.Authorized {
		p.NoOp, p.TargetKind, p.Response = true, "", absorbUnauthorizedResponse()
		return p, nil
	}
	if !validAbsorbSelector(targetName) {
		return zero, fmt.Errorf("invalid absorb target name")
	}
	resolution, err := selectAbsorbNPC(s, actorID, targetName, occurrence)
	if err != nil {
		return zero, err
	}
	if resolution.id == "" {
		p.NoOp, p.TargetFound, p.TargetKind, p.Response = true, false, "", absorbNoTargetResponse()
		return p, nil
	}
	p.TargetFound, p.TargetID, p.TargetKind, p.TargetName = true, resolution.id, AbsorbTargetNPC, resolution.name
	p.ClearInvisible = flag(actor.Body.Flags[:], absorbActorInvisibleFlag)
	roomTexts := make([]string, 0, 2)
	responsePrefix := ""
	if p.ClearInvisible {
		responsePrefix = absorbRevealResponse()
		roomTexts = append(roomTexts, absorbRevealRoom(actor.Body))
	}

	// The source reveals before reading LT_TURNS, so a cooldown candidate can
	// still atomically clear PINVIS and publish its event.
	timer := actor.Body.Timers[absorbCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("absorb cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait > math.MaxInt32 {
			return zero, fmt.Errorf("absorb cooldown overflow")
		}
		p.NoOp, p.Cooldown, p.WaitSeconds = true, true, int32(wait)
		waitText, waitErr := absorbWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return zero, waitErr
		}
		p.Response = responsePrefix + waitText
		p.ExpectedEvent = absorbEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	if flag(resolution.target.Flags[:], absorbTargetNoHarmFlag) {
		p.NoOp, p.Rejected = true, true
		p.Response = responsePrefix + absorbNoHarmResponse(resolution.target)
		p.ExpectedEvent = absorbEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	npc := s.NPCs[resolution.id]
	hasEnemy, _, err := absorbEnemy(npc, actorID)
	if err != nil {
		return zero, fmt.Errorf("%w: target=%s", err, resolution.id)
	}
	p.EnemyAdded = !hasEnemy
	p.AttackInterval, p.CooldownInterval = absorbCooldownSeconds, absorbCooldownSeconds
	p.Chance, err = AbsorbChance(actor.Body, resolution.target)
	if err != nil {
		return zero, err
	}
	damage, err := absorbDamage(actor.Body)
	if err != nil {
		return zero, err
	}
	if !flag(resolution.target.Flags[:], absorbTargetUndeadFlag) {
		if err := absorbPotentialCombat(actor.Body, resolution.target, npc, actorID, p.Chance, damage); err != nil {
			return zero, err
		}
	}

	// add_enm_crt and both actor timers precede mrand in the C routine.
	p.ActorTimerWrite, p.Attempted = true, true
	p.Roll, err = absorbRandom(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	p.ChanceSucceeded, p.Hit = p.Roll <= p.Chance, p.Roll <= p.Chance
	if !p.ChanceSucceeded {
		p.Response = responsePrefix + absorbMissResponse(resolution.target)
		p.ExpectedEvent = absorbEvent(actorID, actor.Body, resolution, append(roomTexts, absorbMissRoom(actor.Body, resolution.target)))
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	p.Succeeded = true
	p.Response = responsePrefix + absorbSuccessResponse()
	p.ExpectedEvent = absorbEvent(actorID, actor.Body, resolution, append(roomTexts, absorbSuccessRoom(actor.Body, resolution.target)))
	p.Broadcast = p.ExpectedEvent != nil
	p.CombatApplied = true
	if flag(resolution.target.Flags[:], absorbTargetUndeadFlag) {
		p.Undead, p.MPChanged, p.MPAfter = true, actor.Body.MPCurrent != 0, 0
		p.Response += absorbUndeadResponse()
		return p, nil
	}

	p.Damage = damage
	p.Absorbed = min(int(resolution.target.HPCurrent), damage)
	p.EnemyDamage = int32(p.Absorbed)
	p.TargetHPBefore = int(resolution.target.HPCurrent)
	p.TargetHP = p.TargetHPBefore - damage
	actorHP := int(actor.Body.HPCurrent) + max(1, damage) // source cap is commented out
	if actorHP < int(absorbMinHP) || actorHP > int(absorbMaxHP) {
		return zero, ErrAbsorbHPOverflow
	}
	p.ActorHP = int16(actorHP)
	p.DamageApplied = true
	return p, nil
}

// ApplyAbsorb validates and atomically applies a candidate planned from this
// exact snapshot.  It never calls a random source and never leaves a corpse
// or an unresolved combat continuation in persistent state.
func (s State) ApplyAbsorb(p AbsorbProposal) (State, AbsorbResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, AbsorbResult{}, err
	}
	if p.ActorID == "" || p.Now < 0 || p.before.Version != 1 || p.Response == "" || !reflect.DeepEqual(s, p.before) {
		return State{}, AbsorbResult{}, fmt.Errorf("stale or invalid absorb proposal")
	}
	actor, room, err := absorbActor(s, p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID || actor.Body.Name != p.ActorName {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb actor changed")
	}
	if actor.Body.Class > absorbMaxLegacyClass {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb actor class outside legacy table")
	}
	authorized := actor.Body.Class == absorbMageClass || actor.Body.Class >= absorbInvincibleClass
	if p.Authorized != authorized {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb authorization changed")
	}
	result := absorbProposalResult(p)

	if p.QueryName == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ExpectedEvent != nil || p.Broadcast || p.ActorHPBefore != actor.Body.HPCurrent || p.ActorHP != actor.Body.HPCurrent || p.MPBefore != actor.Body.MPCurrent || p.MPAfter != actor.Body.MPCurrent || p.Response != absorbNoArgumentResponse() {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb missing-target proposal")
		}
		return s.clone(), result, nil
	}

	if !authorized {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ExpectedEvent != nil || p.Broadcast || p.Response != absorbUnauthorizedResponse() {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb authorization proposal")
		}
		return s.clone(), result, nil
	}
	if p.TargetOccurrence < 1 {
		return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb target occurrence")
	}
	if !validAbsorbSelector(p.QueryName) {
		return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb target selector")
	}
	resolution, err := selectAbsorbNPC(s, p.ActorID, p.QueryName, p.TargetOccurrence)
	if err != nil {
		return State{}, AbsorbResult{}, err
	}
	if resolution.id == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ExpectedEvent != nil || p.Broadcast || p.Response != absorbNoTargetResponse() {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb absent-target proposal")
		}
		return s.clone(), result, nil
	}
	if !p.TargetFound || p.TargetID != resolution.id || p.TargetKind != AbsorbTargetNPC || p.TargetName != resolution.name {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb target identity changed")
	}
	reveal := flag(actor.Body.Flags[:], absorbActorInvisibleFlag)
	if p.ClearInvisible != reveal || p.CombatDeferred {
		if p.CombatDeferred {
			return State{}, AbsorbResult{}, ErrAbsorbCombatSideEffectPending
		}
		return State{}, AbsorbResult{}, fmt.Errorf("absorb invisibility proposal changed")
	}
	if p.CombatDeferred {
		return State{}, AbsorbResult{}, ErrAbsorbCombatSideEffectPending
	}
	if p.ActorHPBefore != actor.Body.HPCurrent || p.MPBefore != actor.Body.MPCurrent {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb actor vital projection changed")
	}

	roomTexts := make([]string, 0, 2)
	responsePrefix := ""
	if reveal {
		responsePrefix = absorbRevealResponse()
		roomTexts = append(roomTexts, absorbRevealRoom(actor.Body))
	}
	timer := actor.Body.Timers[absorbCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if p.Cooldown {
		if !p.NoOp || p.Rejected || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ActorTimerWrite || p.EnemyAdded || p.DamageApplied || p.MPChanged || int64(p.Now) >= deadline {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb cooldown proposal")
		}
		wait := deadline - int64(p.Now)
		if wait < 1 || wait > math.MaxInt32 || p.WaitSeconds != int32(wait) {
			return State{}, AbsorbResult{}, fmt.Errorf("absorb cooldown changed")
		}
		waitText, waitErr := absorbWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return State{}, AbsorbResult{}, waitErr
		}
		wantEvent := absorbEvent(p.ActorID, actor.Body, resolution, roomTexts)
		if p.Response != responsePrefix+waitText || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, AbsorbResult{}, fmt.Errorf("stale absorb cooldown projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, absorbActorInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = p.Response, reveal, wantEvent
		result.Broadcast, result.Cooldown, result.TargetFound = wantEvent != nil, true, true
		result.ClearInvisible = reveal
		if err := next.Validate(); err != nil {
			return State{}, AbsorbResult{}, err
		}
		return next, result, nil
	}
	if p.Rejected {
		if !p.NoOp || p.Cooldown || p.Attempted || p.Succeeded || p.Roll != 0 || p.Chance != 0 || p.ActorTimerWrite || p.EnemyAdded || p.DamageApplied || p.MPChanged || int64(p.Now) < deadline || !flag(resolution.target.Flags[:], absorbTargetNoHarmFlag) {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb protected-target proposal")
		}
		wantEvent := absorbEvent(p.ActorID, actor.Body, resolution, roomTexts)
		wantResponse := responsePrefix + absorbNoHarmResponse(resolution.target)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, AbsorbResult{}, fmt.Errorf("stale absorb protected-target projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, absorbActorInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = p.Response, reveal, wantEvent
		result.Broadcast, result.Rejected, result.TargetFound = wantEvent != nil, true, true
		result.ClearInvisible = reveal
		if err := next.Validate(); err != nil {
			return State{}, AbsorbResult{}, err
		}
		return next, result, nil
	}

	npc := s.NPCs[resolution.id]
	hasEnemy, existingDamage, err := absorbEnemy(npc, p.ActorID)
	if err != nil {
		return State{}, AbsorbResult{}, fmt.Errorf("%w: target=%s", err, resolution.id)
	}
	if p.EnemyAdded == hasEnemy {
		return State{}, AbsorbResult{}, fmt.Errorf("absorb enemy projection changed")
	}
	chance, err := AbsorbChance(actor.Body, resolution.target)
	if err != nil || chance != p.Chance {
		if err != nil {
			return State{}, AbsorbResult{}, err
		}
		return State{}, AbsorbResult{}, fmt.Errorf("absorb chance changed")
	}
	damage, err := absorbDamage(actor.Body)
	if err != nil {
		return State{}, AbsorbResult{}, err
	}
	if !flag(resolution.target.Flags[:], absorbTargetUndeadFlag) {
		if err := absorbPotentialCombat(actor.Body, resolution.target, npc, p.ActorID, chance, damage); err != nil {
			return State{}, AbsorbResult{}, err
		}
	}
	if !p.Attempted || !p.ActorTimerWrite || p.AttackInterval != absorbCooldownSeconds || p.CooldownInterval != absorbCooldownSeconds || p.Roll < 1 || p.Roll > 100 || p.ChanceSucceeded != (p.Roll <= chance) || p.Hit != p.ChanceSucceeded {
		return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb attempt proposal")
	}
	if !p.ChanceSucceeded {
		if p.Succeeded || p.Undead || p.DamageApplied || p.CombatApplied || p.Damage != 0 || p.Absorbed != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 0 || p.TargetHP != 0 || p.ActorHP != actor.Body.HPCurrent || p.MPAfter != actor.Body.MPCurrent {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb miss projection")
		}
		wantEvent := absorbEvent(p.ActorID, actor.Body, resolution, append(roomTexts, absorbMissRoom(actor.Body, resolution.target)))
		wantResponse := responsePrefix + absorbMissResponse(resolution.target)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, AbsorbResult{}, fmt.Errorf("stale absorb miss projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, absorbActorInvisibleFlag, false)
		}
		nextActor.Body.Timers[absorbCooldownTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: absorbCooldownSeconds, Misc: timer.Misc}
		nextActor.Body.Timers[absorbAttackTimerIndex].LastTime = p.Now
		next.Players[p.ActorID] = nextActor
		if p.EnemyAdded {
			npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: p.ActorID}, Damage: 0})
			next.NPCs[resolution.id] = npc
		}
		result.Response, result.Changed, result.Event = p.Response, true, wantEvent
		result.Broadcast, result.TargetFound = wantEvent != nil, true
		result.ClearInvisible, result.ActorTimerWritten, result.EnemyAdded = reveal, true, p.EnemyAdded
		result.AttackInterval, result.CooldownInterval = absorbCooldownSeconds, absorbCooldownSeconds
		if err := next.Validate(); err != nil {
			return State{}, AbsorbResult{}, err
		}
		return next, result, nil
	}

	if !p.Succeeded || p.DamageApplied != !p.Undead || p.CombatApplied != true || p.Response == "" {
		return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb success projection")
	}
	wantEvent := absorbEvent(p.ActorID, actor.Body, resolution, append(roomTexts, absorbSuccessRoom(actor.Body, resolution.target)))
	wantResponse := responsePrefix + absorbSuccessResponse()
	if p.Undead {
		if !flag(resolution.target.Flags[:], absorbTargetUndeadFlag) || p.Damage != 0 || p.Absorbed != 0 || p.EnemyDamage != 0 || p.TargetHPBefore != 0 || p.TargetHP != 0 || p.ActorHP != actor.Body.HPCurrent || p.MPAfter != 0 || p.MPChanged != (actor.Body.MPCurrent != 0) {
			return State{}, AbsorbResult{}, fmt.Errorf("invalid absorb undead projection")
		}
		wantResponse += absorbUndeadResponse()
	} else {
		if flag(resolution.target.Flags[:], absorbTargetUndeadFlag) || p.Damage != damage || p.Absorbed != min(int(resolution.target.HPCurrent), damage) || p.EnemyDamage != int32(p.Absorbed) || p.TargetHPBefore != int(resolution.target.HPCurrent) || p.TargetHP != p.TargetHPBefore-damage || p.ActorHP != int16(int(actor.Body.HPCurrent)+max(1, damage)) || p.MPAfter != actor.Body.MPCurrent {
			return State{}, AbsorbResult{}, fmt.Errorf("absorb damage projection changed")
		}
		if int64(p.ActorHP) > int64(absorbMaxHP) || int64(p.ActorHP) < int64(absorbMinHP) || int64(p.TargetHP) > int64(absorbMaxHP) || int64(p.TargetHP) < int64(absorbMinHP) {
			return State{}, AbsorbResult{}, ErrAbsorbHPOverflow
		}
	}
	if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
		return State{}, AbsorbResult{}, fmt.Errorf("stale absorb success projection")
	}
	if hasEnemy && !p.EnemyAdded && int64(existingDamage)+int64(p.EnemyDamage) > math.MaxInt32 {
		return State{}, AbsorbResult{}, ErrAbsorbCombatSideEffectPending
	}

	next := s.clone()
	nextActor := next.Players[p.ActorID]
	if reveal {
		setSettingFlag(&nextActor.Body, absorbActorInvisibleFlag, false)
	}
	nextActor.Body.Timers[absorbCooldownTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: absorbCooldownSeconds, Misc: timer.Misc}
	nextActor.Body.Timers[absorbAttackTimerIndex].LastTime = p.Now
	if p.Undead {
		nextActor.Body.MPCurrent = 0
	} else {
		nextActor.Body.HPCurrent = p.ActorHP
	}
	next.Players[p.ActorID] = nextActor
	npc = next.NPCs[resolution.id]
	if p.EnemyAdded {
		npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: p.ActorID}, Damage: 0})
	}
	if !p.Undead {
		npc.Body.HPCurrent = int16(p.TargetHP)
		for i := range npc.Enemies {
			if npc.Enemies[i].Target == (EntityRef{Kind: "player", ID: p.ActorID}) {
				if int64(npc.Enemies[i].Damage)+int64(p.EnemyDamage) > math.MaxInt32 {
					return State{}, AbsorbResult{}, ErrAbsorbCombatSideEffectPending
				}
				npc.Enemies[i].Damage += p.EnemyDamage
				break
			}
		}
	}
	next.NPCs[resolution.id] = npc
	result.Response, result.Changed, result.Event = p.Response, true, wantEvent
	result.Broadcast, result.TargetFound = wantEvent != nil, true
	result.ClearInvisible, result.ActorTimerWritten = reveal, true
	result.EnemyAdded, result.AttackInterval, result.CooldownInterval = p.EnemyAdded, absorbCooldownSeconds, absorbCooldownSeconds
	if p.Undead {
		result.MPChanged = actor.Body.MPCurrent != 0
	}
	if err := next.Validate(); err != nil {
		return State{}, AbsorbResult{}, err
	}
	return next, result, nil
}

// Absorb is the convenient plan/apply form for callers that do not retain a
// candidate between the world loop's read and commit phases.
func (s State) Absorb(actorID, targetName string, now int32, roll func(int, int) int) (State, AbsorbResult, error) {
	p, err := s.PlanAbsorb(actorID, targetName, now, roll)
	if err != nil {
		return State{}, AbsorbResult{}, err
	}
	return s.ApplyAbsorb(p)
}

func (s State) AbsorbByName(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (State, AbsorbResult, error) {
	p, err := s.PlanAbsorbWithOccurrence(actorID, targetName, occurrence, now, roll)
	if err != nil {
		return State{}, AbsorbResult{}, err
	}
	return s.ApplyAbsorb(p)
}
