package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// forge is the bounded Go port of command7.c:forge (cmdlist-85 `제련`).
// This slice covers select_arm param 1 (weapon-type prompt), param 2
// (digits 1-5 load object 900-904, then the material prompt), param 3
// (materials 1/2/3 set sdice and sum), param 4 (담금질 1-5 set
// shotsmax/shotscur and add to sum), param 5 (weapon name after temper:
// 3-20 bytes, no parentheses, then the confirm prompt), and param 6
// (예 prefix charges gold atomically and add_obj_crt; anything else
// cancels; insufficient gold frees the object). newforge/`무기만들기`
// stays fail-closed. Room flag RFORGE is observed from the canonical
// snapshot only; a missing bit is the original print, not an invented
// forge.
const (
	forgeRoomFlag           uint  = 35 // RFORGE
	forgeNoBroadcastFlag    uint  = 3  // PNOBRD
	forgeReadingFlag        uint  = 29 // PREADI
	forgeSelectArmPhase           = 2  // select_arm RETURN param after case 1
	forgeMaterialPhase            = 3  // select_arm RETURN param after case 2
	forgeQuenchPhase              = 4  // select_arm RETURN param after case 3
	forgeNamePhase                = 5  // select_arm RETURN param after case 4
	forgeConfirmPhase             = 6  // select_arm RETURN param after case 5
	forgeWeaponNameMinBytes       = 3
	forgeWeaponNameMaxBytes       = 20
	forgeWeaponObjectBase   int16 = 900
	forgeClericClass        byte  = 3  // CLERIC
	forgeMageClass          byte  = 5  // MAGE
	forgePaladinClass       byte  = 6  // PALADIN
	forgeClassSelectFlag    uint  = 31 // OCLSEL
	forgeAssassinOnlyFlag   uint  = 32 // OASSNO
	forgeBarbarianOnlyFlag  uint  = 33 // OBARBO
	forgeFighterOnlyFlag    uint  = 35 // OFIGHO
	forgeRangerOnlyFlag     uint  = 38 // ORNGRO
	forgeThiefOnlyFlag      uint  = 39 // OTHIEO
	forgeSteelCost          int32 = 50000
	forgePreciousCost       int32 = 200000
	forgeDiamondCost        int32 = 300000
	forgeSteelDice          int16 = 3
	forgePreciousDice       int16 = 4
	forgeDiamondDice        int16 = 5
	forgeQuench100Shots     int16 = 100
	forgeQuench200Shots     int16 = 200
	forgeQuench300Shots     int16 = 300
	forgeQuench400Shots     int16 = 400
	forgeQuench500Shots     int16 = 500
	forgeQuench100Cost      int32 = 50000
	forgeQuench200Cost      int32 = 200000
	forgeQuench300Cost      int32 = 500000
	forgeQuench400Cost      int32 = 1000000
	forgeQuench500Cost      int32 = 2000000
)

const (
	ForgeRoomFlag           = forgeRoomFlag
	ForgeNoBroadcastFlag    = forgeNoBroadcastFlag
	ForgeReadingFlag        = forgeReadingFlag
	ForgeSelectArmPhase     = forgeSelectArmPhase
	ForgeMaterialPhase      = forgeMaterialPhase
	ForgeQuenchPhase        = forgeQuenchPhase
	ForgeNamePhase          = forgeNamePhase
	ForgeConfirmPhase       = forgeConfirmPhase
	ForgeWeaponNameMinBytes = forgeWeaponNameMinBytes
	ForgeWeaponNameMaxBytes = forgeWeaponNameMaxBytes
	ForgeWeaponObjectBase   = forgeWeaponObjectBase
	ForgeClericClass        = forgeClericClass
	ForgeMageClass          = forgeMageClass
	ForgePaladinClass       = forgePaladinClass
	ForgeClassSelectFlag    = forgeClassSelectFlag
	ForgeAssassinOnlyFlag   = forgeAssassinOnlyFlag
	ForgeBarbarianOnlyFlag  = forgeBarbarianOnlyFlag
	ForgeFighterOnlyFlag    = forgeFighterOnlyFlag
	ForgeRangerOnlyFlag     = forgeRangerOnlyFlag
	ForgeThiefOnlyFlag      = forgeThiefOnlyFlag
	ForgeSteelCost          = forgeSteelCost
	ForgePreciousCost       = forgePreciousCost
	ForgeDiamondCost        = forgeDiamondCost
	ForgeSteelDice          = forgeSteelDice
	ForgePreciousDice       = forgePreciousDice
	ForgeDiamondDice        = forgeDiamondDice
	ForgeQuench100Shots     = forgeQuench100Shots
	ForgeQuench200Shots     = forgeQuench200Shots
	ForgeQuench300Shots     = forgeQuench300Shots
	ForgeQuench400Shots     = forgeQuench400Shots
	ForgeQuench500Shots     = forgeQuench500Shots
	ForgeQuench100Cost      = forgeQuench100Cost
	ForgeQuench200Cost      = forgeQuench200Cost
	ForgeQuench300Cost      = forgeQuench300Cost
	ForgeQuench400Cost      = forgeQuench400Cost
	ForgeQuench500Cost      = forgeQuench500Cost
)

type ForgeAction string

const (
	ForgeNotForge       ForgeAction = "not_forge"
	ForgePrompt         ForgeAction = "prompt"
	ForgeReprompt       ForgeAction = "reprompt"
	ForgeMaterial       ForgeAction = "material"
	ForgeMaterialDenied ForgeAction = "material_denied"
	ForgeQuench         ForgeAction = "quench"
	ForgeName           ForgeAction = "name"
	ForgeConfirm        ForgeAction = "confirm"
	ForgeGive           ForgeAction = "give"
	ForgeTooPoor        ForgeAction = "too_poor"
	ForgeCancel         ForgeAction = "cancel"
)

