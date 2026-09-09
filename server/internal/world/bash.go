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

// The indices and flags below are the source values used by command8.c:bash.
// Keeping them explicit is important while the legacy body remains the
// canonical storage shape: a transport-local stun or cooldown must not be
// substituted for the durable legacy slots.
const (
	bashAttackTimerIndex    = 3  // LT_ATTCK
	bashBefuddleTimerIndex  = 40 // LT_BEFUD
	bashFighterClass        = 4  // FIGHTER
	bashBarbarianClass      = 2  // BARBARIAN
	bashInvincibleClass     = 9  // INVINCIBLE
	bashCaretakerClass      = 10 // CARETAKER
	bashMaxLegacyClass      = 12 // DM
	bashRoomNoKillFlag      = 11 // RNOKIL
	bashRoomSurvivalFlag    = 36 // RSUVIV
	bashPlayerHiddenFlag    = 1  // PHIDDN
	bashPlayerInvisibleFlag = 2  // PINVIS
	bashPlayerDetectFlag    = 21 // PDINVI
	bashPlayerMaleFlag      = 12 // PMALES
	bashPlayerChaosFlag     = 28 // PCHAOS
	bashPlayerBlindFlag     = 42 // PBLIND
	bashPlayerCharmFlag     = 45 // PCHARM
	bashPlayerFamilyFlag    = 55 // PFAMIL
	bashNPCNoMagicFlag      = 20 // MMGONL
	bashNPCEnchantOnlyFlag  = 22 // MENONL
	bashNPCNoHarmFlag       = 24 // MUNKIL
	bashNPCDMInvisibleFlag  = 10 // PDMINV on caretaker+ entries
	bashNPCBefuddledFlag    = 51 // MBEFUD
	bashWieldSlot           = 19 // WIELD-1
)

// Export source indices and class values for command adapters and migration
// contract tests. The lower-case constants remain implementation details.
const (
	BashTimerIndex          = bashAttackTimerIndex
	BashBefuddleTimerIndex  = bashBefuddleTimerIndex
	BashAttackTimerIndex    = bashAttackTimerIndex
	BashFighterClass        = bashFighterClass
	BashBarbarianClass      = bashBarbarianClass
	BashInvincibleClass     = bashInvincibleClass
	BashCaretakerClass      = bashCaretakerClass
	BashRoomNoKillFlag      = bashRoomNoKillFlag
	BashRoomSurvivalFlag    = bashRoomSurvivalFlag
	BashPlayerHiddenFlag    = bashPlayerHiddenFlag
	BashPlayerInvisibleFlag = bashPlayerInvisibleFlag
	BashPlayerDetectFlag    = bashPlayerDetectFlag
	BashPlayerMaleFlag      = bashPlayerMaleFlag
	BashPlayerChaosFlag     = bashPlayerChaosFlag
	BashPlayerBlindFlag     = bashPlayerBlindFlag
	BashPlayerCharmFlag     = bashPlayerCharmFlag
	BashPlayerFamilyFlag    = bashPlayerFamilyFlag
	BashWieldSlot           = bashWieldSlot
)

var (
	// ErrBashDeathTransitionPending is returned before a lethal candidate is
	// exposed. The caller must compose the canonical player/NPC death reducer
	// before admitting such a command.
	ErrBashDeathTransitionPending = errors.New("lethal bash death transition pending")
	// ErrBashCharmStateUnresolved is deliberately fail-closed. The C charm
	// list is descriptor-owned and is not represented in the Go snapshot.
	ErrBashCharmStateUnresolved = errors.New("bash charm relation unresolved")
	// ErrBashWarStateUnresolved prevents a family-vs-family decision when the
	// imported AT_WAR state is absent rather than silently treating it as peace.
	ErrBashWarStateUnresolved = errors.New("bash family-war state unresolved")
)

// Compatibility spelling for callers that use the shorter pending name.
var ErrBashDeathPending = ErrBashDeathTransitionPending

// BashTargetKind is closed so a receipt cannot name an unrecognised target
// category.
type BashTargetKind string

const (
	BashTargetNPC    BashTargetKind = "npc"
	BashTargetPlayer BashTargetKind = "player"
)

// BashEvent is a post-commit room projection. Actor response is carried by
// the receipt, so room text excludes the actor. Player targets additionally
// receive TargetText and are excluded from the room fan-out.
type BashEvent struct {
	RoomID          int16          `json:"room_id"`
	ExcludeActorID  string         `json:"exclude_actor_id"`
	ExcludeTargetID string         `json:"exclude_target_id,omitempty"`
	ActorID         string         `json:"actor_id"`
	ActorName       string         `json:"actor_name"`
	TargetID        string         `json:"target_id"`
	TargetKind      BashTargetKind `json:"target_kind"`
	TargetName      string         `json:"target_name"`
	Texts           []string       `json:"texts"`
	TargetText      string         `json:"target_text,omitempty"`
}

// BashResult is the durable, replay-safe projection of one command. All
// random outcomes needed to explain the transition are retained; a replay
// returns this value from storage and never calls the random source again.
type BashResult struct {
	Action           string         `json:"action"`
	Response         string         `json:"response"`
	Changed          bool           `json:"changed"`
	Broadcast        bool           `json:"broadcast"`
	NoOp             bool           `json:"no_op,omitempty"`
	Rejected         bool           `json:"rejected,omitempty"`
	Status           string         `json:"status,omitempty"`
	Cooldown         bool           `json:"cooldown,omitempty"`
	WaitSeconds      int32          `json:"wait_seconds,omitempty"`
	Interval         int32          `json:"interval,omitempty"`
	ActorID          string         `json:"actor_id"`
	TargetID         string         `json:"target_id,omitempty"`
	TargetKind       BashTargetKind `json:"target_kind,omitempty"`
	TargetName       string         `json:"target_name,omitempty"`
	WeaponID         string         `json:"weapon_id,omitempty"`
	WeaponName       string         `json:"weapon_name,omitempty"`
	Chance           int            `json:"chance,omitempty"`
	Roll             int            `json:"roll,omitempty"` // chance roll alias
	ChanceRoll       int            `json:"chance_roll,omitempty"`
	ChanceSucceeded  bool           `json:"chance_succeeded,omitempty"`
	HitRoll          int            `json:"hit_roll,omitempty"`
	Attempted        bool           `json:"attempted,omitempty"`
	Hit              bool           `json:"hit,omitempty"`
	Succeeded        bool           `json:"succeeded,omitempty"`
	BefuddleRoll     int            `json:"befuddle_roll,omitempty"`
	BefuddleInterval int32          `json:"befuddle_interval,omitempty"`
	DamageRolls      []int          `json:"damage_rolls,omitempty"`
	BonusRoll        int            `json:"bonus_roll,omitempty"`
	RawDamage        int            `json:"raw_damage,omitempty"`
	Damage           int            `json:"damage,omitempty"`
	EnemyDamage      int            `json:"enemy_damage,omitempty"`
	TargetHPBefore   int            `json:"target_hp_before,omitempty"`
	TargetHP         int            `json:"target_hp,omitempty"`
	WeaponBroken     bool           `json:"weapon_broken,omitempty"`
	EnemyAdded       bool           `json:"enemy_added,omitempty"`
	ProficiencyAward int32          `json:"proficiency_award,omitempty"`
	Event            *BashEvent     `json:"event,omitempty"`
}

