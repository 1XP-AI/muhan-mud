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

// The kick slice is a direct, state-only port of src/command8.c:kick.  The
// source uses the general attack timer, but keeps a separate 43rd timer slot
// and changes its interval from two seconds to three seconds only after a hit.
const (
	kickTimerIndex       = 43 // LT_KICK
	kickBarbarianClass   = 2  // BARBARIAN
	kickInvincibleClass  = 9  // INVINCIBLE
	kickCaretakerClass   = 10 // CARETAKER
	kickMaxLegacyClass   = 12 // DM
	kickRoomNoKillFlag   = 11 // RNOKIL
	kickRoomSurvivalFlag = 36 // RSUVIV
	kickPlayerHidden     = 1  // PHIDDN
	kickPlayerInvisible  = 2  // PINVIS/MINVIS
	kickPlayerDetect     = 21 // PDINVI
	kickPlayerChaos      = 28 // PCHAOS
	kickPlayerBlind      = 42 // PBLIND
	kickPlayerCharm      = 45 // PCHARM
	kickPlayerFamily     = 55 // PFAMIL
	kickPlayerMale       = 12 // PMALES
	kickNPCNoMagic       = 20 // MMGONL
	kickNPCEnchantOnly   = 22 // MENONL
	kickNPCNoHarm        = 24 // MUNKIL
	kickNPCDMInvisible   = 10 // PDMINV on caretaker+ entries
	kickWieldSlot        = 19 // WIELD-1
	kickFamilyDaily      = 9  // DL_EXPND
)

// Exported source constants are intentionally small and stable.  They allow
// session/transport adapters and differential tests to refer to legacy slots
// without copying numeric literals into a second contract.
const (
	KickTimerIndex          = kickTimerIndex
	KickBarbarianClass      = kickBarbarianClass
	KickInvincibleClass     = kickInvincibleClass
	KickCaretakerClass      = kickCaretakerClass
	KickMaxLegacyClass      = kickMaxLegacyClass
	KickRoomNoKillFlag      = kickRoomNoKillFlag
	KickRoomSurvivalFlag    = kickRoomSurvivalFlag
	KickPlayerHiddenFlag    = kickPlayerHidden
	KickPlayerInvisibleFlag = kickPlayerInvisible
	KickPlayerDetectFlag    = kickPlayerDetect
	KickPlayerChaosFlag     = kickPlayerChaos
	KickPlayerBlindFlag     = kickPlayerBlind
	KickPlayerCharmFlag     = kickPlayerCharm
	KickPlayerFamilyFlag    = kickPlayerFamily
	KickWieldSlot           = kickWieldSlot
)

var (
	// A lethal result would invoke die(), which also changes progression,
	// equipment ownership, room membership, family-war state, and follower
	// links.  Until one composite reducer owns all of those fields, exposing a
	// partially damaged target would be unsafe.
	ErrKickDeathTransitionPending = errors.New("lethal kick death transition pending")
	// PCHARM is only a marker.  C's is_charm_crt() reads a descriptor-owned
	// linked list that is not in State, so the Go reducer cannot safely infer
	// whether this actor is the charm owner.
	ErrKickCharmStateUnresolved = errors.New("kick charm relation unresolved")
	// Family-vs-family PVP needs the imported AT_WAR pair.  A nil War pointer
	// means that domain is not imported, not that the world is at peace.
	ErrKickWarStateUnresolved = errors.New("kick family-war state unresolved")
)

// KickTargetKind is closed so a receipt cannot be forged with an unknown
// target category.
type KickTargetKind string

const (
	KickTargetNPC    KickTargetKind = "npc"
	KickTargetPlayer KickTargetKind = "player"
)

// KickEvent contains only post-commit projections.  The actor receives
// Result.Response; room fan-out therefore excludes the actor and, for a
// player target, the private target connection too.
type KickEvent struct {
	RoomID          int16          `json:"room_id"`
	ExcludeActorID  string         `json:"exclude_actor_id"`
	ExcludeTargetID string         `json:"exclude_target_id,omitempty"`
	ActorID         string         `json:"actor_id"`
	ActorName       string         `json:"actor_name"`
	TargetID        string         `json:"target_id"`
	TargetKind      KickTargetKind `json:"target_kind"`
	TargetName      string         `json:"target_name"`
	Texts           []string       `json:"texts"`
	TargetText      string         `json:"target_text,omitempty"`
}

// KickResult is the replay-stable receipt projection.  Kick has no source
// proficiency update (the old function declares addprof but never writes it),
// so ProficiencyAward is explicitly retained as zero rather than inventing a
// reward in the Go runtime.
type KickResult struct {
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
	TargetKind       KickTargetKind `json:"target_kind,omitempty"`
	TargetName       string         `json:"target_name,omitempty"`
	WeaponID         string         `json:"weapon_id,omitempty"`
	WeaponName       string         `json:"weapon_name,omitempty"`
	WeaponAdjustment int8           `json:"weapon_adjustment,omitempty"`
	Chance           int            `json:"chance,omitempty"`
	ChanceRoll       int            `json:"chance_roll,omitempty"`
	HitRoll          int            `json:"hit_roll,omitempty"`
	Attempted        bool           `json:"attempted,omitempty"`
	Hit              bool           `json:"hit,omitempty"`
	Succeeded        bool           `json:"succeeded,omitempty"`
	DamageRolls      []int          `json:"damage_rolls,omitempty"`
	RawDamage        int            `json:"raw_damage,omitempty"`
	Damage           int            `json:"damage,omitempty"`
	EnemyDamage      int            `json:"enemy_damage,omitempty"`
	TargetHPBefore   int            `json:"target_hp_before,omitempty"`
	TargetHP         int            `json:"target_hp,omitempty"`
	ProficiencyAward int32          `json:"proficiency_award,omitempty"`
	EnemyAdded       bool           `json:"enemy_added,omitempty"`
	TimerWrite       bool           `json:"timer_write,omitempty"`
	Event            *KickEvent     `json:"event,omitempty"`
}

