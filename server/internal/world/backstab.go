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

// Backstab is the bounded, state-only port of src/command7.c:backstab.  The
// command intentionally has no filesystem, socket, or C-runtime dependency.
// Identity and item ownership are resolved from the supplied canonical
// snapshot; a lethal result is rejected until the existing death reducer can
// be composed by the command owner.
const (
	backstabTimerIndex       = 3 // LT_ATTCK
	backstabCooldownNormal   = 1
	backstabCooldownDex      = 2
	backstabAssassinClass    = 1
	backstabThiefClass       = 8
	backstabInvincibleClass  = 9
	backstabCaretakerClass   = 10
	backstabSubDMClass       = 11
	backstabDMClass          = 12
	backstabWieldSlot        = 19 // WIELD-1
	backstabSharpType        = 0  // SHARP
	backstabThrustType       = 1  // THRUST
	backstabPlayerBlind      = 42 // PBLIND
	backstabPlayerHidden     = 1  // PHIDDN
	backstabPlayerInvisible  = 2  // PINVIS
	backstabPlayerDetect     = 21 // PDINVI
	backstabPlayerChaos      = 28 // PCHAOS
	backstabPlayerFamily     = 55 // PFAMIL
	backstabPlayerCharm      = 45 // PCHARM
	backstabRoomNoKill       = 11 // RNOKIL
	backstabRoomSurvival     = 36 // RSUVIV
	backstabPlayerMale       = 12 // PMALES
	backstabDMInvisible      = 10 // PDMINV
	backstabNPCNoMagic       = 20 // MMGONL
	backstabNPCEnchantedOnly = 22 // MENONL
	backstabNPCNoHarm        = 24 // MUNKIL
)

const (
	BackstabTimerIndex     = backstabTimerIndex
	BackstabNormalInterval = backstabCooldownNormal
	BackstabDexInterval    = backstabCooldownDex
)

var (
	// ErrBackstabDeathTransitionPending means that no partially damaged
	// snapshot was produced.  The caller must compose PlanPlayerDeath or the
	// canonical NPC death reducer before admitting a lethal backstab.
	ErrBackstabDeathTransitionPending = errors.New("backstab lethal death transition pending")
	ErrBackstabCharmStateUnresolved   = errors.New("backstab charm relation unresolved")
)

type BackstabTargetKind string

const (
	BackstabTargetNPC    BackstabTargetKind = "npc"
	BackstabTargetPlayer BackstabTargetKind = "player"
)

// BackstabOptions carries only host-owned inputs.  The command line never
// supplies either clock or randomness; replay uses the stored receipt.
type BackstabOptions struct {
	Now  int32
	Roll func(int, int) int
}

// BackstabEvent contains ordered post-commit room/target projections.  The
// actor receives Result.Response, so room messages exclude ActorID. TargetText
// is private to a player target and is empty for NPC targets.
type BackstabEvent struct {
	RoomID         int16              `json:"room_id"`
	ExcludeActorID string             `json:"exclude_actor_id"`
	ActorID        string             `json:"actor_id"`
	ActorName      string             `json:"actor_name"`
	TargetID       string             `json:"target_id"`
	TargetKind     BackstabTargetKind `json:"target_kind"`
	TargetName     string             `json:"target_name"`
	Texts          []string           `json:"texts"`
	TargetText     string             `json:"target_text,omitempty"`
}

// BackstabResult is safe to persist in a command receipt. It contains every
// random draw and projection needed to replay without target lookup or reroll.
type BackstabResult struct {
	Action           string             `json:"action"`
	Response         string             `json:"response"`
	Changed          bool               `json:"changed"`
	Broadcast        bool               `json:"broadcast"`
	Cooldown         bool               `json:"cooldown,omitempty"`
	WaitSeconds      int32              `json:"wait_seconds,omitempty"`
	Attempted        bool               `json:"attempted,omitempty"`
	Hit              bool               `json:"hit,omitempty"`
	Succeeded        bool               `json:"succeeded,omitempty"`
	Chance           int                `json:"chance,omitempty"`
	Roll             int                `json:"roll,omitempty"`
	Damage           int                `json:"damage,omitempty"`
	TargetHP         int                `json:"target_hp,omitempty"`
	TargetID         string             `json:"target_id,omitempty"`
	TargetKind       BackstabTargetKind `json:"target_kind,omitempty"`
	TargetName       string             `json:"target_name,omitempty"`
	WeaponID         string             `json:"weapon_id,omitempty"`
	WeaponName       string             `json:"weapon_name,omitempty"`
	EnemyAdded       bool               `json:"enemy_added,omitempty"`
	WeaponBroken     bool               `json:"weapon_broken,omitempty"`
	ProficiencyAward int32              `json:"proficiency_award,omitempty"`
	Event            *BackstabEvent     `json:"event,omitempty"`
}

