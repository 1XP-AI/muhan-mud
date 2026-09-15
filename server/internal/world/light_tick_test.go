package world

import "testing"

func TestLightTickConsumesOnlyFirstUsableLight(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{"empty": {Object: LegacyObject{Type: 12, Flags: [8]byte{1: 8}}}, "torch": {Object: LegacyObject{Name: "torch", Type: 12, ShotsCurrent: 1, Flags: [8]byte{1: 8}}}, "other": {Object: LegacyObject{Type: 12, ShotsCurrent: 3, Flags: [8]byte{1: 8}}}}, Ready: [20]string{0: "empty", 1: "torch", 2: "other"}}
	got, err := TickLight(LegacyMonster{}, c)
	if err != nil || got.ExtinguishedID != "torch" || got.Items.Items["torch"].Object.ShotsCurrent != 0 || got.Items.Items["other"].Object.ShotsCurrent != 3 || c.Items["torch"].Object.ShotsCurrent != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	again, err := TickLight(LegacyMonster{}, got.Items)
	if err != nil || again.ExtinguishedID != "" || again.Items.Items["other"].Object.ShotsCurrent != 2 {
		t.Fatalf("%+v %v", again, err)
	}
}

func TestLightTickSpellAndNonConsumable(t *testing.T) {
	c := ItemCollection{Items: map[string]Item{"lamp": {Object: LegacyObject{Type: 12, ShotsCurrent: 3, Flags: [8]byte{1: 8}}}}, Ready: [20]string{0: "lamp"}}
	p := LegacyMonster{}
	p.Flags[2] = 2
	got, err := TickLight(p, c)
	if err != nil || got.Items.Items["lamp"].Object.ShotsCurrent != 3 {
		t.Fatal("spell consumed item")
	}
	item := c.Items["lamp"]
	item.Object.Type = 1
	c.Items["lamp"] = item
	got, err = TickLight(LegacyMonster{}, c)
	if err != nil || got.Items.Items["lamp"].Object.ShotsCurrent != 3 {
		t.Fatal("non light-source consumed")
	}
}
