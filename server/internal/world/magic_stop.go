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

// These are the source-backed legacy slots used by command7.c:magic_stop
// (the public alias is registered by global.c).  The reducer keeps the
// original indexes in the canonical body instead of inventing a transport
// lock or a new status bit.
const (
	magicStopAttackTimerIndex    = 3  // LT_ATTCK
	magicStopSpellTimerIndex     = 9  // LT_SPELL
	magicStopCooldownTimerIndex  = 15 // LT_TURNS
	magicStopCooldownSeconds     = int32(20)
	magicStopRangerClass         = 7  // RANGER
	magicStopInvincibleClass     = 9  // INVINCIBLE
	magicStopMaxLegacyClass      = 12 // DM
	magicStopPlayerType          = 0  // PLAYER
	magicStopMonsterType         = 1  // MONSTER
	magicStopPlayerInvisibleFlag = 2  // PINVIS
	magicStopPlayerDMInvisible   = 10 // PDMINV
	magicStopPlayerDetectFlag    = 21 // PDINVI
	magicStopPlayerResistMagic   = 32 // PRMAGI (player target branch)
	magicStopPlayerMaleFlag      = 12 // PMALES
	magicStopNPCInvisibleFlag    = 2  // MINVIS
	magicStopNPCNoHarmFlag       = 24 // MUNKIL
	magicStopNPCResistMagicFlag  = 27 // MRMAGI
	magicStopNPCResistBefuddle   = 43 // MRBEFD
	magicStopNPCSummonFlag       = 61 // MSUMMO
)

// Exported aliases expose only source-confirmed slots and constants to the
// session and future tick code.  There is no magic_stop spell bit in the C
// routine: success records LT_SPELL, while Spells remains unchanged.
const (
	MagicStopAttackTimerIndex      = magicStopAttackTimerIndex
	MagicStopSpellTimerIndex       = magicStopSpellTimerIndex
	MagicStopCooldownTimerIndex    = magicStopCooldownTimerIndex
	MagicStopAttackTimer           = magicStopAttackTimerIndex
	MagicStopSpellTimer            = magicStopSpellTimerIndex
	MagicStopTimerIndex            = magicStopCooldownTimerIndex
	MagicStopCooldownTimer         = magicStopCooldownTimerIndex
	MagicStopCooldownSeconds       = magicStopCooldownSeconds
	MagicStopRangerClass           = magicStopRangerClass
	MagicStopInvincibleClass       = magicStopInvincibleClass
	MagicStopMaxLegacyClass        = magicStopMaxLegacyClass
	MagicStopPlayerType            = magicStopPlayerType
	MagicStopMonsterType           = magicStopMonsterType
	MagicStopActorInvisibleFlag    = magicStopPlayerInvisibleFlag
	MagicStopPlayerInvisibleFlag   = magicStopPlayerInvisibleFlag
	MagicStopActorDetectFlag       = magicStopPlayerDetectFlag
	MagicStopPlayerDetectFlag      = magicStopPlayerDetectFlag
	MagicStopTargetInvisibleFlag   = magicStopNPCInvisibleFlag
	MagicStopNPCInvisibleFlag      = magicStopNPCInvisibleFlag
	MagicStopTargetDMInvisible     = magicStopPlayerDMInvisible
	MagicStopPlayerDMInvisibleFlag = magicStopPlayerDMInvisible
	MagicStopTargetNoHarmFlag      = magicStopNPCNoHarmFlag
	MagicStopNPCNoHarmFlag         = magicStopNPCNoHarmFlag
	MagicStopTargetResistMagic     = magicStopNPCResistMagicFlag
	MagicStopNPCResistMagicFlag    = magicStopNPCResistMagicFlag
	MagicStopTargetResistBefuddle  = magicStopNPCResistBefuddle
	MagicStopNPCResistBefuddleFlag = magicStopNPCResistBefuddle
)

var (
	// ErrMagicStopNPCStateUnresolved prevents a command from silently
	// consulting Resource.Monsters or another non-canonical identity source.
	ErrMagicStopNPCStateUnresolved = errors.New("magic_stop canonical NPC state unresolved")
	// ErrMagicStopCombatSideEffectPending remains the fail-closed boundary for
	// lethal, summoned, legacy, or otherwise unresolved combat continuations.
	// Ordinary canonical NPC nonlethal continuation is admitted below; death
	// and flee are still deliberately outside this reducer.
	ErrMagicStopCombatSideEffectPending = errors.New("magic_stop combat side effect pending")
)

// MagicStopTargetKind is closed because command7.c starts find_crt at
// first_mon.  Player targets and their PRMAGI branch are outside this bounded
// slice; accepting them here would make an unproven player combat transition.
type MagicStopTargetKind string

const (
	MagicStopTargetNPC MagicStopTargetKind = "npc"
	MagicStopNPC       MagicStopTargetKind = MagicStopTargetNPC
)

// MagicStopEvent is the deterministic room projection owned by the receipt.
// The actor receives Result.Response, so room fan-out excludes ActorID.
type MagicStopEvent struct {
	RoomID         int16               `json:"room_id"`
	ExcludeActorID string              `json:"exclude_actor_id"`
	ActorID        string              `json:"actor_id"`
	ActorName      string              `json:"actor_name"`
	TargetID       string              `json:"target_id"`
	TargetKind     MagicStopTargetKind `json:"target_kind"`
	TargetName     string              `json:"target_name"`
	Texts          []string            `json:"texts"`
	// Text is retained as a convenient single projection for transports that
	// do not stream each ordered line separately.
	Text string `json:"text,omitempty"`
}

