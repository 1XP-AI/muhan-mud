package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The values in this file are kept next to the source command's implementation
// (magic1.c) until the wider spell/permission model is admitted.  They are
// indexes into the canonical legacy bit fields, not a new permission system.
const (
	teachPlayerHiddenFlag      = 1  // PHIDDN
	teachPlayerInvisibleFlag   = 2  // PINVIS/MINVIS for player entries
	teachPlayerDMInvisibleFlag = 10 // PDMINV
	teachPlayerDetectFlag      = 21 // PDINVI
	teachPlayerBlindFlag       = 42 // PBLIND
	teachPlayerSilentFlag      = 44 // PSILNC
	teachClericClass           = 3  // CLERIC
	teachMageClass             = 5  // MAGE
	teachInvincibleClass       = 9  // INVINCIBLE
	teachCaretakerClass        = 10 // CARETAKER
	teachSubDMClass            = 11 // SUB_DM
	teachSpellCount            = 56
)

const (
	// Exported source indexes let command/transport adapters retain the same
	// bit-field contract without importing C headers.
	TeachHiddenFlag      = teachPlayerHiddenFlag
	TeachInvisibleFlag   = teachPlayerInvisibleFlag
	TeachDMInvisibleFlag = teachPlayerDMInvisibleFlag
	TeachDetectFlag      = teachPlayerDetectFlag
	TeachBlindFlag       = teachPlayerBlindFlag
	TeachSilentFlag      = teachPlayerSilentFlag
	TeachClericClass     = teachClericClass
	TeachMageClass       = teachMageClass
	TeachInvincibleClass = teachInvincibleClass
	TeachCaretakerClass  = teachCaretakerClass
	TeachSubDMClass      = teachSubDMClass
	TeachSpellCount      = teachSpellCount
)

var (
	ErrTeachTargetUnavailable = errors.New("teach target unavailable")
	ErrTeachSpellUnavailable  = errors.New("teach spell unavailable")
	ErrTeachSpellAmbiguous    = errors.New("teach spell name is ambiguous")
	ErrTeachActorBlind        = errors.New("teach is unavailable while blind")
	ErrTeachActorSilent       = errors.New("teach is unavailable while silenced")
	ErrTeachTeacherClass      = errors.New("teach requires a cleric, mage, or caretaker")
	ErrTeachSourceSpell       = errors.New("teacher has not learned the spell")
	ErrTeachPermission        = errors.New("teacher cannot teach this spell level")
)

// Compatibility spellings keep the source-oriented error vocabulary usable by
// callers while the wider command catalog is being admitted.
var (
	ErrTeachBlind         = ErrTeachActorBlind
	ErrTeachSilent        = ErrTeachActorSilent
	ErrTeachClass         = ErrTeachTeacherClass
	ErrTeachSpellNotKnown = ErrTeachSourceSpell
	ErrTeachLevel         = ErrTeachPermission
	ErrTeachTarget        = ErrTeachTargetUnavailable
	ErrTeachSpell         = ErrTeachSpellUnavailable
	ErrTeachSpellName     = ErrTeachSpellAmbiguous
)

// TeachTargetKind is deliberately closed.  magic1.c:teach searches
// first_ply, so NPCs are not silently promoted to teach recipients even though
// the world snapshot also contains a canonical NPC identity graph.
type TeachTargetKind string

const TeachTargetPlayer TeachTargetKind = "player"

// TeachEvent contains recipient-specific projections of the committed
// command.  The actor receives TeachResult.Response, the target receives
// TargetText, and other same-room players receive Text.  Publishing is owned
// by the transport after the receipt commits; this value is safe to persist in
// a receipt because it was derived from canonical identities during planning.
type TeachEvent struct {
	RoomID          int16           `json:"room_id"`
	ActorID         string          `json:"actor_id"`
	ActorName       string          `json:"actor_name"`
	TargetID        string          `json:"target_id"`
	TargetName      string          `json:"target_name"`
	TargetKind      TeachTargetKind `json:"target_kind"`
	ExcludeActorID  string          `json:"exclude_actor_id"`
	ExcludeTargetID string          `json:"exclude_target_id"`
	TargetText      string          `json:"target_text"`
	Text            string          `json:"text"`
}