// BackstabProposal is a state-bound candidate. before and next are private so
// neither an untrusted client nor a JSON payload can provide a state update.
type BackstabProposal struct {
	ActorID           string
	RoomID            int16
	TargetID          string
	TargetKind        BackstabTargetKind
	TargetName        string
	WeaponID          string
	WeaponName        string
	Now               int32
	Chance            int
	Roll              int
	Damage            int
	RawDamage         int
	TargetHP          int
	Proficiency       int32
	ProficiencyDamage int
	Interval          int32
	WaitSeconds       int32
	Cooldown          bool
	NoOp              bool
	Attempted         bool
	Hit               bool
	Succeeded         bool
	ClearHidden       bool
	ClearInvisible    bool
	TimerWrite        bool
	EnemyAdded        bool
	WeaponBroken      bool
	Broadcast         bool
	Response          string
	ExpectedEvent     *BackstabEvent

	before State
	next   State
}

type backstabResolution struct {
	targetID   string
	targetKind BackstabTargetKind
	target     LegacyMonster
}

func validBackstabName(name string) bool {
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

func backstabWaitResponse(seconds int32) (string, error) {
	if seconds < 1 || int64(seconds) > math.MaxInt32 {
		return "", fmt.Errorf("backstab cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds), nil
}

func backstabVisible(actor, target LegacyMonster) bool {
	if target.Class >= backstabCaretakerClass && flag(target.Flags[:], backstabDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], backstabPlayerInvisible) || flag(actor.Flags[:], backstabPlayerDetect)
}

func backstabActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if !ok || actorID == "" || !actor.Online || actor.Body.Type != 0 || !validBackstabName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online backstab actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("backstab actor room membership absent")
	}
	return actor, room, nil
}

// selectBackstabTarget preserves the C monster-first, then player fallback
// order. The bounded parser admits exact display names; ordered room IDs are
// authoritative and avoid map-order nondeterminism.
func (s State) selectBackstabTarget(actorID, name string) (backstabResolution, error) {
	actor, room, err := backstabActor(s, actorID)
	if err != nil {
		return backstabResolution{}, err
	}
	if !validBackstabName(name) {
		return backstabResolution{}, fmt.Errorf("invalid backstab target name")
	}
	if s.NPCs == nil && len(room.NPCIDs) != 0 {
		return backstabResolution{}, fmt.Errorf("canonical backstab NPC identities required")
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validBackstabName(npc.Body.Name) {
			return backstabResolution{}, fmt.Errorf("unresolved canonical backstab NPC identity")
		}
		if strings.EqualFold(npc.Body.Name, name) && backstabVisible(actor.Body, npc.Body) {
			return backstabResolution{targetID: id, targetKind: BackstabTargetNPC, target: npc.Body}, nil
		}
	}
	// command7.c refuses the player fallback while the actor is blind. The
	// caller already checked this, but retaining the guard documents the order.
	if flag(actor.Body.Flags[:], backstabPlayerBlind) {
		return backstabResolution{}, nil
	}
	for _, id := range room.PlayerIDs {
		player, ok := s.Players[id]
		if id == "" || !ok || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID || !validBackstabName(player.Body.Name) {
			return backstabResolution{}, fmt.Errorf("unresolved canonical backstab player identity")
		}
		// C's strlen(cmnd->str[1]) < 2 guard applies only after player lookup.
		if id == actorID || len([]byte(name)) < 2 || !strings.EqualFold(player.Body.Name, name) || !backstabVisible(actor.Body, player.Body) {
			continue
		}
		return backstabResolution{targetID: id, targetKind: BackstabTargetPlayer, target: player.Body}, nil
	}
	return backstabResolution{}, nil
}

func backstabEnemyContains(npc NPCState, actorID string) (bool, error) {
	if npc.Enemies == nil {
		return false, fmt.Errorf("backstab NPC enemy relations unresolved")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Target == want {
			if enemy.Damage < 0 {
				return false, fmt.Errorf("backstab NPC enemy relation unresolved")
			}
			return true, nil
		}
	}
	return false, nil
}