// KickProposal is bound to the exact snapshot used by PlanKick.  before is
// private so a client cannot provide an arbitrary candidate to ApplyKick.
type KickProposal struct {
	ActorID          string
	RoomID           int16
	TargetID         string
	TargetKind       KickTargetKind
	TargetName       string
	WeaponID         string
	WeaponName       string
	WeaponAdjustment int8
	Now              int32
	Interval         int32
	Chance           int
	ChanceRoll       int
	HitRoll          int
	Attempted        bool
	Hit              bool
	Succeeded        bool
	DamageRolls      []int
	RawDamage        int
	Damage           int
	EnemyDamage      int
	TargetHPBefore   int
	TargetHP         int
	ProficiencyAward int32
	EnemyAdded       bool
	TimerWrite       bool
	ClearHidden      bool
	ClearInvisible   bool
	Cooldown         bool
	WaitSeconds      int32
	NoOp             bool
	Rejected         bool
	Status           string
	Response         string
	Broadcast        bool
	ExpectedEvent    *KickEvent

	before State
}

type kickResolution struct {
	TargetID   string
	TargetKind KickTargetKind
	Target     LegacyMonster
}

const (
	kickStatusReady        = "ready"
	kickStatusNoTarget     = "no-target"
	kickStatusSafeRoom     = "safe-room"
	kickStatusActorLawful  = "actor-lawful"
	kickStatusTargetLawful = "target-lawful"
	kickStatusNoHarm       = "no-harm"
	kickStatusMagicOnly    = "magic-only"
	kickStatusEnchantOnly  = "enchant-only"
	kickStatusUnauthorized = "unauthorized"
	kickStatusBlind        = "blind"
	kickStatusCooldown     = "cooldown"
)

func validKickName(name string) bool {
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

func kickActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if !ok || actorID == "" || !actor.Online || actor.Body.Type != 0 || !validKickName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online kick actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("kick actor room membership absent")
	}
	return actor, room, nil
}

func kickVisible(actor, target LegacyMonster) bool {
	// find_crt skips caretaker+ PDMINV before matching, then hides MINVIS
	// unless the searching player has PDINVI.  PHIDDN is deliberately not in
	// this predicate because the C helper does not inspect it.
	if target.Class >= kickCaretakerClass && flag(target.Flags[:], kickNPCDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], kickPlayerInvisible) || flag(actor.Flags[:], kickPlayerDetect)
}

func (s State) selectKickTarget(actorID, targetName string) (kickResolution, error) {
	actor, room, err := kickActor(s, actorID)
	if err != nil {
		return kickResolution{}, err
	}
	if !validKickName(targetName) {
		return kickResolution{}, fmt.Errorf("invalid kick target name")
	}
	// A partially migrated room must never be mistaken for an empty room.
	if len(room.Resource.Monsters) != 0 {
		return kickResolution{}, fmt.Errorf("canonical kick NPC identities required")
	}
	if s.NPCs == nil && len(room.NPCIDs) != 0 {
		return kickResolution{}, fmt.Errorf("canonical kick NPC identities required")
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validKickName(npc.Body.Name) {
			return kickResolution{}, fmt.Errorf("unresolved canonical kick NPC identity")
		}
		if npc.Body.HPCurrent < 1 {
			return kickResolution{}, fmt.Errorf("kick NPC target is not alive")
		}
		if strings.EqualFold(npc.Body.Name, targetName) && kickVisible(actor.Body, npc.Body) {
			return kickResolution{TargetID: id, TargetKind: KickTargetNPC, Target: npc.Body}, nil
		}
	}
	// command8.c searches first_mon before first_ply.  The bounded adapter
	// accepts an exact display name, while retaining the source's first-match
	// ordering for duplicate canonical names.
	for _, id := range room.PlayerIDs {
		player, ok := s.Players[id]
		if id == "" || !ok || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID || !validKickName(player.Body.Name) {
			return kickResolution{}, fmt.Errorf("unresolved canonical kick player identity")
		}
		if player.Body.HPCurrent < 1 {
			return kickResolution{}, fmt.Errorf("kick player target is not alive")
		}
		if id != actorID && strings.EqualFold(player.Body.Name, targetName) && kickVisible(actor.Body, player.Body) {
			return kickResolution{TargetID: id, TargetKind: KickTargetPlayer, Target: player.Body}, nil
		}
	}
	return kickResolution{}, nil
}

func kickPlayerStatus(s State, actor, target LegacyMonster, room RoomState) (string, error) {
	if flag(room.Resource.Flags[:], kickRoomNoKillFlag) {
		return kickStatusSafeRoom, nil
	}
	// check_war() is consulted only in the both-family branch.  Its global
	// value is not represented by a nil *FamilyWar, so do not guess peace.
	gateLawful := true
	if flag(actor.Flags[:], kickPlayerFamily) && flag(target.Flags[:], kickPlayerFamily) {
		if s.War == nil {
			return "", ErrKickWarStateUnresolved
		}
		gateLawful = s.War.AllowsDeathLoss(actor.Daily[kickFamilyDaily].Max, target.Daily[kickFamilyDaily].Max)
	}
	if gateLawful && !flag(room.Resource.Flags[:], kickRoomSurvivalFlag) {
		if !flag(actor.Flags[:], kickPlayerChaos) {
			return kickStatusActorLawful, nil
		}
		if !flag(target.Flags[:], kickPlayerChaos) {
			return kickStatusTargetLawful, nil
		}
	}
	// is_charm_crt(actor.name,target) is descriptor-local.  A PCHARM bit is
	// therefore an unresolved relation, not proof that kicking is allowed.
	if flag(target.Flags[:], kickPlayerCharm) {
		return "", ErrKickCharmStateUnresolved
	}
	return kickStatusReady, nil
}