// BashProposal is a state-bound candidate. before is private so a terminal
// client cannot submit an arbitrary state transition; ApplyBash verifies it
// against the exact snapshot that was planned.
type BashProposal struct {
	ActorID          string
	RoomID           int16
	TargetID         string
	TargetKind       BashTargetKind
	TargetName       string
	WeaponID         string
	WeaponName       string
	Now              int32
	Interval         int32
	Chance           int
	Roll             int
	ChanceRoll       int
	ChanceSucceeded  bool
	HitRoll          int
	Attempted        bool
	Hit              bool
	Succeeded        bool
	BefuddleRoll     int
	BefuddleInterval int32
	DamageRolls      []int
	BonusRoll        int
	RawDamage        int
	Damage           int
	EnemyDamage      int
	TargetHPBefore   int
	TargetHP         int
	ProficiencyAward int32
	WaitSeconds      int32
	Cooldown         bool
	NoOp             bool
	Rejected         bool
	Status           string
	ClearHidden      bool
	ClearInvisible   bool
	TimerWrite       bool
	WeaponBroken     bool
	EnemyAdded       bool
	Broadcast        bool
	Response         string
	ExpectedEvent    *BashEvent

	before State
}

type bashResolution struct {
	Status     string
	TargetID   string
	TargetKind BashTargetKind
	Target     LegacyMonster
}

const (
	bashStatusReady           = "ready"
	bashStatusNoTarget        = "no-target"
	bashStatusAlreadyFighting = "already-fighting"
	bashStatusSafeRoom        = "safe-room"
	bashStatusActorLawful     = "actor-lawful"
	bashStatusTargetLawful    = "target-lawful"
	bashStatusNoHarm          = "no-harm"
	bashStatusMagicOnly       = "magic-only"
	bashStatusEnchantOnly     = "enchant-only"
	bashStatusUnauthorized    = "unauthorized"
	bashStatusBlind           = "blind"
	bashStatusCooldown        = "cooldown"
)

func validBashName(name string) bool {
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

func bashActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if !ok || actorID == "" || !actor.Online || actor.Body.Type != 0 || !validBashName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online bash actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("bash actor room membership absent")
	}
	return actor, room, nil
}

func bashVisible(actor, target LegacyMonster) bool {
	if target.Class >= bashCaretakerClass && flag(target.Flags[:], bashNPCDMInvisibleFlag) {
		return false
	}
	return !flag(target.Flags[:], bashPlayerInvisibleFlag) || flag(actor.Flags[:], bashPlayerDetectFlag)
}

func (s State) selectBashTarget(actorID, targetName string) (bashResolution, error) {
	actor, room, err := bashActor(s, actorID)
	if err != nil {
		return bashResolution{}, err
	}
	if !validBashName(targetName) {
		return bashResolution{}, fmt.Errorf("invalid bash target name")
	}
	// Legacy room monsters have no stable identity in the Go snapshot. Never
	// fall back to them or silently treat a partially imported room as empty.
	if len(room.Resource.Monsters) != 0 {
		return bashResolution{}, fmt.Errorf("canonical bash NPC identities required")
	}
	if s.NPCs == nil && len(room.NPCIDs) != 0 {
		return bashResolution{}, fmt.Errorf("canonical bash NPC identities required")
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validBashName(npc.Body.Name) {
			return bashResolution{}, fmt.Errorf("unresolved canonical bash NPC identity")
		}
		if npc.Body.HPCurrent < 1 {
			return bashResolution{}, fmt.Errorf("bash NPC target is not alive")
		}
		if strings.EqualFold(npc.Body.Name, targetName) && bashVisible(actor.Body, npc.Body) {
			return bashResolution{TargetID: id, TargetKind: BashTargetNPC, Target: npc.Body}, nil
		}
	}
	// command8.c uses find_crt's player list only after the monster list. The
	// bounded adapter admits exact names, not legacy prefix/occurrence syntax.
	for _, id := range room.PlayerIDs {
		player, ok := s.Players[id]
		if id == "" || !ok || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID || !validBashName(player.Body.Name) {
			return bashResolution{}, fmt.Errorf("unresolved canonical bash player identity")
		}
		if player.Body.HPCurrent < 1 {
			return bashResolution{}, fmt.Errorf("bash player target is not alive")
		}
		// The source's strlen(cmnd->str[1]) < 2 guard applies only to the
		// player fallback, after player lookup.
		if id == actorID || len([]byte(targetName)) < 2 || !strings.EqualFold(player.Body.Name, targetName) || !bashVisible(actor.Body, player.Body) {
			continue
		}
		return bashResolution{TargetID: id, TargetKind: BashTargetPlayer, Target: player.Body}, nil
	}
	return bashResolution{Status: bashStatusNoTarget}, nil
}

func bashEnemyContains(npc NPCState, actorID string) (bool, error) {
	if npc.Enemies == nil {
		return false, fmt.Errorf("bash NPC enemy relations unresolved")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Damage < 0 {
			return false, fmt.Errorf("bash NPC enemy damage is negative")
		}
		if enemy.Target == want {
			return true, nil
		}
	}
	return false, nil
}

func bashPlayerPermission(s State, actor, target LegacyMonster, room RoomState) (string, error) {
	gateLawful := true
	if flag(actor.Flags[:], bashPlayerFamilyFlag) && flag(target.Flags[:], bashPlayerFamilyFlag) {
		if s.War == nil {
			return "", ErrBashWarStateUnresolved
		}
		// check_war returns true outside the active pair. AllowsDeathLoss is
		// the same predicate expressed from the canonical FamilyWar model.
		gateLawful = s.War.AllowsDeathLoss(actor.Daily[9].Max, target.Daily[9].Max)
	}
	if gateLawful && !flag(room.Resource.Flags[:], bashRoomSurvivalFlag) {
		if !flag(actor.Flags[:], bashPlayerChaosFlag) {
			return bashStatusActorLawful, nil
		}
		if !flag(target.Flags[:], bashPlayerChaosFlag) {
			return bashStatusTargetLawful, nil
		}
	}
	// is_charm_crt() reads a descriptor-local charm list that is absent from
	// State. A set PCHARM marker therefore cannot prove that this actor is not
	// charmed; fail closed instead of inventing a relation.
	if flag(target.Flags[:], bashPlayerCharmFlag) {
		return "", ErrBashCharmStateUnresolved
	}
	return bashStatusReady, nil
}