// MagicStopResult contains the actor response, all random draws, and the
// post-commit room projection. Combat fields are explicit so a durable receipt
// can replay the NPC hostility, nonlethal HP, and timer transition without
// re-running the random source.
type MagicStopResult struct {
	Action             string              `json:"action"`
	Response           string              `json:"response"`
	Changed            bool                `json:"changed"`
	Broadcast          bool                `json:"broadcast"`
	NoOp               bool                `json:"no_op,omitempty"`
	Authorized         bool                `json:"authorized"`
	TargetFound        bool                `json:"target_found"`
	Cooldown           bool                `json:"cooldown,omitempty"`
	Rejected           bool                `json:"rejected,omitempty"`
	WaitSeconds        int32               `json:"wait_seconds,omitempty"`
	Attempted          bool                `json:"attempted,omitempty"`
	Succeeded          bool                `json:"succeeded,omitempty"`
	Chance             int                 `json:"chance,omitempty"`
	Roll               int                 `json:"roll,omitempty"`
	DamageRoll         int                 `json:"damage_roll,omitempty"`
	Damage             int                 `json:"damage,omitempty"`
	EnemyDamage        int                 `json:"enemy_damage,omitempty"`
	TargetHPBefore     int                 `json:"target_hp_before,omitempty"`
	TargetHP           int                 `json:"target_hp,omitempty"`
	DurationRoll1      int                 `json:"duration_roll_1,omitempty"`
	DurationRoll2      int                 `json:"duration_roll_2,omitempty"`
	SpellInterval      int32               `json:"spell_interval,omitempty"`
	SpellTimerWritten  bool                `json:"spell_timer_written,omitempty"`
	StatusTimerWritten bool                `json:"status_timer_written,omitempty"`
	StatusBitChanged   bool                `json:"status_bit_changed,omitempty"`
	CombatApplied      bool                `json:"combat_applied,omitempty"`
	CombatDeferred     bool                `json:"combat_deferred,omitempty"`
	DamageApplied      bool                `json:"damage_applied,omitempty"`
	EnemyAdded         bool                `json:"enemy_added,omitempty"`
	ActorID            string              `json:"actor_id"`
	ActorName          string              `json:"actor_name,omitempty"`
	RoomID             int16               `json:"room_id,omitempty"`
	TargetID           string              `json:"target_id,omitempty"`
	TargetKind         MagicStopTargetKind `json:"target_kind,omitempty"`
	TargetName         string              `json:"target_name,omitempty"`
	TargetOccurrence   int                 `json:"target_occurrence,omitempty"`
	MPBefore           int16               `json:"mp_before"`
	MPAfter            int16               `json:"mp_after"`
	Event              *MagicStopEvent     `json:"event,omitempty"`
}

// MagicStopProposal is a state-bound candidate.  before is private so an
// untrusted payload cannot provide an authorization snapshot.  All random
// draws happen in PlanMagicStop; ApplyMagicStop validates the recorded values
// and never calls a random source.
type MagicStopProposal struct {
	ActorID          string
	ActorName        string
	RoomID           int16
	QueryName        string
	TargetID         string
	TargetKind       MagicStopTargetKind
	TargetName       string
	TargetOccurrence int
	Now              int32
	Chance           int
	Roll             int
	DamageRoll       int
	Damage           int
	EnemyDamage      int
	TargetHPBefore   int
	TargetHP         int
	DurationRoll1    int
	DurationRoll2    int
	SpellInterval    int32
	MPBefore         int16
	MPAfter          int16

	Authorized       bool
	TargetFound      bool
	Cooldown         bool
	Rejected         bool
	NoOp             bool
	Attempted        bool
	Succeeded        bool
	ClearInvisible   bool
	TimerWrite       bool
	SpellTimerWrite  bool
	StatusTimerWrite bool
	StatusBitChanged bool
	CombatApplied    bool
	CombatDeferred   bool
	DamageApplied    bool
	EnemyAdded       bool
	Broadcast        bool
	WaitSeconds      int32
	Response         string
	ExpectedEvent    *MagicStopEvent

	before State
}

// Candidate/Outcome aliases keep the operation discoverable without creating
// a second wire or reducer type.
type MagicStopCandidate = MagicStopProposal
type MagicStopOutcome = MagicStopResult

type magicStopResolution struct {
	id         string
	name       string
	target     LegacyMonster
	occurrence int
}

func validMagicStopBodyName(name string) bool {
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

func validMagicStopSelector(name string) bool {
	if !validMagicStopBodyName(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func magicStopActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != magicStopPlayerType || !validMagicStopBodyName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online magic_stop actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("magic_stop actor room membership absent")
	}
	return actor, room, nil
}

func magicStopTargetVisible(actor, target LegacyMonster) bool {
	// This is creature.c:find_crt's exact two-part gate.  PDMINV is read from
	// the same legacy flag array on a monster, as in the original C layout.
	if target.Class >= magicStopMaxLegacyClass-2 && flag(target.Flags[:], magicStopPlayerDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], magicStopNPCInvisibleFlag) || flag(actor.Flags[:], magicStopPlayerDetectFlag)
}

func magicStopTargetMatches(target LegacyMonster, query string) bool {
	// find_crt accepts either the displayed name or one of the canonical
	// creature keys. The terminal adapter still admits one token only, but
	// resolving both forms preserves the source identity contract without
	// consulting a non-canonical resource list.
	if strings.EqualFold(target.Name, query) {
		return true
	}
	for _, key := range target.Keys {
		if key != "" && strings.EqualFold(key, query) {
			return true
		}
	}
	return false
}

