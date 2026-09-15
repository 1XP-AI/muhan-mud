package world

import (
	"errors"
	"reflect"
	"testing"
)

func spellCatalogTestState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"alice"},
			},
		},
		Players: map[string]PlayerState{
			"alice": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
}

func TestSpellCatalogMatchesSourceSpllistAndOspell(t *testing.T) {
	catalog, err := SpellCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != SpellCatalogSize {
		t.Fatalf("catalog length=%d want=%d", len(catalog), SpellCatalogSize)
	}
	wantNames := []string{
		"회복", "삭풍", "발광", "해독", "성현진", "수호진", "화궁", "은둔법",
		"도력반", "은둔감지술", "주문감지술", "축지법", "혼동", "뇌전", "동설주", "빙의",
		"귀환", "소환", "원기회복", "완치", "추적", "부양술", "방열진", "비상술",
		"보마진", "권풍술", "지동술", "화선도", "탄수공", "풍마현", "파초식", "폭진",
		"낙석", "화풍술", "화룡대천", "토합술", "주작현", "열사천", "파천풍", "지옥패",
		"태양안", "선악감지", "저주해소", "방한진", "수생술", "지방호", "천리안", "백치술",
		"치료", "개안술", "공포", "전회복", "전송", "실명", "봉합구", "이혼대법",
	}
	wantLevels := []byte{
		1, 2, 2, 1, 2, 2, 3, 5, 4, 3, 3, 4, 3, 4, 5, 4,
		3, 3, 3, 5, 4, 3, 3, 4, 3, 5, 2, 2, 2, 3, 3, 3,
		5, 3, 3, 4, 4, 4, 5, 5, 5, 3, 4, 3, 3, 3, 4, 5,
		3, 3, 4, 4, 4, 5, 5, 5,
	}
	if len(wantNames) != SpellCatalogSize || len(wantLevels) != SpellCatalogSize {
		t.Fatal("source fixture has an unexpected size")
	}
	for index, entry := range catalog {
		if entry.Index != index || entry.MagicPower != byte(index+1) || entry.Name != wantNames[index] || entry.Level != wantLevels[index] {
			t.Fatalf("entry[%d]=%+v", index, entry)
		}
	}

	wantOffensive := map[int]SpellCatalogEntry{
		1:  {Realm: SpellRealmWind, MPCost: 3, DiceCount: 1, DiceSides: 8, DicePlus: 0, BonusType: 1},
		6:  {Realm: SpellRealmFire, MPCost: 7, DiceCount: 2, DiceSides: 5, DicePlus: 8, BonusType: 2},
		13: {Realm: SpellRealmWind, MPCost: 15, DiceCount: 3, DiceSides: 4, DicePlus: 18, BonusType: 3},
		14: {Realm: SpellRealmWater, MPCost: 25, DiceCount: 4, DiceSides: 5, DicePlus: 30, BonusType: 3},
		25: {Realm: SpellRealmWind, MPCost: 10, DiceCount: 2, DiceSides: 5, DicePlus: 13, BonusType: 2},
		26: {Realm: SpellRealmEarth, MPCost: 3, DiceCount: 1, DiceSides: 8, DicePlus: 0, BonusType: 1},
		27: {Realm: SpellRealmFire, MPCost: 3, DiceCount: 1, DiceSides: 7, DicePlus: 1, BonusType: 1},
		28: {Realm: SpellRealmWater, MPCost: 3, DiceCount: 1, DiceSides: 8, DicePlus: 0, BonusType: 1},
		29: {Realm: SpellRealmWind, MPCost: 7, DiceCount: 2, DiceSides: 5, DicePlus: 7, BonusType: 2},
		30: {Realm: SpellRealmWater, MPCost: 7, DiceCount: 2, DiceSides: 5, DicePlus: 8, BonusType: 2},
		31: {Realm: SpellRealmEarth, MPCost: 7, DiceCount: 2, DiceSides: 5, DicePlus: 7, BonusType: 2},
		32: {Realm: SpellRealmEarth, MPCost: 10, DiceCount: 2, DiceSides: 5, DicePlus: 13, BonusType: 2},
		33: {Realm: SpellRealmFire, MPCost: 10, DiceCount: 2, DiceSides: 5, DicePlus: 13, BonusType: 2},
		34: {Realm: SpellRealmWater, MPCost: 10, DiceCount: 2, DiceSides: 5, DicePlus: 13, BonusType: 2},
		35: {Realm: SpellRealmEarth, MPCost: 15, DiceCount: 3, DiceSides: 4, DicePlus: 19, BonusType: 3},
		36: {Realm: SpellRealmFire, MPCost: 15, DiceCount: 3, DiceSides: 4, DicePlus: 18, BonusType: 3},
		37: {Realm: SpellRealmWater, MPCost: 15, DiceCount: 3, DiceSides: 4, DicePlus: 18, BonusType: 3},
		38: {Realm: SpellRealmWind, MPCost: 25, DiceCount: 4, DiceSides: 5, DicePlus: 30, BonusType: 3},
		39: {Realm: SpellRealmEarth, MPCost: 25, DiceCount: 4, DiceSides: 5, DicePlus: 30, BonusType: 3},
		40: {Realm: SpellRealmFire, MPCost: 25, DiceCount: 4, DiceSides: 5, DicePlus: 30, BonusType: 3},
	}
	if len(wantOffensive) != 20 {
		t.Fatal("ospell fixture has an unexpected size")
	}
	for index, entry := range catalog {
		want, offensive := wantOffensive[index]
		if offensive {
			if entry.Execution != SpellExecutionOffensivePending || entry.Realm != want.Realm || entry.MPCost != want.MPCost || entry.DiceCount != want.DiceCount || entry.DiceSides != want.DiceSides || entry.DicePlus != want.DicePlus || entry.BonusType != want.BonusType {
				t.Fatalf("offensive entry[%d]=%+v want metadata=%+v", index, entry, want)
			}
		} else if entry.Execution == SpellExecutionOffensivePending {
			t.Fatalf("non-ospell entry[%d] marked offensive: %+v", index, entry)
		}
	}
	if got, err := SpellCatalogEntryAt(1); err != nil || got.Name != "삭풍" || got.MagicPower != 2 {
		t.Fatalf("index lookup=%+v err=%v", got, err)
	}
	if got, err := SpellCatalogEntryForMagicPower(2); err != nil || got.Index != 1 || got.Name != "삭풍" {
		t.Fatalf("magicpower lookup=%+v err=%v", got, err)
	}
	if _, err := SpellCatalogEntryAt(-1); !errors.Is(err, ErrSpellCatalogUnavailable) {
		t.Fatalf("negative index err=%v", err)
	}
	if _, err := SpellCatalogEntryForMagicPower(0); !errors.Is(err, ErrSpellCatalogUnavailable) {
		t.Fatalf("zero magicpower err=%v", err)
	}
}

