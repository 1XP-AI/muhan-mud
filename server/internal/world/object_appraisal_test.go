package world

import (
	"reflect"
	"strings"
	"testing"
)

func objectAppraisalTestFlag(bit int) [8]byte {
	var flags [8]byte
	flags[bit/8] |= 1 << (bit % 8)
	return flags
}

func objectAppraisalTestState(class byte) State {
	var traits [8]byte
	for _, bit := range []int{objectAppraisalNoMagicFlag, objectAppraisalGoodOnlyFlag, objectAppraisalEvilOnlyFlag, objectAppraisalEnchantedFlag, objectAppraisalFemaleOnlyFlag, objectAppraisalMaleOnlyFlag} {
		traits[bit/8] |= 1 << (bit % 8)
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, Class: class, RoomID: 1},
				Online: true,
				Items: &ItemCollection{
					Items: map[string]Item{
						"sword-1": {Object: LegacyObject{Name: "검", ShotsMax: 9, ShotsCurrent: 7, DiceCount: 2, DiceSides: 6, DicePlus: 3, Adjustment: 254, Flags: traits}},
						"sword-2": {Object: LegacyObject{Name: "검", ShotsMax: 5, ShotsCurrent: 4, DiceCount: 1, DiceSides: 4, DicePlus: 0}},
						"armor":   {Object: LegacyObject{Name: "갑옷", Type: 5, ShotsMax: 20, ShotsCurrent: 17, Armor: 5}},
						"bag":     {Object: LegacyObject{Name: "가방", Type: 9}, Contents: []string{"gem"}},
						"gem":     {Object: LegacyObject{Name: "보석", Type: 13, ShotsCurrent: 1}},
						"lamp":    {Object: LegacyObject{Name: "등불", Type: 12, ShotsCurrent: 3}},
						"hidden":  {Object: LegacyObject{Name: "숨은검", Flags: objectAppraisalTestFlag(objectInvisibleFlag)}},
					},
					Inventory: []string{"sword-1", "sword-2", "armor", "bag", "lamp", "hidden"},
				},
			},
		},
	}
}

