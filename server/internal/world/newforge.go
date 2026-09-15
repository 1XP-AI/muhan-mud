package world

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// newforge is the bounded Go port of command7.c:newforge (cmdlist-85
// `무기만들기`). Distinct from forge/`제련`. This slice covers the C first
// prompt/gate (RFORGE, room 611, select_newarm case 1), select_newarm
// case 2 (digits 1-5 load_obj 900-904, then the newforge material prompt),
// select_newarm case 3 (에메랄드/티타늄/일루션 set OENCHA+ndice/sdice/pdice
// and forge2 sum, then the newforge quench prompt), select_newarm case 4
// (담금질 1-5 set shotsmax/shotscur and add to forge2, then the name
// prompt), select_newarm case 5 (weapon name after quench: 3-20
// bytes, no parentheses, then the confirm prompt), and select_newarm
// case 6 (예 prefix charges gold and add_obj_crt; otherwise cancel).
// C F_SET(PNOBRD) only wraps the case-1 print; F_CLR runs before DOPROMPT.
// Distinct from 제련 select_arm case 6 (steel/precious/diamond costs).
const (
	newForgeRoomFlag                 = forgeRoomFlag        // RFORGE
	newForgeNoBroadcastFlag          = forgeNoBroadcastFlag // PNOBRD
	newForgeReadingFlag              = forgeReadingFlag     // PREADI
	newForgeEnchantedFlag      uint  = 14                   // OENCHA
	newForgeSelectArmPhase           = 2                    // select_newarm RETURN param after case 1
	newForgeMaterialPhase            = 3                    // select_newarm RETURN param after case 2
	newForgeQuenchPhase              = 4                    // select_newarm RETURN param after case 3
	newForgeNamePhase                = 5                    // select_newarm RETURN param after case 4
	newForgeConfirmPhase             = 6                    // select_newarm RETURN param after case 5
	newForgeWeaponNameMinBytes       = 3
	newForgeWeaponNameMaxBytes       = 20
	newForgeWeaponObjectBase   int16 = 900
	newForgeRoomID             int16 = 611
	newForgeEmeraldCost        int32 = 1000000
	newForgeTitaniumCost       int32 = 2000000
	newForgeIllusionCost       int32 = 3000000
	newForgeEmeraldDiceCount   int16 = 4
	newForgeTitaniumDiceCount  int16 = 5
	newForgeIllusionDiceCount  int16 = 6
	newForgeMaterialDiceSides  int16 = 5
	newForgeMaterialDicePlus   int16 = 5
	newForgeQuench100Shots     int16 = 100
	newForgeQuench200Shots     int16 = 200
	newForgeQuench300Shots     int16 = 300
	newForgeQuench400Shots     int16 = 400
	newForgeQuench500Shots     int16 = 500
	newForgeQuench100Cost      int32 = 1000000
	newForgeQuench200Cost      int32 = 2000000
	newForgeQuench300Cost      int32 = 3000000
	newForgeQuench400Cost      int32 = 4000000
	newForgeQuench500Cost      int32 = 5000000
)

const (
	NewForgeRoomFlag           = newForgeRoomFlag
	NewForgeNoBroadcastFlag    = newForgeNoBroadcastFlag
	NewForgeReadingFlag        = newForgeReadingFlag
	NewForgeEnchantedFlag      = newForgeEnchantedFlag
	NewForgeSelectArmPhase     = newForgeSelectArmPhase
	NewForgeMaterialPhase      = newForgeMaterialPhase
	NewForgeQuenchPhase        = newForgeQuenchPhase
	NewForgeNamePhase          = newForgeNamePhase
	NewForgeConfirmPhase       = newForgeConfirmPhase
	NewForgeWeaponNameMinBytes = newForgeWeaponNameMinBytes
	NewForgeWeaponNameMaxBytes = newForgeWeaponNameMaxBytes
	NewForgeWeaponObjectBase   = newForgeWeaponObjectBase
	NewForgeRoomID             = newForgeRoomID
	NewForgeEmeraldCost        = newForgeEmeraldCost
	NewForgeTitaniumCost       = newForgeTitaniumCost
	NewForgeIllusionCost       = newForgeIllusionCost
	NewForgeEmeraldDiceCount   = newForgeEmeraldDiceCount
	NewForgeTitaniumDiceCount  = newForgeTitaniumDiceCount
	NewForgeIllusionDiceCount  = newForgeIllusionDiceCount
	NewForgeMaterialDiceSides  = newForgeMaterialDiceSides
	NewForgeMaterialDicePlus   = newForgeMaterialDicePlus
	NewForgeQuench100Shots     = newForgeQuench100Shots
	NewForgeQuench200Shots     = newForgeQuench200Shots
	NewForgeQuench300Shots     = newForgeQuench300Shots
	NewForgeQuench400Shots     = newForgeQuench400Shots
	NewForgeQuench500Shots     = newForgeQuench500Shots
	NewForgeQuench100Cost      = newForgeQuench100Cost
	NewForgeQuench200Cost      = newForgeQuench200Cost
	NewForgeQuench300Cost      = newForgeQuench300Cost
	NewForgeQuench400Cost      = newForgeQuench400Cost
	NewForgeQuench500Cost      = newForgeQuench500Cost
)

type NewForgeAction string

const (
	NewForgeNotForge  NewForgeAction = "not_forge"
	NewForgeWrongRoom NewForgeAction = "wrong_room"
	NewForgePrompt    NewForgeAction = "prompt"
	NewForgeReprompt  NewForgeAction = "reprompt"
	NewForgeMaterial  NewForgeAction = "material"
	NewForgeQuench    NewForgeAction = "quench"
	NewForgeName      NewForgeAction = "name"
	NewForgeConfirm   NewForgeAction = "confirm"
	NewForgeGive      NewForgeAction = "give"
	NewForgeTooPoor   NewForgeAction = "too_poor"
	NewForgeCancel    NewForgeAction = "cancel"
)

