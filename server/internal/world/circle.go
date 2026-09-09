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

// The circle command is implemented in src/command8.c (global command 50).
// These values are kept as source-backed legacy indices.  A circle attempt is
// an attack-cooldown operation, while a successful attempt writes the target's
// LT_BEFUD timer and (for an NPC) MBEFUD.
const (
	circleAttackTimer       = 3  // LT_ATTCK
	circleBefuddleTimer     = 40 // LT_BEFUD
	circleFighterClass      = 4  // FIGHTER
	circleBarbarianClass    = 2  // BARBARIAN
	circleInvincibleClass   = 9  // INVINCIBLE
	circleCaretakerClass    = 10 // CARETAKER
	circleMaxClass          = 12 // DM
	circlePlayerHidden      = 1  // PHIDDN
	circlePlayerInvisible   = 2  // PINVIS
	circlePlayerDMInvisible = 10 // PDMINV
	circlePlayerMale        = 12 // PMALES
	circlePlayerDetect      = 21 // PDINVI
	circlePlayerChaos       = 28 // PCHAOS
	circlePlayerBlind       = 42 // PBLIND
	circlePlayerCharm       = 45 // PCHARM
	circlePlayerFamily      = 55 // PFAMIL
	circleRoomNoKill        = 11 // RNOKIL
	circleRoomSurvival      = 36 // RSUVIV
	circleNPCUndead         = 14 // MUNDED
	circleNPCNoHarm         = 24 // MUNKIL
	circleNPCNoCircle       = 44 // MNOCIR
	circleNPCBefuddled      = 51 // MBEFUD
)

// Export the source slots and classes for command/expiry integration and
// contract tests.  The implementation uses the lower-case names above so a
// caller cannot accidentally replace the durable slots with a transport
// lockout.
const (
	CircleTimerIndex         = circleAttackTimer
	CircleBefuddleTimerIndex = circleBefuddleTimer
	CircleFighterClass       = circleFighterClass
	CircleBarbarianClass     = circleBarbarianClass
	CircleInvincibleClass    = circleInvincibleClass
	CircleMaxClass           = circleMaxClass
)

var (
	// ErrCircleDeathTransitionPending is returned for a dead actor/target.
	// Circle itself does not damage or resurrect a creature, so admitting a
	// dead identity would make the combat state ambiguous; the caller must use
	// the canonical death/recovery path first.
	ErrCircleDeathTransitionPending = errors.New("circle death transition pending")
	ErrCircleTargetDead             = ErrCircleDeathTransitionPending
	ErrCircleCharmStateUnresolved   = errors.New("circle charm relation unresolved")
	ErrCircleFamilyWarUnresolved    = errors.New("circle family-war state unresolved")
)

// CircleTargetKind identifies the canonical same-room target domain.
type CircleTargetKind string

const (
	CircleTargetNPC    CircleTargetKind = "npc"
	CircleTargetPlayer CircleTargetKind = "player"
	// Short aliases make the result convenient for callers that already use
	// the target-kind names from the other combat reducers.
	CircleNPC    CircleTargetKind = CircleTargetNPC
	CirclePlayer CircleTargetKind = CircleTargetPlayer
)

// CircleEvent contains the post-commit room projection.  The actor receives
// CircleResult.Response, so ExcludeActorID is explicit.  TargetText is the
// private target-player projection; NPC targets leave it empty.
type CircleEvent struct {
	RoomID         int16            `json:"room_id"`
	ExcludeActorID string           `json:"exclude_actor_id"`
	ActorID        string           `json:"actor_id"`
	ActorName      string           `json:"actor_name"`
	TargetID       string           `json:"target_id"`
	TargetKind     CircleTargetKind `json:"target_kind"`
	TargetName     string           `json:"target_name"`
	Texts          []string         `json:"texts"`
	TargetText     string           `json:"target_text,omitempty"`
	RevealText     string           `json:"reveal_text,omitempty"`
}

// CircleResult is the durable actor response and the complete deterministic
// projection needed to replay the command without selecting a new identity or
// consuming another random value.
type CircleResult struct {
	Action      string           `json:"action"`
	Response    string           `json:"response"`
	Changed     bool             `json:"changed"`
	Broadcast   bool             `json:"broadcast"`
	Cooldown    bool             `json:"cooldown,omitempty"`
	WaitSeconds int32            `json:"wait_seconds,omitempty"`
	Attempted   bool             `json:"attempted,omitempty"`
	Succeeded   bool             `json:"succeeded,omitempty"`
	Chance      int              `json:"chance,omitempty"`
	Roll        int              `json:"roll,omitempty"`
	Delay       int32            `json:"delay,omitempty"`
	Interval    int32            `json:"interval,omitempty"`
	TargetID    string           `json:"target_id,omitempty"`
	TargetKind  CircleTargetKind `json:"target_kind,omitempty"`
	TargetName  string           `json:"target_name,omitempty"`
	EnemyAdded  bool             `json:"enemy_added,omitempty"`
	Befuddled   bool             `json:"befuddled,omitempty"`
	NoHarm      bool             `json:"no_harm,omitempty"`
	Event       *CircleEvent     `json:"event,omitempty"`
}