const (
	ForgeNotForgeResponse       = "여기는 대장간이 아닙니다."
	ForgePromptResponse         = "\n\n어떤 종류의 무기를 원하십니까?\n1. 도 2. 검 3. 봉 4. 창 5. 궁 ?\n:"
	ForgeRepromptResponse       = "하나를 선택하시오: "
	ForgeMaterialResponse       = "\n타격치에 영향을 주는 재료를 선택하십시요.\n1.강 철 오만냥 2.귀금속 이십만냥 3.금강석 삼십만냥\n:"
	ForgeMaterialDeniedResponse = "당신은 이런 무기를 사용할 능력이 없습니다.\n다른재료를 선택하십시요\n"
	ForgeQuenchResponse         = "\n훌륭한  무기는  담금질로 내구력이  향상됩니다.\n몇번의  담금질을 원합니까?\n1. 100번 오만냥 2. 200번 이십만냥  3. 300번 오십만냥\n4. 400번 백만냥 5. 500번 이백만냥\n: "
	ForgeNameResponse           = "\n당신의 무기의 이름을 지으십시요.\n나중에 이름을 고칠수 없으니 조심해서 지으셔야 합니다\n: "
	ForgeNameParenResponse      = "이름에는 괄호가 들어갈수 없습니다.\n이름을 다시 넣으십시요:"
	ForgeNameLongResponse       = "입력된 이름이 너무  깁니다.\n이름을 다시 넣으십시요(3자이상 20자이하): "
	ForgeNameShortResponse      = "입력된 이름이 너무  짧습니다.\n이름을 다시 넣으십시요(3자이상 20자이하): "
	ForgeConfirmResponse        = "\n모든것에 만족하십니까? (예/아니오)\n: "
	ForgeGiveResponse           = "\n주인이 당신에게 새로 제작된 무기를 건네줍니다."
	ForgeTooPoorResponse        = "\n당신은 그 조건의 무기를  만들 만한 돈이 없습니다.\n주인이 당신에게 \"외상은 안되요~\"라고 말합니다."
	ForgeCancelResponse         = "무기 제련을 취소하였습니다."
)

var (
	ErrForgeActorAbsent              = errors.New("online canonical forge actor absent")
	ErrForgeFlagsUnresolved          = errors.New("canonical room flags required")
	ErrForgeStaleProposal            = errors.New("stale or invalid forge proposal")
	ErrForgeInvalidProposal          = errors.New("invalid forge proposal")
	ErrForgeNameInvalid              = errors.New("forge actor name is unsafe")
	ErrForgeNotReading               = errors.New("forge select_arm continuation is not active")
	ErrForgeCatalogUnmigrated        = errors.New("forge weapon catalog 900-904 is unmigrated")
	ErrForgeSelectArmInput           = errors.New("invalid forge select_arm input")
	ErrForgeGoldObjectUnmigrated     = errors.New("canonical gold and object graph required")
	ErrForgeItemAllocatorUnavailable = errors.New("forge item ID allocator required")
)

// ForgeEvent is one post-commit broadcast_rom line for select_arm case 6.
type ForgeEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

type ForgeResult struct {
	Action         ForgeAction  `json:"action"`
	ActorID        string       `json:"actor_id"`
	RoomID         int16        `json:"room_id"`
	Response       string       `json:"response"`
	Changed        bool         `json:"changed"`
	Continuation   int          `json:"continuation,omitempty"`
	NoBroadcast    bool         `json:"no_broadcast,omitempty"`
	Reading        bool         `json:"reading,omitempty"`
	WeaponChoice   int          `json:"weapon_choice,omitempty"`
	ObjectID       int16        `json:"object_id,omitempty"`
	ObjectName     string       `json:"object_name,omitempty"`
	MaterialChoice int          `json:"material_choice,omitempty"`
	DiceSides      int16        `json:"dice_sides,omitempty"`
	Sum            int32        `json:"sum,omitempty"`
	QuenchChoice   int          `json:"quench_choice,omitempty"`
	ShotsMax       int16        `json:"shots_max,omitempty"`
	ShotsCurrent   int16        `json:"shots_current,omitempty"`
	MaterialSum    int32        `json:"material_sum,omitempty"`
	ItemID         string       `json:"item_id,omitempty"`
	GoldAfter      int32        `json:"gold_after"`
	Events         []ForgeEvent `json:"events,omitempty"`
}

type ForgeProposal struct {
	Action         ForgeAction
	ActorID        string
	RoomID         int16
	Response       string
	Changed        bool
	Continuation   int
	BeforeFlags    [8]byte
	AfterFlags     [8]byte
	Input          string
	WeaponChoice   int
	ObjectID       int16
	MaterialChoice int
	DiceSides      int16
	Sum            int32
	MaterialSum    int32
	QuenchChoice   int
	ShotsMax       int16
	ShotsCurrent   int16
	WeaponName     string
	ItemID         string
	GoldAfter      int32
	Events         []ForgeEvent

	expectedActor      PlayerState
	expectedRoomFlags  [8]byte
	expectedObject     LegacyObject
	expectedAfterItems ItemCollection
	hasAfterItems      bool
}

func forgeFlag(flags [8]byte, bit uint) bool {
	return flag(flags[:], bit)
}

func forgeWithFlag(flags [8]byte, bit uint, on bool) [8]byte {
	idx := bit / 8
	if idx >= uint(len(flags)) {
		return flags
	}
	if on {
		flags[idx] |= 1 << (bit % 8)
	} else {
		flags[idx] &^= 1 << (bit % 8)
	}
	return flags
}