// resolveBash implements all source checks that happen before LT_ATTCK is
// read. It intentionally excludes the later MUNKIL/MMGONL/MENONL checks,
// because command8.c has already recorded the actor timer and visibility
// reveal before those checks return.
func (s State) resolveBash(actorID, targetName string) (bashResolution, error) {
	actor, room, err := bashActor(s, actorID)
	if err != nil {
		return bashResolution{}, err
	}
	res, err := s.selectBashTarget(actorID, targetName)
	if err != nil {
		return bashResolution{}, err
	}
	if res.Status == bashStatusNoTarget {
		return res, nil
	}
	res.Status = bashStatusReady
	if res.TargetKind == BashTargetNPC {
		npc := s.NPCs[res.TargetID]
		hostile, err := bashEnemyContains(npc, actorID)
		if err != nil {
			return bashResolution{}, err
		}
		if hostile {
			res.Status = bashStatusAlreadyFighting
		}
		return res, nil
	}
	if flag(room.Resource.Flags[:], bashRoomNoKillFlag) {
		res.Status = bashStatusSafeRoom
		return res, nil
	}
	status, err := bashPlayerPermission(s, actor.Body, res.Target, room)
	if err != nil {
		return bashResolution{}, err
	}
	res.Status = status
	return res, nil
}

func bashWeapon(actor PlayerState) (string, LegacyObject, bool, error) {
	if actor.Items == nil {
		if len(actor.Body.Inventory) != 0 {
			return "", LegacyObject{}, false, fmt.Errorf("canonical bash inventory required")
		}
		return "", LegacyObject{}, false, nil
	}
	if len(actor.Body.Inventory) != 0 {
		return "", LegacyObject{}, false, fmt.Errorf("duplicate legacy and canonical bash inventory")
	}
	if err := actor.Items.Validate(); err != nil {
		return "", LegacyObject{}, false, err
	}
	id := actor.Items.Ready[bashWieldSlot]
	if id == "" {
		return "", LegacyObject{}, false, nil
	}
	item, ok := actor.Items.Items[id]
	if !ok {
		return "", LegacyObject{}, false, fmt.Errorf("bash wielded item absent")
	}
	return id, item.Object, true, nil
}

func bashPronoun(target LegacyMonster) string {
	if flag(target.Flags[:], bashPlayerMaleFlag) {
		return "그"
	}
	return "그녀"
}

func bashResolutionResponse(status string, target LegacyMonster) string {
	switch status {
	case bashStatusNoTarget:
		return "그런 것은 여기 없습니다.\r\n"
	case bashStatusAlreadyFighting:
		return fmt.Sprintf("당신은 %s와 싸우는 중입니다.\r\n", bashPronoun(target))
	case bashStatusSafeRoom:
		return "이 방에서는 싸울 수 없습니다.\r\n"
	case bashStatusActorLawful:
		return "당신은 선해서 다른 사용자를 공격할 수 없습니다.\r\n"
	case bashStatusTargetLawful:
		return "그 사용자는 선해서 보호받고 있습니다.\r\n"
	case bashStatusNoHarm:
		return fmt.Sprintf("당신은 %s를 해칠 수 없습니다.\r\n", bashPronoun(target))
	case bashStatusMagicOnly:
		return fmt.Sprintf("당신의 무기가 %s에게 아무소용이 없는듯 합니다.\r\n", target.Name)
	case bashStatusEnchantOnly:
		return fmt.Sprintf("당신의 무기가 %s에게 아무 소용이 없는듯 합니다.\r\n", target.Name)
	case bashStatusUnauthorized:
		return "권법가와 검사만 쓸수 있는 기술입니다.\r\n"
	case bashStatusBlind:
		return "누굴 때립니까?\r\n"
	default:
		return ""
	}
}