const (
	NewForgeNotForgeResponse  = "여기는 대장간이 아닙니다."
	NewForgeWrongRoomResponse = "여기서는 무기를 만들 수가 없습니다."
	NewForgePromptResponse    = "\n\n어떤 종류의 무기를 원하십니까?\n1. 도 2. 검 3. 봉 4. 창 5. 궁 ?\n:"
	NewForgeRepromptResponse  = "하나를 선택하시오: "
	NewForgeMaterialResponse  = "\n타격치에 영향을 주는 재료를 선택하십시요.\n1.에메랄드 100만냥 2.티타늄 200만냥  3.일루션 300만냥\n:"
	NewForgeQuenchResponse    = "\n훌륭한  무기는  담금질로 내구력이  향상됩니다.\n몇번의  담금질을 원합니까?\n1. 100번  백만냥 2. 300번  2백만냥 3. 500번  3백만냥\n4. 700번 4백만냥 5. 900번 5백만냥\n: "
	NewForgeNameResponse      = "\n당신의 무기의 이름을 지으십시요.\n나중에 이름을 고칠수 없으니 조심해서 지으셔야 합니다\n: "
	NewForgeNameParenResponse = "이름에는 괄호가 들어갈수 없습니다.\n이름을 다시 넣으십시요:"
	NewForgeNameLongResponse  = "입력된 이름이 너무  깁니다.\n이름을 다시 넣으십시요(3자이상 20자이하): "
	NewForgeNameShortResponse = "입력된 이름이 너무  짧습니다.\n이름을 다시 넣으십시요(3자이상 20자이하): "
	NewForgeConfirmResponse   = "\n모든것에 만족하십니까? (예/아니오)\n: "
	NewForgeGiveResponse      = "\n주인이 당신에게 새로 제작된 무기를 건네줍니다."
	NewForgeTooPoorResponse   = "\n당신은 그 조건의 무기를  만들 만한 돈이 없습니다.\n주인이 당신에게 \"외상은 안되요~\"라고 말합니다."
	NewForgeCancelResponse    = "무기 제련을 취소하였습니다."
)

var (
	ErrNewForgeActorAbsent              = errors.New("online canonical newforge actor absent")
	ErrNewForgeFlagsUnresolved          = errors.New("canonical room flags required")
	ErrNewForgeStaleProposal            = errors.New("stale or invalid newforge proposal")
	ErrNewForgeInvalidProposal          = errors.New("invalid newforge proposal")
	ErrNewForgeNameInvalid              = errors.New("newforge actor name is unsafe")
	ErrNewForgeNotReading               = errors.New("newforge select_newarm continuation is not active")
	ErrNewForgeCatalogUnmigrated        = errors.New("newforge weapon catalog 900-904 is unmigrated")
	ErrNewForgeSelectArmInput           = errors.New("invalid newforge select_newarm input")
	ErrNewForgeGoldObjectUnmigrated     = errors.New("canonical gold and object graph required")
	ErrNewForgeItemAllocatorUnavailable = errors.New("newforge item ID allocator required")
)

// NewForgeEvent is one post-commit broadcast_rom line for select_newarm case 6.
type NewForgeEvent struct {
	RoomID         int16  `json:"room_id"`
	ActorID        string `json:"actor_id"`
	ActorName      string `json:"actor_name"`
	ExcludeActorID string `json:"exclude_actor_id"`
	Text           string `json:"text"`
}

type NewForgeResult struct {
	Action         NewForgeAction  `json:"action"`
	ActorID        string          `json:"actor_id"`
	RoomID         int16           `json:"room_id"`
	Response       string          `json:"response"`
	Changed        bool            `json:"changed"`
	Continuation   int             `json:"continuation,omitempty"`
	NoBroadcast    bool            `json:"no_broadcast,omitempty"`
	Reading        bool            `json:"reading,omitempty"`
	WeaponChoice   int             `json:"weapon_choice,omitempty"`
	ObjectID       int16           `json:"object_id,omitempty"`
	ObjectName     string          `json:"object_name,omitempty"`
	MaterialChoice int             `json:"material_choice,omitempty"`
	DiceCount      int16           `json:"dice_count,omitempty"`
	DiceSides      int16           `json:"dice_sides,omitempty"`
	DicePlus       int16           `json:"dice_plus,omitempty"`
	Sum            int32           `json:"sum,omitempty"`
	Enchanted      bool            `json:"enchanted,omitempty"`
	QuenchChoice   int             `json:"quench_choice,omitempty"`
	ShotsMax       int16           `json:"shots_max,omitempty"`
	ShotsCurrent   int16           `json:"shots_current,omitempty"`
	MaterialSum    int32           `json:"material_sum,omitempty"`
	WeaponName     string          `json:"weapon_name,omitempty"`
	ItemID         string          `json:"item_id,omitempty"`
	GoldAfter      int32           `json:"gold_after"`
	Events         []NewForgeEvent `json:"events,omitempty"`
}

type NewForgeProposal struct {
	Action         NewForgeAction
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
	DiceCount      int16
	DiceSides      int16
	DicePlus       int16
	Sum            int32
	MaterialSum    int32
	QuenchChoice   int
	ShotsMax       int16
	ShotsCurrent   int16
	WeaponName     string
	ItemID         string
	GoldAfter      int32
	Events         []NewForgeEvent

	expectedActor      PlayerState
	expectedRoomFlags  [8]byte
	expectedObject     LegacyObject
	expectedAfterItems ItemCollection
	hasAfterItems      bool
}

func newForgeActor(s State, actorID string) (PlayerState, RoomState, error) {
	actor, room, err := forgeActor(s, actorID)
	if err == nil {
		return actor, room, nil
	}
	switch {
	case errors.Is(err, ErrForgeActorAbsent):
		return PlayerState{}, RoomState{}, ErrNewForgeActorAbsent
	case errors.Is(err, ErrForgeNameInvalid):
		return PlayerState{}, RoomState{}, ErrNewForgeNameInvalid
	case errors.Is(err, ErrForgeFlagsUnresolved):
		return PlayerState{}, RoomState{}, ErrNewForgeFlagsUnresolved
	default:
		return PlayerState{}, RoomState{}, err
	}
}

func newForgeResult(p NewForgeProposal) NewForgeResult {
	return NewForgeResult{
		Action: p.Action, ActorID: p.ActorID, RoomID: p.RoomID, Response: p.Response,
		Changed: p.Changed, Continuation: p.Continuation,
		NoBroadcast:    forgeFlag(p.AfterFlags, newForgeNoBroadcastFlag),
		Reading:        forgeFlag(p.AfterFlags, newForgeReadingFlag),
		WeaponChoice:   p.WeaponChoice,
		ObjectID:       p.ObjectID,
		ObjectName:     p.expectedObject.Name,
		MaterialChoice: p.MaterialChoice,
		DiceCount:      p.DiceCount,
		DiceSides:      p.DiceSides,
		DicePlus:       p.DicePlus,
		Sum:            p.Sum,
		Enchanted:      forgeFlag(p.expectedObject.Flags, newForgeEnchantedFlag),
		QuenchChoice:   p.QuenchChoice,
		ShotsMax:       p.ShotsMax,
		ShotsCurrent:   p.ShotsCurrent,
		MaterialSum:    p.MaterialSum,
		WeaponName:     p.WeaponName,
		ItemID:         p.ItemID,
		GoldAfter:      p.GoldAfter,
		Events:         append([]NewForgeEvent(nil), p.Events...),
	}
}

