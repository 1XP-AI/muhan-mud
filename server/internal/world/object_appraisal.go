package world

import (
	"errors"
	"fmt"
	"strings"
)

// Object appraisal is the bounded read-only port of command3.c:info_obj.
// Keep the class values local to this command boundary: these are legacy
// character-class numbers, not a new public class enum.
const (
	objectAppraisalThiefClass      = 8 // THIEF
	objectAppraisalInvincibleClass = 9 // INVINCIBLE
	objectAppraisalDetectFlag      = 21

	objectAppraisalNoMagicFlag    = 10 // ONOMAG
	objectAppraisalGoodOnlyFlag   = 12 // OGOODO
	objectAppraisalEvilOnlyFlag   = 13 // OEVILO
	objectAppraisalEnchantedFlag  = 14 // OENCHA
	objectAppraisalFemaleOnlyFlag = 26 // ONOMAL
	objectAppraisalMaleOnlyFlag   = 27 // ONOFEM
)

var (
	// ErrObjectAppraisalUnauthorized preserves command3.c's actor-facing
	// permission boundary. It is returned before item lookup, so an
	// unauthorized actor cannot use this command as an inventory oracle.
	ErrObjectAppraisalUnauthorized = errors.New("도둑만 물건을 감정할수 있습니다.")
	// ErrObjectAppraisalMissingItem is the source response for a name or
	// occurrence that is not present in the visible direct inventory roots.
	ErrObjectAppraisalMissingItem = errors.New("당신은 그런것을 갖고 있지 않습니다.")
)

// ObjectAppraisalResult is the deterministic, typed observation returned by
// AppraiseObjectByName. Response is the source-compatible terminal rendering;
// the remaining fields make the observation replay/audit friendly without
// requiring a client to parse Korean text.
//
// Item IDs are canonical server identities. They are included for receipt
// correlation only and must not be accepted as a later mutation authority.
type ObjectAppraisalResult struct {
	Action       string   `json:"action"`
	ActorID      string   `json:"actor_id"`
	RoomID       int16    `json:"room_id"`
	ItemID       string   `json:"item_id"`
	ItemName     string   `json:"item_name"`
	Occurrence   int      `json:"occurrence"`
	Type         byte     `json:"type"`
	TypeLabel    string   `json:"type_label"`
	Category     string   `json:"category"`
	WeaponType   string   `json:"weapon_type,omitempty"`
	ShotsCurrent int16    `json:"shots_current"`
	ShotsMax     int16    `json:"shots_max"`
	DiceCount    int16    `json:"dice_count,omitempty"`
	DiceSides    int16    `json:"dice_sides,omitempty"`
	DicePlus     int16    `json:"dice_plus,omitempty"`
	Adjustment   int8     `json:"adjustment,omitempty"`
	Armor        int8     `json:"armor,omitempty"`
	Flags        [8]byte  `json:"flags"`
	Traits       []string `json:"traits"`
	Response     string   `json:"response"`
}

// AppraisalResult is a short semantic alias for callers that use the verb
// rather than the source command's object-oriented name.
type AppraisalResult = ObjectAppraisalResult

// AppraiseObjectByName validates the canonical player inventory boundary and
// renders the command3.c:info_obj observation. It never returns a candidate
// State and never changes PHIDDN or any other player field.
//
// Selection is deliberately narrower than legacy find_obj: it requires an
// online canonical player, an exact case-insensitive display name, and a
// positive one-based occurrence in direct Inventory order. Equipped items,
// child objects, legacy linked-list inventory, and an un-migrated player are
// not searched or guessed. A direct container root remains a valid target;
// its children are still not searchable through this command.
func (s State) AppraiseObjectByName(actorID, name string, occurrence int) (ObjectAppraisalResult, error) {
	if err := s.Validate(); err != nil {
		return ObjectAppraisalResult{}, err
	}
	if actorID == "" {
		return ObjectAppraisalResult{}, fmt.Errorf("object appraisal actor required")
	}
	actor, ok := s.Players[actorID]
	if !ok || !actor.Online || actor.Body.Type != 0 || actor.Body.Name == "" {
		return ObjectAppraisalResult{}, fmt.Errorf("online object appraisal actor absent")
	}
	if actor.Body.Class != objectAppraisalThiefClass && actor.Body.Class < objectAppraisalInvincibleClass {
		return ObjectAppraisalResult{}, ErrObjectAppraisalUnauthorized
	}
	if occurrence < 1 {
		return ObjectAppraisalResult{}, fmt.Errorf("invalid object appraisal occurrence")
	}
	if name == "" || strings.TrimSpace(name) != name {
		return ObjectAppraisalResult{}, fmt.Errorf("object appraisal item name required")
	}
	// A non-nil ItemCollection is the explicit migration marker. Never fall
	// back to Body.Inventory: doing so would mix canonical and legacy identity
	// domains and make a replay depend on an unversioned linked-list shape.
	if actor.Items == nil || len(actor.Body.Inventory) != 0 {
		return ObjectAppraisalResult{}, fmt.Errorf("online player with migrated items required")
	}
	if err := actor.Items.Validate(); err != nil {
		return ObjectAppraisalResult{}, err
	}

	detectInvisible := flag(actor.Body.Flags[:], objectAppraisalDetectFlag)
	found := 0
	for _, id := range actor.Items.Inventory {
		item, ok := actor.Items.Items[id]
		if !ok || id == "" || !strings.EqualFold(item.Object.Name, name) {
			continue
		}
		// This follows find_obj's PDINVI gate while retaining direct-root
		// selection. A hidden/invisible object is not an oracle for actors who
		// cannot detect invisible items.
		if flag(item.Object.Flags[:], objectInvisibleFlag) && !detectInvisible {
			continue
		}
		found++
		if found != occurrence {
			continue
		}
		result := renderObjectAppraisal(id, item.Object, occurrence)
		result.ActorID = actorID
		result.RoomID = actor.Body.RoomID
		return result, nil
	}
	return ObjectAppraisalResult{}, ErrObjectAppraisalMissingItem
}

