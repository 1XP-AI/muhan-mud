package world

import (
	"strings"
	"testing"
)

func playerItemsFixture(t *testing.T) State {
	t.Helper()
	s := npcAttackFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"coin-1": {Object: LegacyObject{Name: "동전"}},
		"coin-2": {Object: LegacyObject{Name: "동전"}},
		"cloak":  {Object: LegacyObject{Name: "망토", Adjustment: 2}},
		"sword":  {Object: LegacyObject{Name: "검"}},
	}, Inventory: []string{"coin-1", "coin-2", "cloak"}, Ready: [20]string{19: "sword"}}
	s.Players["a"] = p
	return s
}

func TestPlayerInventoryPreservesOrderGroupsAdjacentItemsAndHidesInvisible(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	item := p.Items.Items["cloak"]
	item.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	p.Items.Items["cloak"] = item
	s.Players["a"] = p
	text, err := s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  (x2) 동전.\r\n" {
		t.Fatalf("inventory=%q err=%v", text, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  (x2) 동전, 망토.\r\n" {
		t.Fatalf("detected inventory=%q err=%v", text, err)
	}
}

func TestPlayerInventoryAppliesMagicSuffixOnlyWithDetectMagic(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items.Items["scroll"] = Item{Object: LegacyObject{Name: "두루마리", MagicPower: 1}}
	p.Items.Inventory = append(p.Items.Inventory, "scroll")
	s.Players["a"] = p
	text, err := s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  (x2) 동전, 망토, 두루마리.\r\n" {
		t.Fatalf("no mag inventory=%q err=%v", text, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerDetectMagicFlag/8] |= 1 << (playerDetectMagicFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  (x2) 동전, 망토(+2), 두루마리(주문).\r\n" {
		t.Fatalf("mag inventory=%q err=%v", text, err)
	}
}

func TestPlayerInventoryEmptyCatalog(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"sword": {Object: LegacyObject{Name: "검"}},
	}, Ready: [20]string{19: "sword"}}
	s.Players["a"] = p
	text, err := s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  없음.\r\n" {
		t.Fatalf("empty inventory=%q err=%v", text, err)
	}
}

func TestPlayerInventoryOmitsNonEmptyAllInvisibleCatalogWithoutDetection(t *testing.T) {
	s := playerItemsFixture(t)
	invisible := LegacyObject{Name: "비밀"}
	invisible.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"secret-1": {Object: invisible},
		"secret-2": {Object: invisible},
	}, Inventory: []string{"secret-1", "secret-2"}}
	s.Players["a"] = p
	text, err := s.PlayerInventory("a")
	if err != nil || text != "" {
		t.Fatalf("undetected invisible inventory=%q err=%v", text, err)
	}

	p = s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerInventory("a")
	want := "소지품:\r\n  (x2) 비밀.\r\n"
	if err != nil || text != want {
		t.Fatalf("detected invisible inventory=%q err=%v want=%q", text, err, want)
	}
}

func TestPlayerInventoryDoesNotInventEmptyName(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"blank": {Object: LegacyObject{}},
	}, Inventory: []string{"blank"}}
	s.Players["a"] = p
	text, err := s.PlayerInventory("a")
	if err != nil || text != "소지품:\r\n  .\r\n" || strings.Contains(text, "이름 없는") {
		t.Fatalf("blank inventory=%q err=%v", text, err)
	}
}

func TestPlayerInventoryAndEquipmentRespectBlindness(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Body.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	s.Players["a"] = p
	inventory, err := s.PlayerInventory("a")
	if err != nil || inventory != "당신은 눈이 멀어서 아무것도 볼 수가 없습니다!\r\n" {
		t.Fatalf("blind inventory=%q err=%v", inventory, err)
	}
	equipment, err := s.PlayerEquipment("a")
	if err != nil || equipment != "당신은 아무것도 볼수가 없습니다. 당신은 눈이 멀어 있습니다.\r\n" {
		t.Fatalf("blind equipment=%q err=%v", equipment, err)
	}
}

func TestPlayerEquipmentUsesEquipListOrderAndLabels(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items.Items["helm"] = Item{Object: LegacyObject{Name: "투구"}}
	p.Items.Items["armor"] = Item{Object: LegacyObject{Name: "갑옷"}}
	p.Items.Items["ring"] = Item{Object: LegacyObject{Name: "반지"}}
	p.Items.Ready = [20]string{0: "armor", 6: "helm", 8: "ring", 19: "sword"}
	s.Players["a"] = p
	text, err := s.PlayerEquipment("a")
	want := "착용 장비:\r\n[ 머리 ]  투구\r\n[  몸  ]  갑옷\r\n[손가락]  반지\r\n[ 무기 ]  검\r\n"
	if err != nil || text != want {
		t.Fatalf("equipment=%q err=%v want=%q", text, err, want)
	}
}

func TestPlayerEquipmentAppliesMagicSuffixOnlyWithDetectMagic(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items.Items["sword"] = Item{Object: LegacyObject{Name: "검", Adjustment: 3}}
	s.Players["a"] = p
	text, err := s.PlayerEquipment("a")
	if err != nil || text != "착용 장비:\r\n[ 무기 ]  검\r\n" {
		t.Fatalf("no mag equipment=%q err=%v", text, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerDetectMagicFlag/8] |= 1 << (playerDetectMagicFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerEquipment("a")
	if err != nil || text != "착용 장비:\r\n[ 무기 ]  검(+3)\r\n" {
		t.Fatalf("mag equipment=%q err=%v", text, err)
	}
}

func TestPlayerEquipmentEmptyCatalog(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Items = &ItemCollection{Items: map[string]Item{
		"coin-1": {Object: LegacyObject{Name: "동전"}},
	}, Inventory: []string{"coin-1"}}
	s.Players["a"] = p
	text, err := s.PlayerEquipment("a")
	if err != nil || text != "당신은 걸치고 있는게 아무것도 없습니다.\r\n" {
		t.Fatalf("empty equipment=%q err=%v", text, err)
	}
}

func TestPlayerEquipmentEmptyCatalogPrecedesBlindResponse(t *testing.T) {
	s := playerItemsFixture(t)
	p := s.Players["a"]
	p.Body.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	p.Items = &ItemCollection{Items: map[string]Item{
		"coin-1": {Object: LegacyObject{Name: "동전"}},
	}, Inventory: []string{"coin-1"}}
	s.Players["a"] = p
	text, err := s.PlayerEquipment("a")
	want := "당신은 걸치고 있는게 아무것도 없습니다.\r\n"
	if err != nil || text != want {
		t.Fatalf("blind empty equipment=%q err=%v want=%q", text, err, want)
	}
}