func backstabWeapon(actor PlayerState) (string, LegacyObject, bool, error) {
	if actor.Items == nil {
		return "", LegacyObject{}, false, nil
	}
	if err := actor.Items.Validate(); err != nil {
		return "", LegacyObject{}, false, err
	}
	id := actor.Items.Ready[backstabWieldSlot]
	if id == "" {
		return "", LegacyObject{}, false, nil
	}
	item, ok := actor.Items.Items[id]
	if !ok {
		return "", LegacyObject{}, false, fmt.Errorf("backstab wielded item absent")
	}
	if item.Object.Type != backstabSharpType && item.Object.Type != backstabThrustType {
		return id, item.Object, false, nil
	}
	return id, item.Object, true, nil
}

func backstabResolutionStatus(s State, actorID string, res backstabResolution) (string, error) {
	if res.targetID == "" {
		return "missing", nil
	}
	actor := s.Players[actorID]
	room := s.Rooms[actor.Body.RoomID]
	if res.targetKind == BackstabTargetNPC {
		npc := s.NPCs[res.targetID]
		enemy, err := backstabEnemyContains(npc, actorID)
		if err != nil {
			return "", err
		}
		if enemy {
			return "already-fighting", nil
		}
		if flag(res.target.Flags[:], backstabNPCNoHarm) {
			return "no-harm", nil
		}
		return "ready", nil
	}
	if flag(room.Resource.Flags[:], backstabRoomNoKill) {
		return "safe-room", nil
	}
	// The legacy charm list lives in descriptor-only state and has no
	// canonical representation yet. Do not attack while its active marker is
	// present; this is a fail-closed boundary, not an invented relation.
	if flag(actor.Body.Flags[:], backstabPlayerCharm) {
		return "", ErrBackstabCharmStateUnresolved
	}
	if s.War == nil && flag(actor.Body.Flags[:], backstabPlayerFamily) && flag(res.target.Flags[:], backstabPlayerFamily) {
		return "", fmt.Errorf("backstab family-war state unresolved")
	}
	if !flag(actor.Body.Flags[:], backstabPlayerFamily) || !flag(res.target.Flags[:], backstabPlayerFamily) || (s.War != nil && s.War.AllowsDeathLoss(actor.Body.Daily[9].Max, res.target.Daily[9].Max)) {
		if !flag(actor.Body.Flags[:], backstabPlayerChaos) && !flag(room.Resource.Flags[:], backstabRoomSurvival) {
			return "actor-lawful", nil
		}
		if !flag(res.target.Flags[:], backstabPlayerChaos) && !flag(room.Resource.Flags[:], backstabRoomSurvival) {
			return "target-lawful", nil
		}
	}
	if flag(res.target.Flags[:], backstabPlayerCharm) && flag(actor.Body.Flags[:], backstabPlayerCharm) {
		return "", ErrBackstabCharmStateUnresolved
	}
	return "ready", nil
}

func backstabStatusResponse(status string, target LegacyMonster) string {
	pronoun := "그녀"
	if flag(target.Flags[:], backstabPlayerMale) {
		pronoun = "그"
	}
	switch status {
	case "already-fighting":
		return fmt.Sprintf("당신은 %s와 싸울 수 없습니다.\r\n", pronoun)
	case "no-harm":
		return fmt.Sprintf("당신은 %s를 해칠 수 없습니다.\r\n", pronoun)
	case "safe-room":
		return "이 곳에서는 싸울 수 없습니다.\r\n"
	case "actor-lawful":
		return "당신은 선해서 다른 사람을 공격할 수 없습니다.\r\n"
	case "target-lawful":
		return "그 사용자는 선해서 보호받고 있습니다.\r\n"
	default:
		return "그런건 여기 없어요.\r\n"
	}
}

func backstabPokeResponse(target LegacyMonster) string {
	return fmt.Sprintf("당신은 %s의 뒤로 몰래 기어가 옆구리를 쿡~ 찌릅니다.\r\n", target.Name)
}

func backstabPokeRoom(actor, target LegacyMonster, player bool) string {
	ending := "."
	if player {
		ending = "!"
	}
	return fmt.Sprintf("\n%s%s %s%s의 옆구리를 쿡~ 찌릅니다%s\r\n", actor.Name, legacySubjectParticle(actor.Name), target.Name, legacySubjectParticle(target.Name), ending)
}

