package world

import (
	"fmt"
	"strings"
)

const (
	playerBlindFlag           = 42
	playerDetectInvisibleFlag = 21
	playerDetectMagicFlag     = 20
	objectInvisibleFlag       = 2
)

// command3.c:equip_list print order and labels, not MAXWEAR index order.
var equipListSlots = []struct {
	index int
	label string
}{
	{6, "[ 머리 ]"},
	{18, "[ 얼굴 ]"},
	{3, "[  목  ]"},
	{4, "[  목  ]"},
	{0, "[  몸  ]"},
	{1, "[  팔  ]"},
	{5, "[  손  ]"},
	{8, "[손가락]"},
	{9, "[손가락]"},
	{10, "[손가락]"},
	{11, "[손가락]"},
	{12, "[손가락]"},
	{13, "[손가락]"},
	{14, "[손가락]"},
	{15, "[손가락]"},
	{2, "[ 다리 ]"},
	{7, "[  발  ]"},
	{16, "[쥔물건]"},
	{17, "[ 방패 ]"},
	{19, "[ 무기 ]"},
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

// objectCatalogName ports misc.c:obj_str count/MAG display. Count>1 is
// `(xN) ` then the trimmed name. MAG then appends `(+adj)` or `(주문)`.
func objectCatalogName(obj LegacyObject, detectMagic bool, count int) string {
	var b strings.Builder
	if count > 1 {
		fmt.Fprintf(&b, "(x%d) ", count)
	}
	b.WriteString(strings.TrimRight(obj.Name, " "))
	if detectMagic {
		if obj.Adjustment != 0 {
			fmt.Fprintf(&b, "(%+d)", int8(obj.Adjustment))
		} else if obj.MagicPower != 0 {
			b.WriteString("(주문)")
		}
	}
	return b.String()
}

func inventoryObjectVisible(obj LegacyObject, detectInvisible bool) bool {
	return detectInvisible || !flag(obj.Flags[:], objectInvisibleFlag)
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
	detectMagic := flag(p.Body.Flags[:], playerDetectMagicFlag)
	entries := make([]string, 0, len(p.Items.Inventory))
	for i := 0; i < len(p.Items.Inventory); i++ {
		id := p.Items.Inventory[i]
		item := p.Items.Items[id]
		if !inventoryObjectVisible(item.Object, detectInvisible) {
			continue
		}
		count := 1
		for i+1 < len(p.Items.Inventory) {
			next := p.Items.Items[p.Items.Inventory[i+1]]
			if !inventoryObjectVisible(next.Object, detectInvisible) || next.Object.Name != item.Object.Name || next.Object.Adjustment != item.Object.Adjustment {
				break
			}
			i++
			count++
		}
		entries = append(entries, objectCatalogName(item.Object, detectMagic, count))
	}
	if len(entries) == 0 {
		if len(p.Items.Inventory) > 0 {
			return "", nil
		}
		return "소지품:\r\n  없음.\r\n", nil
	}
	return "소지품:\r\n  " + strings.Join(entries, ", ") + ".\r\n", nil
}

// PlayerEquipment renders ready slots in command3.c:equip_list order.
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
	detectMagic := flag(p.Body.Flags[:], playerDetectMagicFlag)
	var out strings.Builder
	out.WriteString("착용 장비:\r\n")
	for _, slot := range equipListSlots {
		id := p.Items.Ready[slot.index]
		if id == "" {
			continue
		}
		out.WriteString(slot.label)
		out.WriteString("  ")
		out.WriteString(objectCatalogName(p.Items.Items[id].Object, detectMagic, 1))
		out.WriteString("\r\n")
	}
	return out.String(), nil
}