func selectMagicStopNPC(s State, actorID, query string, occurrence int) (magicStopResolution, error) {
	if occurrence < 1 {
		return magicStopResolution{}, fmt.Errorf("magic_stop target occurrence must be positive")
	}
	if !validMagicStopSelector(query) {
		return magicStopResolution{}, fmt.Errorf("invalid magic_stop target name")
	}
	actor, room, err := magicStopActor(s, actorID)
	if err != nil {
		return magicStopResolution{}, err
	}
	if s.NPCs == nil {
		return magicStopResolution{}, ErrMagicStopNPCStateUnresolved
	}
	found := 0
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != magicStopMonsterType || npc.Body.RoomID != room.Resource.ID || !validMagicStopBodyName(npc.Body.Name) {
			return magicStopResolution{}, fmt.Errorf("unresolved canonical magic_stop NPC identity")
		}
		if !magicStopTargetMatches(npc.Body, query) || !magicStopTargetVisible(actor.Body, npc.Body) {
			continue
		}
		found++
		if found == occurrence {
			return magicStopResolution{id: id, name: npc.Body.Name, target: npc.Body, occurrence: found}, nil
		}
	}
	return magicStopResolution{}, nil
}

// MagicStopChance returns the source formula before the injected 1..100 draw.
// The legacy bonus table is finite; an out-of-table DEX value is rejected
// rather than indexing memory as the C implementation would.
func MagicStopChance(actor, target LegacyMonster) (int, error) {
	if actor.Stats[1] > 63 {
		return 0, fmt.Errorf("magic_stop actor dexterity outside legacy bonus table")
	}
	chance := ((int(actor.Level)+3)/4 - (int(target.Level)+3)/4) * 20
	chance += legacyStatBonus[actor.Stats[1]] * 6
	if chance > 80 {
		chance = 80
	}
	if actor.Class == magicStopRangerClass {
		chance += 10
	}
	return chance, nil
}

// MagicStopHitChance is a descriptive alias for callers that prefer the
// outcome-oriented name.
func MagicStopHitChance(actor, target LegacyMonster) (int, error) {
	return MagicStopChance(actor, target)
}

func magicStopWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("magic_stop cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\n", seconds), nil
}

func magicStopRoll(roll func(int, int) int, low, high int) (value int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("magic_stop random source panicked: %v", recovered)
		}
	}()
	return randomIn(roll, low, high)
}

func magicStopDurationFromRolls(actor LegacyMonster, target LegacyMonster, first, second int) (int32, error) {
	if first < 1 || first > 6 || second < 1 || second > 6 {
		return 0, fmt.Errorf("magic_stop duration roll outside 1..6")
	}
	if actor.Stats[1] > 63 {
		return 0, fmt.Errorf("magic_stop actor dexterity outside legacy bonus table")
	}
	if target.Class >= magicStopMaxLegacyClass-2 && flag(target.Flags[:], magicStopPlayerDMInvisible) {
		return 0, fmt.Errorf("magic_stop target became invisible")
	}
	duration := legacyStatBonus[actor.Stats[1]]*2 + first + second
	if flag(target.Flags[:], magicStopNPCResistMagicFlag) || flag(target.Flags[:], magicStopNPCResistBefuddle) {
		duration = 5
	} else if duration < 15 {
		duration = 15
	}
	if duration > 20 {
		duration = 20
	}
	return int32(duration), nil
}

func magicStopPendingError(reason string) error {
	return fmt.Errorf("%w: %s", ErrMagicStopCombatSideEffectPending, reason)
}

func magicStopUnresolvedError(reason string) error {
	return fmt.Errorf("%w: %w: %s", ErrMagicStopCombatSideEffectPending, ErrMagicStopNPCStateUnresolved, reason)
}

// magicStopNPCCombatState is the ordinary-NPC continuation boundary. The
// source routine only reaches add_enm_crt after the visible-target,
// cooldown, and MUNKIL gates; this helper is intentionally called after those
// gates so unresolved canonical relations never consume randomness or create a
// receipt. A negative enemy damage value is the recovery/import unresolved
// marker and must not be treated as zero.
func magicStopNPCCombatState(s State, resolution magicStopResolution, actorID string) (NPCState, int, error) {
	if s.NPCs == nil || resolution.id == "" {
		return NPCState{}, -1, magicStopUnresolvedError("canonical NPC map absent")
	}
	npc, ok := s.NPCs[resolution.id]
	if !ok || npc.Body.Type != magicStopMonsterType || npc.Body.RoomID != resolution.target.RoomID || !validMagicStopBodyName(npc.Body.Name) {
		return NPCState{}, -1, magicStopUnresolvedError("canonical NPC identity changed")
	}
	if npc.Body.Class > magicStopMaxLegacyClass {
		return NPCState{}, -1, magicStopUnresolvedError("NPC class outside legacy range")
	}
	if len(npc.Body.Inventory) != 0 {
		return NPCState{}, -1, magicStopUnresolvedError("legacy NPC inventory unresolved")
	}
	if npc.Enemies == nil {
		return NPCState{}, -1, magicStopUnresolvedError("NPC enemy relations unresolved")
	}
	if npc.Body.HPCurrent < 1 {
		return NPCState{}, -1, magicStopPendingError("NPC target HP is not positive")
	}
	if flag(npc.Body.Flags[:], magicStopNPCSummonFlag) {
		return NPCState{}, -1, magicStopPendingError("summoned NPC combat continuation pending")
	}

	want := EntityRef{Kind: "player", ID: actorID}
	seen := make(map[EntityRef]bool, len(npc.Enemies))
	matching := -1
	for i, enemy := range npc.Enemies {
		if enemy.Target.ID == "" || (enemy.Target.Kind != "player" && enemy.Target.Kind != "npc") || seen[enemy.Target] {
			return NPCState{}, -1, magicStopUnresolvedError("invalid NPC enemy graph")
		}
		seen[enemy.Target] = true
		if enemy.Damage < 0 {
			return NPCState{}, -1, magicStopUnresolvedError("negative NPC enemy damage")
		}
		if enemy.Target == want {
			matching = i
		}
	}
	if matching >= 0 && int64(npc.Enemies[matching].Damage)+int64(npc.Body.HPCurrent/2) > math.MaxInt32 {
		return NPCState{}, -1, magicStopPendingError("NPC enemy damage overflow")
	}
	return npc, matching, nil
}