// CircleProposal is a snapshot-bound candidate.  The private before snapshot
// prevents an untrusted command payload from manufacturing actor/target
// authority.  ApplyCircle rechecks all public fields and deterministic
// projections against that exact snapshot before cloning and committing.
type CircleProposal struct {
	ActorID    string
	RoomID     int16
	TargetID   string
	TargetKind CircleTargetKind
	TargetName string
	Now        int32

	Chance      int
	Roll        int
	Delay       int32
	Interval    int32
	WaitSeconds int32

	NoOp           bool
	Blocked        bool
	NoHarm         bool
	Cooldown       bool
	Attempted      bool
	Succeeded      bool
	ClearHidden    bool
	ClearInvisible bool
	TimerWrite     bool
	EnemyAdded     bool
	Befuddled      bool
	Broadcast      bool
	Changed        bool

	Response      string
	ExpectedEvent *CircleEvent

	before State
}

type circleResolution struct {
	targetID   string
	targetKind CircleTargetKind
	target     LegacyMonster
}

func validCircleName(name string) bool {
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

func circleActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || !validCircleName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online circle actor absent")
	}
	if actor.Body.HPCurrent < 1 {
		return PlayerState{}, RoomState{}, fmt.Errorf("%w: circle actor is dead", ErrCircleDeathTransitionPending)
	}
	if actor.Body.Class > circleMaxClass {
		return PlayerState{}, RoomState{}, fmt.Errorf("circle actor class outside canonical table")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("circle actor room membership absent")
	}
	return actor, room, nil
}

func circleVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt skips PDMINV caretaker/DM identities and requires
	// PDINVI to select an ordinary invisible identity.
	if target.Class >= circleCaretakerClass && flag(target.Flags[:], circlePlayerDMInvisible) {
		return false
	}
	return !flag(target.Flags[:], circlePlayerInvisible) || flag(actor.Flags[:], circlePlayerDetect)
}

// selectCircleTarget preserves circle's monster-first, then player fallback
// order.  The bounded command accepts exact display names only; prefix/key and
// occurrence parsing remain outside this slice. Room slices, rather than map
// iteration, are authoritative for duplicate-name selection.
func (s State) selectCircleTarget(actorID, name string) (circleResolution, error) {
	actor, room, err := circleActor(s, actorID)
	if err != nil {
		return circleResolution{}, err
	}
	if !validCircleName(name) {
		return circleResolution{}, nil
	}
	if s.NPCs == nil && len(room.NPCIDs) != 0 {
		return circleResolution{}, fmt.Errorf("canonical circle NPC identities required")
	}
	for _, id := range room.NPCIDs {
		npc, ok := s.NPCs[id]
		if id == "" || !ok || npc.Body.Type != 1 || npc.Body.RoomID != room.Resource.ID || !validCircleName(npc.Body.Name) {
			return circleResolution{}, fmt.Errorf("unresolved canonical circle NPC identity")
		}
		if npc.Body.Class > circleMaxClass {
			return circleResolution{}, fmt.Errorf("circle NPC class outside canonical table")
		}
		if strings.EqualFold(npc.Body.Name, name) && circleVisible(actor.Body, npc.Body) {
			return circleResolution{targetID: id, targetKind: CircleTargetNPC, target: npc.Body}, nil
		}
	}
	// The source checks strlen(cmnd->str[1]) < 2 only after player fallback
	// lookup. It is a byte boundary, not a rune boundary.
	if len([]byte(name)) < 2 {
		return circleResolution{}, nil
	}
	for _, id := range room.PlayerIDs {
		player, ok := s.Players[id]
		if id == "" || !ok || !player.Online || player.Body.Type != 0 || player.Body.RoomID != room.Resource.ID || !validCircleName(player.Body.Name) {
			return circleResolution{}, fmt.Errorf("unresolved canonical circle player identity")
		}
		if player.Body.Class > circleMaxClass {
			return circleResolution{}, fmt.Errorf("circle player class outside canonical table")
		}
		if id == actorID || !strings.EqualFold(player.Body.Name, name) || !circleVisible(actor.Body, player.Body) {
			continue
		}
		return circleResolution{targetID: id, targetKind: CircleTargetPlayer, target: player.Body}, nil
	}
	return circleResolution{}, nil
}

func circleNPCEnemy(s State, npcID, actorID string) (bool, error) {
	npc, ok := s.NPCs[npcID]
	if s.NPCs == nil || !ok || npc.Enemies == nil {
		return false, fmt.Errorf("circle NPC enemy relations unresolved")
	}
	want := EntityRef{Kind: "player", ID: actorID}
	for _, enemy := range npc.Enemies {
		if enemy.Target == want {
			if enemy.Damage < 0 {
				return false, fmt.Errorf("circle NPC enemy relation unresolved")
			}
			return true, nil
		}
	}
	return false, nil
}