func newForgeProposalMatches(actual, expected NewForgeProposal) bool {
	return actual.Action == expected.Action && actual.ActorID == expected.ActorID &&
		actual.RoomID == expected.RoomID && actual.Response == expected.Response &&
		actual.Changed == expected.Changed && actual.Continuation == expected.Continuation &&
		actual.BeforeFlags == expected.BeforeFlags && actual.AfterFlags == expected.AfterFlags &&
		actual.Input == expected.Input && actual.WeaponChoice == expected.WeaponChoice &&
		actual.ObjectID == expected.ObjectID && actual.MaterialChoice == expected.MaterialChoice &&
		actual.DiceCount == expected.DiceCount && actual.DiceSides == expected.DiceSides &&
		actual.DicePlus == expected.DicePlus && actual.Sum == expected.Sum &&
		actual.MaterialSum == expected.MaterialSum && actual.QuenchChoice == expected.QuenchChoice &&
		actual.ShotsMax == expected.ShotsMax && actual.ShotsCurrent == expected.ShotsCurrent &&
		actual.WeaponName == expected.WeaponName && actual.ItemID == expected.ItemID &&
		actual.GoldAfter == expected.GoldAfter && actual.hasAfterItems == expected.hasAfterItems &&
		actual.expectedRoomFlags == expected.expectedRoomFlags &&
		reflect.DeepEqual(actual.Events, expected.Events) &&
		reflect.DeepEqual(actual.expectedActor, expected.expectedActor) &&
		reflect.DeepEqual(actual.expectedObject, expected.expectedObject) &&
		reflect.DeepEqual(actual.expectedAfterItems, expected.expectedAfterItems)
}

func NewForgeWeaponObjectID(choice int) int16 {
	if choice < 1 || choice > 5 {
		return 0
	}
	return newForgeWeaponObjectBase + int16(choice-1)
}

// PlanNewForge is command7.c:newforge's admission. Missing RFORGE is the
// original typed no-op print. A present RFORGE outside room 611 is the
// second typed no-op. Room 611 with RFORGE starts select_newarm case 1:
// PREADI is set for the weapon-type prompt and the RETURN param is 2.
// Unmigrated/absent room flags fail closed.
func (s State) PlanNewForge(actorID string) (NewForgeProposal, error) {
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, BeforeFlags: actor.Body.Flags,
		AfterFlags: actor.Body.Flags, expectedActor: cloneForgeActor(actor),
		expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, true)
	after = forgeWithFlag(after, newForgeNoBroadcastFlag, false)
	p.Action = NewForgePrompt
	p.Response = NewForgePromptResponse
	p.Changed = true
	p.Continuation = newForgeSelectArmPhase
	p.AfterFlags = after
	return p, nil
}