func backstabRevealResponse() string { return "\n당신의 모습이 서서히 드러납니다.\r\n" }

func backstabRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 모습이 서서히 드러납니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func backstabMissResponse() string { return "\n허공을 쳤습니다.\r\n" }

func backstabMissRoom(actor LegacyMonster) string {
	pronoun := "그녀"
	if flag(actor.Flags[:], backstabPlayerMale) {
		pronoun = "그"
	}
	return fmt.Sprintf("\n%s가 기습을 시도했지만 상대방이 피했습니다.\r\n", pronoun)
}

func backstabRoll(roll func(int, int) int, low, high int) (int, error) {
	return randomIn(roll, low, high)
}

func backstabDamage(body LegacyMonster, weapon LegacyObject, roll func(int, int) int) (int, error) {
	damage, err := meleeDice(body, &weapon, roll)
	if err != nil {
		return 0, err
	}
	if damage < 1 {
		return 0, fmt.Errorf("backstab weapon damage is non-positive")
	}
	return damage, nil
}

func backstabEffectiveDamage(kind BackstabTargetKind, target LegacyMonster, raw int) (int, error) {
	if raw < 1 || raw > math.MaxInt32 {
		return 0, fmt.Errorf("backstab damage outside range")
	}
	// command7.c still records the raw m damage for NPC hostility and
	// proficiency, but caretaker+ NPCs lose exactly one HP per admitted hit.
	if kind == BackstabTargetNPC && target.Class > backstabCaretakerClass {
		return 1, nil
	}
	return raw, nil
}

func backstabAwardProficiency(actor *LegacyMonster, weapon LegacyObject, dealt int, target LegacyMonster) (int32, error) {
	if dealt < 0 || target.Experience < 0 || target.HPMax <= 0 {
		return 0, fmt.Errorf("invalid backstab proficiency context")
	}
	index := int(weapon.Type)
	if index > 4 {
		index = 4
	}
	award64 := int64(dealt) * int64(target.Experience) / int64(target.HPMax)
	if award64 > int64(target.Experience) {
		award64 = int64(target.Experience)
	}
	if award64 < 0 || award64 > math.MaxInt32 || int64(actor.Proficiency[index])+award64 > math.MaxInt32 {
		return 0, fmt.Errorf("backstab proficiency overflow")
	}
	actor.Proficiency[index] += int32(award64)
	return int32(award64), nil
}

func backstabEvent(actorID string, actor LegacyMonster, res backstabResolution, reveal bool, roomTexts []string, targetText string) *BackstabEvent {
	if !reveal && len(roomTexts) == 0 && targetText == "" {
		return nil
	}
	return &BackstabEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: res.targetID, TargetKind: res.targetKind,
		TargetName: res.target.Name, Texts: append([]string(nil), roomTexts...), TargetText: targetText,
	}
}