// CircleChance ports the source formula exactly for the admitted class/stat
// table. Undefined legacy bonus indices and unknown creature classes are
// rejected instead of being clamped into a different combat result.
func CircleChance(actor, target LegacyMonster) (int, error) {
	if actor.Class > circleMaxClass || target.Class > circleMaxClass {
		return 0, fmt.Errorf("circle class outside canonical table")
	}
	if actor.Stats[1] > 63 || target.Stats[1] > 63 {
		return 0, fmt.Errorf("circle dexterity outside legacy bonus table")
	}
	actorBand := (int(actor.Level) + 3) / 4
	targetBand := (int(target.Level) + 3) / 4
	chance := 50 + (actorBand-targetBand)*10 + (legacyStatBonus[actor.Stats[1]]-legacyStatBonus[target.Stats[1]])*2
	if target.Type == 1 && flag(target.Flags[:], circleNPCUndead) {
		chance -= 5 + targetBand*2
	}
	if chance > 80 {
		chance = 80
	}
	if flag(target.Flags[:], circleNPCNoCircle) || flag(actor.Flags[:], circlePlayerBlind) {
		chance = 1
	}
	return chance, nil
}

// circleRandom validates injected randomness and turns a panicking host RNG
// into an ordinary planning error. Replays never call this function because
// the original roll is persisted in CircleResult.
func circleRandom(roll func(int, int) int, low, high int) (value int, err error) {
	if roll == nil || low > high {
		return 0, fmt.Errorf("invalid circle random request %d..%d", low, high)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			value = 0
			err = fmt.Errorf("circle random source panicked: %v", recovered)
		}
	}()
	value = roll(low, high)
	if value < low || value > high {
		return 0, fmt.Errorf("circle random result outside %d..%d", low, high)
	}
	return value, nil
}

func circleWaitResponse(wait int64) (string, error) {
	if wait < 1 || wait > math.MaxInt32 {
		return "", fmt.Errorf("circle cooldown outside response range")
	}
	if wait == 1 {
		return "1초만 기다리세요.\r\n", nil
	}
	return fmt.Sprintf("%d초동안 기다리세요.\r\n", wait), nil
}

func circleObjectParticle(name string) string {
	// io.c's under_han chooses 을/를 from the final Hangul syllable. Keep its
	// parenthesized suffix quirk for names already containing a display suffix.
	if len(name) > 511 {
		name = name[:511]
	}
	if strings.HasSuffix(name, ")") {
		if at := strings.LastIndexByte(name, '('); at >= 0 {
			name = name[:at]
		}
	}
	last, _ := utf8.DecodeLastRuneInString(name)
	if last >= 0xac00 && last <= 0xd7a3 && (last-0xac00)%28 != 0 {
		return "을"
	}
	return "를"
}

func circlePronoun(target LegacyMonster) string {
	if flag(target.Flags[:], circlePlayerMale) {
		return "그"
	}
	return "그녀"
}

func circleUnauthorizedResponse() string {
	return "권법가와 검사만 쓸수 있는 기술입니다.\r\n"
}

func circleNoArgumentResponse() string {
	return "누구를 교란시키려구요?\r\n"
}

func circleMissingResponse() string {
	return "그런것은 여기 없습니다.\r\n"
}

func circleNoHarmResponse(target LegacyMonster) string {
	return fmt.Sprintf("당신은 %s를 해칠수 없습니다.\r\n", circlePronoun(target))
}

func circleRevealResponse() string {
	return "당신의 모습이 서서히 드러납니다.\r\n"
}

func circleRevealRoom(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s%s 모습이 서서히 드러납니다.\r\n", actor.Name, legacySubjectParticle(actor.Name))
}

func circleSuccessResponse(target LegacyMonster) string {
	return fmt.Sprintf("당신은 이리저리 왔다갔다 하면서 %s%s 교란시킵니다.\r\n", target.Name, circleObjectParticle(target.Name))
}

func circleSuccessRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s이 %s 주위를 뱅글뱅글 돕니다.\r\n", actor.Name, target.Name)
}

func circleSuccessTarget(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s이 당신주위를 어지럽게 돌아다닙니다.\r\n", actor.Name)
}

func circleFailureResponse() string {
	return "당신은 적을 교란시키는데 실패하였습니다.\r\n"
}

func circleFailureRoom(actor, target LegacyMonster) string {
	return fmt.Sprintf("\n%s이 %s%s 교란시키려고 합니다.\r\n", actor.Name, target.Name, circleObjectParticle(target.Name))
}

func circleFailureTarget(actor LegacyMonster) string {
	return fmt.Sprintf("\n%s이 당신을 교란시키려고 합니다.\r\n", actor.Name)
}