func validForgeActorName(name string) bool {
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

func forgeActor(s State, actorID string) (PlayerState, RoomState, error) {
	if err := s.Validate(); err != nil {
		if actor, ok := s.Players[actorID]; actorID != "" && ok {
			if _, roomOK := s.Rooms[actor.Body.RoomID]; !roomOK {
				return PlayerState{}, RoomState{}, ErrForgeFlagsUnresolved
			}
		}
		return PlayerState{}, RoomState{}, err
	}
	actor, ok := s.Players[actorID]
	if actorID == "" || !ok || !actor.Online || actor.Body.Type != 0 {
		return PlayerState{}, RoomState{}, ErrForgeActorAbsent
	}
	if !validForgeActorName(actor.Body.Name) {
		return PlayerState{}, RoomState{}, ErrForgeNameInvalid
	}
	room, ok := s.Rooms[actor.Body.RoomID]
	if !ok || room.Resource.ID != actor.Body.RoomID || !containsString(room.PlayerIDs, actorID) {
		return PlayerState{}, RoomState{}, ErrForgeFlagsUnresolved
	}
	return actor, room, nil
}

func forgeResult(p ForgeProposal) ForgeResult {
	return ForgeResult{
		Action: p.Action, ActorID: p.ActorID, RoomID: p.RoomID, Response: p.Response,
		Changed: p.Changed, Continuation: p.Continuation,
		NoBroadcast:    forgeFlag(p.AfterFlags, forgeNoBroadcastFlag),
		Reading:        forgeFlag(p.AfterFlags, forgeReadingFlag),
		WeaponChoice:   p.WeaponChoice,
		ObjectID:       p.ObjectID,
		ObjectName:     p.expectedObject.Name,
		MaterialChoice: p.MaterialChoice,
		DiceSides:      p.DiceSides,
		Sum:            p.Sum,
		QuenchChoice:   p.QuenchChoice,
		ShotsMax:       p.ShotsMax,
		ShotsCurrent:   p.ShotsCurrent,
		MaterialSum:    p.MaterialSum,
		ItemID:         p.ItemID,
		GoldAfter:      p.GoldAfter,
		Events:         append([]ForgeEvent(nil), p.Events...),
	}
}

func forgeProposalMatches(actual, expected ForgeProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID &&
		actual.RoomID == expected.RoomID && actual.Response == expected.Response &&
		actual.Changed == expected.Changed && actual.Continuation == expected.Continuation &&
		actual.BeforeFlags == expected.BeforeFlags && actual.AfterFlags == expected.AfterFlags &&
		actual.Input == expected.Input && actual.WeaponChoice == expected.WeaponChoice &&
		actual.ObjectID == expected.ObjectID &&
		actual.MaterialChoice == expected.MaterialChoice && actual.DiceSides == expected.DiceSides &&
		actual.Sum == expected.Sum && actual.MaterialSum == expected.MaterialSum &&
		actual.QuenchChoice == expected.QuenchChoice &&
		actual.ShotsMax == expected.ShotsMax && actual.ShotsCurrent == expected.ShotsCurrent &&
		actual.WeaponName == expected.WeaponName && actual.ItemID == expected.ItemID &&
		actual.GoldAfter == expected.GoldAfter && actual.hasAfterItems == expected.hasAfterItems &&
		actual.expectedRoomFlags == expected.expectedRoomFlags &&
		reflect.DeepEqual(actual.Events, expected.Events) &&
		reflect.DeepEqual(actual.expectedActor, expected.expectedActor) &&
		reflect.DeepEqual(actual.expectedObject, expected.expectedObject) &&
		reflect.DeepEqual(actual.expectedAfterItems, expected.expectedAfterItems)
}

func cloneForgeObject(object LegacyObject) LegacyObject {
	return cloneObjects([]LegacyObject{object})[0]
}

func validForgeSelectArmInput(input string) error {
	if !utf8.ValidString(input) {
		return ErrForgeSelectArmInput
	}
	for _, r := range input {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (unicode.IsSpace(r) && r != ' ') {
			return ErrForgeSelectArmInput
		}
	}
	return nil
}

func forgeWeaponChoice(input string) (int, bool) {
	if input == "" {
		return 0, false
	}
	ch := input[0]
	if ch < '1' || ch > '5' {
		return 0, false
	}
	return int(ch - '0'), true
}

func forgeMaterialChoice(input string) (int, bool) {
	if input == "" {
		return 0, false
	}
	ch := input[0]
	if ch >= 'A' && ch <= 'Z' {
		ch += 'a' - 'A'
	}
	if ch < '1' || ch > '3' {
		return 0, false
	}
	return int(ch - '0'), true
}

func forgeMaterialClassDenied(class byte) bool {
	return class == forgeClericClass || class == forgeMageClass || class == forgePaladinClass
}

func forgeGoldObjectGraphMigrated(actor PlayerState) error {
	if actor.Body.Gold < 0 {
		return ErrForgeGoldObjectUnmigrated
	}
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return ErrForgeGoldObjectUnmigrated
	}
	if err := actor.Items.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrForgeGoldObjectUnmigrated, err)
	}
	return nil
}

func forgeLoadWeaponTemplate(catalog SpawnCatalog, objectID int16) (LegacyObject, error) {
	if catalog == nil {
		return LegacyObject{}, ErrForgeCatalogUnmigrated
	}
	if objectID < forgeWeaponObjectBase || objectID > forgeWeaponObjectBase+4 {
		return LegacyObject{}, fmt.Errorf("%w: object %d", ErrForgeCatalogUnmigrated, objectID)
	}
	object, err := catalog.Object(objectID)
	if err != nil {
		return LegacyObject{}, fmt.Errorf("%w: object %d: %v", ErrForgeCatalogUnmigrated, objectID, err)
	}
	if err := validForgeWeaponTemplate(object); err != nil {
		return LegacyObject{}, fmt.Errorf("%w: object %d", err, objectID)
	}
	return cloneForgeObject(object), nil
}

func forgeDiamondClassFlags(flags [8]byte) [8]byte {
	flags = forgeWithFlag(flags, forgeClassSelectFlag, true)
	flags = forgeWithFlag(flags, forgeAssassinOnlyFlag, true)
	flags = forgeWithFlag(flags, forgeBarbarianOnlyFlag, true)
	flags = forgeWithFlag(flags, forgeFighterOnlyFlag, true)
	flags = forgeWithFlag(flags, forgeRangerOnlyFlag, true)
	flags = forgeWithFlag(flags, forgeThiefOnlyFlag, true)
	return flags
}

func ForgeWeaponObjectID(choice int) int16 {
	if choice < 1 || choice > 5 {
		return 0
	}
	return forgeWeaponObjectBase + int16(choice-1)
}

func validForgeWeaponTemplate(object LegacyObject) error {
	if object.Name == "" || !utf8.ValidString(object.Name) || strings.TrimSpace(object.Name) != object.Name {
		return ErrForgeCatalogUnmigrated
	}
	for _, r := range object.Name {
		if unicode.IsControl(r) {
			return ErrForgeCatalogUnmigrated
		}
	}
	if object.Weight < 0 {
		return ErrForgeCatalogUnmigrated
	}
	return nil
}

func cloneForgeActor(actor PlayerState) PlayerState {
	actor.Aliases = clonePlayerAliases(actor.Aliases)
	actor.PlayerEnemies = append([]string(nil), actor.PlayerEnemies...)
	actor.FollowerIDs = append([]string(nil), actor.FollowerIDs...)
	if actor.NPCFollowerIDs != nil {
		actor.NPCFollowerIDs = append([]string(nil), actor.NPCFollowerIDs...)
	}
	if actor.FollowerRefs != nil {
		actor.FollowerRefs = append([]EntityRef(nil), actor.FollowerRefs...)
	}
	actor.Body.Inventory = cloneObjects(actor.Body.Inventory)
	if actor.Items != nil {
		items := actor.Items.clone()
		actor.Items = &items
	}
	return actor
}