// PlanBackstab ports the non-lethal canonical portion of command7.c. It
// preserves source ordering: class/argument/weapon/cooldown gates precede
// target lookup; only an admitted attempt clears hidden/invisibility and
// writes LT_ATTCK. A lethal swing fails closed before any candidate state is
// returned because death has to be one atomic transaction.
func (s State) PlanBackstab(actorID, targetName string, now int32, roll func(int, int) int) (BackstabProposal, error) {
	zero := BackstabProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 {
		return zero, fmt.Errorf("invalid backstab actor or clock")
	}
	actor, room, err := backstabActor(s, actorID)
	if err != nil {
		return zero, err
	}
	proposal := BackstabProposal{ActorID: actorID, RoomID: room.Resource.ID, Now: now, before: s.clone()}
	if actor.Body.Class > backstabDMClass {
		return zero, fmt.Errorf("backstab actor class outside canonical table")
	}
	if actor.Body.Class != backstabAssassinClass && actor.Body.Class != backstabThiefClass && actor.Body.Class < backstabInvincibleClass {
		proposal.NoOp = true
		proposal.Response = "도둑과 자객만 사용할 수 있는 기술입니다.\r\n"
		return proposal, nil
	}
	if !validBackstabName(targetName) || flag(actor.Body.Flags[:], backstabPlayerBlind) {
		proposal.NoOp = true
		proposal.Response = "누구를 기습하시려구요?\r\n"
		return proposal, nil
	}
	weaponID, weapon, weaponOK, err := backstabWeapon(actor)
	if err != nil {
		return zero, err
	}
	if !weaponOK {
		proposal.NoOp = true
		proposal.Response = "기습을 하시려면 도나 검종류의 무기가 필요합니다.\r\n"
		return proposal, nil
	}
	proposal.WeaponID, proposal.WeaponName = weaponID, weapon.Name
	timer := actor.Body.Timers[backstabTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("backstab timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		if wait > math.MaxInt32 {
			return zero, fmt.Errorf("backstab cooldown overflow")
		}
		proposal.NoOp, proposal.Cooldown, proposal.WaitSeconds = true, true, int32(wait)
		proposal.Response, err = backstabWaitResponse(proposal.WaitSeconds)
		if err != nil {
			return zero, err
		}
		return proposal, nil
	}
	res, err := s.selectBackstabTarget(actorID, targetName)
	if err != nil {
		return zero, err
	}
	if res.targetID == "" {
		proposal.NoOp = true
		proposal.Response = "그런건 여기 없어요.\r\n"
		return proposal, nil
	}
	proposal.TargetID, proposal.TargetKind, proposal.TargetName = res.targetID, res.targetKind, res.target.Name
	status, err := backstabResolutionStatus(s, actorID, res)
	if err != nil {
		return zero, err
	}
	if status != "ready" {
		proposal.NoOp = true
		proposal.Response = backstabStatusResponse(status, res.target)
		return proposal, nil
	}
	if res.targetKind == BackstabTargetNPC && flag(res.target.Flags[:], backstabNPCNoMagic) {
		// The source adds hostility and emits the poke before discovering that
		// physical weapons are ineffective. It is still a valid state attempt.
		proposal.Response = backstabPokeResponse(res.target) + fmt.Sprintf("당신의 무기는 %s에게는 아무 소용이 없습니다.\r\n", res.target.Name)
	} else if res.targetKind == BackstabTargetNPC && flag(res.target.Flags[:], backstabNPCEnchantedOnly) && int8(weapon.Adjustment) < 1 {
		proposal.Response = backstabPokeResponse(res.target) + fmt.Sprintf("당신의 무기는 %s에게 아무 소용이 없습니다.\r\n", res.target.Name)
	} else {
		proposal.Response = backstabPokeResponse(res.target)
	}
	proposal.ClearHidden = true
	proposal.ClearInvisible = flag(actor.Body.Flags[:], backstabPlayerInvisible)
	proposal.TimerWrite = true
	proposal.Interval = backstabCooldownNormal
	if actor.Body.Stats[1] > 18 {
		proposal.Interval = backstabCooldownDex
	}
	roomTexts := make([]string, 0, 2)
	if proposal.ClearInvisible {
		roomTexts = append(roomTexts, backstabRevealRoom(actor.Body))
		proposal.Response = backstabRevealResponse() + proposal.Response
	}
	roomTexts = append(roomTexts, backstabPokeRoom(actor.Body, res.target, res.targetKind == BackstabTargetPlayer))
	if res.targetKind == BackstabTargetPlayer {
		proposal.ExpectedEvent = backstabEvent(actorID, actor.Body, res, false, roomTexts, fmt.Sprintf("\n%s%s 당신의 옆구리를 쿡~ 찌릅니다.\r\n", actor.Body.Name, legacySubjectParticle(actor.Body.Name)))
	} else {
		proposal.ExpectedEvent = backstabEvent(actorID, actor.Body, res, false, roomTexts, "")
	}
	proposal.Broadcast = proposal.ExpectedEvent != nil
	proposal.EnemyAdded = res.targetKind == BackstabTargetNPC
	if flag(res.target.Flags[:], backstabNPCNoMagic) || (res.targetKind == BackstabTargetNPC && flag(res.target.Flags[:], backstabNPCEnchantedOnly) && int8(weapon.Adjustment) < 1) {
		return proposal, nil
	}
	if weapon.ShotsCurrent < 1 {
		proposal.WeaponBroken = true
		proposal.Interval *= 2
		proposal.Response += fmt.Sprintf("\n%s 부서졌습니다.\r\n", weapon.Name)
		proposal.ExpectedEvent.Texts = append(proposal.ExpectedEvent.Texts, backstabMissRoom(actor.Body))
		return proposal, nil
	}
	// The source records timer/reveal before consuming the attack roll.
	threshold := int(int8(actor.Body.Thaco)) - int(int8(res.target.Armor))/8 + 2
	if !flag(actor.Body.Flags[:], backstabPlayerHidden) {
		threshold = 21
	}
	proposal.Chance = threshold
	proposal.Roll, err = backstabRoll(roll, 1, 20)
	if err != nil {
		return zero, err
	}
	proposal.Attempted = true
	proposal.Hit = proposal.Roll >= threshold
	if !proposal.Hit {
		proposal.Response += backstabMissResponse()
		proposal.Interval *= 3
		proposal.ExpectedEvent.Texts = append(proposal.ExpectedEvent.Texts, backstabMissRoom(actor.Body))
		return proposal, nil
	}
	rawDamage, err := backstabDamage(actor.Body, weapon, roll)
	if err != nil {
		return zero, err
	}
	if actor.Body.Class == backstabThiefClass {
		multiplier, multiplierErr := backstabRoll(roll, 20, 35)
		if multiplierErr != nil {
			return zero, multiplierErr
		}
		rawDamage *= multiplier / 10
	} else {
		rawDamage *= 5
	}
	damage, damageErr := backstabEffectiveDamage(res.targetKind, res.target, rawDamage)
	if damageErr != nil {
		return zero, damageErr
	}
	proficiencyDamage := min(int(res.target.HPCurrent), rawDamage)
	proposal.Damage = damage
	proposal.RawDamage = rawDamage
	proposal.ProficiencyDamage = proficiencyDamage
	proposal.Succeeded = true
	proposal.TargetHP = int(res.target.HPCurrent) - damage
	if proposal.TargetHP < 1 {
		return zero, fmt.Errorf("%w: target=%s", ErrBackstabDeathTransitionPending, res.targetID)
	}
	award, err := backstabAwardProficiency(&actor.Body, weapon, proficiencyDamage, res.target)
	if err != nil {
		return zero, err
	}
	proposal.Proficiency = award
	proposal.Response += fmt.Sprintf("\n당신은 %d 만큼의 피해를 주었습니다.\r\n", damage)
	if res.targetKind == BackstabTargetPlayer {
		proposal.ExpectedEvent.TargetText += fmt.Sprintf("\n당신은 %s에게서 %d 만큼의 피해를 입었습니다.\r\n", actor.Body.Name, damage)
	}
	return proposal, nil
}

