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
	if err != nil || !strings.Contains(text, "동전 x2") || strings.Contains(text, "망토") || strings.Contains(text, "검") {
		t.Fatalf("inventory=%q err=%v", text, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerInventory("a")
	if err != nil || !strings.Contains(text, "동전 x2") || !strings.Contains(text, "망토(+2)") {
		t.Fatalf("detected inventory=%q err=%v", text, err)
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
	if err != nil || !strings.Contains(equipment, "눈이 멀어") {
		t.Fatalf("blind equipment=%q err=%v", equipment, err)
	}
}

func TestPlayerEquipmentUsesCanonicalReadySlotLabels(t *testing.T) {
	s := playerItemsFixture(t)
	text, err := s.PlayerEquipment("a")
	if err != nil || !strings.Contains(text, "[ 무기 ]  검") || strings.Contains(text, "item-") {
		t.Fatalf("equipment=%q err=%v", text, err)
	}
}