// TeachResult is the deterministic actor-facing receipt projection.  No
// random source, clock, or allocator is involved in this command.
type TeachResult struct {
	Action     string          `json:"action"`
	Response   string          `json:"response"`
	Changed    bool            `json:"changed"`
	Broadcast  bool            `json:"broadcast"`
	ActorID    string          `json:"actor_id"`
	ActorName  string          `json:"actor_name"`
	TargetID   string          `json:"target_id"`
	TargetName string          `json:"target_name"`
	TargetKind TeachTargetKind `json:"target_kind"`
	RoomID     int16           `json:"room_id"`
	Occurrence int             `json:"occurrence"`
	SpellIndex int             `json:"spell_index"`
	SpellName  string          `json:"spell_name"`
	SpellLevel byte            `json:"spell_level"`
	Event      *TeachEvent     `json:"event,omitempty"`
}

// TeachProposal is bound to one exact State snapshot.  The expected bodies
// and ordered room membership make ApplyTeach reject a stale target or actor;
// they are private so a client cannot manufacture an authority proof.
type TeachProposal struct {
	Action         string
	ActorID        string
	TargetID       string
	TargetName     string
	TargetKind     TeachTargetKind
	RoomID         int16
	TargetSelector string
	Occurrence     int
	SpellSelector  string
	SpellIndex     int
	SpellName      string
	SpellLevel     byte
	ClearHidden    bool
	SetTargetSpell bool
	Changed        bool
	Broadcast      bool
	Response       string
	TargetText     string
	RoomText       string

	expectedActor       LegacyMonster
	expectedTarget      LegacyMonster
	expectedRoomPlayers []string
}

// teachSpellLevels mirrors spllist[].spllv in src/global.c.  Names are read
// from legacyInfoSpellNames so there remains one canonical spell-name table;
// a missing/invalid entry is an unresolved migration boundary.
var teachSpellLevels = [...]byte{
	1, 2, 2, 1, 2, 2, 3, 5, 4, 3, 3, 4, 3, 4, 5, 4,
	3, 3, 3, 5, 4, 3, 3, 4, 3, 5, 2, 2, 2, 3, 3, 3,
	5, 3, 3, 4, 4, 4, 5, 5, 5, 3, 4, 3, 3, 3, 4, 5,
	3, 3, 4, 4, 4, 5, 5, 5,
}

func teachSpellAt(index int) (string, byte, error) {
	if index < 0 || index >= teachSpellCount || index >= len(legacyInfoSpellNames) || index >= len(teachSpellLevels) {
		return "", 0, fmt.Errorf("%w: index=%d", ErrTeachSpellUnavailable, index)
	}
	name := legacyInfoSpellNames[index]
	if name == "" || !utf8.ValidString(name) || strings.TrimSpace(name) != name {
		return "", 0, fmt.Errorf("%w: index=%d", ErrTeachSpellUnavailable, index)
	}
	level := teachSpellLevels[index]
	if level < 1 || level > 5 {
		return "", 0, fmt.Errorf("%w: invalid level at index=%d", ErrTeachSpellUnavailable, index)
	}
	return name, level, nil
}

func validTeachSelector(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// legacyUpFirstASCII mirrors teach's cmnd->str[1][0] = up(...).  The source
// only changes the first byte; spell names are intentionally not normalized.
func legacyUpFirstASCII(value string) string {
	if value == "" {
		return value
	}
	first := value[0]
	if first < 'a' || first > 'z' {
		return value
	}
	return string(first-'a'+'A') + value[1:]
}

func selectTeachSpell(selector string) (int, string, byte, error) {
	if !validTeachSelector(selector) {
		return 0, "", 0, fmt.Errorf("%w: invalid selector", ErrTeachSpellUnavailable)
	}
	exact := -1
	matches := 0
	matchIndex := -1
	for index := 0; index < teachSpellCount; index++ {
		name, _, err := teachSpellAt(index)
		if err != nil {
			return 0, "", 0, err
		}
		if name == selector {
			exact = index
			break // source exact match takes precedence over prefix matches
		}
		if strings.HasPrefix(name, selector) {
			matches++
			matchIndex = index
		}
	}
	if exact >= 0 {
		name, level, err := teachSpellAt(exact)
		return exact, name, level, err
	}
	if matches == 0 {
		return 0, "", 0, fmt.Errorf("%w: %q", ErrTeachSpellUnavailable, selector)
	}
	if matches > 1 {
		return 0, "", 0, fmt.Errorf("%w: %q", ErrTeachSpellAmbiguous, selector)
	}
	name, level, err := teachSpellAt(matchIndex)
	return matchIndex, name, level, err
}

func teachPlayerMatches(player LegacyMonster, selector string) bool {
	if strings.HasPrefix(player.Name, selector) {
		return true
	}
	for _, key := range player.Keys {
		if key != "" && strings.HasPrefix(key, selector) {
			return true
		}
	}
	return false
}

func teachActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" || !validTeachSelector(actor.Body.Name) {
		return PlayerState{}, RoomState{}, fmt.Errorf("online teach actor absent")
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || !roomContainsPlayer(room, actorID) {
		return PlayerState{}, RoomState{}, fmt.Errorf("teach actor room membership absent")
	}
	return actor, room, nil
}