// magicStopAddEnemyDamage mirrors add_enm_crt/add_enm_dmg while preserving the
// canonical room/NPC order. A newly inserted edge is appended, and an existing
// edge is accumulated exactly once; State.Validate rejects duplicate edges.
func magicStopAddEnemyDamage(npc *NPCState, actorID string, added bool, amount int) error {
	if npc == nil || npc.Enemies == nil || actorID == "" || amount < 0 || int64(amount) > math.MaxInt32 {
		return fmt.Errorf("invalid magic_stop enemy damage")
	}
	firstEnemy := len(npc.Enemies) == 0
	want := EntityRef{Kind: "player", ID: actorID}
	matching := -1
	for i, enemy := range npc.Enemies {
		if enemy.Target == want {
			if matching >= 0 {
				return fmt.Errorf("magic_stop duplicate NPC enemy relation")
			}
			matching = i
		}
	}
	if added {
		if matching >= 0 {
			return fmt.Errorf("magic_stop NPC enemy relation already exists")
		}
		npc.Enemies = append(npc.Enemies, NPCEnemy{Target: want})
		matching = len(npc.Enemies) - 1
		// creature.c:add_enm_crt leaves NUMHITS alone for the first edge, but
		// resets it after appending to an already populated enemy list.
		if !firstEnemy {
			npc.Body.Quests[0] = 0
		}
	} else if matching < 0 {
		return fmt.Errorf("magic_stop NPC enemy relation absent")
	}
	if int64(npc.Enemies[matching].Damage)+int64(amount) > math.MaxInt32 {
		return fmt.Errorf("magic_stop enemy damage overflow")
	}
	npc.Enemies[matching].Damage += int32(amount)
	return nil
}

func magicStopDamageResponse(target LegacyMonster, damage int) string {
	return fmt.Sprintf("%s의 급소를 짚어서 %d의 피해를 입혔습니다.\n", target.Name, damage)
}

// magicStopActorRoomName mirrors command7.c's room-side %M for the player
// actor after magic_stop has cleared PINVIS. %M supplies the player honorific;
// the following particles are literal source text, not name-derived josa.
func magicStopActorRoomName(actor LegacyMonster) string {
	return combatPlayerLabel(actor.Name, false)
}

func magicStopDamageRoom(actor, target LegacyMonster, damage int) string {
	return fmt.Sprintf("%s이 %s의 급소를 짚어서 %d의 피해를 입혔습니다.\n", magicStopActorRoomName(actor), target.Name, damage)
}

func magicStopRevealResponse() string {
	return "\n당신의 모습이 나타나기 시작합니다.\n"
}

func magicStopRevealResponseIf(reveal bool) string {
	if !reveal {
		return ""
	}
	return magicStopRevealResponse()
}

func magicStopRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s의 모습이 보이기 시작합니다.\n", magicStopActorRoomName(actor))
}

func magicStopNoTargetResponse() string {
	return "\n그런 괴물은 존재하지 않습니다.\n"
}

func magicStopNoArgumentResponse() string {
	return "\n누구의 혈도를 봉쇄하실려구요?\n"
}

func magicStopUnauthorizedResponse() string {
	return "\n포졸만이 혈도봉쇄를 사용할수 있습니다.\n"
}

func magicStopNoHarmResponse(target LegacyMonster) string {
	pronoun := "그녀"
	if flag(target.Flags[:], magicStopPlayerMaleFlag) {
		pronoun = "그"
	}
	return fmt.Sprintf("\n당신은 %s의 혈도를 봉쇄할수 없습니다.\n", pronoun)
}

func magicStopMissResponse() string {
	return "\n당신은 적의 혈도를 재빨리 봉쇄했습니다.\n그러나 적이 살짝 피해 빗나갔습니다.\n"
}

func magicStopSuccessResponse() string {
	return "\n당신은 적의 혈도를 재빨리 봉쇄했습니다.\n적의 혈도를 짚어 주문을 봉쇄했습니다.\n "
}

func magicStopMissRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s이 적의 혈도를 재빨리 봉쇄했습니다.\n그러나 %s가 살짝 피했습니다.\n", magicStopActorRoomName(actor), target.Name)
}

func magicStopSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s이 적의 혈도를 재빨리 봉쇄했습니다.\n%s의 혈도가 짚혀 주문이 봉쇄되었습니다.\n", magicStopActorRoomName(actor), target.Name)
}

func magicStopEvent(actorID string, actor LegacyMonster, resolution magicStopResolution, texts []string) *MagicStopEvent {
	if len(texts) == 0 {
		return nil
	}
	copied := append([]string(nil), texts...)
	return &MagicStopEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: resolution.id, TargetKind: MagicStopTargetNPC,
		TargetName: resolution.name, Texts: copied, Text: strings.Join(copied, ""),
	}
}

func magicStopAppendReveal(actor LegacyMonster, response string, texts []string, reveal bool) (string, []string) {
	if !reveal {
		return response, texts
	}
	return magicStopRevealResponse() + response, append(texts, magicStopRevealRoom(actor))
}

// PlanMagicStop is the default one-based target occurrence form used by the
// terminal command.  The source parser supplies val[1]=1 when omitted.
func (s State) PlanMagicStop(actorID, targetName string, now int32, roll func(int, int) int) (MagicStopProposal, error) {
	return s.PlanMagicStopWithOccurrence(actorID, targetName, 1, now, roll)
}