func circleEvent(actorID string, actor LegacyMonster, target circleResolution, reveal bool, attempted, succeeded bool) *CircleEvent {
	texts := make([]string, 0, 2)
	var revealText string
	if reveal {
		revealText = circleRevealRoom(actor)
		texts = append(texts, revealText)
	}
	if attempted {
		if succeeded {
			texts = append(texts, circleSuccessRoom(actor, target.target))
		} else {
			texts = append(texts, circleFailureRoom(actor, target.target))
		}
	}
	if len(texts) == 0 {
		return nil
	}
	event := &CircleEvent{
		RoomID: actor.RoomID, ExcludeActorID: actorID,
		ActorID: actorID, ActorName: actor.Name,
		TargetID: target.targetID, TargetKind: target.targetKind,
		TargetName: target.target.Name, Texts: texts, RevealText: revealText,
	}
	if target.targetKind == CircleTargetPlayer && attempted {
		if succeeded {
			event.TargetText = circleSuccessTarget(actor)
		} else {
			event.TargetText = circleFailureTarget(actor)
		}
	}
	return event
}

func circleResult(p CircleProposal) CircleResult {
	return CircleResult{
		Action: "circle", Response: p.Response, Changed: p.Changed,
		Broadcast: p.Broadcast, Cooldown: p.Cooldown,
		WaitSeconds: p.WaitSeconds, Attempted: p.Attempted,
		Succeeded: p.Succeeded, Chance: p.Chance, Roll: p.Roll,
		Delay: p.Delay, Interval: p.Interval, TargetID: p.TargetID,
		TargetKind: p.TargetKind, TargetName: p.TargetName,
		EnemyAdded: p.EnemyAdded, Befuddled: p.Befuddled,
		NoHarm: p.NoHarm, Event: p.ExpectedEvent,
	}
}

func circlePlayerStatus(s State, actorID string, target circleResolution) (string, error) {
	if target.targetKind != CircleTargetPlayer {
		return "ready", nil
	}
	actor, _, err := circleActor(s, actorID)
	if err != nil {
		return "", err
	}
	room := s.Rooms[actor.Body.RoomID]
	if flag(room.Resource.Flags[:], circleRoomNoKill) {
		return "safe-room", nil
	}
	actorFamily := flag(actor.Body.Flags[:], circlePlayerFamily)
	targetFamily := flag(target.target.Flags[:], circlePlayerFamily)
	if actorFamily && targetFamily {
		if s.War == nil {
			return "", ErrCircleFamilyWarUnresolved
		}
		if s.War.AllowsDeathLoss(actor.Body.Daily[9].Max, target.target.Daily[9].Max) {
			if !flag(actor.Body.Flags[:], circlePlayerChaos) && !flag(room.Resource.Flags[:], circleRoomSurvival) {
				return "actor-lawful", nil
			}
			if !flag(target.target.Flags[:], circlePlayerChaos) && !flag(room.Resource.Flags[:], circleRoomSurvival) {
				return "target-lawful", nil
			}
		}
	} else {
		if !flag(actor.Body.Flags[:], circlePlayerChaos) && !flag(room.Resource.Flags[:], circleRoomSurvival) {
			return "actor-lawful", nil
		}
		if !flag(target.target.Flags[:], circlePlayerChaos) && !flag(room.Resource.Flags[:], circleRoomSurvival) {
			return "target-lawful", nil
		}
	}
	// is_charm_crt is descriptor-local in the legacy runtime. There is no
	// canonical relation in State, so a target PCHARM marker cannot safely be
	// interpreted as a harmless ordinary player target. The source checks the
	// target marker only; an actor marker alone is not a charm relation.
	if flag(target.target.Flags[:], circlePlayerCharm) {
		return "", ErrCircleCharmStateUnresolved
	}
	return "ready", nil
}

func circleBlockedResponse(status string) string {
	switch status {
	case "safe-room":
		return "이 방에서는 싸울 수 없습니다.\r\n"
	case "actor-lawful":
		return "당신은 선해서 다른 사용자를 공격할 수 없습니다.\r\n"
	case "target-lawful":
		return "그 사용자는 선해서 보호받고 있습니다.\r\n"
	default:
		return circleMissingResponse()
	}
}

func circleAuthorization(class byte) (bool, error) {
	if class > circleMaxClass {
		return false, fmt.Errorf("circle actor class outside canonical table")
	}
	return class == circleFighterClass || class == circleBarbarianClass || class >= circleInvincibleClass, nil
}