func bashWaitResponse(seconds int64) (string, error) {
	if seconds < 1 || seconds > math.MaxInt32 {
		return "", fmt.Errorf("bash cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds), nil
}

func bashRevealResponse() string { return "\n당신의 모습이 서서히 드러납니다.\r\n" }

func bashRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 모습이 서서히 드러납니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func bashAttemptRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s %s에게 맹공을 가하려 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func bashAttemptTarget(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 당신에게 맹공을 가하려 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func bashSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s %s에게 맹공을 가합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func bashSuccessTarget(actor LegacyMonster, damage int) string {
	return fmt.Sprintf("\n%s%s %d점의 맹공을 가했습니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), damage)
}

func bashEvent(actorID string, actor LegacyMonster, res bashResolution, texts []string, targetText string) *BashEvent {
	if len(texts) == 0 && targetText == "" {
		return nil
	}
	event := &BashEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: res.TargetID, TargetKind: res.TargetKind,
		TargetName: res.Target.Name, Texts: append([]string(nil), texts...), TargetText: targetText,
	}
	if res.TargetKind == BashTargetPlayer {
		event.ExcludeTargetID = res.TargetID
	}
	return event
}

func bashRandomIn(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil || low > high {
		return 0, fmt.Errorf("invalid bash random request %d..%d", low, high)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("bash random source panicked")
		}
	}()
	value = roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("bash random value outside %d..%d", low, high)
	}
	return value, nil
}

func bashChance(actor, target LegacyMonster) (int, error) {
	if actor.Stats[0] > 63 || actor.Stats[1] > 63 || target.Stats[1] > 63 {
		return 0, fmt.Errorf("bash stat outside legacy bonus table")
	}
	chance := 50 + (((int(actor.Level)+3)/4)-((int(target.Level)+3)/4))*10
	chance += legacyStatBonus[actor.Stats[0]] * 3
	chance += (legacyStatBonus[actor.Stats[1]] - legacyStatBonus[target.Stats[1]]) * 2
	if actor.Class == bashBarbarianClass {
		chance += 5
	}
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

func bashHitThreshold(actor, target LegacyMonster) int {
	return int(int8(actor.Thaco)) - int(int8(target.Armor))/8
}

func bashDice(body LegacyMonster, weapon *LegacyObject, roll func(int, int) int) (int, []int, error) {
	count, sides, plus := int(body.DiceCount), int(body.DiceSides), int(body.DicePlus)
	if weapon != nil {
		count, sides, plus = int(weapon.DiceCount), int(weapon.DiceSides), int(weapon.DicePlus)
	}
	if count < 1 || sides < 1 || count > 4096 {
		return 0, nil, fmt.Errorf("invalid bash damage dice")
	}
	total := int64(plus)
	rolls := make([]int, 0, count)
	for i := 0; i < count; i++ {
		n, err := bashRandomIn(roll, 1, sides)
		if err != nil {
			return 0, nil, err
		}
		rolls = append(rolls, n)
		total += int64(n)
		if total > math.MaxInt32 || total < math.MinInt32 {
			return 0, nil, fmt.Errorf("bash damage dice overflow")
		}
	}
	if total < 1 {
		return 0, nil, fmt.Errorf("bash damage is non-positive")
	}
	return int(total), rolls, nil
}

func bashScale(value int, numerator, denominator int64) (int, error) {
	if value < 0 || numerator < 0 || denominator < 1 {
		return 0, fmt.Errorf("invalid bash damage scale")
	}
	result := int64(value) * numerator / denominator
	if result < 1 || result > math.MaxInt32 {
		return 0, fmt.Errorf("bash damage scale outside range")
	}
	return int(result), nil
}

func bashPostStatus(res bashResolution, weaponOK bool, weapon LegacyObject) string {
	if res.TargetKind != BashTargetNPC {
		return bashStatusReady
	}
	if flag(res.Target.Flags[:], bashNPCNoHarmFlag) {
		return bashStatusNoHarm
	}
	if flag(res.Target.Flags[:], bashNPCNoMagicFlag) {
		return bashStatusMagicOnly
	}
	if flag(res.Target.Flags[:], bashNPCEnchantOnlyFlag) && (!weaponOK || int8(weapon.Adjustment) < 1) {
		return bashStatusEnchantOnly
	}
	return bashStatusReady
}

func bashWeaponLabel(id string, weapon LegacyObject, ok bool) (string, error) {
	if !ok {
		return "", nil
	}
	if id == "" {
		return "", fmt.Errorf("bash weapon identity absent")
	}
	return weapon.Name, nil
}

// PlanBash ports the source-confirmed command8.c:bash prefix and one
// non-lethal swing. Legacy target occurrence parsing, descriptor charm lists,
// and die()/check_for_flee continuations are intentionally outside this
// reducer. A lethal result returns ErrBashDeathTransitionPending without a
// partial candidate.
func (s State) PlanBash(actorID, targetName string, now int32, roll func(int, int) int) (BashProposal, error) {
	zero := BashProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 || !validBashName(targetName) {
		return zero, fmt.Errorf("invalid bash actor, target, or clock")
	}
	actor, room, err := bashActor(s, actorID)
	if err != nil {
		return zero, err
	}
	p := BashProposal{ActorID: actorID, RoomID: room.Resource.ID, TargetName: targetName, Now: now, before: s.clone()}
	if flag(actor.Body.Flags[:], bashPlayerBlindFlag) {
		p.NoOp, p.Rejected, p.Status, p.Response = true, true, bashStatusBlind, bashResolutionResponse(bashStatusBlind, LegacyMonster{})
		return p, nil
	}
	if actor.Body.Class > bashMaxLegacyClass {
		return zero, fmt.Errorf("bash actor class outside canonical legacy table")
	}
	if actor.Body.Class != bashFighterClass && actor.Body.Class != bashBarbarianClass && actor.Body.Class < bashInvincibleClass {
		p.NoOp, p.Rejected, p.Status, p.Response = true, true, bashStatusUnauthorized, bashResolutionResponse(bashStatusUnauthorized, LegacyMonster{})
		return p, nil
	}
	res, err := s.resolveBash(actorID, targetName)
	if err != nil {
		return zero, err
	}
	p.TargetID, p.TargetKind, p.TargetName, p.TargetHPBefore, p.TargetHP = res.TargetID, res.TargetKind, targetName, int(res.Target.HPCurrent), int(res.Target.HPCurrent)
	if res.TargetID != "" {
		// A receipt names the canonical display value that was resolved, while
		// no-target receipts retain the original query for exact replay checks.
		p.TargetName = res.Target.Name
	}
	if res.Status != bashStatusReady {
		p.NoOp, p.Rejected, p.Status = true, true, res.Status
		p.Response = bashResolutionResponse(res.Status, res.Target)
		return p, nil
	}

	timer := actor.Body.Timers[bashAttackTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("bash timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait, waitErr := bashWaitResponse(deadline - int64(now))
		if waitErr != nil {
			return zero, waitErr
		}
		p.NoOp, p.Rejected, p.Cooldown, p.Status, p.WaitSeconds, p.Response = true, true, true, bashStatusCooldown, int32(deadline-int64(now)), wait
		return p, nil
	}
	weaponID, weapon, weaponOK, err := bashWeapon(actor)
	if err != nil {
		return zero, err
	}
	p.WeaponID, p.WeaponName, err = weaponID, "", nil
	if p.WeaponName, err = bashWeaponLabel(weaponID, weapon, weaponOK); err != nil {
		return zero, err
	}

	p.Interval, p.ClearHidden, p.ClearInvisible, p.TimerWrite = 1, true, flag(actor.Body.Flags[:], bashPlayerInvisibleFlag), true
	roomTexts := make([]string, 0, 2)
	if p.ClearInvisible {
		p.Response += bashRevealResponse()
		roomTexts = append(roomTexts, bashRevealRoom(actor.Body))
	}
	postStatus := bashPostStatus(res, weaponOK, weapon)
	p.Status = postStatus
	if postStatus != bashStatusReady {
		p.Rejected = true
		p.Response += bashResolutionResponse(postStatus, res.Target)
		p.ExpectedEvent = bashEvent(actorID, actor.Body, res, roomTexts, "")
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	p.EnemyAdded = res.TargetKind == BashTargetNPC
	p.Chance, err = bashChance(actor.Body, res.Target)
	if err != nil {
		return zero, err
	}
	p.ChanceRoll, err = bashRandomIn(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	p.Roll, p.Attempted = p.ChanceRoll, true
	p.ChanceSucceeded = p.ChanceRoll <= p.Chance
	if !p.ChanceSucceeded {
		p.Response += "\n당신의 맹공이 실패했습니다.\r\n"
		texts := append(roomTexts, bashAttemptRoom(actor.Body, res.Target))
		targetText := ""
		if res.TargetKind == BashTargetPlayer {
			targetText = bashAttemptTarget(actor.Body)
		}
		p.ExpectedEvent = bashEvent(actorID, actor.Body, res, texts, targetText)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	if weaponOK && weapon.ShotsCurrent < 1 {
		p.WeaponBroken = true
		p.Response += fmt.Sprintf("\n%s 부서져 버렸습니다.\r\n", weapon.Name)
		p.ExpectedEvent = bashEvent(actorID, actor.Body, res, roomTexts, "")
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	p.HitRoll, err = bashRandomIn(roll, 1, 20)
	if err != nil {
		return zero, err
	}
	p.Hit = p.HitRoll >= bashHitThreshold(actor.Body, res.Target)
	if !p.Hit {
		p.Response += "\n당신의 맹공이 실패했습니다.\r\n"
		texts := append(roomTexts, bashAttemptRoom(actor.Body, res.Target))
		targetText := ""
		if res.TargetKind == BashTargetPlayer {
			targetText = bashAttemptTarget(actor.Body)
		}
		p.ExpectedEvent = bashEvent(actorID, actor.Body, res, texts, targetText)
		p.Broadcast = p.ExpectedEvent != nil
		return p, nil
	}
	p.Succeeded = true
	p.BefuddleRoll, err = bashRandomIn(roll, 5, 8)
	if err != nil {
		return zero, err
	}
	p.BefuddleInterval = int32(p.BefuddleRoll)
	baseDamage, damageRolls, err := bashDice(actor.Body, funcObject(weaponOK, weapon), roll)
	if err != nil {
		return zero, err
	}
	p.DamageRolls = damageRolls
	p.RawDamage = baseDamage
	if weaponOK {
		p.RawDamage, err = bashScale(p.RawDamage, 3, 2)
		if err != nil {
			return zero, err
		}
	} else {
		p.RawDamage, err = bashScale(p.RawDamage, 3, 1)
		if err != nil {
			return zero, err
		}
	}
	if actor.Body.Class == bashBarbarianClass {
		p.BonusRoll, err = bashRandomIn(roll, 1, 100)
		if err != nil {
			return zero, err
		}
		if p.BonusRoll > 50 {
			p.RawDamage, err = bashScale(p.RawDamage, 3, 2)
			if err != nil {
				return zero, err
			}
		}
	} else if actor.Body.Class == bashFighterClass {
		p.BonusRoll, err = bashRandomIn(roll, 1, 100)
		if err != nil {
			return zero, err
		}
		if p.BonusRoll > 50 {
			p.RawDamage, err = bashScale(p.RawDamage, 3, 1)
			if err != nil {
				return zero, err
			}
		}
	}
	if p.RawDamage < 1 {
		return zero, fmt.Errorf("bash raw damage is non-positive")
	}
	p.EnemyDamage = min(int(res.Target.HPCurrent), p.RawDamage)
	p.Damage = p.RawDamage
	if actor.Body.Class == bashFighterClass && int(res.Target.HPCurrent)/4 > p.Damage {
		p.Damage = int(res.Target.HPCurrent) / 4
	}
	if res.Target.Class > bashCaretakerClass {
		p.Damage = 1
	}
	if p.Damage < 1 || int(res.Target.HPCurrent)-p.Damage < 1 {
		return zero, fmt.Errorf("%w: target=%s", ErrBashDeathTransitionPending, res.TargetID)
	}
	if res.TargetKind == BashTargetNPC && weaponOK {
		if res.Target.Experience < 0 || res.Target.HPMax <= 0 {
			return zero, fmt.Errorf("bash proficiency context unresolved")
		}
		award := int64(p.EnemyDamage) * int64(res.Target.Experience) / int64(res.Target.HPMax)
		if award > int64(res.Target.Experience) {
			award = int64(res.Target.Experience)
		}
		index := int(weapon.Type)
		if index > 4 {
			index = 4
		}
		if award < 0 || award > math.MaxInt32 || int64(actor.Body.Proficiency[index])+award > math.MaxInt32 {
			return zero, fmt.Errorf("bash proficiency overflow")
		}
		p.ProficiencyAward = int32(award)
	}
	p.TargetHP = int(res.Target.HPCurrent) - p.Damage
	p.Response += fmt.Sprintf("\n당신은 %d점의 맹공을 가했습니다.\r\n", p.Damage)
	texts := append(roomTexts, bashSuccessRoom(actor.Body, res.Target))
	targetText := ""
	if res.TargetKind == BashTargetPlayer {
		targetText = bashSuccessTarget(actor.Body, p.Damage)
	}
	p.ExpectedEvent = bashEvent(actorID, actor.Body, res, texts, targetText)
	p.Broadcast = p.ExpectedEvent != nil
	return p, nil
}

func funcObject(ok bool, object LegacyObject) *LegacyObject {
	if !ok {
		return nil
	}
	copy := object
	return &copy
}

func bashResultFromProposal(p BashProposal) BashResult {
	return BashResult{
		Action: "bash", Response: p.Response, Changed: p.TimerWrite || p.ClearHidden || p.ClearInvisible || p.WeaponBroken || p.EnemyAdded || p.Damage != 0,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Rejected: p.Rejected, Status: p.Status, Cooldown: p.Cooldown, WaitSeconds: p.WaitSeconds,
		ActorID: p.ActorID, TargetID: p.TargetID, TargetKind: p.TargetKind, TargetName: p.TargetName, WeaponID: p.WeaponID, WeaponName: p.WeaponName,
		Interval: p.Interval, Chance: p.Chance, Roll: p.Roll, ChanceRoll: p.ChanceRoll, ChanceSucceeded: p.ChanceSucceeded, HitRoll: p.HitRoll,
		Attempted: p.Attempted, Hit: p.Hit, Succeeded: p.Succeeded, BefuddleRoll: p.BefuddleRoll, BefuddleInterval: p.BefuddleInterval,
		DamageRolls: append([]int(nil), p.DamageRolls...), BonusRoll: p.BonusRoll, RawDamage: p.RawDamage, Damage: p.Damage,
		EnemyDamage: p.EnemyDamage, TargetHPBefore: p.TargetHPBefore, TargetHP: p.TargetHP, WeaponBroken: p.WeaponBroken,
		EnemyAdded: p.EnemyAdded, ProficiencyAward: p.ProficiencyAward, Event: p.ExpectedEvent,
	}
}

func bashProposalRandomEmpty(p BashProposal) bool {
	return p.Chance == 0 && p.Roll == 0 && p.ChanceRoll == 0 && !p.ChanceSucceeded && p.HitRoll == 0 && !p.Attempted && !p.Hit && !p.Succeeded && p.BefuddleRoll == 0 && p.BefuddleInterval == 0 && len(p.DamageRolls) == 0 && p.BonusRoll == 0 && p.RawDamage == 0 && p.Damage == 0 && p.EnemyDamage == 0 && p.ProficiencyAward == 0 && !p.WeaponBroken && !p.EnemyAdded && p.ExpectedEvent == nil
}

func bashCompareResolution(p BashProposal, res bashResolution) error {
	if p.TargetID != res.TargetID || p.TargetKind != res.TargetKind {
		return fmt.Errorf("stale bash target identity")
	}
	if res.TargetID != "" && !strings.EqualFold(res.Target.Name, p.TargetName) {
		return fmt.Errorf("stale bash target name")
	}
	return nil
}

func bashExpectedReveal(actor LegacyMonster, clearInvisible bool) (string, []string) {
	if !clearInvisible {
		return "", nil
	}
	return bashRevealResponse(), []string{bashRevealRoom(actor)}
}

// ApplyBash validates the planned snapshot and commits exactly one
// non-lethal candidate. It never calls the random source; all draws are
// checked from the proposal fields.
func (s State) ApplyBash(p BashProposal) (State, BashResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, BashResult{}, err
	}
	if p.ActorID == "" || p.before.Version == 0 || p.Response == "" || !reflect.DeepEqual(s, p.before) {
		return State{}, BashResult{}, fmt.Errorf("stale or invalid bash proposal")
	}
	actor, room, err := bashActor(s, p.ActorID)
	if err != nil || room.Resource.ID != p.RoomID {
		return State{}, BashResult{}, fmt.Errorf("bash actor changed")
	}
	if actor.Body.Class > bashMaxLegacyClass {
		return State{}, BashResult{}, fmt.Errorf("bash actor class outside canonical legacy table")
	}
	result := bashResultFromProposal(p)
	if p.NoOp {
		if !p.Rejected || p.TimerWrite || p.ClearHidden || p.ClearInvisible || p.Interval != 0 || p.Attempted || p.Hit || p.Succeeded || (p.Cooldown && p.Status != bashStatusCooldown) || (!p.Cooldown && p.WaitSeconds != 0) || !bashProposalRandomEmpty(p) {
			return State{}, BashResult{}, fmt.Errorf("invalid bash no-op proposal")
		}
		if p.Status == bashStatusUnauthorized {
			if actor.Body.Class == bashFighterClass || actor.Body.Class == bashBarbarianClass || actor.Body.Class >= bashInvincibleClass || p.Response != bashResolutionResponse(bashStatusUnauthorized, LegacyMonster{}) {
				return State{}, BashResult{}, fmt.Errorf("bash authorization changed")
			}
			return s.clone(), result, nil
		}
		if p.Status == bashStatusBlind {
			if !flag(actor.Body.Flags[:], bashPlayerBlindFlag) || p.Response != bashResolutionResponse(bashStatusBlind, LegacyMonster{}) {
				return State{}, BashResult{}, fmt.Errorf("bash blindness changed")
			}
			return s.clone(), result, nil
		}
		res, resolveErr := s.resolveBash(p.ActorID, p.TargetName)
		if resolveErr != nil {
			return State{}, BashResult{}, resolveErr
		}
		if p.Status == bashStatusCooldown {
			if !p.Cooldown || res.Status != bashStatusReady || bashCompareResolution(p, res) != nil {
				return State{}, BashResult{}, fmt.Errorf("stale bash cooldown target")
			}
			timer := actor.Body.Timers[bashAttackTimerIndex]
			if timer.LastTime < 0 || timer.Interval < 0 || int64(p.Now) >= int64(timer.LastTime)+int64(timer.Interval) {
				return State{}, BashResult{}, fmt.Errorf("stale bash cooldown")
			}
			wait, waitErr := bashWaitResponse(int64(timer.LastTime) + int64(timer.Interval) - int64(p.Now))
			if waitErr != nil || p.WaitSeconds != int32(int64(timer.LastTime)+int64(timer.Interval)-int64(p.Now)) || p.Response != wait {
				return State{}, BashResult{}, fmt.Errorf("stale bash cooldown response")
			}
			return s.clone(), result, nil
		}
		if res.Status != p.Status || bashCompareResolution(p, res) != nil || p.Response != bashResolutionResponse(res.Status, res.Target) {
			return State{}, BashResult{}, fmt.Errorf("stale bash rejection")
		}
		return s.clone(), result, nil
	}
	if p.Cooldown || p.Status == "" || p.Status == bashStatusCooldown || !p.TimerWrite || !p.ClearHidden || p.Interval != 1 || p.Now < 0 || (p.Status == bashStatusReady && p.Rejected) || (p.Status != bashStatusReady && !p.Rejected) {
		return State{}, BashResult{}, fmt.Errorf("invalid bash state-changing proposal")
	}
	if actor.Body.Class != bashFighterClass && actor.Body.Class != bashBarbarianClass && actor.Body.Class < bashInvincibleClass {
		return State{}, BashResult{}, fmt.Errorf("bash authorization changed")
	}
	res, err := s.resolveBash(p.ActorID, p.TargetName)
	if err != nil || res.Status != bashStatusReady {
		return State{}, BashResult{}, fmt.Errorf("bash target authorization changed")
	}
	if err := bashCompareResolution(p, res); err != nil {
		return State{}, BashResult{}, err
	}
	if p.ClearInvisible != flag(actor.Body.Flags[:], bashPlayerInvisibleFlag) {
		return State{}, BashResult{}, fmt.Errorf("bash invisibility proposal changed")
	}
	weaponID, weapon, weaponOK, err := bashWeapon(actor)
	if err != nil || weaponID != p.WeaponID {
		return State{}, BashResult{}, fmt.Errorf("bash wielded weapon changed")
	}
	if weaponOK && weapon.Name != p.WeaponName {
		return State{}, BashResult{}, fmt.Errorf("bash wielded weapon changed")
	}
	if !weaponOK && p.WeaponName != "" {
		return State{}, BashResult{}, fmt.Errorf("bash weapon projection changed")
	}
	timer := actor.Body.Timers[bashAttackTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 || int64(p.Now) < int64(timer.LastTime)+int64(timer.Interval) {
		return State{}, BashResult{}, fmt.Errorf("bash cooldown changed")
	}
	postStatus := bashPostStatus(res, weaponOK, weapon)
	if postStatus != p.Status {
		return State{}, BashResult{}, fmt.Errorf("bash weapon authorization changed")
	}
	if int(res.Target.HPCurrent) != p.TargetHPBefore {
		return State{}, BashResult{}, fmt.Errorf("bash target HP changed")
	}

	next := s.clone()
	nextActor := next.Players[p.ActorID]
	setSettingFlag(&nextActor.Body, bashPlayerHiddenFlag, false)
	if p.ClearInvisible {
		setSettingFlag(&nextActor.Body, bashPlayerInvisibleFlag, false)
	}
	nextActor.Body.Timers[bashAttackTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.Interval}

	if p.Status != bashStatusReady {
		if p.Attempted || p.Hit || p.Succeeded || p.ChanceRoll != 0 || p.HitRoll != 0 || p.BefuddleRoll != 0 || len(p.DamageRolls) != 0 || p.BonusRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.EnemyAdded || p.WeaponBroken {
			return State{}, BashResult{}, fmt.Errorf("invalid bash rejected projection")
		}
		wantResponse, revealTexts := bashExpectedReveal(actor.Body, p.ClearInvisible)
		wantResponse += bashResolutionResponse(p.Status, res.Target)
		wantEvent := bashEvent(p.ActorID, actor.Body, res, revealTexts, "")
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, BashResult{}, fmt.Errorf("stale bash rejected projection")
		}
		next.Players[p.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BashResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}

	chance, err := bashChance(actor.Body, res.Target)
	if err != nil || chance != p.Chance || p.ChanceRoll < 1 || p.ChanceRoll > 100 || p.Roll != p.ChanceRoll || !p.Attempted || p.ChanceSucceeded != (p.ChanceRoll <= p.Chance) {
		return State{}, BashResult{}, fmt.Errorf("invalid bash chance outcome")
	}
	if !p.ChanceSucceeded {
		if p.Hit || p.Succeeded || p.HitRoll != 0 || p.BefuddleRoll != 0 || len(p.DamageRolls) != 0 || p.BonusRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.WeaponBroken {
			return State{}, BashResult{}, fmt.Errorf("invalid bash chance miss projection")
		}
		if p.EnemyAdded != (res.TargetKind == BashTargetNPC) {
			return State{}, BashResult{}, fmt.Errorf("invalid bash hostility projection")
		}
		if err := bashAddEnemy(&next, res.TargetID, p.ActorID, p.EnemyAdded); err != nil {
			return State{}, BashResult{}, err
		}
		wantResponse, revealTexts := bashExpectedReveal(actor.Body, p.ClearInvisible)
		wantResponse += "\n당신의 맹공이 실패했습니다.\r\n"
		texts := append(revealTexts, bashAttemptRoom(actor.Body, res.Target))
		targetText := ""
		if res.TargetKind == BashTargetPlayer {
			targetText = bashAttemptTarget(actor.Body)
		}
		wantEvent := bashEvent(p.ActorID, actor.Body, res, texts, targetText)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, BashResult{}, fmt.Errorf("stale bash chance miss projection")
		}
		next.Players[p.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BashResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}

	if p.WeaponBroken {
		if !weaponOK || weapon.ShotsCurrent >= 1 || p.Hit || p.Succeeded || p.HitRoll != 0 || p.BefuddleRoll != 0 || len(p.DamageRolls) != 0 || p.BonusRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 || p.Interval != 1 || p.EnemyAdded != (res.TargetKind == BashTargetNPC) {
			return State{}, BashResult{}, fmt.Errorf("invalid bash broken-weapon projection")
		}
		if err := bashAddEnemy(&next, res.TargetID, p.ActorID, p.EnemyAdded); err != nil {
			return State{}, BashResult{}, err
		}
		if err := moveReadyWeaponToInventory(&nextActor, p.WeaponID); err != nil {
			return State{}, BashResult{}, err
		}
		wantResponse, revealTexts := bashExpectedReveal(actor.Body, p.ClearInvisible)
		wantResponse += fmt.Sprintf("\n%s 부서져 버렸습니다.\r\n", weapon.Name)
		wantEvent := bashEvent(p.ActorID, actor.Body, res, revealTexts, "")
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, BashResult{}, fmt.Errorf("stale bash broken-weapon projection")
		}
		next.Players[p.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BashResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}

	if p.HitRoll < 1 || p.HitRoll > 20 || p.Hit != (p.HitRoll >= bashHitThreshold(actor.Body, res.Target)) || p.Succeeded != p.Hit {
		return State{}, BashResult{}, fmt.Errorf("invalid bash hit outcome")
	}
	if !p.Hit {
		if p.Succeeded || p.BefuddleRoll != 0 || len(p.DamageRolls) != 0 || p.BonusRoll != 0 || p.Damage != 0 || p.EnemyDamage != 0 {
			return State{}, BashResult{}, fmt.Errorf("invalid bash hit miss projection")
		}
		if p.EnemyAdded != (res.TargetKind == BashTargetNPC) {
			return State{}, BashResult{}, fmt.Errorf("invalid bash hostility projection")
		}
		if err := bashAddEnemy(&next, res.TargetID, p.ActorID, p.EnemyAdded); err != nil {
			return State{}, BashResult{}, err
		}
		wantResponse, revealTexts := bashExpectedReveal(actor.Body, p.ClearInvisible)
		wantResponse += "\n당신의 맹공이 실패했습니다.\r\n"
		texts := append(revealTexts, bashAttemptRoom(actor.Body, res.Target))
		targetText := ""
		if res.TargetKind == BashTargetPlayer {
			targetText = bashAttemptTarget(actor.Body)
		}
		wantEvent := bashEvent(p.ActorID, actor.Body, res, texts, targetText)
		if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
			return State{}, BashResult{}, fmt.Errorf("stale bash hit miss projection")
		}
		next.Players[p.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BashResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}

	if p.BefuddleRoll < 5 || p.BefuddleRoll > 8 || p.BefuddleInterval != int32(p.BefuddleRoll) || p.EnemyAdded != (res.TargetKind == BashTargetNPC) {
		return State{}, BashResult{}, fmt.Errorf("invalid bash befuddle projection")
	}
	base, expectedDamageRolls, err := bashDiceFromDraws(actor.Body, funcObject(weaponOK, weapon), p.DamageRolls)
	if err != nil || len(expectedDamageRolls) != len(p.DamageRolls) {
		return State{}, BashResult{}, fmt.Errorf("invalid bash damage rolls")
	}
	rawDamage, err := bashScaledDamage(base, weaponOK, actor.Body.Class, p.BonusRoll)
	if err != nil || rawDamage != p.RawDamage {
		return State{}, BashResult{}, fmt.Errorf("bash damage projection changed")
	}
	enemyDamage := min(int(res.Target.HPCurrent), rawDamage)
	damage := rawDamage
	if actor.Body.Class == bashFighterClass && int(res.Target.HPCurrent)/4 > damage {
		damage = int(res.Target.HPCurrent) / 4
	}
	if res.Target.Class > bashCaretakerClass {
		damage = 1
	}
	if damage < 1 || int(res.Target.HPCurrent)-damage < 1 {
		return State{}, BashResult{}, ErrBashDeathTransitionPending
	}
	if enemyDamage != p.EnemyDamage || damage != p.Damage || p.TargetHP != int(res.Target.HPCurrent)-damage {
		return State{}, BashResult{}, fmt.Errorf("bash target damage projection changed")
	}
	proficiency := int32(0)
	if res.TargetKind == BashTargetNPC && weaponOK {
		if res.Target.Experience < 0 || res.Target.HPMax <= 0 {
			return State{}, BashResult{}, fmt.Errorf("bash proficiency context unresolved")
		}
		award := int64(enemyDamage) * int64(res.Target.Experience) / int64(res.Target.HPMax)
		if award > int64(res.Target.Experience) || award < 0 || award > math.MaxInt32 {
			return State{}, BashResult{}, fmt.Errorf("bash proficiency overflow")
		}
		index := int(weapon.Type)
		if index > 4 {
			index = 4
		}
		if int64(actor.Body.Proficiency[index])+award > math.MaxInt32 {
			return State{}, BashResult{}, fmt.Errorf("bash proficiency overflow")
		}
		proficiency = int32(award)
	}
	if p.ProficiencyAward != proficiency {
		return State{}, BashResult{}, fmt.Errorf("bash proficiency projection changed")
	}
	if err := bashAddEnemy(&next, res.TargetID, p.ActorID, p.EnemyAdded); err != nil {
		return State{}, BashResult{}, err
	}
	if weaponOK {
		index := int(weapon.Type)
		if index > 4 {
			index = 4
		}
		nextActor.Body.Proficiency[index] += proficiency
	}
	if res.TargetKind == BashTargetNPC {
		npc := next.NPCs[p.TargetID]
		npc.Body.Timers[bashBefuddleTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.BefuddleInterval}
		setSettingFlag(&npc.Body, bashNPCBefuddledFlag, true)
		npc.Body.HPCurrent = int16(p.TargetHP)
		if err := bashAddEnemyDamage(&npc, p.ActorID, p.EnemyDamage); err != nil {
			return State{}, BashResult{}, err
		}
		next.NPCs[p.TargetID] = npc
	} else {
		target := next.Players[p.TargetID]
		target.Body.Timers[bashBefuddleTimerIndex] = LegacyTimer{LastTime: p.Now, Interval: p.BefuddleInterval}
		target.Body.HPCurrent = int16(p.TargetHP)
		next.Players[p.TargetID] = target
	}
	wantResponse, revealTexts := bashExpectedReveal(actor.Body, p.ClearInvisible)
	wantResponse += fmt.Sprintf("\n당신은 %d점의 맹공을 가했습니다.\r\n", p.Damage)
	texts := append(revealTexts, bashSuccessRoom(actor.Body, res.Target))
	targetText := ""
	if res.TargetKind == BashTargetPlayer {
		targetText = bashSuccessTarget(actor.Body, p.Damage)
	}
	wantEvent := bashEvent(p.ActorID, actor.Body, res, texts, targetText)
	if p.Response != wantResponse || !reflect.DeepEqual(p.ExpectedEvent, wantEvent) || p.Broadcast != (wantEvent != nil) {
		return State{}, BashResult{}, fmt.Errorf("stale bash success projection")
	}
	next.Players[p.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, BashResult{}, err
	}
	result.Changed = true
	return next, result, nil
}

func bashDiceFromDraws(body LegacyMonster, weapon *LegacyObject, draws []int) (int, []int, error) {
	count, sides, plus := int(body.DiceCount), int(body.DiceSides), int(body.DicePlus)
	if weapon != nil {
		count, sides, plus = int(weapon.DiceCount), int(weapon.DiceSides), int(weapon.DicePlus)
	}
	if count < 1 || sides < 1 || count > 4096 || len(draws) != count {
		return 0, nil, fmt.Errorf("invalid bash damage draw count")
	}
	total := int64(plus)
	for _, n := range draws {
		if n < 1 || n > sides {
			return 0, nil, fmt.Errorf("bash damage draw outside dice")
		}
		total += int64(n)
		if total > math.MaxInt32 || total < math.MinInt32 {
			return 0, nil, fmt.Errorf("bash damage dice overflow")
		}
	}
	if total < 1 {
		return 0, nil, fmt.Errorf("bash damage is non-positive")
	}
	return int(total), append([]int(nil), draws...), nil
}

func bashScaledDamage(base int, weapon bool, class byte, bonusRoll int) (int, error) {
	damage := base
	if weapon {
		var err error
		damage, err = bashScale(damage, 3, 2)
		if err != nil {
			return 0, err
		}
	} else {
		var err error
		damage, err = bashScale(damage, 3, 1)
		if err != nil {
			return 0, err
		}
	}
	if class == bashBarbarianClass {
		if bonusRoll < 1 || bonusRoll > 100 {
			return 0, fmt.Errorf("invalid bash barbarian bonus roll")
		}
		if bonusRoll > 50 {
			var err error
			damage, err = bashScale(damage, 3, 2)
			if err != nil {
				return 0, err
			}
		}
	} else if class == bashFighterClass {
		if bonusRoll < 1 || bonusRoll > 100 {
			return 0, fmt.Errorf("invalid bash fighter bonus roll")
		}
		if bonusRoll > 50 {
			var err error
			damage, err = bashScale(damage, 3, 1)
			if err != nil {
				return 0, err
			}
		}
	} else if bonusRoll != 0 {
		return 0, fmt.Errorf("unexpected bash bonus roll")
	}
	return damage, nil
}

func bashAddEnemy(s *State, targetID, actorID string, wanted bool) error {
	if !wanted {
		return nil
	}
	npc, ok := s.NPCs[targetID]
	if !ok || npc.Enemies == nil {
		return fmt.Errorf("bash NPC enemy relations unresolved")
	}
	for _, enemy := range npc.Enemies {
		if enemy.Target == (EntityRef{Kind: "player", ID: actorID}) {
			return fmt.Errorf("bash NPC became hostile before apply")
		}
	}
	npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: actorID}, Damage: 0})
	s.NPCs[targetID] = npc
	return nil
}

func bashAddEnemyDamage(npc *NPCState, actorID string, amount int) error {
	if npc == nil || amount < 0 {
		return fmt.Errorf("invalid bash enemy damage")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for i, enemy := range npc.Enemies {
		if enemy.Target == want {
			if int64(enemy.Damage)+int64(amount) > math.MaxInt32 {
				return fmt.Errorf("bash enemy damage overflow")
			}
			npc.Enemies[i].Damage += int32(amount)
			return nil
		}
	}
	return fmt.Errorf("bash enemy relation absent")
}

// BashByName is the direct pure convenience form used by unit callers.
func (s State) BashByName(actorID, targetName string, now int32, roll func(int, int) int) (State, BashResult, error) {
	proposal, err := s.PlanBash(actorID, targetName, now, roll)
	if err != nil {
		return State{}, BashResult{}, err
	}
	return s.ApplyBash(proposal)
}