func TestSpellCatalogIsFreshAndFailsClosedOnCorruptSourceTable(t *testing.T) {
	first, err := SpellCatalog()
	if err != nil {
		t.Fatal(err)
	}
	first[0].Name = "변조"
	first[1].MPCost = 999
	second, err := SpellCatalog()
	if err != nil || second[0].Name != "회복" || second[1].MPCost != 3 {
		t.Fatalf("catalog was not independent second=%+v err=%v", second[:2], err)
	}

	original := legacyInfoSpellNames[0]
	legacyInfoSpellNames[0] = ""
	t.Cleanup(func() { legacyInfoSpellNames[0] = original })
	if _, err := SpellCatalog(); !errors.Is(err, ErrSpellCatalogInvalid) {
		t.Fatalf("corrupt source err=%v", err)
	}
}

func TestPlanApplySpellListMatchesInfo2OrderAndIsReadOnly(t *testing.T) {
	state := spellCatalogTestState()
	actor := state.Players["alice"]
	actor.Body.Spells[0] |= 1<<0 | 1<<1
	actor.Body.Spells[6] |= 1 << 7
	state.Players["alice"] = actor
	before := state.clone()

	proposal, err := state.PlanSpellList("alice")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != "spell_list" || proposal.ActorID != "alice" || proposal.RoomID != 1 || len(proposal.Spells) != 3 || proposal.Spells[0].Name != "삭풍" || proposal.Spells[1].Name != "이혼대법" || proposal.Spells[2].Name != "회복" || proposal.Response != "\n주문: 삭풍, 이혼대법, 회복.\n" {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("planning mutated state")
	}

	next, result, err := state.ApplySpellList(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, before) || result.Changed || result.Action != "spell_list" || result.ActorID != "alice" || result.ActorName != "Alice" || !reflect.DeepEqual(result.Spells, proposal.Spells) || result.Response != proposal.Response {
		t.Fatalf("next=%+v result=%+v", next, result)
	}
	next.Players["alice"] = PlayerState{}
	if state.Players["alice"].Body.Name != "Alice" {
		t.Fatal("read-only result shares mutable state")
	}
}

func TestApplySpellListRejectsStaleOrTamperedReceipt(t *testing.T) {
	state := spellCatalogTestState()
	proposal, err := state.PlanSpellList("alice")
	if err != nil {
		t.Fatal(err)
	}

	tampered := proposal
	tampered.Spells = append([]SpellCatalogEntry(nil), proposal.Spells...)
	tampered.Spells = append(tampered.Spells, SpellCatalogEntry{Index: 1, MagicPower: 2, Name: "삭풍", Level: 2, Execution: SpellExecutionOffensivePending})
	if _, _, err := state.ApplySpellList(tampered); !errors.Is(err, ErrSpellListTampered) {
		t.Fatalf("tampered err=%v", err)
	}

	stale := state.clone()
	actor := stale.Players["alice"]
	actor.Body.Spells[0] |= 1
	stale.Players["alice"] = actor
	if _, _, err := stale.ApplySpellList(proposal); !errors.Is(err, ErrSpellListStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}

	for _, actor := range []PlayerState{
		{Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: false},
		{Body: LegacyMonster{Name: "", Type: 0, RoomID: 1}, Online: true},
	} {
		invalid := spellCatalogTestState()
		invalid.Players["alice"] = actor
		if _, err := invalid.PlanSpellList("alice"); err == nil {
			t.Fatalf("invalid actor=%+v err=%v", actor, err)
		}
	}
	if _, err := state.PlanSpellList("missing"); !errors.Is(err, ErrSpellListActorAbsent) {
		t.Fatalf("missing actor err=%v", err)
	}
}

func TestSpellListDoesNotInventBlindClassLevelOrResourceGates(t *testing.T) {
	state := spellCatalogTestState()
	actor := state.Players["alice"]
	actor.Body.Class = 4
	actor.Body.Level = 1
	actor.Body.MPCurrent = 0
	actor.Body.Flags[42/8] |= 1 << (42 % 8)
	actor.Body.Spells[0] |= 1 << 1 // source info_2 prints learned bits even when blind/low MP
	state.Players["alice"] = actor
	result, err := state.ProjectSpellList("alice")
	if err != nil || len(result.Spells) != 1 || result.Spells[0].Name != "삭풍" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