func teachTargetVisible(actor, target LegacyMonster) bool {
	// creature.c:find_crt unconditionally hides DM-invisible caretaker+ users,
	// while ordinary PINVIS is visible only to a detector.
	if target.Class >= teachCaretakerClass && flag(target.Flags[:], teachPlayerDMInvisibleFlag) {
		return false
	}
	return !flag(target.Flags[:], teachPlayerInvisibleFlag) || flag(actor.Flags[:], teachPlayerDetectFlag)
}

// selectTeachTarget is intentionally room-order based.  It follows C's
// first_ply linked-list occurrence semantics and matches name/key prefixes;
// it never searches NPCs or a legacy resource copy.
func selectTeachTarget(s State, actorID, selector string, occurrence int) (string, PlayerState, error) {
	actor, room, err := teachActor(s, actorID)
	if err != nil {
		return "", PlayerState{}, err
	}
	if !validTeachSelector(selector) || occurrence < 1 {
		return "", PlayerState{}, fmt.Errorf("%w: invalid selector or occurrence", ErrTeachTargetUnavailable)
	}
	selector = legacyUpFirstASCII(selector)
	matched := 0
	for _, id := range room.PlayerIDs {
		target, ok := s.Players[id]
		if id == "" || !ok || !target.Online || target.Body.Type != 0 || target.Body.RoomID != room.Resource.ID || !validTeachSelector(target.Body.Name) {
			return "", PlayerState{}, fmt.Errorf("%w: unresolved canonical player identity", ErrTeachTargetUnavailable)
		}
		for _, key := range target.Body.Keys {
			if key != "" && (!utf8.ValidString(key) || strings.ContainsAny(key, "\r\n")) {
				return "", PlayerState{}, fmt.Errorf("%w: unresolved canonical player key", ErrTeachTargetUnavailable)
			}
		}
		if !teachPlayerMatches(target.Body, selector) || !teachTargetVisible(actor.Body, target.Body) {
			continue
		}
		matched++
		if matched == occurrence {
			return id, target, nil
		}
	}
	return "", PlayerState{}, fmt.Errorf("%w: %q", ErrTeachTargetUnavailable, selector)
}

func teachTeacherAllowed(class byte) bool {
	return class == teachClericClass || class == teachMageClass || class == teachCaretakerClass
}

func teachLevelAllowed(class byte, level byte) bool {
	switch level {
	case 1:
		return class == teachClericClass || class >= teachInvincibleClass
	case 2:
		return class == teachMageClass || class >= teachInvincibleClass
	case 3:
		return class >= teachInvincibleClass
	case 4:
		return class >= teachCaretakerClass
	case 5:
		return class >= teachSubDMClass
	default:
		return false
	}
}

func teachTargetText(actorName, spellName string) string {
	return fmt.Sprintf("\n%s이 %s 주술의 시범을 보이며 주문 전수를 시킵니다.\r\n오옷~~~ 이 주문을 외우자 주위에 이상한 기운이 모이는 것이 그\r\n그 사람에게 상당한 도움이 될 것 같습니다.\r\n", actorName, spellName)
}