// PlanCircle implements the bounded non-damaging portion of command8.c:circle.
// It preserves source ordering: class/argument/identity/PVP gates precede the
// LT_ATTCK cooldown, stealth release happens only after cooldown admission,
// NPC hostility is added before the one chance roll, and a successful attempt
// writes LT_BEFUD.  Dead identities and unresolved relations fail closed.
func (s State) PlanCircle(actorID, targetName string, now int32, roll func(int, int) int) (CircleProposal, error) {
	if err := s.Validate(); err != nil {
		return CircleProposal{}, err
	}
	if now < 0 {
		return CircleProposal{}, fmt.Errorf("invalid circle clock")
	}
	actor, room, err := circleActor(s, actorID)
	if err != nil {
		return CircleProposal{}, err
	}
	// Keep the raw command token on rejected proposals so ApplyCircle can
	// re-evaluate the same argument/class gate without trusting a response
	// string alone. An admitted target is replaced with its canonical display
	// name below.
	proposal := CircleProposal{ActorID: actorID, RoomID: room.Resource.ID, TargetName: targetName, Now: now, before: s.clone()}
	if !validCircleName(targetName) {
		proposal.NoOp = true
		proposal.Response = circleNoArgumentResponse()
		return proposal, nil
	}
	authorized, err := circleAuthorization(actor.Body.Class)
	if err != nil {
		return CircleProposal{}, err
	}
	if !authorized {
		proposal.NoOp = true
		proposal.Response = circleUnauthorizedResponse()
		return proposal, nil
	}
	resolution, err := s.selectCircleTarget(actorID, targetName)
	if err != nil {
		return CircleProposal{}, err
	}
	if resolution.targetID == "" {
		proposal.NoOp = true
		proposal.Response = circleMissingResponse()
		return proposal, nil
	}
	if resolution.target.HPCurrent < 1 {
		return CircleProposal{}, fmt.Errorf("%w: circle target %s is dead", ErrCircleDeathTransitionPending, resolution.targetID)
	}
	proposal.TargetID, proposal.TargetKind, proposal.TargetName = resolution.targetID, resolution.targetKind, resolution.target.Name
	if resolution.targetKind == CircleTargetPlayer {
		status, statusErr := circlePlayerStatus(s, actorID, resolution)
		if statusErr != nil {
			return CircleProposal{}, statusErr
		}
		if status != "ready" {
			proposal.NoOp, proposal.Blocked = true, true
			proposal.Response = circleBlockedResponse(status)
			return proposal, nil
		}
	}
	timer := actor.Body.Timers[circleAttackTimer]
	if timer.LastTime < 0 || timer.Interval < 0 {
		return CircleProposal{}, fmt.Errorf("circle attack timer outside legacy range")
	}
	deadline := int64(timer.LastTime) + int64(timer.Interval)
	if int64(now) < deadline {
		wait := deadline - int64(now)
		proposal.NoOp, proposal.Cooldown, proposal.WaitSeconds = true, true, int32(wait)
		proposal.Response, err = circleWaitResponse(wait)
		if err != nil {
			return CircleProposal{}, err
		}
		return proposal, nil
	}
	proposal.ClearHidden = true
	proposal.ClearInvisible = flag(actor.Body.Flags[:], circlePlayerInvisible)
	proposal.Changed = proposal.ClearInvisible || flag(actor.Body.Flags[:], circlePlayerHidden)
	proposal.Broadcast = proposal.ClearInvisible

	// The source clears stealth before checking MUNKIL. This is intentionally a
	// state-changing rejection, not an ordinary no-op.
	proposal.Chance, err = CircleChance(actor.Body, resolution.target)
	if err != nil {
		return CircleProposal{}, err
	}
	if resolution.targetKind == CircleTargetNPC && flag(resolution.target.Flags[:], circleNPCNoHarm) {
		proposal.NoOp, proposal.NoHarm = true, true
		proposal.Response = circleNoHarmResponse(resolution.target)
		if proposal.ClearInvisible {
			proposal.Response = circleRevealResponse() + proposal.Response
		}
		proposal.ExpectedEvent = circleEvent(actorID, actor.Body, resolution, proposal.ClearInvisible, false, false)
		proposal.Broadcast = proposal.ExpectedEvent != nil
		return proposal, nil
	}
	if resolution.targetKind == CircleTargetNPC {
		present, enemyErr := circleNPCEnemy(s, resolution.targetID, actorID)
		if enemyErr != nil {
			return CircleProposal{}, enemyErr
		}
		proposal.EnemyAdded = !present
	}
	proposal.TimerWrite = true
	proposal.Roll, err = circleRandom(roll, 1, 100)
	if err != nil {
		return CircleProposal{}, err
	}
	proposal.Attempted = true
	proposal.Succeeded = proposal.Roll <= proposal.Chance
	if proposal.Succeeded {
		var delay int
		if actor.Body.Class == circleBarbarianClass {
			delay, err = circleRandom(roll, 6, 9)
		} else {
			delay, err = circleRandom(roll, 6, 12)
		}
		if err != nil {
			return CircleProposal{}, err
		}
		proposal.Delay = int32(delay)
		proposal.Interval = 2
		proposal.Befuddled = true
		proposal.Response = circleSuccessResponse(resolution.target)
	} else {
		proposal.Interval = 3
		proposal.Response = circleFailureResponse()
	}
	if proposal.ClearInvisible {
		proposal.Response = circleRevealResponse() + proposal.Response
	}
	proposal.ExpectedEvent = circleEvent(actorID, actor.Body, resolution, proposal.ClearInvisible, true, proposal.Succeeded)
	proposal.Broadcast = proposal.ExpectedEvent != nil
	proposal.Changed = true
	return proposal, nil
}