// AppraiseObject is the concise API spelling used by command adapters.
func (s State) AppraiseObject(actorID, name string, occurrence int) (ObjectAppraisalResult, error) {
	return s.AppraiseObjectByName(actorID, name, occurrence)
}

// InspectObjectByName is a compatibility spelling for read-only callers. It
// delegates to the same selector so the command cannot drift into a broader
// legacy lookup path.
func (s State) InspectObjectByName(actorID, name string, occurrence int) (ObjectAppraisalResult, error) {
	return s.AppraiseObjectByName(actorID, name, occurrence)
}

func renderObjectAppraisal(id string, object LegacyObject, occurrence int) ObjectAppraisalResult {
	typeLabel, category, weaponType := objectAppraisalType(object.Type)
	traits := objectAppraisalTraits(object.Flags)
	result := ObjectAppraisalResult{
		Action:       "object_appraisal",
		ItemID:       id,
		ItemName:     object.Name,
		Occurrence:   occurrence,
		Type:         object.Type,
		TypeLabel:    typeLabel,
		Category:     category,
		WeaponType:   weaponType,
		ShotsCurrent: object.ShotsCurrent,
		ShotsMax:     object.ShotsMax,
		DiceCount:    object.DiceCount,
		DiceSides:    object.DiceSides,
		DicePlus:     object.DicePlus,
		Adjustment:   int8(object.Adjustment),
		Armor:        int8(object.Armor),
		Flags:        object.Flags,
		Traits:       traits,
	}
	result.Response = renderObjectAppraisalResponse(object, result, traits)
	return result
}

// objectAppraisalType follows the switch in command3.c exactly. The label is
// the text after "종류: ", without the source's final punctuation.
func objectAppraisalType(objectType byte) (label, category, weaponType string) {
	switch objectType {
	case 0:
		return "도 무기", "weapon", "도"
	case 1:
		return "검 무기", "weapon", "검"
	case 2:
		return "봉 무기", "weapon", "봉"
	case 3:
		return "창 무기", "weapon", "창"
	case 4:
		return "궁 무기", "weapon", "궁"
	case 5:
		return "방어구", "armor", ""
	case 6:
		return "약", "potion", ""
	case 7:
		return "주문서", "scroll", ""
	case 8:
		return "주문걸린 물건", "wand", ""
	case 9:
		return "담는 종류", "container", ""
	case 11:
		return "열쇠", "key", ""
	case 12:
		return "광원", "light", ""
	case 13:
		return "모르겠음", "misc", ""
	default:
		// command3.c prints an empty type label for unrecognised values. Keep
		// that behavior rather than inventing a type for unreviewed data.
		return "", "unknown", ""
	}
}

func objectAppraisalTraits(flags [8]byte) []string {
	traits := make([]string, 0, 6)
	if flag(flags[:], objectAppraisalNoMagicFlag) {
		traits = append(traits, "도술사 불제자 거부")
	}
	if flag(flags[:], objectAppraisalGoodOnlyFlag) {
		traits = append(traits, "선한 사람용")
	}
	if flag(flags[:], objectAppraisalEvilOnlyFlag) {
		traits = append(traits, "악한 사람용")
	}
	if flag(flags[:], objectAppraisalEnchantedFlag) {
		traits = append(traits, "빙의 되있음")
	}
	if flag(flags[:], objectAppraisalFemaleOnlyFlag) {
		traits = append(traits, "남성 금지")
	}
	if flag(flags[:], objectAppraisalMaleOnlyFlag) {
		traits = append(traits, "여성 금지")
	}
	return traits
}

func renderObjectAppraisalResponse(object LegacyObject, result ObjectAppraisalResult, traits []string) string {
	var out strings.Builder
	fmt.Fprintf(&out, "이름: %s\n", object.Name)
	fmt.Fprintf(&out, "사용회수 %d\n", object.ShotsCurrent)
	out.WriteString("종류: ")
	if object.Type <= 4 {
		out.WriteString(result.WeaponType)
		out.WriteString(" 무기.\n")
		fmt.Fprintf(&out, "타격치: %d면%d굴림 더하기 %d", object.DiceSides, object.DiceCount, object.DicePlus)
		if result.Adjustment != 0 {
			// command3.c keeps the literal '+' before %d, which means a
			// negative signed-char adjustment is rendered as (+-N).
			fmt.Fprintf(&out, " (+%d)\n", result.Adjustment)
		} else {
			out.WriteByte('\n')
		}
	} else {
		if object.Type == 5 {
			out.WriteString("방어구\n")
			fmt.Fprintf(&out, "방어력: %2.2d", result.Armor)
		} else {
			out.WriteString(result.TypeLabel)
		}
		out.WriteByte('\n')
	}
	out.WriteString("특성 : ")
	if len(traits) == 0 {
		out.WriteString("특성 없음.")
	} else {
		out.WriteString(strings.Join(traits, ", "))
		out.WriteByte('.')
	}
	out.WriteByte('\n')
	return out.String()
}