func teachActorText(targetName, spellName string) string {
	return fmt.Sprintf("\n%s 주술을 %s에게 시범을 보이며 주문 전수를 시킵니다.\r\n오옷~~~ 이 주문을 외우자 주위에 이상한 기운이 모이는 것이\r\n그 사람에게 상당한 도움이 될 것 같습니다.\r\n", spellName, targetName)
}

func teachRoomText(actorName, targetName, spellName string) string {
	return fmt.Sprintf("\n%s이 %s에게 %s 주술의 시범을 보이며 주문 전수를\r\n시킵니다.\r\n오옷~~~ 이 주문을 외우자 주위에 이상한 기운이 모이는 것이\r\n그 사람에게 상당한 도움이 될 것 같습니다.\r\n", actorName, targetName, spellName)
}

// PlanTeach ports the stateful and permission-bearing portion of
// magic1.c:teach.  The source's target is an online same-room player selected
// from first_ply; target spell state is canonical and the actor's hidden bit is
// cleared only after every admission gate passes.
func (s State) PlanTeach(actorID, targetSelector, spellSelector string, occurrence int) (TeachProposal, error) {
	if err := s.Validate(); err != nil {
		return TeachProposal{}, err
	}
	actor, room, err := teachActor(s, actorID)
	if err != nil {
		return TeachProposal{}, err
	}
	if flag(actor.Body.Flags[:], teachPlayerBlindFlag) {
		return TeachProposal{}, ErrTeachActorBlind
	}
	if flag(actor.Body.Flags[:], teachPlayerSilentFlag) {
		return TeachProposal{}, ErrTeachActorSilent
	}
	if !teachTeacherAllowed(actor.Body.Class) {
		return TeachProposal{}, ErrTeachTeacherClass
	}
	targetID, target, err := selectTeachTarget(s, actorID, targetSelector, occurrence)
	if err != nil {
		return TeachProposal{}, err
	}
	spellIndex, spellName, spellLevel, err := selectTeachSpell(spellSelector)
	if err != nil {
		return TeachProposal{}, err
	}
	if !flag(actor.Body.Spells[:], uint(spellIndex)) {
		return TeachProposal{}, ErrTeachSourceSpell
	}
	if !teachLevelAllowed(actor.Body.Class, spellLevel) {
		return TeachProposal{}, ErrTeachPermission
	}
	actorHidden := flag(actor.Body.Flags[:], teachPlayerHiddenFlag)
	targetHasSpell := flag(target.Body.Spells[:], uint(spellIndex))
	return TeachProposal{
		Action: "teach", ActorID: actorID, TargetID: targetID, TargetName: target.Body.Name, TargetKind: TeachTargetPlayer,
		RoomID: actor.Body.RoomID, TargetSelector: targetSelector, Occurrence: occurrence,
		SpellSelector: spellSelector, SpellIndex: spellIndex, SpellName: spellName, SpellLevel: spellLevel,
		ClearHidden: true, SetTargetSpell: true, Changed: actorHidden || !targetHasSpell, Broadcast: true,
		Response: teachActorText(target.Body.Name, spellName), TargetText: teachTargetText(actor.Body.Name, spellName),
		RoomText:      teachRoomText(actor.Body.Name, target.Body.Name, spellName),
		expectedActor: actor.Body, expectedTarget: target.Body,
		expectedRoomPlayers: append([]string(nil), room.PlayerIDs...),
	}, nil
}

