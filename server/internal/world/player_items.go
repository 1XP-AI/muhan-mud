package world

import (
	"fmt"
	"strings"
)

const (
	playerBlindFlag           = 42
	playerDetectInvisibleFlag = 21
	objectInvisibleFlag       = 2
)

var equipmentSlotLabels = [20]string{
	"몸", "팔", "다리", "목", "목", "손", "머리", "발",
	"손가락", "손가락", "손가락", "손가락", "손가락", "손가락", "손가락", "손가락",
	"쥔물건", "방패", "얼굴", "무기",
}

func (s State) playerItemsForRead(actorID string) (PlayerState, error) {
	if err := s.Validate(); err != nil {
		return PlayerState{}, err
	}
	p, ok := s.Players[actorID]
	if !ok || !p.Online || p.Items == nil {
		return PlayerState{}, fmt.Errorf("online player with migrated items required")
	}
	if err := p.Items.Validate(); err != nil {
		return PlayerState{}, err
	}
	return p, nil
}

func displayItemName(item Item) string {
	name := strings.TrimSpace(item.Object.Name)
	if name == "" {
		name = "이름 없는 물건"
	}
	if item.Object.Adjustment != 0 {
		name = fmt.Sprintf("%s(%+d)", name, int(int8(item.Object.Adjustment)))
	}
	return name
}

// PlayerInventory renders the ordered root inventory without exposing item IDs.
// It follows command2's blind and detect-invisible visibility boundary and keeps
// adjacent equal objects grouped without map-order dependence.
func (s State) PlayerInventory(actorID string) (string, error) {
	p, err := s.playerItemsForRead(actorID)
	if err != nil {
		return "", err
	}
	if flag(p.Body.Flags[:], playerBlindFlag) {
		return "당신은 눈이 멀어서 아무것도 볼 수가 없습니다!\r\n", nil
	}
	detectInvisible := flag(p.Body.Flags[:], playerDetectInvisibleFlag)
	entries := make([]string, 0, len(p.Items.Inventory))
	for i := 0; i < len(p.Items.Inventory); i++ {
		id := p.Items.Inventory[i]
		item := p.Items.Items[id]
		if !detectInvisible && flag(item.Object.Flags[:], objectInvisibleFlag) {
			continue
		}
		name := displayItemName(item)
		count := 1
		for i+1 < len(p.Items.Inventory) {
			next := p.Items.Items[p.Items.Inventory[i+1]]
			if (!detectInvisible && flag(next.Object.Flags[:], objectInvisibleFlag)) || next.Object.Name != item.Object.Name || next.Object.Adjustment != item.Object.Adjustment {
				break
			}
			i++
			count++
		}
		if count > 1 {
			name = fmt.Sprintf("%s x%d", name, count)
		}
		entries = append(entries, name)
	}
	if len(entries) == 0 {
		return "소지품:\r\n  없음.\r\n", nil
	}
	return "소지품:\r\n  " + strings.Join(entries, ", ") + ".\r\n", nil
}

// PlayerEquipment renders the 20 canonical ready slots in C MAXWEAR order.
// Equipment visibility is blocked while blind, but the player can inspect their
// own ready objects when not blind even if another player would not detect them.
func (s State) PlayerEquipment(actorID string) (string, error) {
	p, err := s.playerItemsForRead(actorID)
	if err != nil {
		return "", err
	}
	found := false
	for _, id := range p.Items.Ready {
		if id != "" {
			found = true
			break
		}
	}
	if !found {
		return "당신은 걸치고 있는게 아무것도 없습니다.\r\n", nil
	}
	if flag(p.Body.Flags[:], playerBlindFlag) {
		return "당신은 아무것도 볼수가 없습니다. 당신은 눈이 멀어 있습니다.\r\n", nil
	}
	var out strings.Builder
	out.WriteString("착용 장비:\r\n")
	for slot, id := range p.Items.Ready {
		if id == "" {
			continue
		}
		fmt.Fprintf(&out, "[ %s ]  %s\r\n", equipmentSlotLabels[slot], displayItemName(p.Items.Items[id]))
	}
	return out.String(), nil
}