func circleProposalTarget(s State, proposal CircleProposal) (circleResolution, error) {
	if proposal.TargetID == "" || proposal.TargetName == "" {
		return circleResolution{}, nil
	}
	resolution, err := s.selectCircleTarget(proposal.ActorID, proposal.TargetName)
	if err != nil {
		return circleResolution{}, err
	}
	if resolution.targetID != proposal.TargetID || resolution.targetKind != proposal.TargetKind || resolution.target.Name != proposal.TargetName {
		return circleResolution{}, fmt.Errorf("circle target changed")
	}
	if resolution.target.HPCurrent < 1 {
		return circleResolution{}, fmt.Errorf("%w: circle target is dead", ErrCircleDeathTransitionPending)
	}
	return resolution, nil
}

func circleValidateNoOp(s State, proposal CircleProposal, actor PlayerState) (CircleResult, error) {
	if !proposal.NoOp || proposal.Attempted || proposal.Succeeded || proposal.Roll != 0 || proposal.Delay != 0 || proposal.Interval != 0 || proposal.TimerWrite || proposal.EnemyAdded || proposal.Befuddled {
		return CircleResult{}, fmt.Errorf("invalid circle no-op proposal")
	}
	if proposal.NoHarm {
		if proposal.Cooldown || proposal.Blocked || proposal.TargetID == "" || proposal.TargetKind != CircleTargetNPC || proposal.WaitSeconds != 0 || proposal.ClearHidden != true || proposal.Response == "" {
			return CircleResult{}, fmt.Errorf("invalid circle no-harm proposal")
		}
		// The target, visibility projection, chance, and event are rechecked by
		// ApplyCircle against the same snapshot. Keep the proposal's event in
		// the result here so callers cannot accidentally lose the reveal
		// projection on a valid MUNKIL no-harm attempt.
		return circleResult(proposal), nil
	}
	if proposal.Cooldown {
		if proposal.Blocked || proposal.ClearHidden || proposal.ClearInvisible || proposal.TargetID == "" || proposal.TargetKind == "" || proposal.TargetName == "" || proposal.Chance != 0 || proposal.Changed || proposal.Broadcast || proposal.ExpectedEvent != nil {
			return CircleResult{}, fmt.Errorf("invalid circle cooldown proposal")
		}
		resolution, targetErr := circleProposalTarget(s, proposal)
		if targetErr != nil || resolution.targetKind != proposal.TargetKind {
			return CircleResult{}, fmt.Errorf("circle cooldown target changed")
		}
		timer := actor.Body.Timers[circleAttackTimer]
		if timer.LastTime < 0 || timer.Interval < 0 {
			return CircleResult{}, fmt.Errorf("circle attack timer changed")
		}
		deadline := int64(timer.LastTime) + int64(timer.Interval)
		if int64(proposal.Now) >= deadline {
			return CircleResult{}, fmt.Errorf("circle cooldown elapsed")
		}
		wait := deadline - int64(proposal.Now)
		if wait > math.MaxInt32 || proposal.WaitSeconds != int32(wait) {
			return CircleResult{}, fmt.Errorf("circle cooldown changed")
		}
		response, err := circleWaitResponse(wait)
		if err != nil || response != proposal.Response {
			return CircleResult{}, fmt.Errorf("circle cooldown response changed")
		}
		result := circleResult(proposal)
		result.Changed, result.Broadcast = false, false
		result.Event = nil
		return result, nil
	}
	if proposal.WaitSeconds != 0 || proposal.Chance != 0 || proposal.ExpectedEvent != nil {
		return CircleResult{}, fmt.Errorf("invalid circle rejected proposal")
	}
	if proposal.Blocked {
		if proposal.TargetID == "" || proposal.ClearHidden || proposal.ClearInvisible || proposal.Broadcast || proposal.Changed || proposal.Response == "" {
			return CircleResult{}, fmt.Errorf("invalid circle player rejection")
		}
		resolution, err := circleProposalTarget(s, proposal)
		if err != nil || resolution.targetKind != CircleTargetPlayer {
			return CircleResult{}, fmt.Errorf("circle player rejection target changed")
		}
		status, err := circlePlayerStatus(s, proposal.ActorID, resolution)
		if err != nil || status == "ready" || circleBlockedResponse(status) != proposal.Response {
			return CircleResult{}, fmt.Errorf("circle player rejection changed")
		}
		result := circleResult(proposal)
		result.Changed, result.Broadcast, result.Event = false, false, nil
		return result, nil
	}
	// Missing/unauthorized/argument no-ops have no canonical target and no
	// mutable projection. Re-evaluate the exact gate so a public proposal
	// cannot turn one response into another.
	if proposal.TargetID != "" || proposal.TargetKind != "" || proposal.ClearHidden || proposal.ClearInvisible || proposal.Broadcast || proposal.Changed || proposal.WaitSeconds != 0 {
		return CircleResult{}, fmt.Errorf("invalid circle no-target proposal")
	}
	var want string
	if !validCircleName(proposal.TargetName) {
		want = circleNoArgumentResponse()
	} else {
		authorized, err := circleAuthorization(actor.Body.Class)
		if err != nil {
			return CircleResult{}, err
		}
		if !authorized {
			want = circleUnauthorizedResponse()
		} else {
			resolution, resolveErr := s.selectCircleTarget(proposal.ActorID, proposal.TargetName)
			if resolveErr != nil {
				return CircleResult{}, resolveErr
			}
			if resolution.targetID != "" {
				return CircleResult{}, fmt.Errorf("circle missing-target proposal changed")
			}
			want = circleMissingResponse()
		}
	}
	if proposal.Response != want {
		return CircleResult{}, fmt.Errorf("circle no-target response changed")
	}
	result := circleResult(proposal)
	result.Changed, result.Broadcast, result.Event = false, false, nil
	return result, nil
}