// ApplyTeach applies a proposal only to the snapshot from which it was
// planned.  All selection, catalog, permission, and expected output checks are
// repeated before cloning, so a stale request cannot grant a different spell.
func (s State) ApplyTeach(proposal TeachProposal) (State, TeachResult, error) {
	if err := s.Validate(); err != nil {
		return State{}, TeachResult{}, err
	}
	if proposal.Action != "teach" || proposal.ActorID == "" || proposal.TargetID == "" || proposal.TargetName == "" || proposal.TargetKind != TeachTargetPlayer || proposal.Occurrence < 1 || proposal.SpellIndex < 0 || proposal.SpellIndex >= teachSpellCount || proposal.SpellName == "" || proposal.SpellLevel < 1 || proposal.SpellLevel > 5 || !proposal.ClearHidden || !proposal.SetTargetSpell || !proposal.Broadcast || proposal.Response == "" || proposal.TargetText == "" || proposal.RoomText == "" {
		return State{}, TeachResult{}, fmt.Errorf("invalid teach proposal")
	}
	actor, room, err := teachActor(s, proposal.ActorID)
	if err != nil {
		return State{}, TeachResult{}, err
	}
	if actor.Body.RoomID != proposal.RoomID || !reflect.DeepEqual(room.PlayerIDs, proposal.expectedRoomPlayers) || !reflect.DeepEqual(actor.Body, proposal.expectedActor) {
		return State{}, TeachResult{}, fmt.Errorf("stale teach actor or room proposal")
	}
	targetID, target, err := selectTeachTarget(s, proposal.ActorID, proposal.TargetSelector, proposal.Occurrence)
	if err != nil || targetID != proposal.TargetID || target.Body.Name != proposal.TargetName || !reflect.DeepEqual(target.Body, proposal.expectedTarget) {
		return State{}, TeachResult{}, fmt.Errorf("stale teach target proposal")
	}
	spellIndex, spellName, spellLevel, err := selectTeachSpell(proposal.SpellSelector)
	if err != nil || spellIndex != proposal.SpellIndex || spellName != proposal.SpellName || spellLevel != proposal.SpellLevel {
		return State{}, TeachResult{}, fmt.Errorf("stale teach spell catalog")
	}
	if flag(actor.Body.Flags[:], teachPlayerBlindFlag) || flag(actor.Body.Flags[:], teachPlayerSilentFlag) || !teachTeacherAllowed(actor.Body.Class) || !flag(actor.Body.Spells[:], uint(spellIndex)) || !teachLevelAllowed(actor.Body.Class, spellLevel) {
		return State{}, TeachResult{}, fmt.Errorf("teach admission changed")
	}
	if proposal.Response != teachActorText(target.Body.Name, spellName) || proposal.TargetText != teachTargetText(actor.Body.Name, spellName) || proposal.RoomText != teachRoomText(actor.Body.Name, target.Body.Name, spellName) {
		return State{}, TeachResult{}, fmt.Errorf("teach projection changed")
	}
	changed := flag(actor.Body.Flags[:], teachPlayerHiddenFlag) || !flag(target.Body.Spells[:], uint(spellIndex))
	if proposal.Changed != changed || !proposal.Broadcast {
		return State{}, TeachResult{}, fmt.Errorf("teach mutation marker changed")
	}

	next := s.clone()
	nextTarget := next.Players[proposal.TargetID]
	nextTarget.Body.Spells[spellIndex/8] |= 1 << (spellIndex % 8)
	next.Players[proposal.TargetID] = nextTarget
	nextActor := next.Players[proposal.ActorID]
	nextActor.Body.Flags[teachPlayerHiddenFlag/8] &^= 1 << (teachPlayerHiddenFlag % 8)
	next.Players[proposal.ActorID] = nextActor
	event := &TeachEvent{
		RoomID: proposal.RoomID, ActorID: proposal.ActorID, ActorName: actor.Body.Name,
		TargetID: proposal.TargetID, TargetName: target.Body.Name, TargetKind: TeachTargetPlayer,
		ExcludeActorID: proposal.ActorID, ExcludeTargetID: proposal.TargetID,
		TargetText: proposal.TargetText, Text: proposal.RoomText,
	}
	result := TeachResult{
		Action: "teach", Response: proposal.Response, Changed: changed, Broadcast: true,
		ActorID: proposal.ActorID, ActorName: actor.Body.Name, TargetID: proposal.TargetID,
		TargetName: target.Body.Name, TargetKind: TeachTargetPlayer, RoomID: proposal.RoomID,
		Occurrence: proposal.Occurrence, SpellIndex: spellIndex, SpellName: spellName,
		SpellLevel: spellLevel, Event: event,
	}
	if err := next.Validate(); err != nil {
		return State{}, TeachResult{}, err
	}
	return next, result, nil
}

// Teach is the concise one-call reducer API.  Session commands should retain
// the Plan/Apply boundary so the receipt can reject stale snapshots.
func (s State) Teach(actorID, targetSelector, spellSelector string, occurrence int) (State, TeachResult, error) {
	proposal, err := s.PlanTeach(actorID, targetSelector, spellSelector, occurrence)
	if err != nil {
		return State{}, TeachResult{}, err
	}
	return s.ApplyTeach(proposal)
}
