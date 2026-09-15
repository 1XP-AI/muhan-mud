package world

import (
	"fmt"
	"sort"
)

func randomIn(roll func(int, int) int, lo, hi int) (int, error) {
	if roll == nil || lo > hi {
		return 0, fmt.Errorf("invalid random request %d..%d", lo, hi)
	}
	n := roll(lo, hi)
	if n < lo || n > hi {
		return 0, fmt.Errorf("random result outside %d..%d", lo, hi)
	}
	return n, nil
}

// EnchantObject ports rand_enchant on a value, retaining the raw signed-char
// adjustment contract. Numeric overflow is rejected instead of wrapping pdice.
func EnchantObject(o LegacyObject, roll func(int, int) int) (LegacyObject, error) {
	n, err := randomIn(roll, 1, 100)
	if err != nil {
		return LegacyObject{}, err
	}
	bonus := 0
	switch {
	case n > 98:
		bonus = 3
	case n > 90:
		bonus = 2
	case n > 50:
		bonus = 1
	}
	dice := int(o.DicePlus)
	if bonus > 0 {
		o.Flags[1] |= 64
		o.Adjustment = byte(bonus)
		dice += bonus
	}
	if dice < int(int8(o.Adjustment)) {
		dice = int(int8(o.Adjustment))
	}
	if dice > 32767 || dice < -32768 {
		return LegacyObject{}, fmt.Errorf("enchantment dice overflow")
	}
	o.DicePlus = int16(dice)
	return o, nil
}

// SpawnPermanentMonster builds one fresh value from a raw catalog template.
// It preserves random call order from add_permcrt_rom, including enchantment
// between carry draws. All dependency failures return no partial instance.
// Runtime ID, room assignment, active list and persistence belong to the caller.
func SpawnPermanentMonster(template LegacyMonster, now int32, load func(int16) (LegacyObject, error), roll func(int, int) int) (LegacyMonster, error) {
	if len(template.Inventory) != 0 {
		return LegacyMonster{}, fmt.Errorf("catalog monster has unexpected inventory")
	}
	m := template
	m.Inventory = nil
	m.Timers[8].LastTime = now
	m.Timers[8].Interval = 60
	n, err := randomIn(roll, 1, 100)
	if err != nil {
		return LegacyMonster{}, err
	}
	attempts := 1
	if n >= 96 {
		attempts = 3
	} else if n >= 90 {
		attempts = 2
	}
	fixed := flag(m.Flags[:], 25)
	for i := 0; i < attempts; i++ {
		upper := 9
		if fixed {
			upper = 49
		}
		slot, err := randomIn(roll, 0, upper)
		if err != nil {
			return LegacyMonster{}, err
		}
		if slot > 9 || m.Carry[slot] == 0 {
			continue
		}
		if load == nil {
			return LegacyMonster{}, fmt.Errorf("missing object loader")
		}
		o, err := load(m.Carry[slot])
		if err != nil {
			return LegacyMonster{}, err
		}
		if len(o.Contents) != 0 {
			return LegacyMonster{}, fmt.Errorf("catalog object has unexpected contents")
		}
		if flag(o.Flags[:], 21) {
			o, err = EnchantObject(o, roll)
			if err != nil {
				return LegacyMonster{}, err
			}
		}
		m.Inventory = append(m.Inventory, o)
	}
	if !fixed && m.Gold != 0 {
		gold, err := randomIn(roll, int(m.Gold/10), int(m.Gold))
		if err != nil {
			return LegacyMonster{}, err
		}
		m.Gold = int32(gold)
	}
	sort.SliceStable(m.Inventory, func(i, j int) bool {
		a, b := m.Inventory[i], m.Inventory[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return int8(a.Adjustment) < int8(b.Adjustment)
	})
	m.Flags[0] |= 1
	return m, nil
}