// ApplyCircle commits only the exact snapshot-bound candidate produced by
// PlanCircle.  It never rerolls and never returns a partially updated state.
func (s State) ApplyCircle(proposal CircleProposal) (State, CircleResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, CircleResult{}, err
	}
	if proposal.ActorID == "" || proposal.before.Version == 0 || proposal.Now < 0 || proposal.Response == "" || !reflect.DeepEqual(s, proposal.before) {
		return State{}, CircleResult{}, fmt.Errorf("stale or invalid circle proposal")
	}
	actor, room, err := circleActor(s, proposal.ActorID)
	if err != nil || room.Resource.ID != proposal.RoomID {
		return State{}, CircleResult{}, fmt.Errorf("circle actor changed")
	}
	if proposal.NoOp {
		result, noOpErr := circleValidateNoOp(s, proposal, actor)
		if noOpErr != nil {
			return State{}, CircleResult{}, noOpErr
		}
		if proposal.NoHarm {
			resolution, targetErr := circleProposalTarget(s, proposal)
			if targetErr != nil || resolution.targetKind != CircleTargetNPC || !flag(resolution.target.Flags[:], circleNPCNoHarm) {
				return State{}, CircleResult{}, fmt.Errorf("circle no-harm target changed")
			}
			chance, chanceErr := CircleChance(actor.Body, resolution.target)
			if chanceErr != nil || proposal.Chance != chance {
				return State{}, CircleResult{}, fmt.Errorf("circle no-harm chance changed")
			}
			if proposal.ClearHidden != true || proposal.ClearInvisible != flag(actor.Body.Flags[:], circlePlayerInvisible) || proposal.Broadcast != (proposal.ExpectedEvent != nil) || proposal.Changed != (flag(actor.Body.Flags[:], circlePlayerHidden) || proposal.ClearInvisible) {
				return State{}, CircleResult{}, fmt.Errorf("circle no-harm visibility projection changed")
			}
			wantResponse := circleNoHarmResponse(resolution.target)
			if proposal.ClearInvisible {
				wantResponse = circleRevealResponse() + wantResponse
			}
			wantEvent := circleEvent(proposal.ActorID, actor.Body, resolution, proposal.ClearInvisible, false, false)
			if proposal.Response != wantResponse || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) {
				return State{}, CircleResult{}, fmt.Errorf("circle no-harm projection changed")
			}
			next := s.clone()
			nextActor := next.Players[proposal.ActorID]
			setSettingFlag(&nextActor.Body, circlePlayerHidden, false)
			if proposal.ClearInvisible {
				setSettingFlag(&nextActor.Body, circlePlayerInvisible, false)
			}
			next.Players[proposal.ActorID] = nextActor
			if err := next.Validate(); err != nil {
				return State{}, CircleResult{}, err
			}
			result := circleResult(proposal)
			result.Changed = flag(actor.Body.Flags[:], circlePlayerHidden) || proposal.ClearInvisible
			result.Broadcast = wantEvent != nil
			result.Event = wantEvent
			return next, result, nil
		}
		return s.clone(), result, nil
	}

	// An admitted action must have passed target and player authorization again.
	if proposal.TargetID == "" || proposal.TargetName == "" || proposal.TargetKind == "" || proposal.Cooldown || !proposal.ClearHidden || !proposal.TimerWrite || proposal.WaitSeconds != 0 || proposal.Interval != 2 && proposal.Interval != 3 {
		return State{}, CircleResult{}, fmt.Errorf("invalid circle authorization proposal")
	}
	resolution, err := circleProposalTarget(s, proposal)
	if err != nil {
		return State{}, CircleResult{}, err
	}
	if resolution.targetKind == CircleTargetPlayer {
		status, statusErr := circlePlayerStatus(s, proposal.ActorID, resolution)
		if statusErr != nil || status != "ready" {
			return State{}, CircleResult{}, fmt.Errorf("circle player authorization changed")
		}
	} else if flag(resolution.target.Flags[:], circleNPCNoHarm) {
		return State{}, CircleResult{}, fmt.Errorf("circle no-harm authorization changed")
	}
	timer := actor.Body.Timers[circleAttackTimer]
	if timer.LastTime < 0 || timer.Interval < 0 || int64(proposal.Now) < int64(timer.LastTime)+int64(timer.Interval) {
		return State{}, CircleResult{}, fmt.Errorf("circle attack cooldown changed")
	}
	if proposal.ClearInvisible != flag(actor.Body.Flags[:], circlePlayerInvisible) {
		return State{}, CircleResult{}, fmt.Errorf("circle invisibility projection changed")
	}
	chance, err := CircleChance(actor.Body, resolution.target)
	if err != nil || chance != proposal.Chance {
		return State{}, CircleResult{}, fmt.Errorf("circle chance changed")
	}
	if !proposal.Attempted || proposal.Roll < 1 || proposal.Roll > 100 || proposal.Succeeded != (proposal.Roll <= proposal.Chance) {
		return State{}, CircleResult{}, fmt.Errorf("invalid circle random outcome")
	}
	if proposal.Succeeded {
		if !proposal.Befuddled || proposal.Delay < 6 || proposal.Delay > 12 || proposal.Interval != 2 {
			return State{}, CircleResult{}, fmt.Errorf("invalid circle success outcome")
		}
		if actor.Body.Class == circleBarbarianClass && (proposal.Delay < 6 || proposal.Delay > 9) {
			return State{}, CircleResult{}, fmt.Errorf("circle barbarian delay outside source range")
		}
	} else if proposal.Befuddled || proposal.Delay != 0 || proposal.Interval != 3 {
		return State{}, CircleResult{}, fmt.Errorf("invalid circle failure outcome")
	}
	present := false
	if resolution.targetKind == CircleTargetNPC {
		present, err = circleNPCEnemy(s, resolution.targetID, proposal.ActorID)
		if err != nil {
			return State{}, CircleResult{}, err
		}
	}
	if proposal.EnemyAdded != (resolution.targetKind == CircleTargetNPC && !present) {
		return State{}, CircleResult{}, fmt.Errorf("circle hostility projection changed")
	}
	if proposal.Changed != true || proposal.Broadcast != (proposal.ExpectedEvent != nil) {
		return State{}, CircleResult{}, fmt.Errorf("circle state projection changed")
	}
	wantResponse := circleFailureResponse()
	if proposal.Succeeded {
		wantResponse = circleSuccessResponse(resolution.target)
	}
	if proposal.ClearInvisible {
		wantResponse = circleRevealResponse() + wantResponse
	}
	wantEvent := circleEvent(proposal.ActorID, actor.Body, resolution, proposal.ClearInvisible, true, proposal.Succeeded)
	if proposal.Response != wantResponse || !reflect.DeepEqual(proposal.ExpectedEvent, wantEvent) {
		return State{}, CircleResult{}, fmt.Errorf("circle room projection changed")
	}
	next := s.clone()
	nextActor := next.Players[proposal.ActorID]
	setSettingFlag(&nextActor.Body, circlePlayerHidden, false)
	if proposal.ClearInvisible {
		setSettingFlag(&nextActor.Body, circlePlayerInvisible, false)
	}
	nextActor.Body.Timers[circleAttackTimer] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Interval}
	next.Players[proposal.ActorID] = nextActor
	if resolution.targetKind == CircleTargetNPC {
		npc := next.NPCs[resolution.targetID]
		if proposal.EnemyAdded {
			npc.Enemies = append(npc.Enemies, NPCEnemy{Target: EntityRef{Kind: "player", ID: proposal.ActorID}, Damage: 0})
		}
		if proposal.Succeeded {
			npc.Body.Timers[circleBefuddleTimer] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Delay}
			setSettingFlag(&npc.Body, circleNPCBefuddled, true)
		}
		next.NPCs[resolution.targetID] = npc
	} else if proposal.Succeeded {
		target := next.Players[resolution.targetID]
		target.Body.Timers[circleBefuddleTimer] = LegacyTimer{LastTime: proposal.Now, Interval: proposal.Delay}
		next.Players[resolution.targetID] = target
	}
	if err := next.Validate(); err != nil {
		return State{}, CircleResult{}, err
	}
	result := circleResult(proposal)
	result.Event = wantEvent
	result.Broadcast = wantEvent != nil
	return next, result, nil
}

// CircleByName is the one-call pure boundary for non-session callers. Session
// commands should use PlanCircle/ApplyCircle through ExecuteGame so the random
// outcome is captured in the durable receipt.
func (s State) CircleByName(actorID, targetName string, now int32, roll func(int, int) int) (State, CircleResult, error) {
	proposal, err := s.PlanCircle(actorID, targetName, now, roll)
	if err != nil {
		return State{}, CircleResult{}, err
	}
	return s.ApplyCircle(proposal)
}

// Circle is a concise compatibility spelling for CircleByName.
func (s State) Circle(actorID, targetName string, now int32, roll func(int, int) int) (State, CircleResult, error) {
	return s.CircleByName(actorID, targetName, now, roll)
}