// PlanForge is command7.c:forge's admission. Missing RFORGE is the original
// typed no-op print. A present RFORGE starts select_arm case 1: PREADI is
// set for the weapon-type prompt and the RETURN param is 2. C F_SET(PNOBRD)
// only wraps the case-1 print; F_CLR runs before DOPROMPT, so the receipt
// must not leave the actor muted. Unmigrated/absent room flags fail closed.
func (s State) PlanForge(actorID string) (ForgeProposal, error) {
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, BeforeFlags: actor.Body.Flags,
		AfterFlags: actor.Body.Flags, expectedActor: cloneForgeActor(actor),
		expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, true)
	after = forgeWithFlag(after, forgeNoBroadcastFlag, false)
	p.Action = ForgePrompt
	p.Response = ForgePromptResponse
	p.Changed = true
	p.Continuation = forgeSelectArmPhase
	p.AfterFlags = after
	return p, nil
}

// ApplyForge atomically writes the select_arm start flags. A not-forge
// candidate is a typed no-op. Replay identity is owned by the receipt;
// a stale snapshot cannot set PREADI or clear PNOBRD after the actor has moved.
func (s State) ApplyForge(p ForgeProposal) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForge(p.ActorID)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	next := s.clone()
	if p.Changed {
		if p.Action != ForgePrompt || p.Continuation != forgeSelectArmPhase || p.Response != ForgePromptResponse {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
		actor := next.Players[p.ActorID]
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// Forge is the reducer-shaped convenience API for the 제련 start.
func (s State) Forge(actorID string) (State, ForgeResult, error) {
	p, err := s.PlanForge(actorID)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForge(p)
}

// PlanForgeSelectArm is command7.c:select_arm case 2. C inspects str[0]:
// digits 1-5 load_obj(900+choice-1) and RETURN param 3 with the material
// prompt; any other first byte re-prompts "하나를 선택하시오: " at param 2.
// load_obj failure is fail-closed here instead of the original Error print.
// Gold is not charged in this phase. PREADI stays set; C F_CLR/F_SET nets
// to the same persisted flag. Case 3 is PlanForgeSelectMaterial.
func (s State) PlanForgeSelectArm(actorID, input string, catalog SpawnCatalog) (ForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return ForgeProposal{}, err
	}
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, forgeReadingFlag) {
		return ForgeProposal{}, ErrForgeNotReading
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeWeaponChoice(input)
	if !ok {
		p.Action = ForgeReprompt
		p.Response = ForgeRepromptResponse
		p.Continuation = forgeSelectArmPhase
		return p, nil
	}
	if catalog == nil {
		return ForgeProposal{}, ErrForgeCatalogUnmigrated
	}
	objectID := ForgeWeaponObjectID(choice)
	object, err := catalog.Object(objectID)
	if err != nil {
		return ForgeProposal{}, fmt.Errorf("%w: object %d: %v", ErrForgeCatalogUnmigrated, objectID, err)
	}
	if err := validForgeWeaponTemplate(object); err != nil {
		return ForgeProposal{}, fmt.Errorf("%w: object %d", err, objectID)
	}
	p.Action = ForgeMaterial
	p.Response = ForgeMaterialResponse
	p.Continuation = forgeMaterialPhase
	p.WeaponChoice = choice
	p.ObjectID = objectID
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func forgeSelectArmActionValid(p ForgeProposal) bool {
	switch p.Action {
	case ForgeReprompt:
		return !p.Changed && p.Continuation == forgeSelectArmPhase &&
			p.Response == ForgeRepromptResponse && p.WeaponChoice == 0 && p.ObjectID == 0
	case ForgeMaterial:
		return !p.Changed && p.Continuation == forgeMaterialPhase &&
			p.Response == ForgeMaterialResponse &&
			p.WeaponChoice >= 1 && p.WeaponChoice <= 5 &&
			p.ObjectID == ForgeWeaponObjectID(p.WeaponChoice)
	case ForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.Response == ForgeNotForgeResponse
	default:
		return false
	}
}

// ApplyForgeSelectArm records the case-2 receipt. The loaded template is
// receipt identity only (C forge1[fd] pointer); inventory and gold are
// unchanged. A stale snapshot or a gold/flag drift cannot invent a weapon.
func (s State) ApplyForgeSelectArm(p ForgeProposal, catalog SpawnCatalog) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForgeSelectArm(p.ActorID, p.Input, catalog)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	if !forgeSelectArmActionValid(p) {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// ForgeSelectArm is the reducer-shaped convenience API for select_arm case 2.
func (s State) ForgeSelectArm(actorID, input string, catalog SpawnCatalog) (State, ForgeResult, error) {
	p, err := s.PlanForgeSelectArm(actorID, input, catalog)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForgeSelectArm(p, catalog)
}

// PlanForgeSelectMaterial is command7.c:select_arm case 3. C uses low(str[0]):
// 1 강철 sdice=3 sum=50000, 2 귀금속 sdice=4 sum=200000, 3 금강석 sdice=5
// sum=300000 plus OCLSEL class bits. Cleric/Paladin/Mage cannot pick 3 and
// re-prompt at param 3. Any other first byte re-prompts "하나를 선택하시오: ".
// Gold is not charged here. Unmigrated gold or item graph fails closed
// before a receipt. Case 4 is PlanForgeSelectQuench. Case 6 stays
// fail-closed.
func (s State) PlanForgeSelectMaterial(actorID, input string, catalog SpawnCatalog, objectID int16) (ForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return ForgeProposal{}, err
	}
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, forgeReadingFlag) {
		return ForgeProposal{}, ErrForgeNotReading
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return ForgeProposal{}, err
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeMaterialChoice(input)
	if !ok {
		p.Action = ForgeReprompt
		p.Response = ForgeRepromptResponse
		p.Continuation = forgeMaterialPhase
		p.ObjectID = 0
		return p, nil
	}
	object, err := forgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return ForgeProposal{}, err
	}
	p.WeaponChoice = int(objectID - forgeWeaponObjectBase + 1)
	p.expectedObject = object
	if choice == 3 && forgeMaterialClassDenied(actor.Body.Class) {
		p.Action = ForgeMaterialDenied
		p.Response = ForgeMaterialDeniedResponse
		p.Continuation = forgeMaterialPhase
		p.MaterialChoice = 3
		return p, nil
	}
	switch choice {
	case 1:
		object.DiceSides = forgeSteelDice
		p.Sum = forgeSteelCost
	case 2:
		object.DiceSides = forgePreciousDice
		p.Sum = forgePreciousCost
	default:
		object.Flags = forgeDiamondClassFlags(object.Flags)
		object.DiceSides = forgeDiamondDice
		p.Sum = forgeDiamondCost
	}
	p.Action = ForgeQuench
	p.Response = ForgeQuenchResponse
	p.Continuation = forgeQuenchPhase
	p.MaterialChoice = choice
	p.DiceSides = object.DiceSides
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func forgeSelectMaterialActionValid(p ForgeProposal) bool {
	switch p.Action {
	case ForgeReprompt:
		return !p.Changed && p.Continuation == forgeMaterialPhase &&
			p.Response == ForgeRepromptResponse && p.MaterialChoice == 0 &&
			p.DiceSides == 0 && p.Sum == 0 && p.WeaponChoice == 0 && p.ObjectID == 0
	case ForgeMaterialDenied:
		return !p.Changed && p.Continuation == forgeMaterialPhase &&
			p.Response == ForgeMaterialDeniedResponse && p.MaterialChoice == 3 &&
			p.DiceSides == 0 && p.Sum == 0 &&
			p.WeaponChoice >= 1 && p.WeaponChoice <= 5 &&
			p.ObjectID == ForgeWeaponObjectID(p.WeaponChoice)
	case ForgeQuench:
		if p.Changed || p.Continuation != forgeQuenchPhase || p.Response != ForgeQuenchResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != ForgeWeaponObjectID(p.WeaponChoice) ||
			p.DiceSides != p.expectedObject.DiceSides || p.Sum <= 0 {
			return false
		}
		switch p.MaterialChoice {
		case 1:
			return p.DiceSides == forgeSteelDice && p.Sum == forgeSteelCost
		case 2:
			return p.DiceSides == forgePreciousDice && p.Sum == forgePreciousCost
		case 3:
			return p.DiceSides == forgeDiamondDice && p.Sum == forgeDiamondCost &&
				forgeFlag(p.expectedObject.Flags, forgeClassSelectFlag)
		default:
			return false
		}
	case ForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceSides == 0 && p.Sum == 0 &&
			p.Response == ForgeNotForgeResponse
	default:
		return false
	}
}

// ApplyForgeSelectMaterial records the case-3 receipt. sdice/sum live on the
// loaded template identity (C forge1/forge2); inventory and gold are
// unchanged. A stale snapshot or gold/graph drift cannot invent a weapon.
func (s State) ApplyForgeSelectMaterial(p ForgeProposal, catalog SpawnCatalog) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForgeSelectMaterial(p.ActorID, p.Input, catalog, p.ObjectID)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	if !forgeSelectMaterialActionValid(p) {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return State{}, ForgeResult{}, err
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
		return State{}, ForgeResult{}, ErrForgeGoldObjectUnmigrated
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// ForgeSelectMaterial is the reducer-shaped convenience API for select_arm case 3.
func (s State) ForgeSelectMaterial(actorID, input string, catalog SpawnCatalog, objectID int16) (State, ForgeResult, error) {
	p, err := s.PlanForgeSelectMaterial(actorID, input, catalog, objectID)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForgeSelectMaterial(p, catalog)
}

func forgeQuenchChoice(input string) (int, bool) {
	if input == "" {
		return 0, false
	}
	ch := input[0]
	if ch >= 'A' && ch <= 'Z' {
		ch += 'a' - 'A'
	}
	if ch < '1' || ch > '5' {
		return 0, false
	}
	return int(ch - '0'), true
}

func forgeMaterialSumKnown(sum int32) (dice int16, choice int, ok bool) {
	switch sum {
	case forgeSteelCost:
		return forgeSteelDice, 1, true
	case forgePreciousCost:
		return forgePreciousDice, 2, true
	case forgeDiamondCost:
		return forgeDiamondDice, 3, true
	default:
		return 0, 0, false
	}
}

func forgeQuenchSpec(choice int) (shots int16, cost int32, ok bool) {
	switch choice {
	case 1:
		return forgeQuench100Shots, forgeQuench100Cost, true
	case 2:
		return forgeQuench200Shots, forgeQuench200Cost, true
	case 3:
		return forgeQuench300Shots, forgeQuench300Cost, true
	case 4:
		return forgeQuench400Shots, forgeQuench400Cost, true
	case 5:
		return forgeQuench500Shots, forgeQuench500Cost, true
	default:
		return 0, 0, false
	}
}

func forgeApplyMaterialIdentity(object LegacyObject, materialSum int32) (LegacyObject, int, int16, error) {
	dice, choice, ok := forgeMaterialSumKnown(materialSum)
	if !ok {
		return LegacyObject{}, 0, 0, ErrForgeGoldObjectUnmigrated
	}
	object.DiceSides = dice
	if choice == 3 {
		object.Flags = forgeDiamondClassFlags(object.Flags)
	}
	return object, choice, dice, nil
}

// PlanForgeSelectQuench is command7.c:select_arm case 4. C uses low(str[0]):
// 1 100 shots +50000, 2 200 +200000, 3 300 +500000, 4 400 +1000000,
// 5 500 +2000000. Any other first byte re-prompts "하나를 선택하시오: "
// at param 4. Gold is not charged here. Unmigrated gold, item graph, or
// a non-material forge2 sum fails closed before a receipt. Case 5 is
// PlanForgeSelectName. Case 6 stays fail-closed.
func (s State) PlanForgeSelectQuench(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32) (ForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return ForgeProposal{}, err
	}
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, forgeReadingFlag) {
		return ForgeProposal{}, ErrForgeNotReading
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return ForgeProposal{}, err
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeQuenchChoice(input)
	if !ok {
		p.Action = ForgeReprompt
		p.Response = ForgeRepromptResponse
		p.Continuation = forgeQuenchPhase
		p.ObjectID = 0
		return p, nil
	}
	shots, cost, ok := forgeQuenchSpec(choice)
	if !ok {
		return ForgeProposal{}, ErrForgeInvalidProposal
	}
	object, err := forgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return ForgeProposal{}, err
	}
	object, materialChoice, dice, err := forgeApplyMaterialIdentity(object, materialSum)
	if err != nil {
		return ForgeProposal{}, err
	}
	object.ShotsMax = shots
	object.ShotsCurrent = shots
	p.Action = ForgeName
	p.Response = ForgeNameResponse
	p.Continuation = forgeNamePhase
	p.WeaponChoice = int(objectID - forgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceSides = dice
	p.MaterialSum = materialSum
	p.Sum = materialSum + cost
	p.QuenchChoice = choice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func forgeSelectQuenchActionValid(p ForgeProposal) bool {
	switch p.Action {
	case ForgeReprompt:
		return !p.Changed && p.Continuation == forgeQuenchPhase &&
			p.Response == ForgeRepromptResponse && p.QuenchChoice == 0 &&
			p.ShotsMax == 0 && p.ShotsCurrent == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.WeaponChoice == 0 && p.ObjectID == 0
	case ForgeName:
		shots, cost, ok := forgeQuenchSpec(p.QuenchChoice)
		if !ok || p.Changed || p.Continuation != forgeNamePhase || p.Response != ForgeNameResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != ForgeWeaponObjectID(p.WeaponChoice) ||
			p.ShotsMax != shots || p.ShotsCurrent != shots ||
			p.expectedObject.ShotsMax != shots || p.expectedObject.ShotsCurrent != shots ||
			p.DiceSides != p.expectedObject.DiceSides ||
			p.MaterialSum <= 0 || p.Sum != p.MaterialSum+cost {
			return false
		}
		_, materialChoice, known := forgeMaterialSumKnown(p.MaterialSum)
		return known && p.MaterialChoice == materialChoice
	case ForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceSides == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 && p.ShotsCurrent == 0 &&
			p.Response == ForgeNotForgeResponse
	default:
		return false
	}
}

// ApplyForgeSelectQuench records the case-4 receipt. shots/sum live on the
// loaded template identity (C forge1/forge2); inventory and gold are
// unchanged. A stale snapshot or gold/graph drift cannot invent a weapon.
func (s State) ApplyForgeSelectQuench(p ForgeProposal, catalog SpawnCatalog) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForgeSelectQuench(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	if !forgeSelectQuenchActionValid(p) {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return State{}, ForgeResult{}, err
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
		return State{}, ForgeResult{}, ErrForgeGoldObjectUnmigrated
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// ForgeSelectQuench is the reducer-shaped convenience API for select_arm case 4.
func (s State) ForgeSelectQuench(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32) (State, ForgeResult, error) {
	p, err := s.PlanForgeSelectQuench(actorID, input, catalog, objectID, materialSum)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForgeSelectQuench(p, catalog)
}

func forgeNameReprompt(input string) (response string, ok bool) {
	if strings.ContainsAny(input, "()") {
		return ForgeNameParenResponse, true
	}
	if len(input) > forgeWeaponNameMaxBytes {
		return ForgeNameLongResponse, true
	}
	if len(input) < forgeWeaponNameMinBytes {
		return ForgeNameShortResponse, true
	}
	return "", false
}

func forgeApplyQuenchIdentity(object LegacyObject, materialSum int32, quenchChoice int) (LegacyObject, int, int16, int16, int32, error) {
	object, materialChoice, dice, err := forgeApplyMaterialIdentity(object, materialSum)
	if err != nil {
		return LegacyObject{}, 0, 0, 0, 0, err
	}
	shots, cost, ok := forgeQuenchSpec(quenchChoice)
	if !ok {
		return LegacyObject{}, 0, 0, 0, 0, ErrForgeGoldObjectUnmigrated
	}
	object.ShotsMax = shots
	object.ShotsCurrent = shots
	return object, materialChoice, dice, shots, materialSum + cost, nil
}

// PlanForgeSelectName is command7.c:select_arm case 5. C copies str onto
// obj_ptr->name after rejecting parentheses and strlen outside 3-20.
// Those re-prompts stay at param 5. A valid name prints the confirm
// prompt and RETURN param 6. Gold is not charged here. Unmigrated gold,
// item graph, catalog, or quench identity fails closed before a receipt.
// C F_CLR(PREADI) then only F_SET on the too-short and success paths;
// the Go receipt keeps PREADI set so the connection-local RETURN can
// continue. Case 6 is PlanForgeSelectConfirm.
func (s State) PlanForgeSelectName(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (ForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return ForgeProposal{}, err
	}
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, forgeReadingFlag) {
		return ForgeProposal{}, ErrForgeNotReading
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return ForgeProposal{}, err
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, true)
	p.AfterFlags = after
	if response, invalid := forgeNameReprompt(input); invalid {
		p.Action = ForgeReprompt
		p.Response = response
		p.Continuation = forgeNamePhase
		p.ObjectID = 0
		return p, nil
	}
	object, err := forgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return ForgeProposal{}, err
	}
	object, materialChoice, dice, shots, sum, err := forgeApplyQuenchIdentity(object, materialSum, quenchChoice)
	if err != nil {
		return ForgeProposal{}, err
	}
	object.Name = input
	p.Action = ForgeConfirm
	p.Response = ForgeConfirmResponse
	p.Continuation = forgeConfirmPhase
	p.WeaponChoice = int(objectID - forgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceSides = dice
	p.MaterialSum = materialSum
	p.Sum = sum
	p.QuenchChoice = quenchChoice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func forgeSelectNameActionValid(p ForgeProposal) bool {
	switch p.Action {
	case ForgeReprompt:
		return !p.Changed && p.Continuation == forgeNamePhase &&
			(p.Response == ForgeNameParenResponse || p.Response == ForgeNameLongResponse || p.Response == ForgeNameShortResponse) &&
			p.QuenchChoice == 0 && p.ShotsMax == 0 && p.ShotsCurrent == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.WeaponChoice == 0 && p.ObjectID == 0
	case ForgeConfirm:
		shots, cost, ok := forgeQuenchSpec(p.QuenchChoice)
		if !ok || p.Changed || p.Continuation != forgeConfirmPhase || p.Response != ForgeConfirmResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != ForgeWeaponObjectID(p.WeaponChoice) ||
			p.ShotsMax != shots || p.ShotsCurrent != shots ||
			p.expectedObject.ShotsMax != shots || p.expectedObject.ShotsCurrent != shots ||
			p.expectedObject.Name != p.Input || p.DiceSides != p.expectedObject.DiceSides ||
			p.MaterialSum <= 0 || p.Sum != p.MaterialSum+cost {
			return false
		}
		if _, invalid := forgeNameReprompt(p.Input); invalid {
			return false
		}
		_, materialChoice, known := forgeMaterialSumKnown(p.MaterialSum)
		return known && p.MaterialChoice == materialChoice
	case ForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceSides == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 && p.ShotsCurrent == 0 &&
			p.Response == ForgeNotForgeResponse
	default:
		return false
	}
}

// ApplyForgeSelectName records the case-5 receipt. The renamed template is
// receipt identity only (C forge1 pointer); inventory and gold are
// unchanged. A stale snapshot or gold/graph drift cannot invent a weapon
// or charge gold. Case 6 is PlanForgeSelectConfirm.
func (s State) ApplyForgeSelectName(p ForgeProposal, catalog SpawnCatalog) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForgeSelectName(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum, p.QuenchChoice)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	if !forgeSelectNameActionValid(p) {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return State{}, ForgeResult{}, err
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
		return State{}, ForgeResult{}, ErrForgeGoldObjectUnmigrated
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// ForgeSelectName is the reducer-shaped convenience API for select_arm case 5.
func (s State) ForgeSelectName(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (State, ForgeResult, error) {
	p, err := s.PlanForgeSelectName(actorID, input, catalog, objectID, materialSum, quenchChoice)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForgeSelectName(p, catalog)
}

func forgeConfirmYes(input string) bool {
	return strings.HasPrefix(input, "예")
}

func ForgeBroadcastText(actorName string) string {
	return fmt.Sprintf("\n%s이   무기를  제련하였습니다.", actorName)
}

func forgeApplyNameIdentity(object LegacyObject, materialSum int32, quenchChoice int, name string) (LegacyObject, int, int16, int16, int32, error) {
	if response, invalid := forgeNameReprompt(name); invalid {
		return LegacyObject{}, 0, 0, 0, 0, fmt.Errorf("%w: %s", ErrForgeGoldObjectUnmigrated, response)
	}
	object, materialChoice, dice, shots, sum, err := forgeApplyQuenchIdentity(object, materialSum, quenchChoice)
	if err != nil {
		return LegacyObject{}, 0, 0, 0, 0, err
	}
	object.Name = name
	return object, materialChoice, dice, shots, sum, nil
}

func forgeItemIDs(s State) map[string]struct{} {
	used := map[string]struct{}{}
	add := func(items *ItemCollection) {
		if items == nil {
			return
		}
		for id := range items.Items {
			used[id] = struct{}{}
		}
	}
	for _, room := range s.Rooms {
		add(room.Items)
	}
	for _, player := range s.Players {
		add(player.Items)
	}
	for _, account := range s.BankAccounts {
		add(account.Items)
	}
	for _, npc := range s.NPCs {
		add(npc.Items)
	}
	return used
}

func forgeGiveWeapon(s State, actor PlayerState, object LegacyObject, allocate func() (string, error)) (ItemCollection, string, error) {
	if allocate == nil {
		return ItemCollection{}, "", ErrForgeItemAllocatorUnavailable
	}
	used := forgeItemIDs(s)
	uniqueAllocate := func() (string, error) {
		id, err := allocate()
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("%w: empty item ID", ErrForgeItemAllocatorUnavailable)
		}
		if _, exists := used[id]; exists {
			return "", fmt.Errorf("%w: duplicate item ID", ErrForgeItemAllocatorUnavailable)
		}
		used[id] = struct{}{}
		return id, nil
	}
	reward, err := ImportItems([]LegacyObject{cloneForgeObject(object)}, uniqueAllocate)
	if err != nil {
		return ItemCollection{}, "", fmt.Errorf("%w: %v", ErrForgeItemAllocatorUnavailable, err)
	}
	if actor.Items == nil {
		return ItemCollection{}, "", ErrForgeGoldObjectUnmigrated
	}
	plan, err := TransferItemRoots(reward, *actor.Items, reward.Inventory)
	if err != nil {
		return ItemCollection{}, "", err
	}
	return plan.Destination, reward.Inventory[0], nil
}

// PlanForgeSelectConfirm is command7.c:select_arm case 6. C F_CLR(PREADI)
// then strncmp(str,"예",2): gold < sum prints the credit refusal and frees
// the object; otherwise add_obj_crt, print the hand-off, broadcast_rom,
// and gold -= sum. Any other first bytes cancel and free the object.
// Unmigrated gold, item graph, catalog, name, or quench identity fails
// closed before a receipt. newforge/`무기만들기` is not this path.
func (s State) PlanForgeSelectConfirm(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string, allocate func() (string, error)) (ForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return ForgeProposal{}, err
	}
	actor, room, err := forgeActor(s, actorID)
	if err != nil {
		return ForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, forgeReadingFlag) {
		return ForgeProposal{}, ErrForgeNotReading
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return ForgeProposal{}, err
	}
	p := ForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
		WeaponName: weaponName, GoldAfter: actor.Body.Gold,
	}
	if !forgeFlag(room.Resource.Flags, forgeRoomFlag) {
		p.Action = ForgeNotForge
		p.Response = ForgeNotForgeResponse
		p.ObjectID = 0
		p.WeaponName = ""
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, forgeReadingFlag, false)
	p.AfterFlags = after
	p.Changed = true
	object, err := forgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return ForgeProposal{}, err
	}
	object, materialChoice, dice, shots, sum, err := forgeApplyNameIdentity(object, materialSum, quenchChoice, weaponName)
	if err != nil {
		return ForgeProposal{}, err
	}
	p.WeaponChoice = int(objectID - forgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceSides = dice
	p.MaterialSum = materialSum
	p.Sum = sum
	p.QuenchChoice = quenchChoice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	if !forgeConfirmYes(input) {
		p.Action = ForgeCancel
		p.Response = ForgeCancelResponse
		return p, nil
	}
	if actor.Body.Gold < sum {
		p.Action = ForgeTooPoor
		p.Response = ForgeTooPoorResponse
		return p, nil
	}
	afterItems, itemID, err := forgeGiveWeapon(s, actor, object, allocate)
	if err != nil {
		return ForgeProposal{}, err
	}
	p.Action = ForgeGive
	p.Response = ForgeGiveResponse
	p.ItemID = itemID
	p.GoldAfter = actor.Body.Gold - sum
	p.expectedAfterItems = afterItems.clone()
	p.hasAfterItems = true
	p.Events = []ForgeEvent{{
		RoomID: p.RoomID, ActorID: actorID, ActorName: actor.Body.Name,
		ExcludeActorID: actorID, Text: ForgeBroadcastText(actor.Body.Name),
	}}
	return p, nil
}

func forgeSelectConfirmActionValid(p ForgeProposal) bool {
	readingCleared := p.Changed && !forgeFlag(p.AfterFlags, forgeReadingFlag) &&
		forgeFlag(p.BeforeFlags, forgeReadingFlag)
	identityOK := p.WeaponChoice >= 1 && p.WeaponChoice <= 5 &&
		p.ObjectID == ForgeWeaponObjectID(p.WeaponChoice) &&
		p.WeaponName == p.expectedObject.Name && p.expectedObject.Name != "" &&
		p.ShotsMax == p.expectedObject.ShotsMax && p.ShotsCurrent == p.expectedObject.ShotsCurrent &&
		p.DiceSides == p.expectedObject.DiceSides
	if identityOK {
		shots, cost, ok := forgeQuenchSpec(p.QuenchChoice)
		_, materialChoice, known := forgeMaterialSumKnown(p.MaterialSum)
		identityOK = ok && known && p.MaterialChoice == materialChoice &&
			p.ShotsMax == shots && p.ShotsCurrent == shots &&
			p.MaterialSum > 0 && p.Sum == p.MaterialSum+cost
		if _, invalid := forgeNameReprompt(p.WeaponName); invalid {
			identityOK = false
		}
	}
	switch p.Action {
	case ForgeCancel:
		return readingCleared && identityOK && p.Continuation == 0 &&
			p.Response == ForgeCancelResponse && p.ItemID == "" &&
			!p.hasAfterItems && len(p.Events) == 0 &&
			p.GoldAfter == p.expectedActor.Body.Gold && !forgeConfirmYes(p.Input)
	case ForgeTooPoor:
		return readingCleared && identityOK && p.Continuation == 0 &&
			p.Response == ForgeTooPoorResponse && p.ItemID == "" &&
			!p.hasAfterItems && len(p.Events) == 0 &&
			p.GoldAfter == p.expectedActor.Body.Gold && forgeConfirmYes(p.Input) &&
			p.expectedActor.Body.Gold < p.Sum
	case ForgeGive:
		if !readingCleared || !identityOK || p.Continuation != 0 ||
			p.Response != ForgeGiveResponse || p.ItemID == "" || !p.hasAfterItems ||
			len(p.Events) != 1 || !forgeConfirmYes(p.Input) ||
			p.GoldAfter != p.expectedActor.Body.Gold-p.Sum ||
			p.expectedActor.Body.Gold < p.Sum {
			return false
		}
		item, ok := p.expectedAfterItems.Items[p.ItemID]
		if !ok || item.Object.Name != p.WeaponName ||
			!containsString(p.expectedAfterItems.Inventory, p.ItemID) ||
			item.Object.DiceSides != p.DiceSides ||
			item.Object.ShotsMax != p.ShotsMax || item.Object.ShotsCurrent != p.ShotsCurrent {
			return false
		}
		event := p.Events[0]
		return event.RoomID == p.RoomID && event.ActorID == p.ActorID &&
			event.ExcludeActorID == p.ActorID && event.Text == ForgeBroadcastText(p.expectedActor.Body.Name)
	case ForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceSides == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 && p.ShotsCurrent == 0 &&
			p.WeaponName == "" && p.ItemID == "" && !p.hasAfterItems && len(p.Events) == 0 &&
			p.Response == ForgeNotForgeResponse
	default:
		return false
	}
}

// ApplyForgeSelectConfirm records the case-6 receipt. A yes with enough gold
// charges gold and inserts the named weapon in one candidate. Replay identity
// is owned by the receipt; a stale snapshot cannot charge twice or mint a
// second object. Cancel and insufficient gold clear PREADI without touching
// gold or inventory.
func (s State) ApplyForgeSelectConfirm(p ForgeProposal, catalog SpawnCatalog, allocate func() (string, error)) (State, ForgeResult, error) {
	if p.ActorID == "" {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	expected, err := s.PlanForgeSelectConfirm(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum, p.QuenchChoice, p.WeaponName, allocate)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	if !forgeProposalMatches(p, expected) {
		return State{}, ForgeResult{}, ErrForgeStaleProposal
	}
	if !forgeSelectConfirmActionValid(p) {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := forgeGoldObjectGraphMigrated(actor); err != nil {
		return State{}, ForgeResult{}, err
	}
	switch p.Action {
	case ForgeGive:
		if actor.Body.Gold < p.Sum || p.GoldAfter != actor.Body.Gold-p.Sum || !p.hasAfterItems {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
		actor.Body.Flags = p.AfterFlags
		actor.Body.Gold = p.GoldAfter
		items := p.expectedAfterItems.clone()
		actor.Items = &items
		next.Players[p.ActorID] = actor
		if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold-p.Sum {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
		if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
			return State{}, ForgeResult{}, ErrForgeGoldObjectUnmigrated
		}
		if _, ok := next.Players[p.ActorID].Items.Items[p.ItemID]; !ok {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
	case ForgeCancel, ForgeTooPoor:
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
		if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
		if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
			return State{}, ForgeResult{}, ErrForgeGoldObjectUnmigrated
		}
	case ForgeNotForge:
		if p.Changed {
			return State{}, ForgeResult{}, ErrForgeInvalidProposal
		}
	default:
		return State{}, ForgeResult{}, ErrForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, ForgeResult{}, err
	}
	return next, forgeResult(p), nil
}

// ForgeSelectConfirm is the reducer-shaped convenience API for select_arm case 6.
func (s State) ForgeSelectConfirm(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string, allocate func() (string, error)) (State, ForgeResult, error) {
	p, err := s.PlanForgeSelectConfirm(actorID, input, catalog, objectID, materialSum, quenchChoice, weaponName, allocate)
	if err != nil {
		return State{}, ForgeResult{}, err
	}
	return s.ApplyForgeSelectConfirm(p, catalog, allocate)
}