func TestAppraiseObjectByNameRendersSourceContractAndTypedFields(t *testing.T) {
	s := objectAppraisalTestState(objectAppraisalThiefClass)
	result, err := s.AppraiseObjectByName("actor", "검", 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "object_appraisal" || result.ItemID != "sword-1" || result.ItemName != "검" || result.Occurrence != 1 {
		t.Fatalf("result identity=%+v", result)
	}
	if result.Type != 0 || result.TypeLabel != "도 무기" || result.Category != "weapon" || result.WeaponType != "도" {
		t.Fatalf("result type=%+v", result)
	}
	if result.ShotsCurrent != 7 || result.ShotsMax != 9 || result.DiceCount != 2 || result.DiceSides != 6 || result.DicePlus != 3 || result.Adjustment != -2 {
		t.Fatalf("result weapon values=%+v", result)
	}
	wantTraits := []string{"도술사 불제자 거부", "선한 사람용", "악한 사람용", "빙의 되있음", "남성 금지", "여성 금지"}
	if !reflect.DeepEqual(result.Traits, wantTraits) {
		t.Fatalf("traits=%v want=%v", result.Traits, wantTraits)
	}
	wantResponse := "이름: 검\n사용회수 7\n종류: 도 무기.\n타격치: 6면2굴림 더하기 3 (+-2)\n특성 : 도술사 불제자 거부, 선한 사람용, 악한 사람용, 빙의 되있음, 남성 금지, 여성 금지.\n"
	if result.Response != wantResponse {
		t.Fatalf("response=%q want=%q", result.Response, wantResponse)
	}
}

func TestAppraiseObjectByNameSelectsExactDirectRootOccurrence(t *testing.T) {
	s := objectAppraisalTestState(objectAppraisalThiefClass)
	result, err := s.AppraiseObjectByName("actor", "검", 2)
	if err != nil || result.ItemID != "sword-2" || result.ShotsCurrent != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := s.AppraiseObjectByName("actor", "검", 0); err == nil {
		t.Fatal("zero occurrence accepted")
	}
	if _, err := s.AppraiseObjectByName("actor", "검으로", 1); err == nil {
		t.Fatal("prefix name accepted")
	}
	if result, err := s.AppraiseObjectByName("actor", "가방", 1); err != nil || result.ItemID != "bag" || result.TypeLabel != "담는 종류" {
		t.Fatalf("direct container root was not appraisable: result=%+v err=%v", result, err)
	}
	if _, err := s.AppraiseObjectByName("actor", "보석", 1); err == nil {
		t.Fatal("nested child was selected through direct-root API")
	}
}

func TestAppraiseObjectByNameRequiresThiefOrInvincibleAndCanonicalInventory(t *testing.T) {
	for _, class := range []byte{0, 7} {
		if _, err := objectAppraisalTestState(class).AppraiseObjectByName("actor", "검", 1); err != ErrObjectAppraisalUnauthorized {
			t.Fatalf("class=%d err=%v want unauthorized", class, err)
		}
	}
	for _, class := range []byte{objectAppraisalThiefClass, objectAppraisalInvincibleClass, 12} {
		if _, err := objectAppraisalTestState(class).AppraiseObjectByName("actor", "검", 1); err != nil {
			t.Fatalf("class=%d err=%v", class, err)
		}
	}

	legacy := objectAppraisalTestState(objectAppraisalThiefClass)
	actor := legacy.Players["actor"]
	actor.Items = nil
	actor.Body.Inventory = []LegacyObject{{Name: "검"}}
	legacy.Players["actor"] = actor
	if _, err := legacy.AppraiseObjectByName("actor", "검", 1); err == nil || !strings.Contains(err.Error(), "migrated items") {
		t.Fatalf("legacy inventory was not rejected: %v", err)
	}

	hidden := objectAppraisalTestState(objectAppraisalThiefClass)
	if _, err := hidden.AppraiseObjectByName("actor", "숨은검", 1); err == nil {
		t.Fatal("invisible root was visible without detect-invisible")
	}
	actor = hidden.Players["actor"]
	actor.Body.Flags[objectAppraisalDetectFlag/8] |= 1 << (objectAppraisalDetectFlag % 8)
	hidden.Players["actor"] = actor
	if _, err := hidden.AppraiseObjectByName("actor", "숨은검", 1); err != nil {
		t.Fatalf("detect-invisible actor could not appraise root: %v", err)
	}
}

func TestAppraiseObjectByNameRendersArmorAndNonWeaponTypes(t *testing.T) {
	s := objectAppraisalTestState(objectAppraisalInvincibleClass)
	armor, err := s.AppraiseObjectByName("actor", "갑옷", 1)
	if err != nil {
		t.Fatal(err)
	}
	if armor.TypeLabel != "방어구" || armor.Category != "armor" || armor.Armor != 5 || armor.Response != "이름: 갑옷\n사용회수 17\n종류: 방어구\n방어력: 05\n특성 : 특성 없음.\n" {
		t.Fatalf("armor=%+v", armor)
	}
	for _, tc := range []struct {
		name, want string
		typeCode   byte
	}{
		{name: "가방", want: "담는 종류", typeCode: 9},
		{name: "등불", want: "광원", typeCode: 12},
	} {
		result, err := s.AppraiseObjectByName("actor", tc.name, 1)
		if err != nil || result.Type != tc.typeCode || result.TypeLabel != tc.want || result.Category == "" {
			t.Fatalf("type=%+v err=%v", result, err)
		}
	}
}

func TestAppraiseObjectDoesNotMutateState(t *testing.T) {
	s := objectAppraisalTestState(objectAppraisalThiefClass)
	before := s
	if _, err := s.AppraiseObject("actor", "검", 1); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("object appraisal mutated the world snapshot")
	}
}