// ApplyBackstab validates the exact planned snapshot and commits only the
// admitted non-lethal candidate. Any late failure returns a zero State.
func (s State) ApplyBackstab(proposal BackstabProposal) (State, BackstabResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, BackstabResult{}, err
	}
	if proposal.ActorID == "" || proposal.before.Version == 0 || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, BackstabResult{}, fmt.Errorf("stale or invalid backstab proposal")
	}
	actor, room, err := backstabActor(s, proposal.ActorID)
	if err != nil || room.Resource.ID != proposal.RoomID {
		return State{}, BackstabResult{}, fmt.Errorf("backstab actor changed")
	}
	result := BackstabResult{
		Action: "backstab", Response: proposal.Response, Changed: proposal.TimerWrite || proposal.ClearHidden || proposal.ClearInvisible || proposal.WeaponBroken,
		Broadcast: proposal.Broadcast, Cooldown: proposal.Cooldown, WaitSeconds: proposal.WaitSeconds,
		Attempted: proposal.Attempted, Hit: proposal.Hit, Succeeded: proposal.Succeeded,
		Chance: proposal.Chance, Roll: proposal.Roll, Damage: proposal.Damage, TargetHP: proposal.TargetHP,
		TargetID: proposal.TargetID, TargetKind: proposal.TargetKind, TargetName: proposal.TargetName,
		WeaponID: proposal.WeaponID, WeaponName: proposal.WeaponName, EnemyAdded: proposal.EnemyAdded,
		WeaponBroken: proposal.WeaponBroken, ProficiencyAward: proposal.Proficiency, Event: proposal.ExpectedEvent,
	}
	if proposal.NoOp {
		if proposal.Cooldown {
			if proposal.TargetID != "" || proposal.TimerWrite || proposal.ClearHidden || proposal.ClearInvisible || proposal.Attempted || proposal.Hit || proposal.Succeeded || proposal.Roll != 0 || proposal.Changed() {
				return State{}, BackstabResult{}, fmt.Errorf("invalid backstab cooldown proposal")
			}
			return s.clone(), result, nil
		}
		if proposal.TimerWrite || proposal.ClearHidden || proposal.ClearInvisible || proposal.Attempted || proposal.Hit || proposal.Succeeded || proposal.Roll != 0 || proposal.Damage != 0 || proposal.RawDamage != 0 || proposal.ProficiencyDamage != 0 || proposal.EnemyAdded || proposal.WeaponBroken || proposal.ExpectedEvent != nil {
			return State{}, BackstabResult{}, fmt.Errorf("invalid backstab no-op proposal")
		}
		return s.clone(), result, nil
	}
	if actor.Body.Class != backstabAssassinClass && actor.Body.Class != backstabThiefClass && actor.Body.Class < backstabInvincibleClass {
		return State{}, BackstabResult{}, fmt.Errorf("backstab authorization changed")
	}
	if proposal.Cooldown || !proposal.TimerWrite || !proposal.ClearHidden || proposal.Interval < 1 || proposal.Interval > 6 || proposal.Now < 0 {
		return State{}, BackstabResult{}, fmt.Errorf("invalid backstab authorization proposal")
	}
	timer := actor.Body.Timers[backstabTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 || int64(proposal.Now) < int64(timer.LastTime)+int64(timer.Interval) {
		return State{}, BackstabResult{}, fmt.Errorf("backstab cooldown changed")
	}
	res, err := s.selectBackstabTarget(proposal.ActorID, proposal.TargetName)
	if err != nil || res.targetID != proposal.TargetID || res.targetKind != proposal.TargetKind || res.target.Name != proposal.TargetName {
		return State{}, BackstabResult{}, fmt.Errorf("backstab target changed")
	}
	weaponID, weapon, weaponOK, err := backstabWeapon(actor)
	if err != nil || !weaponOK || weaponID != proposal.WeaponID || weapon.Name != proposal.WeaponName {
		return State{}, BackstabResult{}, fmt.Errorf("backstab weapon changed")
	}
	status, err := backstabResolutionStatus(s, proposal.ActorID, res)
	if err != nil || status != "ready" {
		return State{}, BackstabResult{}, fmt.Errorf("backstab target authorization changed")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	setSettingFlag(&nextActor.Body, backstabPlayerHidden, false)
	if proposal.ClearInvisible {
		if !flag(actor.Body.Flags[:], backstabPlayerInvisible) {
			return State{}, BackstabResult{}, fmt.Errorf("backstab invisibility proposal changed")
		}
		setSettingFlag(&nextActor.Body, backstabPlayerInvisible, false)
	}
	if proposal.Interval != backstabCooldownNormal && proposal.Interval != backstabCooldownDex && proposal.Interval != backstabCooldownNormal*3 && proposal.Interval != backstabCooldownDex*3 && proposal.Interval != backstabCooldownNormal*2 && proposal.Interval != backstabCooldownDex*2 {
		return State{}, BackstabResult{}, fmt.Errorf("backstab interval outside source range")
	}
	nextActor.Body.Timers[backstabTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Interval}
	if res.targetKind == BackstabTargetNPC {
		npc := next.NPCs[proposal.TargetID]
		enemy, enemyErr := backstabEnemyContains(npc, proposal.ActorID)
		if enemyErr != nil {
			return State{}, BackstabResult{}, enemyErr
		}
		if enemy {
			return State{}, BackstabResult{}, fmt.Errorf("backstab NPC became hostile")
		}
		if proposal.EnemyAdded {
			npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: proposal.ActorID}, Damage: 0})
		}
		next.NPCs[proposal.TargetID] = npc
	}
	if proposal.EnemyAdded != (res.targetKind == BackstabTargetNPC) {
		return State{}, BackstabResult{}, fmt.Errorf("invalid backstab hostility projection")
	}
	if proposal.EnemyAdded && (!proposal.Attempted && !proposal.WeaponBroken && !flag(res.target.Flags[:], backstabNPCNoMagic) && !flag(res.target.Flags[:], backstabNPCEnchantedOnly)) {
		return State{}, BackstabResult{}, fmt.Errorf("invalid backstab enemy proposal")
	}
	if flag(res.target.Flags[:], backstabNPCNoMagic) || (res.targetKind == BackstabTargetNPC && flag(res.target.Flags[:], backstabNPCEnchantedOnly) && int8(weapon.Adjustment) < 1) {
		if proposal.Attempted || proposal.Hit || proposal.Succeeded || proposal.Roll != 0 || proposal.Damage != 0 || proposal.RawDamage != 0 || proposal.ProficiencyDamage != 0 {
			return State{}, BackstabResult{}, fmt.Errorf("invalid magic-only backstab proposal")
		}
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BackstabResult{}, err
		}
		return next, result, nil
	}
	if proposal.WeaponBroken {
		if proposal.Attempted || proposal.Hit || proposal.Succeeded || proposal.Roll != 0 || proposal.Damage != 0 || proposal.RawDamage != 0 || proposal.ProficiencyDamage != 0 || proposal.Interval != (backstabCooldownNormal*2) && proposal.Interval != (backstabCooldownDex*2) {
			return State{}, BackstabResult{}, fmt.Errorf("invalid broken backstab proposal")
		}
		if err := moveReadyWeaponToInventory(&nextActor, proposal.WeaponID); err != nil {
			return State{}, BackstabResult{}, err
		}
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, BackstabResult{}, err
		}
		return next, result, nil
	}
	if proposal.Roll < 1 || proposal.Roll > 20 || proposal.Attempted != (proposal.Roll != 0) || proposal.Hit != (proposal.Roll >= proposal.Chance) {
		return State{}, BackstabResult{}, fmt.Errorf("invalid backstab random outcome")
	}
	if proposal.Hit != proposal.Succeeded {
		return State{}, BackstabResult{}, fmt.Errorf("invalid backstab success outcome")
	}
	targetBody := res.target
	if proposal.Hit {
		if proposal.RawDamage < 1 || proposal.ProficiencyDamage < 1 || proposal.ProficiencyDamage > int(targetBody.HPCurrent) || int(targetBody.HPCurrent)-proposal.Damage < 1 {
			return State{}, BackstabResult{}, ErrBackstabDeathTransitionPending
		}
		expectedDamage, damageErr := backstabEffectiveDamage(res.targetKind, targetBody, proposal.RawDamage)
		if damageErr != nil || expectedDamage != proposal.Damage {
			return State{}, BackstabResult{}, fmt.Errorf("backstab damage projection changed")
		}
		expectedDealt := min(int(targetBody.HPCurrent), proposal.RawDamage)
		if proposal.ProficiencyDamage != expectedDealt {
			return State{}, BackstabResult{}, fmt.Errorf("backstab proficiency damage projection changed")
		}
		targetBody.HPCurrent = int16(int(targetBody.HPCurrent) - proposal.Damage)
		dealt := proposal.ProficiencyDamage
		award, awardErr := backstabAwardProficiency(&nextActor.Body, weapon, dealt, res.target)
		if awardErr != nil || award != proposal.Proficiency {
			return State{}, BackstabResult{}, fmt.Errorf("backstab proficiency changed: %v", awardErr)
		}
		if res.targetKind == BackstabTargetNPC {
			npc := next.NPCs[proposal.TargetID]
			npc.Body.HPCurrent = targetBody.HPCurrent
			for i, enemy := range npc.Enemies {
				if enemy.Target == (EntityRef{Kind: "player", ID: proposal.ActorID}) {
					if int64(enemy.Damage)+int64(dealt) > math.MaxInt32 {
						return State{}, BackstabResult{}, fmt.Errorf("backstab enemy damage overflow")
					}
					enemy.Damage += int32(dealt)
					npc.Enemies[i] = enemy
					break
				}
			}
			next.NPCs[proposal.TargetID] = npc
		} else {
			nextTarget := next.Players[proposal.TargetID]
			nextTarget.Body.HPCurrent = targetBody.HPCurrent
			next.Players[proposal.TargetID] = nextTarget
		}
	} else if proposal.Interval != (backstabCooldownNormal*3) && proposal.Interval != (backstabCooldownDex*3) {
		return State{}, BackstabResult{}, fmt.Errorf("backstab miss interval changed")
	}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, BackstabResult{}, err
	}
	return next, result, nil
}

// Changed is an internal helper used only to validate cooldown proposals.
func (p BackstabProposal) Changed() bool {
	return p.TimerWrite || p.ClearHidden || p.ClearInvisible || p.Attempted || p.Hit || p.Succeeded || p.Roll != 0 || p.Damage != 0 || p.TargetID != "" || p.ExpectedEvent != nil
}

func (s State) BackstabByName(actorID, targetName string, now int32, roll func(int, int) int) (State, BackstabResult, error) {
	proposal, err := s.PlanBackstab(actorID, targetName, now, roll)
	if err != nil {
		return State{}, BackstabResult{}, err
	}
	return s.ApplyBackstab(proposal)
}