func kickStatusResponse(status string, target LegacyMonster) string {
	pronoun := "그녀"
	if flag(target.Flags[:], kickPlayerMale) {
		pronoun = "그"
	}
	switch status {
	case kickStatusNoTarget:
		return "그런 것은 여기 없습니다.\r\n"
	case kickStatusSafeRoom:
		return "이 방에서는 싸울 수 없습니다.\r\n"
	case kickStatusActorLawful:
		return "당신은 선해서 다른 사용자를 공격할 수 없습니다.\r\n"
	case kickStatusTargetLawful:
		return "그 사용자는 선해서 보호받고 있습니다.\r\n"
	case kickStatusNoHarm:
		return fmt.Sprintf("당신은 %s를 해칠 수 없습니다.\r\n", pronoun)
	case kickStatusMagicOnly:
		return fmt.Sprintf("당신의 공격이 %s에게 아무소용이 없는듯 합니다.\r\n", target.Name)
	case kickStatusEnchantOnly:
		return fmt.Sprintf("당신의 공격이 %s에게 아무 소용이 없는듯 합니다.\r\n", target.Name)
	case kickStatusUnauthorized:
		return "권법가만 쓸수 있는 기술입니다.\r\n"
	case kickStatusBlind:
		return "누굴 공격합니까?\r\n"
	default:
		return ""
	}
}

func kickWaitResponse(seconds int64) (string, error) {
	if seconds < 1 || seconds > math.MaxInt32 {
		return "", fmt.Errorf("kick cooldown outside response range")
	}
	if seconds == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", seconds), nil
}

func kickWeapon(actor PlayerState) (string, LegacyObject, bool, error) {
	if actor.Items == nil {
		if len(actor.Body.Inventory) != 0 {
			return "", LegacyObject{}, false, fmt.Errorf("canonical kick inventory required")
		}
		return "", LegacyObject{}, false, nil
	}
	if len(actor.Body.Inventory) != 0 {
		return "", LegacyObject{}, false, fmt.Errorf("duplicate legacy and canonical kick inventory")
	}
	if err := actor.Items.Validate(); err != nil {
		return "", LegacyObject{}, false, err
	}
	id := actor.Items.Ready[kickWieldSlot]
	if id == "" {
		return "", LegacyObject{}, false, nil
	}
	item, ok := actor.Items.Items[id]
	if !ok {
		return "", LegacyObject{}, false, fmt.Errorf("kick wielded item absent")
	}
	return id, item.Object, true, nil
}

func kickChance(actor, target LegacyMonster) (int, error) {
	if actor.Class > kickMaxLegacyClass || target.Class > kickMaxLegacyClass || actor.Stats[0] > 63 || actor.Stats[1] > 63 || target.Stats[1] > 63 {
		return 0, fmt.Errorf("kick stat or class outside legacy table")
	}
	chance := 50 + (((int(actor.Level)+3)/4)-((int(target.Level)+3)/4))*10
	chance += legacyStatBonus[actor.Stats[0]] * 3
	chance += (legacyStatBonus[actor.Stats[1]] - legacyStatBonus[target.Stats[1]]) * 2
	if actor.Class == kickBarbarianClass {
		chance += 5
	}
	if actor.Class > kickInvincibleClass {
		chance += 5
	}
	if chance > 85 {
		chance = 85
	}
	return chance, nil
}

func kickRandomIn(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil || low > high {
		return 0, fmt.Errorf("invalid kick random request %d..%d", low, high)
	}
	defer func() {
		if recover() != nil {
			value = 0
			err = fmt.Errorf("kick random source panicked")
		}
	}()
	value = roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("kick random value outside %d..%d", low, high)
	}
	return value, nil
}

func kickDamage(body LegacyMonster, draws []int) (int, error) {
	count, sides, plus := int(body.DiceCount), int(body.DiceSides), int(body.DicePlus)
	if count < 1 || sides < 1 || count > 4096 || len(draws) != count {
		return 0, fmt.Errorf("invalid kick damage dice")
	}
	total := int64(plus)
	for _, draw := range draws {
		if draw < 1 || draw > sides {
			return 0, fmt.Errorf("kick damage roll outside dice")
		}
		total += int64(draw)
		if total < math.MinInt32 || total > math.MaxInt32 {
			return 0, fmt.Errorf("kick damage dice overflow")
		}
	}
	if total < 1 || total > math.MaxInt32/8 {
		return 0, fmt.Errorf("kick damage is outside range")
	}
	return int(total) * 8, nil
}

func kickEvent(actorID string, actor LegacyMonster, target kickResolution, texts []string, targetText string) *KickEvent {
	if len(texts) == 0 && targetText == "" {
		return nil
	}
	event := &KickEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID, ActorID: actorID,
		ActorName: actor.Name, TargetID: target.TargetID, TargetKind: target.TargetKind,
		TargetName: target.Target.Name, Texts: append([]string(nil), texts...), TargetText: targetText,
	}
	if target.TargetKind == KickTargetPlayer {
		event.ExcludeTargetID = target.TargetID
	}
	return event
}

func kickRevealResponse() string { return "\n당신의 모습이 서서히 드러납니다.\r\n" }

func kickRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 모습이 서서히 드러납니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func kickAttemptRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s %s에게 발차기를 하려고 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func kickAttemptTarget(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 당신에게 발차기를 하려고 합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func kickSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s%s %s에게 발차기를 가합니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), target.Name)
}

func kickSuccessTarget(actor LegacyMonster, damage int) string {
	return fmt.Sprintf("%s%s 발차기로 %d점의 공격을 가했습니다.\r\n", actor.Name, legacySubjectParticle(actor.Name), damage)
}

func kickCompareResolution(proposal KickProposal, resolution kickResolution) error {
	if proposal.TargetID != resolution.TargetID || proposal.TargetKind != resolution.TargetKind {
		return fmt.Errorf("stale kick target identity")
	}
	if resolution.TargetID != "" && !strings.EqualFold(proposal.TargetName, resolution.Target.Name) {
		return fmt.Errorf("stale kick target name")
	}
	return nil
}