// ApplyNewForge atomically writes the select_newarm start flags. A not-forge
// or wrong-room candidate is a typed no-op. Replay identity is owned by the
// receipt; a stale snapshot cannot set PREADI or clear PNOBRD after the
// actor has moved.
func (s State) ApplyNewForge(p NewForgeProposal) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForge(p.ActorID)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	next := s.clone()
	if p.Changed {
		if p.Action != NewForgePrompt || p.Continuation != newForgeSelectArmPhase || p.Response != NewForgePromptResponse {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
		actor := next.Players[p.ActorID]
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForge is the reducer-shaped convenience API for the 무기만들기 start.
func (s State) NewForge(actorID string) (State, NewForgeResult, error) {
	p, err := s.PlanNewForge(actorID)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForge(p)
}

// PlanNewForgeSelectArm is command7.c:select_newarm case 2. C inspects
// str[0]: digits 1-5 load_obj(900+choice-1) and RETURN param 3 with the
// 에메랄드/티타늄/일루션 prompt; any other first byte re-prompts
// "하나를 선택하시오: " at param 2. load_obj failure is fail-closed here
// instead of the original Error print. Gold is not charged in this phase.
// PREADI stays set; C F_CLR/F_SET nets to the same persisted flag.
// Case 3 is PlanNewForgeSelectMaterial. Case 4 is PlanNewForgeSelectQuench.
// Case 5 is PlanNewForgeSelectName. Case 6 is PlanNewForgeSelectConfirm.
func (s State) PlanNewForgeSelectArm(actorID, input string, catalog SpawnCatalog) (NewForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return NewForgeProposal{}, ErrNewForgeSelectArmInput
	}
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, newForgeReadingFlag) {
		return NewForgeProposal{}, ErrNewForgeNotReading
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeWeaponChoice(input)
	if !ok {
		p.Action = NewForgeReprompt
		p.Response = NewForgeRepromptResponse
		p.Continuation = newForgeSelectArmPhase
		return p, nil
	}
	if catalog == nil {
		return NewForgeProposal{}, ErrNewForgeCatalogUnmigrated
	}
	objectID := NewForgeWeaponObjectID(choice)
	object, err := catalog.Object(objectID)
	if err != nil {
		return NewForgeProposal{}, fmt.Errorf("%w: object %d: %v", ErrNewForgeCatalogUnmigrated, objectID, err)
	}
	if err := validForgeWeaponTemplate(object); err != nil {
		return NewForgeProposal{}, fmt.Errorf("%w: object %d", ErrNewForgeCatalogUnmigrated, objectID)
	}
	p.Action = NewForgeMaterial
	p.Response = NewForgeMaterialResponse
	p.Continuation = newForgeMaterialPhase
	p.WeaponChoice = choice
	p.ObjectID = objectID
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func newForgeSelectArmActionValid(p NewForgeProposal) bool {
	switch p.Action {
	case NewForgeReprompt:
		return !p.Changed && p.Continuation == newForgeSelectArmPhase &&
			p.Response == NewForgeRepromptResponse && p.WeaponChoice == 0 && p.ObjectID == 0
	case NewForgeMaterial:
		return !p.Changed && p.Continuation == newForgeMaterialPhase &&
			p.Response == NewForgeMaterialResponse &&
			p.WeaponChoice >= 1 && p.WeaponChoice <= 5 &&
			p.ObjectID == NewForgeWeaponObjectID(p.WeaponChoice)
	case NewForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.Response == NewForgeNotForgeResponse
	case NewForgeWrongRoom:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.Response == NewForgeWrongRoomResponse
	default:
		return false
	}
}

// ApplyNewForgeSelectArm records the case-2 receipt. The loaded template is
// receipt identity only (C forge1[fd] pointer); inventory and gold are
// unchanged. A stale snapshot or a gold/flag drift cannot invent a weapon.
func (s State) ApplyNewForgeSelectArm(p NewForgeProposal, catalog SpawnCatalog) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForgeSelectArm(p.ActorID, p.Input, catalog)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	if !newForgeSelectArmActionValid(p) {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForgeSelectArm is the reducer-shaped convenience API for select_newarm case 2.
func (s State) NewForgeSelectArm(actorID, input string, catalog SpawnCatalog) (State, NewForgeResult, error) {
	p, err := s.PlanNewForgeSelectArm(actorID, input, catalog)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForgeSelectArm(p, catalog)
}

func newForgeLoadWeaponTemplate(catalog SpawnCatalog, objectID int16) (LegacyObject, error) {
	if catalog == nil {
		return LegacyObject{}, ErrNewForgeCatalogUnmigrated
	}
	if objectID < newForgeWeaponObjectBase || objectID > newForgeWeaponObjectBase+4 {
		return LegacyObject{}, fmt.Errorf("%w: object %d", ErrNewForgeCatalogUnmigrated, objectID)
	}
	object, err := catalog.Object(objectID)
	if err != nil {
		return LegacyObject{}, fmt.Errorf("%w: object %d: %v", ErrNewForgeCatalogUnmigrated, objectID, err)
	}
	if err := validForgeWeaponTemplate(object); err != nil {
		return LegacyObject{}, fmt.Errorf("%w: object %d", ErrNewForgeCatalogUnmigrated, objectID)
	}
	return cloneForgeObject(object), nil
}

func newForgeMaterialSpec(choice int) (diceCount, diceSides, dicePlus int16, sum int32, ok bool) {
	switch choice {
	case 1:
		return newForgeEmeraldDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, newForgeEmeraldCost, true
	case 2:
		return newForgeTitaniumDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, newForgeTitaniumCost, true
	case 3:
		return newForgeIllusionDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, newForgeIllusionCost, true
	default:
		return 0, 0, 0, 0, false
	}
}

// PlanNewForgeSelectMaterial is command7.c:select_newarm case 3. Distinct
// from 제련 select_arm case 3 (강철/귀금속/금강석 + class deny). C uses
// low(str[0]): 1 에메랄드 ndice=4 sum=1000000, 2 티타늄 ndice=5
// sum=2000000, 3 일루션 ndice=6 sum=3000000, all with sdice=5 pdice=5
// and OENCHA. Any other first byte re-prompts "하나를 선택하시오: " at
// param 3. Gold is not charged here; forge2 sum is receipt identity.
// Unmigrated catalog 900-904 fails closed. Case 4 is PlanNewForgeSelectQuench.
// Case 5 is PlanNewForgeSelectName. Case 6 is PlanNewForgeSelectConfirm.
func (s State) PlanNewForgeSelectMaterial(actorID, input string, catalog SpawnCatalog, objectID int16) (NewForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return NewForgeProposal{}, ErrNewForgeSelectArmInput
	}
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, newForgeReadingFlag) {
		return NewForgeProposal{}, ErrNewForgeNotReading
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeMaterialChoice(input)
	if !ok {
		p.Action = NewForgeReprompt
		p.Response = NewForgeRepromptResponse
		p.Continuation = newForgeMaterialPhase
		p.ObjectID = 0
		return p, nil
	}
	object, err := newForgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	diceCount, diceSides, dicePlus, sum, ok := newForgeMaterialSpec(choice)
	if !ok {
		return NewForgeProposal{}, ErrNewForgeInvalidProposal
	}
	object.Flags = forgeWithFlag(object.Flags, newForgeEnchantedFlag, true)
	object.DiceCount = diceCount
	object.DiceSides = diceSides
	object.DicePlus = dicePlus
	p.Action = NewForgeQuench
	p.Response = NewForgeQuenchResponse
	p.Continuation = newForgeQuenchPhase
	p.WeaponChoice = int(objectID - newForgeWeaponObjectBase + 1)
	p.MaterialChoice = choice
	p.DiceCount = diceCount
	p.DiceSides = diceSides
	p.DicePlus = dicePlus
	p.Sum = sum
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func newForgeSelectMaterialActionValid(p NewForgeProposal) bool {
	switch p.Action {
	case NewForgeReprompt:
		return !p.Changed && p.Continuation == newForgeMaterialPhase &&
			p.Response == NewForgeRepromptResponse && p.MaterialChoice == 0 &&
			p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 && p.Sum == 0 &&
			p.WeaponChoice == 0 && p.ObjectID == 0
	case NewForgeQuench:
		if p.Changed || p.Continuation != newForgeQuenchPhase || p.Response != NewForgeQuenchResponse ||
			p.Response == ForgeQuenchResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != NewForgeWeaponObjectID(p.WeaponChoice) ||
			p.DiceCount != p.expectedObject.DiceCount ||
			p.DiceSides != p.expectedObject.DiceSides ||
			p.DicePlus != p.expectedObject.DicePlus ||
			p.Sum <= 0 || !forgeFlag(p.expectedObject.Flags, newForgeEnchantedFlag) {
			return false
		}
		diceCount, diceSides, dicePlus, sum, ok := newForgeMaterialSpec(p.MaterialChoice)
		return ok && p.DiceCount == diceCount && p.DiceSides == diceSides &&
			p.DicePlus == dicePlus && p.Sum == sum
	case NewForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.Response == NewForgeNotForgeResponse
	case NewForgeWrongRoom:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.Response == NewForgeWrongRoomResponse
	default:
		return false
	}
}

// ApplyNewForgeSelectMaterial records the case-3 receipt. Dice/OENCHA/sum
// live on the loaded template identity (C forge1/forge2); inventory and
// gold are unchanged. A stale snapshot or gold/flag drift cannot invent a
// weapon. Case 4 is ApplyNewForgeSelectQuench. Case 5 is ApplyNewForgeSelectName.
// Case 6 is ApplyNewForgeSelectConfirm.
func (s State) ApplyNewForgeSelectMaterial(p NewForgeProposal, catalog SpawnCatalog) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForgeSelectMaterial(p.ActorID, p.Input, catalog, p.ObjectID)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	if !newForgeSelectMaterialActionValid(p) {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForgeSelectMaterial is the reducer-shaped convenience API for
// select_newarm case 3.
func (s State) NewForgeSelectMaterial(actorID, input string, catalog SpawnCatalog, objectID int16) (State, NewForgeResult, error) {
	p, err := s.PlanNewForgeSelectMaterial(actorID, input, catalog, objectID)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForgeSelectMaterial(p, catalog)
}

func newForgeMaterialSumKnown(sum int32) (diceCount, diceSides, dicePlus int16, choice int, ok bool) {
	switch sum {
	case newForgeEmeraldCost:
		return newForgeEmeraldDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, 1, true
	case newForgeTitaniumCost:
		return newForgeTitaniumDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, 2, true
	case newForgeIllusionCost:
		return newForgeIllusionDiceCount, newForgeMaterialDiceSides, newForgeMaterialDicePlus, 3, true
	default:
		return 0, 0, 0, 0, false
	}
}

func newForgeQuenchSpec(choice int) (shots int16, cost int32, ok bool) {
	switch choice {
	case 1:
		return newForgeQuench100Shots, newForgeQuench100Cost, true
	case 2:
		return newForgeQuench200Shots, newForgeQuench200Cost, true
	case 3:
		return newForgeQuench300Shots, newForgeQuench300Cost, true
	case 4:
		return newForgeQuench400Shots, newForgeQuench400Cost, true
	case 5:
		return newForgeQuench500Shots, newForgeQuench500Cost, true
	default:
		return 0, 0, false
	}
}

func newForgeApplyMaterialIdentity(object LegacyObject, materialSum int32) (LegacyObject, int, int16, int16, int16, error) {
	diceCount, diceSides, dicePlus, choice, ok := newForgeMaterialSumKnown(materialSum)
	if !ok {
		return LegacyObject{}, 0, 0, 0, 0, ErrNewForgeCatalogUnmigrated
	}
	object.Flags = forgeWithFlag(object.Flags, newForgeEnchantedFlag, true)
	object.DiceCount = diceCount
	object.DiceSides = diceSides
	object.DicePlus = dicePlus
	return object, choice, diceCount, diceSides, dicePlus, nil
}

// PlanNewForgeSelectQuench is command7.c:select_newarm case 4. Distinct
// from 제련 select_arm case 4 (오만냥/이십만냥/오십만냥/백만냥/이백만냥).
// C uses low(str[0]): 1 shots=100 sum+=1000000, 2 shots=200 sum+=2000000,
// 3 shots=300 sum+=3000000, 4 shots=400 sum+=4000000, 5 shots=500
// sum+=5000000. The printed menu says 100/300/500/700/900번; the switch
// writes 100/200/300/400/500. Any other first byte re-prompts
// "하나를 선택하시오: " at param 4. Gold is not charged here; forge2 sum
// is receipt identity. Unmigrated catalog 900-904 or a non-newforge
// forge2 sum fails closed. Case 5 is PlanNewForgeSelectName. Case 6 is
// PlanNewForgeSelectConfirm.
func (s State) PlanNewForgeSelectQuench(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32) (NewForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return NewForgeProposal{}, ErrNewForgeSelectArmInput
	}
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, newForgeReadingFlag) {
		return NewForgeProposal{}, ErrNewForgeNotReading
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, true)
	p.AfterFlags = after
	choice, ok := forgeQuenchChoice(input)
	if !ok {
		p.Action = NewForgeReprompt
		p.Response = NewForgeRepromptResponse
		p.Continuation = newForgeQuenchPhase
		p.ObjectID = 0
		return p, nil
	}
	shots, cost, ok := newForgeQuenchSpec(choice)
	if !ok {
		return NewForgeProposal{}, ErrNewForgeInvalidProposal
	}
	object, err := newForgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	object, materialChoice, diceCount, diceSides, dicePlus, err := newForgeApplyMaterialIdentity(object, materialSum)
	if err != nil {
		return NewForgeProposal{}, err
	}
	object.ShotsMax = shots
	object.ShotsCurrent = shots
	p.Action = NewForgeName
	p.Response = NewForgeNameResponse
	p.Continuation = newForgeNamePhase
	p.WeaponChoice = int(objectID - newForgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceCount = diceCount
	p.DiceSides = diceSides
	p.DicePlus = dicePlus
	p.MaterialSum = materialSum
	p.Sum = materialSum + cost
	p.QuenchChoice = choice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func newForgeSelectQuenchActionValid(p NewForgeProposal) bool {
	switch p.Action {
	case NewForgeReprompt:
		return !p.Changed && p.Continuation == newForgeQuenchPhase &&
			p.Response == NewForgeRepromptResponse && p.QuenchChoice == 0 &&
			p.ShotsMax == 0 && p.ShotsCurrent == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.WeaponChoice == 0 && p.ObjectID == 0
	case NewForgeName:
		shots, cost, ok := newForgeQuenchSpec(p.QuenchChoice)
		if !ok || p.Changed || p.Continuation != newForgeNamePhase || p.Response != NewForgeNameResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != NewForgeWeaponObjectID(p.WeaponChoice) ||
			p.ShotsMax != shots || p.ShotsCurrent != shots ||
			p.expectedObject.ShotsMax != shots || p.expectedObject.ShotsCurrent != shots ||
			p.DiceCount != p.expectedObject.DiceCount ||
			p.DiceSides != p.expectedObject.DiceSides ||
			p.DicePlus != p.expectedObject.DicePlus ||
			p.MaterialSum <= 0 || p.Sum != p.MaterialSum+cost ||
			!forgeFlag(p.expectedObject.Flags, newForgeEnchantedFlag) {
			return false
		}
		diceCount, diceSides, dicePlus, materialChoice, known := newForgeMaterialSumKnown(p.MaterialSum)
		return known && p.MaterialChoice == materialChoice && p.DiceCount == diceCount &&
			p.DiceSides == diceSides && p.DicePlus == dicePlus
	case NewForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.Response == NewForgeNotForgeResponse
	case NewForgeWrongRoom:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.Response == NewForgeWrongRoomResponse
	default:
		return false
	}
}

// ApplyNewForgeSelectQuench records the case-4 receipt. shots/sum live on
// the loaded template identity (C forge1/forge2); inventory and gold are
// unchanged. A stale snapshot or gold/flag drift cannot invent a weapon.
// Case 5 is ApplyNewForgeSelectName. Case 6 is ApplyNewForgeSelectConfirm.
func (s State) ApplyNewForgeSelectQuench(p NewForgeProposal, catalog SpawnCatalog) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForgeSelectQuench(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	if !newForgeSelectQuenchActionValid(p) {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForgeSelectQuench is the reducer-shaped convenience API for
// select_newarm case 4.
func (s State) NewForgeSelectQuench(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32) (State, NewForgeResult, error) {
	p, err := s.PlanNewForgeSelectQuench(actorID, input, catalog, objectID, materialSum)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForgeSelectQuench(p, catalog)
}

func newForgeNameReprompt(input string) (response string, ok bool) {
	if strings.ContainsAny(input, "()") {
		return NewForgeNameParenResponse, true
	}
	if len(input) > newForgeWeaponNameMaxBytes {
		return NewForgeNameLongResponse, true
	}
	if len(input) < newForgeWeaponNameMinBytes {
		return NewForgeNameShortResponse, true
	}
	return "", false
}

func newForgeApplyQuenchIdentity(object LegacyObject, materialSum int32, quenchChoice int) (LegacyObject, int, int16, int16, int16, int16, int32, error) {
	object, materialChoice, diceCount, diceSides, dicePlus, err := newForgeApplyMaterialIdentity(object, materialSum)
	if err != nil {
		return LegacyObject{}, 0, 0, 0, 0, 0, 0, err
	}
	shots, cost, ok := newForgeQuenchSpec(quenchChoice)
	if !ok {
		return LegacyObject{}, 0, 0, 0, 0, 0, 0, ErrNewForgeCatalogUnmigrated
	}
	object.ShotsMax = shots
	object.ShotsCurrent = shots
	return object, materialChoice, diceCount, diceSides, dicePlus, shots, materialSum + cost, nil
}

// PlanNewForgeSelectName is command7.c:select_newarm case 5. Distinct from
// 제련 select_arm case 5 (same print text, different forge2 identity).
// C copies str onto obj_ptr->name after rejecting parentheses and strlen
// outside 3-20. Those re-prompts stay at param 5. A valid name prints the
// confirm prompt and RETURN param 6. Gold is not charged here. Unmigrated
// catalog 900-904, a non-newforge forge2 sum, or a missing quench identity
// fails closed before a receipt. C F_CLR(PREADI) then only F_SET on the
// too-short and success paths; the Go receipt keeps PREADI set so the
// connection-local RETURN can continue. Case 6 is PlanNewForgeSelectConfirm.
func (s State) PlanNewForgeSelectName(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (NewForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return NewForgeProposal{}, ErrNewForgeSelectArmInput
	}
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, newForgeReadingFlag) {
		return NewForgeProposal{}, ErrNewForgeNotReading
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		p.ObjectID = 0
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		p.ObjectID = 0
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, true)
	p.AfterFlags = after
	if response, invalid := newForgeNameReprompt(input); invalid {
		p.Action = NewForgeReprompt
		p.Response = response
		p.Continuation = newForgeNamePhase
		p.ObjectID = 0
		return p, nil
	}
	object, err := newForgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	object, materialChoice, diceCount, diceSides, dicePlus, shots, sum, err := newForgeApplyQuenchIdentity(object, materialSum, quenchChoice)
	if err != nil {
		return NewForgeProposal{}, err
	}
	object.Name = input
	p.Action = NewForgeConfirm
	p.Response = NewForgeConfirmResponse
	p.Continuation = newForgeConfirmPhase
	p.WeaponChoice = int(objectID - newForgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceCount = diceCount
	p.DiceSides = diceSides
	p.DicePlus = dicePlus
	p.MaterialSum = materialSum
	p.Sum = sum
	p.QuenchChoice = quenchChoice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	return p, nil
}

func newForgeSelectNameActionValid(p NewForgeProposal) bool {
	switch p.Action {
	case NewForgeReprompt:
		return !p.Changed && p.Continuation == newForgeNamePhase &&
			(p.Response == NewForgeNameParenResponse || p.Response == NewForgeNameLongResponse || p.Response == NewForgeNameShortResponse) &&
			p.QuenchChoice == 0 && p.ShotsMax == 0 && p.ShotsCurrent == 0 && p.Sum == 0 &&
			p.MaterialSum == 0 && p.WeaponChoice == 0 && p.ObjectID == 0
	case NewForgeConfirm:
		shots, cost, ok := newForgeQuenchSpec(p.QuenchChoice)
		if !ok || p.Changed || p.Continuation != newForgeConfirmPhase || p.Response != NewForgeConfirmResponse ||
			p.WeaponChoice < 1 || p.WeaponChoice > 5 ||
			p.ObjectID != NewForgeWeaponObjectID(p.WeaponChoice) ||
			p.ShotsMax != shots || p.ShotsCurrent != shots ||
			p.expectedObject.ShotsMax != shots || p.expectedObject.ShotsCurrent != shots ||
			p.expectedObject.Name != p.Input || p.DiceCount != p.expectedObject.DiceCount ||
			p.DiceSides != p.expectedObject.DiceSides || p.DicePlus != p.expectedObject.DicePlus ||
			p.MaterialSum <= 0 || p.Sum != p.MaterialSum+cost ||
			!forgeFlag(p.expectedObject.Flags, newForgeEnchantedFlag) {
			return false
		}
		if _, invalid := newForgeNameReprompt(p.Input); invalid {
			return false
		}
		diceCount, diceSides, dicePlus, materialChoice, known := newForgeMaterialSumKnown(p.MaterialSum)
		return known && p.MaterialChoice == materialChoice && p.DiceCount == diceCount &&
			p.DiceSides == diceSides && p.DicePlus == dicePlus
	case NewForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.Response == NewForgeNotForgeResponse
	case NewForgeWrongRoom:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.Response == NewForgeWrongRoomResponse
	default:
		return false
	}
}

// ApplyNewForgeSelectName records the case-5 receipt. The renamed template is
// receipt identity only (C forge1 pointer); inventory and gold are
// unchanged. A stale snapshot or gold/flag drift cannot invent a weapon
// or charge gold. Case 6 is ApplyNewForgeSelectConfirm.
func (s State) ApplyNewForgeSelectName(p NewForgeProposal, catalog SpawnCatalog) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForgeSelectName(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum, p.QuenchChoice)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	if !newForgeSelectNameActionValid(p) {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if p.Changed {
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
	}
	if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForgeSelectName is the reducer-shaped convenience API for
// select_newarm case 5.
func (s State) NewForgeSelectName(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int) (State, NewForgeResult, error) {
	p, err := s.PlanNewForgeSelectName(actorID, input, catalog, objectID, materialSum, quenchChoice)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForgeSelectName(p, catalog)
}

func newForgeGoldObjectGraphMigrated(actor PlayerState) error {
	if err := forgeGoldObjectGraphMigrated(actor); err == nil {
		return nil
	}
	return ErrNewForgeGoldObjectUnmigrated
}

func newForgeConfirmYes(input string) bool {
	return strings.HasPrefix(input, "예")
}

func NewForgeBroadcastText(actorName string) string {
	return fmt.Sprintf("\n%s이   무기를  제련하였습니다.", actorName)
}

func newForgeApplyNameIdentity(object LegacyObject, materialSum int32, quenchChoice int, name string) (LegacyObject, int, int16, int16, int16, int16, int32, error) {
	if response, invalid := newForgeNameReprompt(name); invalid {
		return LegacyObject{}, 0, 0, 0, 0, 0, 0, fmt.Errorf("%w: %s", ErrNewForgeGoldObjectUnmigrated, response)
	}
	object, materialChoice, diceCount, diceSides, dicePlus, shots, sum, err := newForgeApplyQuenchIdentity(object, materialSum, quenchChoice)
	if err != nil {
		if errors.Is(err, ErrNewForgeCatalogUnmigrated) {
			return LegacyObject{}, 0, 0, 0, 0, 0, 0, ErrNewForgeGoldObjectUnmigrated
		}
		return LegacyObject{}, 0, 0, 0, 0, 0, 0, err
	}
	object.Name = name
	return object, materialChoice, diceCount, diceSides, dicePlus, shots, sum, nil
}

func newForgeItemIDs(s State) map[string]struct{} {
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

func newForgeGiveWeapon(s State, actor PlayerState, object LegacyObject, allocate func() (string, error)) (ItemCollection, string, error) {
	if allocate == nil {
		return ItemCollection{}, "", ErrNewForgeItemAllocatorUnavailable
	}
	used := newForgeItemIDs(s)
	uniqueAllocate := func() (string, error) {
		id, err := allocate()
		if err != nil {
			return "", err
		}
		if id == "" {
			return "", fmt.Errorf("%w: empty item ID", ErrNewForgeItemAllocatorUnavailable)
		}
		if _, exists := used[id]; exists {
			return "", fmt.Errorf("%w: duplicate item ID", ErrNewForgeItemAllocatorUnavailable)
		}
		used[id] = struct{}{}
		return id, nil
	}
	reward, err := ImportItems([]LegacyObject{cloneForgeObject(object)}, uniqueAllocate)
	if err != nil {
		return ItemCollection{}, "", fmt.Errorf("%w: %v", ErrNewForgeItemAllocatorUnavailable, err)
	}
	if actor.Items == nil {
		return ItemCollection{}, "", ErrNewForgeGoldObjectUnmigrated
	}
	plan, err := TransferItemRoots(reward, *actor.Items, reward.Inventory)
	if err != nil {
		return ItemCollection{}, "", err
	}
	return plan.Destination, reward.Inventory[0], nil
}

// PlanNewForgeSelectConfirm is command7.c:select_newarm case 6. Distinct
// from 제련 select_arm case 6 (steel/precious/diamond forge2). C F_CLR(PREADI)
// then strncmp(str,"예",2): gold < sum prints the credit refusal and frees
// the object; otherwise add_obj_crt, print the hand-off, broadcast_rom,
// and gold -= sum. Any other first bytes cancel and free the object.
// Unmigrated gold, item graph, catalog, name, or quench identity fails
// closed before a receipt.
func (s State) PlanNewForgeSelectConfirm(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string, allocate func() (string, error)) (NewForgeProposal, error) {
	if err := validForgeSelectArmInput(input); err != nil {
		return NewForgeProposal{}, ErrNewForgeSelectArmInput
	}
	actor, room, err := newForgeActor(s, actorID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	if !forgeFlag(actor.Body.Flags, newForgeReadingFlag) {
		return NewForgeProposal{}, ErrNewForgeNotReading
	}
	if err := newForgeGoldObjectGraphMigrated(actor); err != nil {
		return NewForgeProposal{}, err
	}
	p := NewForgeProposal{
		ActorID: actorID, RoomID: actor.Body.RoomID, Input: input, ObjectID: objectID,
		BeforeFlags: actor.Body.Flags, AfterFlags: actor.Body.Flags,
		expectedActor: cloneForgeActor(actor), expectedRoomFlags: room.Resource.Flags,
		WeaponName: weaponName, GoldAfter: actor.Body.Gold,
	}
	if !forgeFlag(room.Resource.Flags, newForgeRoomFlag) {
		p.Action = NewForgeNotForge
		p.Response = NewForgeNotForgeResponse
		p.ObjectID = 0
		p.WeaponName = ""
		return p, nil
	}
	if room.Resource.ID != newForgeRoomID {
		p.Action = NewForgeWrongRoom
		p.Response = NewForgeWrongRoomResponse
		p.ObjectID = 0
		p.WeaponName = ""
		return p, nil
	}
	after := forgeWithFlag(actor.Body.Flags, newForgeReadingFlag, false)
	p.AfterFlags = after
	p.Changed = true
	object, err := newForgeLoadWeaponTemplate(catalog, objectID)
	if err != nil {
		return NewForgeProposal{}, err
	}
	object, materialChoice, diceCount, diceSides, dicePlus, shots, sum, err := newForgeApplyNameIdentity(object, materialSum, quenchChoice, weaponName)
	if err != nil {
		return NewForgeProposal{}, err
	}
	p.WeaponChoice = int(objectID - newForgeWeaponObjectBase + 1)
	p.MaterialChoice = materialChoice
	p.DiceCount = diceCount
	p.DiceSides = diceSides
	p.DicePlus = dicePlus
	p.MaterialSum = materialSum
	p.Sum = sum
	p.QuenchChoice = quenchChoice
	p.ShotsMax = shots
	p.ShotsCurrent = shots
	p.expectedObject = cloneForgeObject(object)
	if !newForgeConfirmYes(input) {
		p.Action = NewForgeCancel
		p.Response = NewForgeCancelResponse
		return p, nil
	}
	if actor.Body.Gold < sum {
		p.Action = NewForgeTooPoor
		p.Response = NewForgeTooPoorResponse
		return p, nil
	}
	afterItems, itemID, err := newForgeGiveWeapon(s, actor, object, allocate)
	if err != nil {
		return NewForgeProposal{}, err
	}
	p.Action = NewForgeGive
	p.Response = NewForgeGiveResponse
	p.ItemID = itemID
	p.GoldAfter = actor.Body.Gold - sum
	p.expectedAfterItems = afterItems.clone()
	p.hasAfterItems = true
	p.Events = []NewForgeEvent{{
		RoomID: p.RoomID, ActorID: actorID, ActorName: actor.Body.Name,
		ExcludeActorID: actorID, Text: NewForgeBroadcastText(actor.Body.Name),
	}}
	return p, nil
}

func newForgeSelectConfirmActionValid(p NewForgeProposal) bool {
	readingCleared := p.Changed && !forgeFlag(p.AfterFlags, newForgeReadingFlag) &&
		forgeFlag(p.BeforeFlags, newForgeReadingFlag)
	identityOK := p.WeaponChoice >= 1 && p.WeaponChoice <= 5 &&
		p.ObjectID == NewForgeWeaponObjectID(p.WeaponChoice) &&
		p.WeaponName == p.expectedObject.Name && p.expectedObject.Name != "" &&
		p.ShotsMax == p.expectedObject.ShotsMax && p.ShotsCurrent == p.expectedObject.ShotsCurrent &&
		p.DiceCount == p.expectedObject.DiceCount && p.DiceSides == p.expectedObject.DiceSides &&
		p.DicePlus == p.expectedObject.DicePlus &&
		forgeFlag(p.expectedObject.Flags, newForgeEnchantedFlag)
	if identityOK {
		shots, cost, ok := newForgeQuenchSpec(p.QuenchChoice)
		diceCount, diceSides, dicePlus, materialChoice, known := newForgeMaterialSumKnown(p.MaterialSum)
		identityOK = ok && known && p.MaterialChoice == materialChoice &&
			p.ShotsMax == shots && p.ShotsCurrent == shots &&
			p.DiceCount == diceCount && p.DiceSides == diceSides && p.DicePlus == dicePlus &&
			p.MaterialSum > 0 && p.Sum == p.MaterialSum+cost
		if _, invalid := newForgeNameReprompt(p.WeaponName); invalid {
			identityOK = false
		}
	}
	switch p.Action {
	case NewForgeCancel:
		return readingCleared && identityOK && p.Continuation == 0 &&
			p.Response == NewForgeCancelResponse && p.ItemID == "" &&
			!p.hasAfterItems && len(p.Events) == 0 &&
			p.GoldAfter == p.expectedActor.Body.Gold && !newForgeConfirmYes(p.Input)
	case NewForgeTooPoor:
		return readingCleared && identityOK && p.Continuation == 0 &&
			p.Response == NewForgeTooPoorResponse && p.ItemID == "" &&
			!p.hasAfterItems && len(p.Events) == 0 &&
			p.GoldAfter == p.expectedActor.Body.Gold && newForgeConfirmYes(p.Input) &&
			p.expectedActor.Body.Gold < p.Sum
	case NewForgeGive:
		if !readingCleared || !identityOK || p.Continuation != 0 ||
			p.Response != NewForgeGiveResponse || p.ItemID == "" || !p.hasAfterItems ||
			len(p.Events) != 1 || !newForgeConfirmYes(p.Input) ||
			p.GoldAfter != p.expectedActor.Body.Gold-p.Sum ||
			p.expectedActor.Body.Gold < p.Sum {
			return false
		}
		item, ok := p.expectedAfterItems.Items[p.ItemID]
		if !ok || item.Object.Name != p.WeaponName ||
			!containsString(p.expectedAfterItems.Inventory, p.ItemID) ||
			item.Object.DiceCount != p.DiceCount || item.Object.DiceSides != p.DiceSides ||
			item.Object.DicePlus != p.DicePlus ||
			item.Object.ShotsMax != p.ShotsMax || item.Object.ShotsCurrent != p.ShotsCurrent {
			return false
		}
		event := p.Events[0]
		return event.RoomID == p.RoomID && event.ActorID == p.ActorID &&
			event.ExcludeActorID == p.ActorID && event.Text == NewForgeBroadcastText(p.expectedActor.Body.Name)
	case NewForgeNotForge:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.WeaponName == "" && p.ItemID == "" && !p.hasAfterItems &&
			len(p.Events) == 0 && p.Response == NewForgeNotForgeResponse
	case NewForgeWrongRoom:
		return !p.Changed && p.Continuation == 0 && p.WeaponChoice == 0 && p.ObjectID == 0 &&
			p.MaterialChoice == 0 && p.DiceCount == 0 && p.DiceSides == 0 && p.DicePlus == 0 &&
			p.Sum == 0 && p.MaterialSum == 0 && p.QuenchChoice == 0 && p.ShotsMax == 0 &&
			p.ShotsCurrent == 0 && p.WeaponName == "" && p.ItemID == "" && !p.hasAfterItems &&
			len(p.Events) == 0 && p.Response == NewForgeWrongRoomResponse
	default:
		return false
	}
}

// ApplyNewForgeSelectConfirm records the case-6 receipt. A yes with enough
// gold charges gold and inserts the named weapon in one candidate. Replay
// identity is owned by the receipt; a stale snapshot cannot charge twice
// or mint a second object. Cancel and insufficient gold clear PREADI
// without touching gold or inventory.
func (s State) ApplyNewForgeSelectConfirm(p NewForgeProposal, catalog SpawnCatalog, allocate func() (string, error)) (State, NewForgeResult, error) {
	if p.ActorID == "" {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	expected, err := s.PlanNewForgeSelectConfirm(p.ActorID, p.Input, catalog, p.ObjectID, p.MaterialSum, p.QuenchChoice, p.WeaponName, allocate)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	if !newForgeProposalMatches(p, expected) {
		return State{}, NewForgeResult{}, ErrNewForgeStaleProposal
	}
	if !newForgeSelectConfirmActionValid(p) {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	next := s.clone()
	actor := next.Players[p.ActorID]
	if actor.Body.Gold != p.expectedActor.Body.Gold || actor.Body.Gold != s.Players[p.ActorID].Body.Gold {
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := newForgeGoldObjectGraphMigrated(actor); err != nil {
		return State{}, NewForgeResult{}, err
	}
	switch p.Action {
	case NewForgeGive:
		if actor.Body.Gold < p.Sum || p.GoldAfter != actor.Body.Gold-p.Sum || !p.hasAfterItems {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
		actor.Body.Flags = p.AfterFlags
		actor.Body.Gold = p.GoldAfter
		items := p.expectedAfterItems.clone()
		actor.Items = &items
		next.Players[p.ActorID] = actor
		if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold-p.Sum {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
		if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
			return State{}, NewForgeResult{}, ErrNewForgeGoldObjectUnmigrated
		}
		if _, ok := next.Players[p.ActorID].Items.Items[p.ItemID]; !ok {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
	case NewForgeCancel, NewForgeTooPoor:
		actor.Body.Flags = p.AfterFlags
		next.Players[p.ActorID] = actor
		if next.Players[p.ActorID].Body.Gold != s.Players[p.ActorID].Body.Gold {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
		if next.Players[p.ActorID].Items == nil || len(next.Players[p.ActorID].Body.Inventory) != 0 {
			return State{}, NewForgeResult{}, ErrNewForgeGoldObjectUnmigrated
		}
	case NewForgeNotForge, NewForgeWrongRoom:
		if p.Changed {
			return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
		}
	default:
		return State{}, NewForgeResult{}, ErrNewForgeInvalidProposal
	}
	if err := next.Validate(); err != nil {
		return State{}, NewForgeResult{}, err
	}
	return next, newForgeResult(p), nil
}

// NewForgeSelectConfirm is the reducer-shaped convenience API for
// select_newarm case 6.
func (s State) NewForgeSelectConfirm(actorID, input string, catalog SpawnCatalog, objectID int16, materialSum int32, quenchChoice int, weaponName string, allocate func() (string, error)) (State, NewForgeResult, error) {
	p, err := s.PlanNewForgeSelectConfirm(actorID, input, catalog, objectID, materialSum, quenchChoice, weaponName, allocate)
	if err != nil {
		return State{}, NewForgeResult{}, err
	}
	return s.ApplyNewForgeSelectConfirm(p, catalog, allocate)
}