// PlanMagicStopWithOccurrence ports the confirmed command7.c ordering:
// argument/class -> canonical same-room visible NPC lookup -> reveal ->
// LT_TURNS cooldown -> MUNKIL gate -> canonical combat graph/HP -> add_enm_crt
// and actor timers -> chance/damage/duration draws. Death and flee
// continuation remain deliberately outside this nonlethal candidate.
func (s State) PlanMagicStopWithOccurrence(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (MagicStopProposal, error) {
	zero := MagicStopProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 || occurrence < 1 {
		return zero, fmt.Errorf("invalid magic_stop actor, occurrence, or clock")
	}
	actor, room, err := magicStopActor(s, actorID)
	if err != nil {
		return zero, err
	}
	p := MagicStopProposal{
		ActorID: actorID, ActorName: actor.Body.Name, RoomID: room.Resource.ID,
		QueryName: targetName, TargetOccurrence: occurrence, Now: now,
		MPBefore: actor.Body.MPCurrent, MPAfter: actor.Body.MPCurrent,
		before: s.clone(),
	}
	if actor.Body.Class > magicStopMaxLegacyClass {
		return zero, fmt.Errorf("magic_stop actor class outside legacy table")
	}
	p.Authorized = actor.Body.Class == magicStopRangerClass || actor.Body.Class >= magicStopInvincibleClass
	if targetName == "" {
		p.NoOp = true
		p.Response = magicStopNoArgumentResponse()
		return p, nil
	}
	if !p.Authorized {
		p.NoOp = true
		p.Response = magicStopUnauthorizedResponse()
		return p, nil
	}
	if !validMagicStopSelector(targetName) {
		return zero, fmt.Errorf("invalid magic_stop target name")
	}
	resolution, err := selectMagicStopNPC(s, actorID, targetName, occurrence)
	if err != nil {
		return zero, err
	}
	if resolution.id == "" {
		p.NoOp = true
		p.Response = magicStopNoTargetResponse()
		return p, nil
	}
	p.TargetFound = true
	p.TargetID, p.TargetKind, p.TargetName = resolution.id, MagicStopTargetNPC, resolution.name
	p.ClearInvisible = flag(actor.Body.Flags[:], magicStopPlayerInvisibleFlag)
	roomTexts := make([]string, 0, 2)
	p.Response, roomTexts = magicStopAppendReveal(actor.Body, "", roomTexts, p.ClearInvisible)

	// C looks up the target before LT_TURNS.  Therefore a hidden actor is
	// revealed even when the already-resolved target is still on cooldown.
	timer := actor.Body.Timers[magicStopCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("magic_stop cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait > math.MaxInt32 {
			return zero, fmt.Errorf("magic_stop cooldown overflow")
		}
		p.NoOp, p.Cooldown, p.WaitSeconds = true, true, int32(wait)
		waitResponse, waitErr := magicStopWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return zero, waitErr
		}
		p.Response += waitResponse
		p.ExpectedEvent = magicStopEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	if flag(resolution.target.Flags[:], magicStopNPCNoHarmFlag) {
		p.NoOp, p.Rejected = true, true
		p.Response += magicStopNoHarmResponse(resolution.target)
		p.ExpectedEvent = magicStopEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	npc, enemyIndex, err := magicStopNPCCombatState(s, resolution, actorID)
	if err != nil {
		return zero, err
	}
	p.TargetHPBefore = int(npc.Body.HPCurrent)
	p.TargetHP = p.TargetHPBefore
	p.EnemyAdded = enemyIndex < 0
	p.CombatApplied = true

	// C assigns LT_TURNS.interval but only updates LT_ATTCK.ltime. Preserve
	// both timers' non-owned fields and reject malformed values before RNG.
	cooldownTimer := actor.Body.Timers[magicStopCooldownTimerIndex]
	attackTimer := actor.Body.Timers[magicStopAttackTimerIndex]
	if cooldownTimer.LastTime < 0 || cooldownTimer.Interval < 0 || attackTimer.LastTime < 0 || attackTimer.Interval < 0 {
		return zero, fmt.Errorf("magic_stop combat timer outside legacy range")
	}
	p.TimerWrite = true
	p.Chance, err = MagicStopChance(actor.Body, npc.Body)
	if err != nil {
		return zero, err
	}
	p.Roll, err = magicStopRoll(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	p.Attempted = true
	p.Succeeded = p.Roll <= p.Chance
	if !p.Succeeded {
		p.Response += magicStopMissResponse()
		roomTexts = append(roomTexts, magicStopMissRoom(actor.Body, npc.Body))
		p.ExpectedEvent = magicStopEvent(actorID, actor.Body, resolution, roomTexts)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}

	p.DamageRoll, err = magicStopRoll(roll, 25, 100)
	if err != nil {
		return zero, err
	}
	p.DamageApplied = p.DamageRoll <= p.Chance
	if p.DamageApplied {
		p.Damage = p.TargetHPBefore / 2
		p.TargetHP = p.TargetHPBefore - p.Damage
		p.EnemyDamage = min(p.TargetHPBefore, p.Damage)
		if p.TargetHP < 1 {
			return zero, magicStopPendingError("lethal NPC target transition pending")
		}
	}

	// C always evaluates dice(2,6,0) after a successful first chance roll,
	// including when the separate damage chance misses.
	p.DurationRoll1, err = magicStopRoll(roll, 1, 6)
	if err != nil {
		return zero, err
	}
	p.DurationRoll2, err = magicStopRoll(roll, 1, 6)
	if err != nil {
		return zero, err
	}
	p.SpellInterval, err = magicStopDurationFromRolls(actor.Body, npc.Body, p.DurationRoll1, p.DurationRoll2)
	if err != nil {
		return zero, err
	}
	p.SpellTimerWrite = true
	p.Response += magicStopSuccessResponse()
	roomTexts = append(roomTexts, magicStopSuccessRoom(actor.Body, npc.Body))
	if p.DamageApplied {
		p.Response += magicStopDamageResponse(npc.Body, p.Damage)
		roomTexts = append(roomTexts, magicStopDamageRoom(actor.Body, npc.Body, p.Damage))
	}
	p.ExpectedEvent = magicStopEvent(actorID, actor.Body, resolution, roomTexts)
	p.Broadcast = p.ExpectedEvent != nil
	return p, nil
}

func magicStopProposalCombatFieldsEmpty(p MagicStopProposal) bool {
	return !p.Attempted && !p.Succeeded && p.Chance == 0 && p.Roll == 0 && p.DamageRoll == 0 &&
		p.Damage == 0 && p.EnemyDamage == 0 && p.TargetHPBefore == 0 && p.TargetHP == 0 &&
		p.DurationRoll1 == 0 && p.DurationRoll2 == 0 && p.SpellInterval == 0 &&
		!p.TimerWrite && !p.SpellTimerWrite && !p.StatusTimerWrite && !p.StatusBitChanged &&
		!p.CombatApplied && !p.CombatDeferred && !p.DamageApplied && !p.EnemyAdded
}

func magicStopProposalResult(p MagicStopProposal) MagicStopResult {
	return MagicStopResult{
		Action: "magic_stop", Response: p.Response,
		Changed:   p.ClearInvisible || p.TimerWrite || p.SpellTimerWrite,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Authorized: p.Authorized,
		TargetFound: p.TargetFound, Cooldown: p.Cooldown, Rejected: p.Rejected,
		WaitSeconds: p.WaitSeconds, Attempted: p.Attempted, Succeeded: p.Succeeded,
		Chance: p.Chance, Roll: p.Roll, DamageRoll: p.DamageRoll,
		Damage: p.Damage, EnemyDamage: p.EnemyDamage,
		TargetHPBefore: p.TargetHPBefore, TargetHP: p.TargetHP,
		DurationRoll1: p.DurationRoll1,
		DurationRoll2: p.DurationRoll2, SpellInterval: p.SpellInterval,
		SpellTimerWritten: p.SpellTimerWrite, StatusTimerWritten: p.StatusTimerWrite,
		StatusBitChanged: p.StatusBitChanged, CombatApplied: p.CombatApplied,
		CombatDeferred: p.CombatDeferred, DamageApplied: p.DamageApplied,
		EnemyAdded: p.EnemyAdded, ActorID: p.ActorID, ActorName: p.ActorName,
		RoomID: p.RoomID, TargetID: p.TargetID, TargetKind: p.TargetKind,
		TargetName: p.TargetName, TargetOccurrence: p.TargetOccurrence,
		MPBefore: p.MPBefore, MPAfter: p.MPAfter, Event: p.ExpectedEvent,
	}
}

// ApplyMagicStop validates a proposal against the exact planned snapshot and
// atomically applies the canonical NPC nonlethal continuation. It never calls
// the random source; every draw and projection is checked against the source
// ordering captured by PlanMagicStop.
func (s State) ApplyMagicStop(p MagicStopProposal) (State, MagicStopResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, MagicStopResult{}, err
	}
	if p.ActorID == "" || p.Now < 0 || p.before.Version != 1 || p.Response == "" || !reflect.DeepEqual(s, p.before) {
		return State{}, MagicStopResult{}, fmt.Errorf("stale or invalid magic_stop proposal")
	}
	actor, room, err := magicStopActor(s, p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID || actor.Body.Name != p.ActorName {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop actor changed")
	}
	if actor.Body.Class > magicStopMaxLegacyClass {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop actor class outside legacy table")
	}
	authorized := actor.Body.Class == magicStopRangerClass || actor.Body.Class >= magicStopInvincibleClass
	if p.Authorized != authorized {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop authorization changed")
	}
	result := magicStopProposalResult(p)

	if p.TargetOccurrence < 1 {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop target occurrence")
	}
	if p.QueryName == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.WaitSeconds != 0 || p.ExpectedEvent != nil || p.Broadcast || !magicStopProposalCombatFieldsEmpty(p) || p.MPBefore != actor.Body.MPCurrent || p.MPAfter != actor.Body.MPCurrent || p.Response != magicStopNoArgumentResponse() {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop missing-target proposal")
		}
		return s.clone(), result, nil
	}

	// Permission failure is deliberately resolved before target lookup, as in
	// command7.c.  This branch cannot carry any target, timer, RNG, or event.
	if !authorized {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.ExpectedEvent != nil || p.Broadcast || !magicStopProposalCombatFieldsEmpty(p) || p.Response != magicStopUnauthorizedResponse() {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop authorization proposal")
		}
		return s.clone(), result, nil
	}

	if !validMagicStopSelector(p.QueryName) {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop target selector")
	}
	resolution, err := selectMagicStopNPC(s, p.ActorID, p.QueryName, p.TargetOccurrence)
	if err != nil {
		return State{}, MagicStopResult{}, err
	}
	if resolution.id == "" {
		if !p.NoOp || p.TargetFound || p.TargetID != "" || p.TargetName != "" || p.TargetKind != "" || p.ClearInvisible || p.Cooldown || p.Rejected || p.WaitSeconds != 0 || p.ExpectedEvent != nil || p.Broadcast || !magicStopProposalCombatFieldsEmpty(p) || p.MPBefore != actor.Body.MPCurrent || p.MPAfter != actor.Body.MPCurrent || p.Response != magicStopNoTargetResponse() {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop absent-target proposal")
		}
		return s.clone(), result, nil
	}
	if !p.TargetFound || p.TargetID != resolution.id || p.TargetKind != MagicStopTargetNPC || p.TargetName != resolution.name {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop target identity changed")
	}
	reveal := flag(actor.Body.Flags[:], magicStopPlayerInvisibleFlag)
	if p.ClearInvisible != reveal {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop invisibility proposal changed")
	}
	if p.StatusBitChanged || p.MPBefore != actor.Body.MPCurrent || p.MPAfter != actor.Body.MPCurrent {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop deferred side-effect projection")
	}
	if p.CombatDeferred {
		// Defense in depth for candidates made by an older caller or a same-
		// package test: an omitted combat continuation can never be committed.
		return State{}, MagicStopResult{}, ErrMagicStopCombatSideEffectPending
	}

	roomTexts := make([]string, 0, 2)
	responsePrefix := ""
	if reveal {
		responsePrefix = magicStopRevealResponse()
		roomTexts = append(roomTexts, magicStopRevealRoom(actor.Body))
	}
	timer := actor.Body.Timers[magicStopCooldownTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop cooldown timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if p.Cooldown {
		if !p.NoOp || p.Rejected || int64(p.Now) >= deadline || !magicStopProposalCombatFieldsEmpty(p) {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop cooldown proposal")
		}
		wait := deadline - int64(p.Now)
		if wait < 1 || wait > math.MaxInt32 || p.WaitSeconds != int32(wait) {
			return State{}, MagicStopResult{}, fmt.Errorf("magic_stop cooldown changed")
		}
		waitResponse, waitErr := magicStopWaitResponse(p.WaitSeconds)
		if waitErr != nil {
			return State{}, MagicStopResult{}, waitErr
		}
		response := responsePrefix + waitResponse
		wantEvent := magicStopEvent(p.ActorID, actor.Body, resolution, roomTexts)
		if p.Response != response || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, MagicStopResult{}, fmt.Errorf("stale magic_stop cooldown projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, magicStopPlayerInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = response, reveal, wantEvent
		result.Broadcast, result.Cooldown, result.TargetFound = wantEvent != nil, true, true
		if err := next.Validate(); err != nil {
			return State{}, MagicStopResult{}, err
		}
		return next, result, nil
	}

	if p.Rejected {
		if !p.NoOp || int64(p.Now) < deadline || !flag(resolution.target.Flags[:], magicStopNPCNoHarmFlag) || !magicStopProposalCombatFieldsEmpty(p) {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop protected-target proposal")
		}
		response := responsePrefix + magicStopNoHarmResponse(resolution.target)
		wantEvent := magicStopEvent(p.ActorID, actor.Body, resolution, roomTexts)
		if p.Response != response || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, MagicStopResult{}, fmt.Errorf("stale magic_stop protected-target projection")
		}
		next := s.clone()
		nextActor := next.Players[p.ActorID]
		if reveal {
			setSettingFlag(&nextActor.Body, magicStopPlayerInvisibleFlag, false)
		}
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = response, reveal, wantEvent
		result.Broadcast, result.Rejected, result.TargetFound = wantEvent != nil, true, true
		if err := next.Validate(); err != nil {
			return State{}, MagicStopResult{}, err
		}
		return next, result, nil
	}

	// Ordinary canonical NPC continuation starts only after the same
	// post-gate graph/HP checks used by planning. A stale or tampered proposal
	// cannot turn a missing relation, summon, legacy inventory, or dead target
	// into a durable combat receipt.
	npc, enemyIndex, err := magicStopNPCCombatState(s, resolution, p.ActorID)
	if err != nil {
		return State{}, MagicStopResult{}, err
	}
	if p.NoOp || p.Rejected || p.Cooldown || !p.CombatApplied || p.CombatDeferred || !p.TimerWrite || p.StatusTimerWrite || p.StatusBitChanged ||
		p.MPBefore != actor.Body.MPCurrent || p.MPAfter != actor.Body.MPCurrent || p.TargetHPBefore != int(npc.Body.HPCurrent) || p.TargetHPBefore < 1 ||
		p.TargetHP < 1 || p.EnemyAdded != (enemyIndex < 0) {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop combat projection")
	}
	if p.ClearInvisible != reveal {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop invisibility proposal changed")
	}
	if p.Now < 0 {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop clock")
	}
	attackTimer := actor.Body.Timers[magicStopAttackTimerIndex]
	if attackTimer.LastTime < 0 || attackTimer.Interval < 0 {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop attack timer outside legacy range")
	}
	next := s.clone()
	nextActor := next.Players[p.ActorID]
	nextNPC := next.NPCs[p.TargetID]
	// add_enm_crt precedes chance and all later timer/damage work in
	// command7.c. Apply the zero-damage insertion first, then the successful
	// branch accumulates its separately recorded min(previous HP, damage) value
	// below.
	if err := magicStopAddEnemyDamage(&nextNPC, p.ActorID, p.EnemyAdded, 0); err != nil {
		return State{}, MagicStopResult{}, err
	}
	chance, chanceErr := MagicStopChance(actor.Body, npc.Body)
	if chanceErr != nil || chance != p.Chance || p.Roll < 1 || p.Roll > 100 || !p.Attempted || p.Succeeded != (p.Roll <= p.Chance) {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop chance outcome")
	}
	if p.TargetHP != p.TargetHPBefore && !p.Succeeded {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop miss HP projection")
	}

	if p.ClearInvisible {
		setSettingFlag(&nextActor.Body, magicStopPlayerInvisibleFlag, false)
	}
	nextCooldown := nextActor.Body.Timers[magicStopCooldownTimerIndex]
	nextCooldown.LastTime = p.Now
	nextActor.Body.Timers[magicStopCooldownTimerIndex] = nextCooldown
	nextAttack := nextActor.Body.Timers[magicStopAttackTimerIndex]
	nextAttack.LastTime = p.Now
	nextActor.Body.Timers[magicStopAttackTimerIndex] = nextAttack
	nextCooldown = nextActor.Body.Timers[magicStopCooldownTimerIndex]
	nextCooldown.Interval = magicStopCooldownSeconds
	nextActor.Body.Timers[magicStopCooldownTimerIndex] = nextCooldown
	if !p.Succeeded {
		if p.SpellTimerWrite || p.DamageRoll != 0 || p.DamageApplied || p.Damage != 0 || p.EnemyDamage != 0 || p.DurationRoll1 != 0 || p.DurationRoll2 != 0 || p.SpellInterval != 0 || p.TargetHP != p.TargetHPBefore {
			return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop chance miss projection")
		}
		wantResponse := magicStopRevealResponseIf(reveal) + magicStopMissResponse()
		wantTexts := make([]string, 0, 2)
		if reveal {
			wantTexts = append(wantTexts, magicStopRevealRoom(actor.Body))
		}
		wantTexts = append(wantTexts, magicStopMissRoom(actor.Body, npc.Body))
		wantEvent := magicStopEvent(p.ActorID, actor.Body, resolution, wantTexts)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, MagicStopResult{}, fmt.Errorf("stale magic_stop miss projection")
		}
		next.NPCs[p.TargetID] = nextNPC
		next.Players[p.ActorID] = nextActor
		result.Response, result.Changed, result.Event = p.Response, true, wantEvent
		result.Broadcast, result.TargetFound = wantEvent != nil, true
		if err := next.Validate(); err != nil {
			return State{}, MagicStopResult{}, err
		}
		return next, result, nil
	}

	if p.DamageRoll < 25 || p.DamageRoll > 100 || p.DamageApplied != (p.DamageRoll <= p.Chance) || p.SpellTimerWrite == false ||
		p.DurationRoll1 < 1 || p.DurationRoll1 > 6 || p.DurationRoll2 < 1 || p.DurationRoll2 > 6 {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop hit projection")
	}
	interval, intervalErr := magicStopDurationFromRolls(actor.Body, npc.Body, p.DurationRoll1, p.DurationRoll2)
	if intervalErr != nil || p.SpellInterval != interval {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop duration projection changed")
	}
	wantDamage := 0
	wantEnemyDamage := 0
	wantTargetHP := p.TargetHPBefore
	if p.DamageApplied {
		wantDamage = p.TargetHPBefore / 2
		wantTargetHP = p.TargetHPBefore - wantDamage
		wantEnemyDamage = min(p.TargetHPBefore, wantDamage)
		if wantTargetHP < 1 {
			return State{}, MagicStopResult{}, magicStopPendingError("lethal NPC target transition pending")
		}
	} else if p.Damage != 0 || p.EnemyDamage != 0 || p.TargetHP != p.TargetHPBefore {
		return State{}, MagicStopResult{}, fmt.Errorf("invalid magic_stop no-damage projection")
	}
	if p.Damage != wantDamage || p.EnemyDamage != wantEnemyDamage || p.TargetHP != wantTargetHP {
		return State{}, MagicStopResult{}, fmt.Errorf("magic_stop damage projection changed")
	}
	if err := magicStopAddEnemyDamage(&nextNPC, p.ActorID, false, p.EnemyDamage); err != nil {
		return State{}, MagicStopResult{}, err
	}
	nextNPC.Body.HPCurrent = int16(p.TargetHP)
	spellTimer := nextNPC.Body.Timers[magicStopSpellTimerIndex]
	spellTimer.LastTime = p.Now
	spellTimer.Interval = p.SpellInterval
	nextNPC.Body.Timers[magicStopSpellTimerIndex] = spellTimer
	wantResponse := magicStopRevealResponseIf(reveal) + magicStopSuccessResponse()
	wantTexts := make([]string, 0, 3)
	if reveal {
		wantTexts = append(wantTexts, magicStopRevealRoom(actor.Body))
	}
	wantTexts = append(wantTexts, magicStopSuccessRoom(actor.Body, npc.Body))
	if p.DamageApplied {
		wantResponse += magicStopDamageResponse(npc.Body, p.Damage)
		wantTexts = append(wantTexts, magicStopDamageRoom(actor.Body, npc.Body, p.Damage))
	}
	wantEvent := magicStopEvent(p.ActorID, actor.Body, resolution, wantTexts)
	if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
		return State{}, MagicStopResult{}, fmt.Errorf("stale magic_stop success projection")
	}
	next.NPCs[p.TargetID] = nextNPC
	next.Players[p.ActorID] = nextActor
	result.Response, result.Changed, result.Event = p.Response, true, wantEvent
	result.Broadcast, result.TargetFound = wantEvent != nil, true
	if err := next.Validate(); err != nil {
		return State{}, MagicStopResult{}, err
	}
	return next, result, nil
}

// MagicStop is a convenience wrapper for callers that do not need to retain
// the intermediate candidate.
func (s State) MagicStop(actorID, targetName string, now int32, roll func(int, int) int) (State, MagicStopResult, error) {
	p, err := s.PlanMagicStop(actorID, targetName, now, roll)
	if err != nil {
		return State{}, MagicStopResult{}, err
	}
	return s.ApplyMagicStop(p)
}

// MagicStopByName is the explicit occurrence convenience form.
func (s State) MagicStopByName(actorID, targetName string, occurrence int, now int32, roll func(int, int) int) (State, MagicStopResult, error) {
	p, err := s.PlanMagicStopWithOccurrence(actorID, targetName, occurrence, now, roll)
	if err != nil {
		return State{}, MagicStopResult{}, err
	}
	return s.ApplyMagicStop(p)
}