func kickEnemyContains(npc NPCState, actorID string) (bool, error) {
	if npc.Enemies == nil {
		return false, fmt.Errorf("kick NPC enemy relations unresolved")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Damage < 0 {
			return false, fmt.Errorf("kick NPC enemy damage is negative")
		}
		if enemy.Target == want {
			return true, nil
		}
	}
	return false, nil
}

func kickAddEnemy(s *State, targetID, actorID string) error {
	npc, ok := s.NPCs[targetID]
	if !ok || npc.Enemies == nil {
		return fmt.Errorf("kick NPC enemy relations unresolved")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Target == want {
			return nil
		}
	}
	npc.Enemies = append(npc.Enemies, NPCEnemy{Target: want})
	s.NPCs[targetID] = npc
	return nil
}

func kickAddEnemyDamage(npc *NPCState, actorID string, amount int) error {
	if npc == nil || amount < 0 {
		return fmt.Errorf("invalid kick enemy damage")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for i, enemy := range npc.Enemies {
		if enemy.Target == want {
			if int64(enemy.Damage)+int64(amount) > math.MaxInt32 {
				return fmt.Errorf("kick enemy damage overflow")
			}
			enemy.Damage += int32(amount)
			npc.Enemies[i] = enemy
			return nil
		}
	}
	return fmt.Errorf("kick enemy relation absent")
}

func kickResultFromProposal(p KickProposal) KickResult {
	return KickResult{
		Action: "kick", Response: p.Response,
		Changed:   p.TimerWrite || p.ClearHidden || p.ClearInvisible || p.Damage != 0 || p.EnemyAdded,
		Broadcast: p.Broadcast, NoOp: p.NoOp, Rejected: p.Rejected, Status: p.Status,
		Cooldown: p.Cooldown, WaitSeconds: p.WaitSeconds, Interval: p.Interval,
		ActorID: p.ActorID, TargetID: p.TargetID, TargetKind: p.TargetKind, TargetName: p.TargetName,
		WeaponID: p.WeaponID, WeaponName: p.WeaponName, WeaponAdjustment: p.WeaponAdjustment,
		Chance: p.Chance, ChanceRoll: p.ChanceRoll, HitRoll: p.HitRoll, Attempted: p.Attempted,
		Hit: p.Hit, Succeeded: p.Succeeded, DamageRolls: append([]int(nil), p.DamageRolls...),
		RawDamage: p.RawDamage, Damage: p.Damage, EnemyDamage: p.EnemyDamage,
		TargetHPBefore: p.TargetHPBefore, TargetHP: p.TargetHP, ProficiencyAward: p.ProficiencyAward,
		EnemyAdded: p.EnemyAdded, TimerWrite: p.TimerWrite, Event: p.ExpectedEvent,
	}
}

func kickProposalEmptyOutcome(p KickProposal) bool {
	return p.Chance == 0 && p.ChanceRoll == 0 && p.HitRoll == 0 && !p.Attempted && !p.Hit && !p.Succeeded && len(p.DamageRolls) == 0 && p.RawDamage == 0 && p.Damage == 0 && p.EnemyDamage == 0 && p.TargetHPBefore == 0 && p.TargetHP == 0 && p.ProficiencyAward == 0 && !p.EnemyAdded
}

func kickSetTimerAndReveal(p *KickProposal, actor LegacyMonster) {
	p.TimerWrite = true
	p.Interval = 2
	p.ClearHidden = true
	p.ClearInvisible = flag(actor.Flags[:], kickPlayerInvisible)
}

func kickProjection(p *KickProposal, actor LegacyMonster, resolution kickResolution, response string, roomTexts []string, targetText string) {
	p.Response = response
	p.ExpectedEvent = kickEvent(p.ActorID, actor, resolution, roomTexts, targetText)
	p.Broadcast = p.ExpectedEvent != nil
}

// PlanKick ports command8.c's non-lethal kick path.  A successful lethal
// damage result returns ErrKickDeathTransitionPending before a candidate is
// exposed, because die() must atomically compose death/progression/equipment,
// family-war, and follower cleanup.
func (s State) PlanKick(actorID, targetName string, now int32, roll func(int, int) int) (KickProposal, error) {
	zero := KickProposal{}
	if err := s.Validate(); err != nil {
		return zero, err
	}
	if actorID == "" || now < 0 {
		return zero, fmt.Errorf("invalid kick actor or clock")
	}
	actor, room, err := kickActor(s, actorID)
	if err != nil {
		return zero, err
	}
	proposal := KickProposal{ActorID: actorID, RoomID: room.Resource.ID, TargetName: targetName, Now: now, before: s.clone()}
	if actor.Body.Class > kickMaxLegacyClass {
		return zero, fmt.Errorf("kick actor class outside canonical table")
	}
	// command8.c checks argument/blind before class authorization.
	if !validKickName(targetName) || flag(actor.Body.Flags[:], kickPlayerBlind) {
		proposal.NoOp = true
		proposal.Rejected = true
		if flag(actor.Body.Flags[:], kickPlayerBlind) {
			proposal.Status = kickStatusBlind
		}
		proposal.Response = kickStatusResponse(kickStatusBlind, LegacyMonster{})
		return proposal, nil
	}
	if actor.Body.Class != kickBarbarianClass && actor.Body.Class < kickInvincibleClass {
		proposal.NoOp = true
		proposal.Rejected = true
		proposal.Status = kickStatusUnauthorized
		proposal.Response = kickStatusResponse(kickStatusUnauthorized, LegacyMonster{})
		return proposal, nil
	}
	resolution, err := s.selectKickTarget(actorID, targetName)
	if err != nil {
		return zero, err
	}
	if resolution.TargetID == "" {
		proposal.NoOp = true
		proposal.Rejected = true
		proposal.Status = kickStatusNoTarget
		proposal.Response = kickStatusResponse(kickStatusNoTarget, LegacyMonster{})
		return proposal, nil
	}
	proposal.TargetID, proposal.TargetKind, proposal.TargetName = resolution.TargetID, resolution.TargetKind, resolution.Target.Name
	if resolution.TargetKind == KickTargetPlayer {
		status, statusErr := kickPlayerStatus(s, actor.Body, resolution.Target, room)
		if statusErr != nil {
			return zero, statusErr
		}
		if status != kickStatusReady {
			proposal.NoOp, proposal.Rejected, proposal.Status = true, true, status
			proposal.Response = kickStatusResponse(status, resolution.Target)
			return proposal, nil
		}
	}
	weaponID, weapon, weaponOK, err := kickWeapon(actor)
	if err != nil {
		return zero, err
	}
	if weaponOK {
		proposal.WeaponID, proposal.WeaponName, proposal.WeaponAdjustment = weaponID, weapon.Name, int8(weapon.Adjustment)
	}
	timer := actor.Body.Timers[kickTimerIndex]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return zero, fmt.Errorf("kick timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait, waitErr := kickWaitResponse(deadline - int64(now))
		if waitErr != nil {
			return zero, waitErr
		}
		proposal.NoOp, proposal.Rejected, proposal.Cooldown = true, true, true
		proposal.Status, proposal.WaitSeconds, proposal.Response = kickStatusCooldown, int32(deadline-int64(now)), wait
		return proposal, nil
	}
	// Timer/reveal are intentionally committed before the source's monster
	// immunity checks. This preserves command8.c's observable side effects.
	proposal.Status = kickStatusReady
	kickSetTimerAndReveal(&proposal, actor.Body)
	response := ""
	roomTexts := make([]string, 0, 2)
	if proposal.ClearInvisible {
		response += kickRevealResponse()
		roomTexts = append(roomTexts, kickRevealRoom(actor.Body))
	}
	if resolution.TargetKind == KickTargetNPC {
		npc := s.NPCs[resolution.TargetID]
		switch {
		case flag(resolution.Target.Flags[:], kickNPCNoHarm):
			proposal.Rejected, proposal.Status = true, kickStatusNoHarm
			kickProjection(&proposal, actor.Body, resolution, response+kickStatusResponse(proposal.Status, resolution.Target), roomTexts, "")
			return proposal, nil
		case flag(resolution.Target.Flags[:], kickNPCNoMagic):
			proposal.Rejected, proposal.Status = true, kickStatusMagicOnly
			kickProjection(&proposal, actor.Body, resolution, response+kickStatusResponse(proposal.Status, resolution.Target), roomTexts, "")
			return proposal, nil
		case flag(resolution.Target.Flags[:], kickNPCEnchantOnly) && actor.Body.Class < kickCaretakerClass && (!weaponOK || int8(weapon.Adjustment) < 1):
			proposal.Rejected, proposal.Status = true, kickStatusEnchantOnly
			kickProjection(&proposal, actor.Body, resolution, response+kickStatusResponse(proposal.Status, resolution.Target), roomTexts, "")
			return proposal, nil
		}
		alreadyEnemy, err := kickEnemyContains(npc, actorID)
		if err != nil {
			return zero, err
		}
		proposal.EnemyAdded = !alreadyEnemy
	}
	chance, err := kickChance(actor.Body, resolution.Target)
	if err != nil {
		return zero, err
	}
	proposal.Chance = chance
	proposal.ChanceRoll, err = kickRandomIn(roll, 1, 100)
	if err != nil {
		return zero, err
	}
	proposal.Attempted = true
	proposal.Hit = proposal.ChanceRoll <= chance
	proposal.Succeeded = false
	if !proposal.Hit {
		response += "당신의 발차기가 실패했습니다.\r\n"
		roomTexts = append(roomTexts, kickAttemptRoom(actor.Body, resolution.Target))
		targetText := ""
		if resolution.TargetKind == KickTargetPlayer {
			targetText = kickAttemptTarget(actor.Body)
		}
		kickProjection(&proposal, actor.Body, resolution, response, roomTexts, targetText)
		return proposal, nil
	}
	proposal.HitRoll, err = kickRandomIn(roll, 1, 20)
	if err != nil {
		return zero, err
	}
	threshold := int(int8(actor.Body.Thaco)) - int(int8(resolution.Target.Armor))/8
	proposal.Succeeded = proposal.HitRoll >= threshold
	if !proposal.Succeeded {
		response += "당신의 발차기가 실패했습니다.\r\n"
		roomTexts = append(roomTexts, kickAttemptRoom(actor.Body, resolution.Target))
		targetText := ""
		if resolution.TargetKind == KickTargetPlayer {
			targetText = kickAttemptTarget(actor.Body)
		}
		kickProjection(&proposal, actor.Body, resolution, response, roomTexts, targetText)
		return proposal, nil
	}
	if actor.Body.DiceCount < 1 || actor.Body.DiceCount > 4096 || actor.Body.DiceSides < 1 {
		return zero, fmt.Errorf("invalid kick damage dice")
	}
	proposal.DamageRolls = make([]int, int(actor.Body.DiceCount))
	for i := range proposal.DamageRolls {
		proposal.DamageRolls[i], err = kickRandomIn(roll, 1, int(actor.Body.DiceSides))
		if err != nil {
			return zero, err
		}
	}
	proposal.RawDamage, err = kickDamage(actor.Body, proposal.DamageRolls)
	if err != nil {
		return zero, err
	}
	proposal.TargetHPBefore = int(resolution.Target.HPCurrent)
	proposal.EnemyDamage = proposal.RawDamage
	if proposal.EnemyDamage > proposal.TargetHPBefore {
		proposal.EnemyDamage = proposal.TargetHPBefore
	}
	proposal.Damage = proposal.RawDamage
	if resolution.Target.Class > kickCaretakerClass {
		proposal.Damage = 1
	}
	if proposal.Damage < 1 || proposal.TargetHPBefore-proposal.Damage < 1 {
		return zero, fmt.Errorf("%w: target=%s", ErrKickDeathTransitionPending, resolution.TargetID)
	}
	proposal.TargetHP = proposal.TargetHPBefore - proposal.Damage
	response += fmt.Sprintf("당신은 발차기로 %d점의 공격을 가했습니다.\r\n", proposal.Damage)
	roomTexts = append(roomTexts, kickSuccessRoom(actor.Body, resolution.Target))
	targetText := ""
	if resolution.TargetKind == KickTargetPlayer {
		targetText = kickSuccessTarget(actor.Body, proposal.Damage)
	}
	proposal.Interval = 3
	kickProjection(&proposal, actor.Body, resolution, response, roomTexts, targetText)
	return proposal, nil
}

// ApplyKick commits a non-lethal candidate and never calls RNG.  Every source
// derived field is checked again, so a concurrent state change cannot turn a
// receipt into an HP/enemy relation mutation for a different target.
func (s State) ApplyKick(proposal KickProposal) (State, KickResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, KickResult{}, err
	}
	if proposal.ActorID == "" || proposal.before.Version == 0 || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, KickResult{}, fmt.Errorf("stale or invalid kick proposal")
	}
	actor, room, err := kickActor(s, proposal.ActorID)
	if err != nil || room.Resource.ID != proposal.RoomID {
		return State{}, KickResult{}, fmt.Errorf("kick actor changed")
	}
	result := kickResultFromProposal(proposal)
	if proposal.NoOp {
		if !proposal.Rejected || proposal.TimerWrite || proposal.ClearHidden || proposal.ClearInvisible || proposal.Cooldown && proposal.Status != kickStatusCooldown || !proposal.Cooldown && proposal.WaitSeconds != 0 || !kickProposalEmptyOutcome(proposal) {
			return State{}, KickResult{}, fmt.Errorf("invalid kick no-op proposal")
		}
		if proposal.Status == kickStatusUnauthorized {
			if actor.Body.Class == kickBarbarianClass || actor.Body.Class >= kickInvincibleClass || proposal.Response != kickStatusResponse(kickStatusUnauthorized, LegacyMonster{}) {
				return State{}, KickResult{}, fmt.Errorf("kick authorization changed")
			}
			return s.clone(), result, nil
		}
		if proposal.Status == kickStatusBlind {
			if !flag(actor.Body.Flags[:], kickPlayerBlind) || proposal.Response != kickStatusResponse(kickStatusBlind, LegacyMonster{}) {
				return State{}, KickResult{}, fmt.Errorf("kick blindness changed")
			}
			return s.clone(), result, nil
		}
		resolution, resolveErr := s.selectKickTarget(proposal.ActorID, proposal.TargetName)
		if proposal.Status == kickStatusNoTarget {
			if resolveErr != nil || resolution.TargetID != "" || proposal.Response != kickStatusResponse(kickStatusNoTarget, LegacyMonster{}) {
				return State{}, KickResult{}, fmt.Errorf("stale kick no-target rejection")
			}
			return s.clone(), result, nil
		}
		if resolveErr != nil {
			return State{}, KickResult{}, resolveErr
		}
		if err := kickCompareResolution(proposal, resolution); err != nil {
			return State{}, KickResult{}, err
		}
		if proposal.Status == kickStatusCooldown {
			timer := actor.Body.Timers[kickTimerIndex]
			deadline := int64(timer.LastTime) + int64(timer.Interval)
			if timer.LastTime < 0 || timer.Interval < 0 || proposal.Now < 0 || int64(proposal.Now) >= deadline || int64(proposal.WaitSeconds) != deadline-int64(proposal.Now) {
				return State{}, KickResult{}, fmt.Errorf("stale kick cooldown")
			}
			response, waitErr := kickWaitResponse(int64(proposal.WaitSeconds))
			if waitErr != nil || response != proposal.Response {
				return State{}, KickResult{}, fmt.Errorf("stale kick cooldown response")
			}
			return s.clone(), result, nil
		}
		if proposal.Status == kickStatusSafeRoom || proposal.Status == kickStatusActorLawful || proposal.Status == kickStatusTargetLawful {
			if resolution.TargetKind != KickTargetPlayer {
				return State{}, KickResult{}, fmt.Errorf("kick PVP rejection target changed")
			}
			status, statusErr := kickPlayerStatus(s, actor.Body, resolution.Target, room)
			if statusErr != nil || status != proposal.Status || proposal.Response != kickStatusResponse(status, resolution.Target) {
				return State{}, KickResult{}, fmt.Errorf("kick PVP rejection changed")
			}
			return s.clone(), result, nil
		}
		return State{}, KickResult{}, fmt.Errorf("unknown kick no-op status")
	}
	if proposal.Cooldown || proposal.Status == kickStatusCooldown || !proposal.TimerWrite || !proposal.ClearHidden || proposal.Interval < 2 || proposal.Interval > 3 || proposal.Now < 0 || (proposal.Status == kickStatusReady && proposal.Rejected) || (proposal.Status != kickStatusReady && !proposal.Rejected) {
		return State{}, KickResult{}, fmt.Errorf("invalid kick state-changing proposal")
	}
	if actor.Body.Class != kickBarbarianClass && actor.Body.Class < kickInvincibleClass {
		return State{}, KickResult{}, fmt.Errorf("kick authorization changed")
	}
	resolution, err := s.selectKickTarget(proposal.ActorID, proposal.TargetName)
	if err != nil || resolution.TargetID == "" {
		return State{}, KickResult{}, fmt.Errorf("kick target changed")
	}
	if err := kickCompareResolution(proposal, resolution); err != nil {
		return State{}, KickResult{}, err
	}
	if resolution.TargetKind == KickTargetPlayer {
		status, statusErr := kickPlayerStatus(s, actor.Body, resolution.Target, room)
		if statusErr != nil || status != kickStatusReady {
			return State{}, KickResult{}, fmt.Errorf("kick PVP authorization changed")
		}
	} else {
		npc := s.NPCs[resolution.TargetID]
		if proposal.Status == kickStatusReady {
			if flag(resolution.Target.Flags[:], kickNPCNoHarm) || flag(resolution.Target.Flags[:], kickNPCNoMagic) {
				return State{}, KickResult{}, fmt.Errorf("kick NPC immunity changed")
			}
			alreadyEnemy, err := kickEnemyContains(npc, proposal.ActorID)
			if err != nil {
				return State{}, KickResult{}, err
			}
			if proposal.EnemyAdded == alreadyEnemy {
				return State{}, KickResult{}, fmt.Errorf("kick hostility projection changed")
			}
		}
	}
	weaponID, weapon, weaponOK, err := kickWeapon(actor)
	if err != nil || weaponID != proposal.WeaponID || (weaponOK && (weapon.Name != proposal.WeaponName || int8(weapon.Adjustment) != proposal.WeaponAdjustment)) || (!weaponOK && (proposal.WeaponName != "" || proposal.WeaponAdjustment != 0)) {
		return State{}, KickResult{}, fmt.Errorf("kick wielded weapon changed")
	}
	if proposal.Status == kickStatusReady && proposal.TargetKind == KickTargetNPC && flag(resolution.Target.Flags[:], kickNPCEnchantOnly) && actor.Body.Class < kickCaretakerClass && (!weaponOK || int8(weapon.Adjustment) < 1) {
		return State{}, KickResult{}, fmt.Errorf("kick enchant-only authorization changed")
	}
	timer := actor.Body.Timers[kickTimerIndex]
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if timer.LastTime < 0 || timer.Interval < 0 || int64(proposal.Now) < deadline {
		return State{}, KickResult{}, fmt.Errorf("kick cooldown changed")
	}
	if proposal.ClearInvisible != flag(actor.Body.Flags[:], kickPlayerInvisible) {
		return State{}, KickResult{}, fmt.Errorf("kick actor invisibility changed")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	setSettingFlag(&nextActor.Body, kickPlayerHidden, false)
	if proposal.ClearInvisible {
		setSettingFlag(&nextActor.Body, kickPlayerInvisible, false)
	}
	nextActor.Body.Timers[kickTimerIndex] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Interval}

	if proposal.Status != kickStatusReady {
		if !proposal.Rejected || proposal.Attempted || proposal.Hit || proposal.Succeeded || proposal.Chance != 0 || proposal.ChanceRoll != 0 || proposal.HitRoll != 0 || len(proposal.DamageRolls) != 0 || proposal.RawDamage != 0 || proposal.Damage != 0 || proposal.EnemyDamage != 0 || proposal.TargetHPBefore != 0 || proposal.TargetHP != 0 || proposal.EnemyAdded || proposal.ProficiencyAward != 0 || proposal.Interval != 2 {
			return State{}, KickResult{}, fmt.Errorf("invalid kick rejected projection")
		}
		if proposal.TargetKind == KickTargetNPC {
			wantStatus := ""
			switch {
			case flag(resolution.Target.Flags[:], kickNPCNoHarm):
				wantStatus = kickStatusNoHarm
			case flag(resolution.Target.Flags[:], kickNPCNoMagic):
				wantStatus = kickStatusMagicOnly
			case flag(resolution.Target.Flags[:], kickNPCEnchantOnly) && actor.Body.Class < kickCaretakerClass && (!weaponOK || int8(weapon.Adjustment) < 1):
				wantStatus = kickStatusEnchantOnly
			}
			if wantStatus == "" || proposal.Status != wantStatus {
				return State{}, KickResult{}, fmt.Errorf("kick NPC immunity projection changed")
			}
		}
		wantResponse := ""
		if proposal.ClearInvisible {
			wantResponse = kickRevealResponse()
		}
		wantResponse += kickStatusResponse(proposal.Status, resolution.Target)
		roomTexts := []string(nil)
		if proposal.ClearInvisible {
			roomTexts = []string{kickRevealRoom(actor.Body)}
		}
		wantEvent := kickEvent(proposal.ActorID, actor.Body, resolution, roomTexts, "")
		if proposal.Response != wantResponse || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) || proposal.Broadcast != (wantEvent != nil) {
			return State{}, KickResult{}, fmt.Errorf("stale kick rejected projection")
		}
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, KickResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}
	if proposal.ChanceRoll < 1 || proposal.ChanceRoll > 100 || proposal.HitRoll != 0 && (proposal.HitRoll < 1 || proposal.HitRoll > 20) || !proposal.Attempted {
		return State{}, KickResult{}, fmt.Errorf("invalid kick random outcome")
	}
	chance, err := kickChance(actor.Body, resolution.Target)
	if err != nil || chance != proposal.Chance || proposal.Hit != (proposal.ChanceRoll <= proposal.Chance) {
		return State{}, KickResult{}, fmt.Errorf("kick chance changed")
	}
	if !proposal.Hit {
		if proposal.HitRoll != 0 || proposal.Succeeded || len(proposal.DamageRolls) != 0 || proposal.RawDamage != 0 || proposal.Damage != 0 || proposal.EnemyDamage != 0 || proposal.TargetHPBefore != 0 || proposal.TargetHP != 0 {
			return State{}, KickResult{}, fmt.Errorf("invalid kick chance miss projection")
		}
		if resolution.TargetKind == KickTargetNPC {
			if err := kickAddEnemy(&next, proposal.TargetID, proposal.ActorID); err != nil {
				return State{}, KickResult{}, err
			}
		}
		response := ""
		roomTexts := make([]string, 0, 2)
		if proposal.ClearInvisible {
			response = kickRevealResponse()
			roomTexts = append(roomTexts, kickRevealRoom(actor.Body))
		}
		response += "당신의 발차기가 실패했습니다.\r\n"
		roomTexts = append(roomTexts, kickAttemptRoom(actor.Body, resolution.Target))
		targetText := ""
		if resolution.TargetKind == KickTargetPlayer {
			targetText = kickAttemptTarget(actor.Body)
		}
		wantEvent := kickEvent(proposal.ActorID, actor.Body, resolution, roomTexts, targetText)
		if proposal.Response != response || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) || proposal.Broadcast != (wantEvent != nil) {
			return State{}, KickResult{}, fmt.Errorf("stale kick chance miss projection")
		}
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, KickResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}
	if proposal.HitRoll < 1 || proposal.HitRoll > 20 || proposal.Succeeded != (proposal.HitRoll >= int(int8(actor.Body.Thaco))-int(int8(resolution.Target.Armor))/8) {
		return State{}, KickResult{}, fmt.Errorf("kick hit threshold changed")
	}
	if !proposal.Succeeded {
		if len(proposal.DamageRolls) != 0 || proposal.RawDamage != 0 || proposal.Damage != 0 || proposal.EnemyDamage != 0 || proposal.TargetHPBefore != 0 || proposal.TargetHP != 0 {
			return State{}, KickResult{}, fmt.Errorf("invalid kick hit miss projection")
		}
		if resolution.TargetKind == KickTargetNPC {
			if err := kickAddEnemy(&next, proposal.TargetID, proposal.ActorID); err != nil {
				return State{}, KickResult{}, err
			}
		}
		response := ""
		roomTexts := make([]string, 0, 2)
		if proposal.ClearInvisible {
			response = kickRevealResponse()
			roomTexts = append(roomTexts, kickRevealRoom(actor.Body))
		}
		response += "당신의 발차기가 실패했습니다.\r\n"
		roomTexts = append(roomTexts, kickAttemptRoom(actor.Body, resolution.Target))
		targetText := ""
		if resolution.TargetKind == KickTargetPlayer {
			targetText = kickAttemptTarget(actor.Body)
		}
		wantEvent := kickEvent(proposal.ActorID, actor.Body, resolution, roomTexts, targetText)
		if proposal.Response != response || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) || proposal.Broadcast != (wantEvent != nil) {
			return State{}, KickResult{}, fmt.Errorf("stale kick hit miss projection")
		}
		next.Players[proposal.ActorID] = nextActor
		if err := next.Validate(); err != nil {
			return State{}, KickResult{}, err
		}
		result.Changed = true
		return next, result, nil
	}
	if proposal.TargetHPBefore != int(resolution.Target.HPCurrent) {
		return State{}, KickResult{}, fmt.Errorf("kick target HP changed")
	}
	if len(proposal.DamageRolls) != int(actor.Body.DiceCount) {
		return State{}, KickResult{}, fmt.Errorf("kick damage roll count changed")
	}
	rawDamage, err := kickDamage(actor.Body, proposal.DamageRolls)
	if err != nil || rawDamage != proposal.RawDamage {
		return State{}, KickResult{}, fmt.Errorf("kick damage changed")
	}
	damage := rawDamage
	if resolution.Target.Class > kickCaretakerClass {
		damage = 1
	}
	if damage != proposal.Damage || proposal.TargetHPBefore < 1 || proposal.TargetHPBefore-proposal.Damage < 1 || proposal.TargetHP != proposal.TargetHPBefore-proposal.Damage {
		return State{}, KickResult{}, ErrKickDeathTransitionPending
	}
	enemyDamage := rawDamage
	if enemyDamage > proposal.TargetHPBefore {
		enemyDamage = proposal.TargetHPBefore
	}
	if proposal.EnemyDamage != enemyDamage || proposal.ProficiencyAward != 0 || proposal.Interval != 3 {
		return State{}, KickResult{}, fmt.Errorf("kick damage projection changed")
	}
	if resolution.TargetKind == KickTargetNPC {
		if err := kickAddEnemy(&next, proposal.TargetID, proposal.ActorID); err != nil {
			return State{}, KickResult{}, err
		}
		npc := next.NPCs[proposal.TargetID]
		npc.Body.HPCurrent = int16(proposal.TargetHP)
		if err := kickAddEnemyDamage(&npc, proposal.ActorID, proposal.EnemyDamage); err != nil {
			return State{}, KickResult{}, err
		}
		next.NPCs[proposal.TargetID] = npc
	} else {
		target := next.Players[proposal.TargetID]
		target.Body.HPCurrent = int16(proposal.TargetHP)
		next.Players[proposal.TargetID] = target
	}
	response := ""
	roomTexts := make([]string, 0, 2)
	if proposal.ClearInvisible {
		response = kickRevealResponse()
		roomTexts = append(roomTexts, kickRevealRoom(actor.Body))
	}
	response += fmt.Sprintf("당신은 발차기로 %d점의 공격을 가했습니다.\r\n", proposal.Damage)
	roomTexts = append(roomTexts, kickSuccessRoom(actor.Body, resolution.Target))
	targetText := ""
	if resolution.TargetKind == KickTargetPlayer {
		targetText = kickSuccessTarget(actor.Body, proposal.Damage)
	}
	wantEvent := kickEvent(proposal.ActorID, actor.Body, resolution, roomTexts, targetText)
	if proposal.Response != response || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) || proposal.Broadcast != (wantEvent != nil) {
		return State{}, KickResult{}, fmt.Errorf("stale kick success projection")
	}
	next.Players[proposal.ActorID] = nextActor
	if err := next.Validate(); err != nil {
		return State{}, KickResult{}, err
	}
	result.Changed = true
	return next, result, nil
}

// KickByName is the pure convenience spelling used by differential tests.
func (s State) KickByName(actorID, targetName string, now int32, roll func(int, int) int) (State, KickResult, error) {
	proposal, err := s.PlanKick(actorID, targetName, now, roll)
	if err != nil {
		return State{}, KickResult{}, err
	}
	return s.ApplyKick(proposal)
}
