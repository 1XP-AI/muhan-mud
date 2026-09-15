package world

import (
	"errors"
	"testing"
)

func zapFlag(bit uint) [8]byte {
	var flags [8]byte
	flags[bit/8] |= 1 << (bit % 8)
	return flags
}

func zapWand(name string, shots int16, magic byte) LegacyObject {
	return LegacyObject{Name: name, Type: zapWandType, ShotsCurrent: shots, ShotsMax: shots, MagicPower: magic}
}

func zapState(object LegacyObject, ready bool) State {
	items := &ItemCollection{Items: map[string]Item{"wand": {Object: object}}}
	if ready {
		items.Ready[0] = "wand"
	} else {
		items.Inventory = []string{"wand"}
	}
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "숲"}},
				Items:     &ItemCollection{Items: map[string]Item{}},
				PlayerIDs: []string{"a", "b"},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Online: true,
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Level: 10, HPMax: 30, HPCurrent: 10, Class: 0},
				Items:  items,
			},
			"b": {
				Online: true,
				Body:   LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Level: 8, HPMax: 20, HPCurrent: 5, Class: 0},
				Items:  &ItemCollection{Items: map[string]Item{}, Inventory: []string{}},
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 16, HPCurrent: 8}},
		},
	}
	s.Rooms[1] = func() RoomState {
		room := s.Rooms[1]
		room.NPCIDs = []string{"wolf"}
		return room
	}()
	return s
}

func zapMustPlan(t *testing.T, s State, item string, occ int, target string, targetOcc int, now int32, rolls ...int) ZapProposal {
	t.Helper()
	i := 0
	p, err := s.PlanZap("a", item, occ, target, targetOcc, ZapOptions{Now: now, Roll: func(low, high int) int {
		if i >= len(rolls) {
			t.Fatalf("unexpected roll %d..%d at %d", low, high, i)
		}
		n := rolls[i]
		i++
		if n < low || n > high {
			t.Fatalf("fixture roll %d outside %d..%d", n, low, high)
		}
		return n
	}})
	if err != nil {
		t.Fatal(err)
	}
	if i != len(rolls) {
		t.Fatalf("used %d rolls want %d", i, len(rolls))
	}
	return p
}

func TestPlanApplyZapVigorSelfConsumesShotAndClearsHidden(t *testing.T) {
	s := zapState(zapWand("회복봉", 2, 1), false)
	actor := s.Players["a"]
	setSettingFlag(&actor.Body, zapHiddenFlag, true)
	s.Players["a"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := zapMustPlan(t, s, "회복봉", 1, "", 1, 100, 1, 4)
	if p.Action != ZapVigorSelf || !p.Changed || !p.ConsumedShot || p.ShotsAfter != 1 || p.HPDelta != 4 {
		t.Fatalf("proposal=%+v", p)
	}
	if s.Players["a"].Body.HPCurrent != 10 || s.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 2 {
		t.Fatal("planning mutated source")
	}
	next, result, err := s.ApplyZap(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["a"]
	if got.Body.HPCurrent != 14 || flag(got.Body.Flags[:], zapHiddenFlag) || got.Items.Items["wand"].Object.ShotsCurrent != 1 {
		t.Fatalf("after=%+v", got)
	}
	if got.Body.Timers[zapSpellTimer] != (LegacyTimer{LastTime: 100, Interval: zapSpellInterval}) {
		t.Fatalf("timer=%+v", got.Body.Timers[zapSpellTimer])
	}
	if result.Response != ZapVigorSelfResponse || result.Action != ZapVigorSelf {
		t.Fatalf("result=%+v", result)
	}
	if _, _, err := next.ApplyZap(p); !errors.Is(err, ErrZapStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
}

func TestPlanZapSpellFailureConsumesShotWithoutHeal(t *testing.T) {
	s := zapState(zapWand("회복봉", 3, 1), false)
	actor := s.Players["a"]
	actor.Body.Class = 4 // FIGHTER: chance is small
	actor.Body.Level = 1
	s.Players["a"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := zapMustPlan(t, s, "회복봉", 1, "", 1, 50, 100)
	if p.Action != ZapSpellFail || !p.SpellFailed || !p.ConsumedShot || p.HPDelta != 0 || p.ShotsAfter != 2 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyZap(p)
	if err != nil {
		t.Fatal(err)
	}
	got := next.Players["a"]
	if got.Body.HPCurrent != 10 || got.Items.Items["wand"].Object.ShotsCurrent != 2 || result.Response != ZapSpellFailResponse {
		t.Fatalf("after=%+v result=%+v", got, result)
	}
}

func TestPlanApplyZapVigorSecondSpellFailKeepsShot(t *testing.T) {
	// magic2.c:vigor rolls spell_fail again for FIGHTER/BARBARIAN when how!=POTION.
	// zap() already succeeded, so n==0 keeps the charge and skips target lookup.
	for _, tc := range []struct {
		name  string
		class byte
	}{
		{"fighter", 4},
		{"barbarian", 2},
	} {
		t.Run(tc.name+" self keeps shot", func(t *testing.T) {
			s := zapState(zapWand("회복봉", 3, 1), false)
			actor := s.Players["a"]
			actor.Body.Class = tc.class
			actor.Body.Level = 1
			actor.Body.Stats[3] = 10
			setSettingFlag(&actor.Body, zapHiddenFlag, true)
			s.Players["a"] = actor
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			p := zapMustPlan(t, s, "회복봉", 1, "", 1, 50, 1, 100)
			if p.Action != ZapSpellFail || !p.SpellFailed || p.ConsumedShot || p.HPDelta != 0 || p.ShotsAfter != 3 || p.TargetID != "" || len(p.Rolls) != 2 {
				t.Fatalf("proposal=%+v", p)
			}
			next, result, err := s.ApplyZap(p)
			if err != nil {
				t.Fatal(err)
			}
			got := next.Players["a"]
			if got.Body.HPCurrent != 10 || got.Items.Items["wand"].Object.ShotsCurrent != 3 || result.Response != ZapSpellFailResponse {
				t.Fatalf("after=%+v result=%+v", got, result)
			}
			if flag(got.Body.Flags[:], zapHiddenFlag) || got.Body.Timers[zapSpellTimer] != (LegacyTimer{LastTime: 50, Interval: zapSpellInterval}) {
				t.Fatalf("admission after=%+v", got.Body)
			}
		})
		t.Run(tc.name+" target skips lookup", func(t *testing.T) {
			s := zapState(zapWand("회복봉", 2, 1), false)
			actor := s.Players["a"]
			actor.Body.Class = tc.class
			actor.Body.Level = 1
			actor.Body.Stats[3] = 10
			s.Players["a"] = actor
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			p := zapMustPlan(t, s, "회복봉", 1, "Bob", 1, 40, 1, 100)
			if p.Action != ZapSpellFail || p.ConsumedShot || p.TargetID != "" || p.HPDelta != 0 || len(p.Events) != 0 {
				t.Fatalf("proposal=%+v", p)
			}
			next, _, err := s.ApplyZap(p)
			if err != nil {
				t.Fatal(err)
			}
			if next.Players["b"].Body.HPCurrent != 5 || next.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 2 {
				t.Fatalf("target mutated after=%+v actor=%+v", next.Players["b"], next.Players["a"])
			}
		})
		t.Run(tc.name+" both succeed then heal", func(t *testing.T) {
			s := zapState(zapWand("회복봉", 2, 1), false)
			actor := s.Players["a"]
			actor.Body.Class = tc.class
			actor.Body.Level = 1
			actor.Body.Stats[3] = 10
			s.Players["a"] = actor
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			p := zapMustPlan(t, s, "회복봉", 1, "", 1, 40, 1, 1, 4)
			if p.Action != ZapVigorSelf || !p.ConsumedShot || p.HPDelta != 4 || p.ShotsAfter != 1 || len(p.Rolls) != 3 {
				t.Fatalf("proposal=%+v", p)
			}
			next, result, err := s.ApplyZap(p)
			if err != nil {
				t.Fatal(err)
			}
			got := next.Players["a"]
			if got.Body.HPCurrent != 14 || got.Items.Items["wand"].Object.ShotsCurrent != 1 || result.Action != ZapVigorSelf {
				t.Fatalf("after=%+v result=%+v", got, result)
			}
		})
		t.Run(tc.name+" target both succeed", func(t *testing.T) {
			s := zapState(zapWand("회복봉", 2, 1), false)
			actor := s.Players["a"]
			actor.Body.Class = tc.class
			actor.Body.Level = 1
			actor.Body.Stats[3] = 10
			s.Players["a"] = actor
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			p := zapMustPlan(t, s, "회복봉", 1, "Bob", 1, 40, 1, 1, 3)
			if p.Action != ZapVigorTarget || !p.ConsumedShot || p.TargetID != "b" || p.HPDelta != 3 || len(p.Rolls) != 3 {
				t.Fatalf("proposal=%+v", p)
			}
			next, _, err := s.ApplyZap(p)
			if err != nil {
				t.Fatal(err)
			}
			if next.Players["b"].Body.HPCurrent != 8 || next.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 1 {
				t.Fatalf("after actor=%+v target=%+v", next.Players["a"], next.Players["b"])
			}
		})
	}
}

func TestPlanZapVigorSecondSpellFailSkipsUnresolvedNPC(t *testing.T) {
	s := zapState(zapWand("회복봉", 1, 1), false)
	actor := s.Players["a"]
	actor.Body.Class = 4
	actor.Body.Level = 1
	actor.Body.Stats[3] = 10
	s.Players["a"] = actor
	s.NPCs = nil
	room := s.Rooms[1]
	room.NPCIDs = nil
	s.Rooms[1] = room
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := zapMustPlan(t, s, "회복봉", 1, "늑대", 1, 20, 1, 100)
	if p.Action != ZapSpellFail || p.ConsumedShot || p.TargetID != "" {
		t.Fatalf("proposal=%+v", p)
	}
	if _, _, err := s.ApplyZap(p); err != nil {
		t.Fatal(err)
	}
}

func TestPlanZapFighterVigorSecondSuccessThenMissingTarget(t *testing.T) {
	s := zapState(zapWand("회복봉", 2, 1), false)
	actor := s.Players["a"]
	actor.Body.Class = 4
	actor.Body.Level = 1
	actor.Body.Stats[3] = 10
	s.Players["a"] = actor
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := zapMustPlan(t, s, "회복봉", 1, "없는사람", 1, 20, 1, 1)
	if p.Action != ZapMissingTarget || p.ConsumedShot || p.ShotsAfter != 2 || len(p.Rolls) != 2 {
		t.Fatalf("proposal=%+v", p)
	}
	next, _, err := s.ApplyZap(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 2 {
		t.Fatal("missing target consumed a shot")
	}
}

func TestPlanZapGatesClassPermissionTargetAndFailClosed(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		p, err := s.PlanZap("a", "", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Changed || p.Action != ZapUsage || p.Response != ZapUsageResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("blind", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		actor := s.Players["a"]
		setSettingFlag(&actor.Body, zapBlindFlag, true)
		s.Players["a"] = actor
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapBlind || p.Response != ZapBlindResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("missing selector", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		p, err := s.PlanZap("a", "없는이름", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapMissing || p.Response != ZapMissingResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("not wand", func(t *testing.T) {
		s := zapState(LegacyObject{Name: "검", Type: 1, ShotsCurrent: 1, MagicPower: 1}, false)
		p, err := s.PlanZap("a", "검", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapNotWand || p.Response != ZapNotWandResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 0, 1), false)
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapEmpty || p.Response != ZapEmptyResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("class", func(t *testing.T) {
		object := zapWand("회복봉", 1, 1)
		object.Flags = zapFlag(zapClassSelect)
		object.Flags[36/8] |= 1 << (36 % 8) // mage-only (OCLSEL+5)
		s := zapState(object, false)
		actor := s.Players["a"]
		actor.Body.Class = 4 // fighter checks OCLSEL+4, which is unset
		s.Players["a"] = actor
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapClass || p.Response != ZapClassResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("alignment evaporate", func(t *testing.T) {
		object := zapWand("회복봉", 1, 1)
		object.Flags = zapFlag(zapGoodOnly)
		s := zapState(object, false)
		actor := s.Players["a"]
		actor.Body.Alignment = -200
		s.Players["a"] = actor
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapEvaporate || !p.MovedToRoom {
			t.Fatalf("p=%+v err=%v", p, err)
		}
		next, _, err := s.ApplyZap(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := next.Players["a"].Items.Items["wand"]; ok {
			t.Fatal("wand remained in inventory")
		}
		if _, ok := next.Rooms[1].Items.Items["wand"]; !ok {
			t.Fatal("wand not moved to room")
		}
	})
	t.Run("nomagic", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		room := s.Rooms[1]
		room.Resource.Flags = zapFlag(zapNoMagicRoom)
		s.Rooms[1] = room
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
		if err != nil || p.Action != ZapNoOp || p.Response != ZapNoOpResponse {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("cooldown", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		actor := s.Players["a"]
		actor.Body.Timers[zapSpellTimer] = LegacyTimer{LastTime: 10, Interval: 3}
		s.Players["a"] = actor
		p, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 11})
		if err != nil || p.Action != ZapCooldown || p.WaitSeconds != 2 || p.Response != "2초동안 기다리세요.\n" {
			t.Fatalf("p=%+v err=%v", p, err)
		}
	})
	t.Run("unmigrated spell", func(t *testing.T) {
		s := zapState(zapWand("삭풍봉", 1, 2), false)
		_, err := s.PlanZap("a", "삭풍봉", 1, "", 1, ZapOptions{Now: 1})
		if !errors.Is(err, ErrZapSpellUnavailable) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("missing target", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 2, 1), false)
		p := zapMustPlan(t, s, "회복봉", 1, "없는사람", 1, 20, 1)
		if p.Action != ZapMissingTarget || p.ConsumedShot || p.ShotsAfter != 2 || p.Response != ZapMissingTargetResponse {
			t.Fatalf("p=%+v", p)
		}
		next, _, err := s.ApplyZap(p)
		if err != nil {
			t.Fatal(err)
		}
		if next.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 2 {
			t.Fatal("missing target consumed a shot")
		}
		if next.Players["a"].Body.Timers[zapSpellTimer].LastTime != 20 {
			t.Fatal("missing target skipped LT_SPELL")
		}
	})
	t.Run("unmigrated npc domain", func(t *testing.T) {
		s := zapState(zapWand("회복봉", 1, 1), false)
		s.NPCs = nil
		room := s.Rooms[1]
		room.NPCIDs = nil
		s.Rooms[1] = room
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
		_, err := s.PlanZap("a", "회복봉", 1, "늑대", 1, ZapOptions{Now: 1, Roll: func(int, int) int { return 1 }})
		if !errors.Is(err, ErrZapNPCUnresolved) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestPlanApplyZapVigorTargetHealsPlayerAndNPC(t *testing.T) {
	s := zapState(zapWand("회복봉", 2, 1), true)
	p := zapMustPlan(t, s, "회복봉", 1, "Bob", 1, 40, 1, 3)
	if p.Action != ZapVigorTarget || p.TargetKind != ZapTargetPlayer || p.TargetID != "b" || p.HPDelta != 3 {
		t.Fatalf("proposal=%+v", p)
	}
	next, result, err := s.ApplyZap(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["b"].Body.HPCurrent != 8 || next.Players["a"].Body.HPCurrent != 10 {
		t.Fatalf("hp actor=%d target=%d", next.Players["a"].Body.HPCurrent, next.Players["b"].Body.HPCurrent)
	}
	if next.Players["a"].Items.Items["wand"].Object.ShotsCurrent != 1 || len(result.Events) != 2 {
		t.Fatalf("result=%+v", result)
	}

	s = zapState(zapWand("회복봉", 2, 1), false)
	p = zapMustPlan(t, s, "회복봉", 1, "늑대", 1, 40, 1, 5)
	if p.Action != ZapVigorTarget || p.TargetKind != ZapTargetNPC || p.TargetID != "wolf" {
		t.Fatalf("npc proposal=%+v", p)
	}
	next, result, err = s.ApplyZap(p)
	if err != nil {
		t.Fatal(err)
	}
	if next.NPCs["wolf"].Body.HPCurrent != 13 || len(result.Events) != 1 {
		t.Fatalf("npc after=%+v result=%+v", next.NPCs["wolf"], result)
	}
}

func TestPlanZapReadyOccurrenceAndCanonicalInventory(t *testing.T) {
	s := zapState(zapWand("회복봉", 1, 1), false)
	s.Players["a"].Items.Items["wand2"] = Item{Object: zapWand("회복봉", 1, 1)}
	s.Players["a"].Items.Inventory = []string{"wand", "wand2"}
	p := zapMustPlan(t, s, "회복봉", 2, "", 1, 9, 1, 2)
	if p.ItemID != "wand2" {
		t.Fatalf("occurrence selected %s", p.ItemID)
	}

	s = zapState(zapWand("회복봉", 1, 1), false)
	actor := s.Players["a"]
	actor.Items = nil
	s.Players["a"] = actor
	_, err := s.PlanZap("a", "회복봉", 1, "", 1, ZapOptions{Now: 1})
	if !errors.Is(err, ErrZapActorAbsent) {
		t.Fatalf("err=%v", err)
	}
}

func zapNamedWand(name string, keys [3]string) LegacyObject {
	object := zapWand(name, 1, 1)
	object.Keys = keys
	return object
}

func TestPlanZapSelectorMatchesCEqualPrefixesAcrossDirectRoots(t *testing.T) {
	s := zapState(zapWand("unused", 1, 1), false)
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"display":   {Object: zapNamedWand("회복봉원본", [3]string{"display-alias", "", ""})},
			"key0":      {Object: zapNamedWand("전혀다른이름0", [3]string{"HEAL-ZERO", "", ""})},
			"key1":      {Object: zapNamedWand("전혀다른이름1", [3]string{"", "HeAl-OnE", ""})},
			"key2":      {Object: zapNamedWand("전혀다른이름2", [3]string{"", "", "hEaL-tWo"})},
			"container": {Object: LegacyObject{Name: "가방"}, Contents: []string{"nested"}},
			"nested":    {Object: zapNamedWand("nested-heal", [3]string{"nested-heal", "", ""})},
		},
		Inventory: []string{"display", "key0", "key1", "key2", "container"},
	}
	s.Players["a"] = actor

	cases := []struct {
		name     string
		selector string
		wantID   string
	}{
		{name: "display prefix", selector: "회복", wantID: "display"},
		{name: "key zero prefix", selector: "heal-zero", wantID: "key0"},
		{name: "key one case fold", selector: "heal-one", wantID: "key1"},
		{name: "key two case fold", selector: "HEAL-TWO", wantID: "key2"},
		{name: "nested item excluded", selector: "nested-heal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.wantID == "" {
				p, err := s.PlanZap("a", tc.selector, 1, "", 1, ZapOptions{Now: 100})
				if err != nil || p.Action != ZapMissing || p.Response != ZapMissingResponse {
					t.Fatalf("nested selector proposal=%+v err=%v", p, err)
				}
				return
			}
			p := zapMustPlan(t, s, tc.selector, 1, "", 1, 100, 1, 1)
			if p.ItemID != tc.wantID || p.Location != ZapInventoryRoot || p.Occurrence != 1 {
				t.Fatalf("selector=%q proposal=%+v", tc.selector, p)
			}
		})
	}
}

func TestPlanZapSelectorUsesIndependentReadyFallbackOccurrence(t *testing.T) {
	s := zapState(zapWand("unused", 1, 1), false)
	actor := s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"direct": {Object: zapNamedWand("Healing Direct", [3]string{"", "", ""})},
			"ready1": {Object: zapNamedWand("Healing Ready One", [3]string{"", "", ""})},
			"ready2": {Object: zapNamedWand("Healing Ready Two", [3]string{"", "", ""})},
		},
		Inventory: []string{"direct"},
	}
	actor.Items.Ready[3] = "ready1"
	actor.Items.Ready[8] = "ready2"
	s.Players["a"] = actor

	p := zapMustPlan(t, s, "healing", 1, "", 1, 100, 1, 1)
	if p.ItemID != "direct" || p.Location != ZapInventoryRoot || p.ReadySlot != -1 {
		t.Fatalf("direct precedence proposal=%+v", p)
	}
	p = zapMustPlan(t, s, "healing", 2, "", 1, 100, 1, 1)
	if p.ItemID != "ready2" || p.Location != ZapReadySlot || p.ReadySlot != 8 {
		t.Fatalf("independent ready occurrence proposal=%+v", p)
	}
	p, err := s.PlanZap("a", "healing", 3, "", 1, ZapOptions{Now: 100})
	if err != nil || p.Action != ZapMissing || p.Response != ZapMissingResponse {
		t.Fatalf("ready occurrence leaked across fallback proposal=%+v err=%v", p, err)
	}
}

func TestPlanZapSelectorHonorsOINVISAndPDINVIForInventoryAndReady(t *testing.T) {
	newItems := func(hiddenID, visibleID string, hidden, visible LegacyObject) *ItemCollection {
		return &ItemCollection{
			Items: map[string]Item{
				hiddenID:  {Object: hidden},
				visibleID: {Object: visible},
			},
			Inventory: []string{hiddenID, visibleID},
		}
	}
	hidden := zapNamedWand("Healing Hidden", [3]string{"", "", ""})
	setObjectFlag(&hidden.Flags, objectInvisibleFlag, true)
	visible := zapNamedWand("Healing Visible", [3]string{"", "", ""})
	s := zapState(zapWand("unused", 1, 1), false)
	actor := s.Players["a"]
	actor.Items = newItems("hidden", "visible", hidden, visible)
	s.Players["a"] = actor

	p := zapMustPlan(t, s, "healing", 1, "", 1, 100, 1, 1)
	if p.ItemID != "visible" || p.Occurrence != 1 {
		t.Fatalf("ordinary actor exposed hidden root proposal=%+v", p)
	}
	p, err := s.PlanZap("a", "healing", 2, "", 1, ZapOptions{Now: 100})
	if err != nil || p.Action != ZapMissing {
		t.Fatalf("hidden root counted as visible occurrence proposal=%+v err=%v", p, err)
	}
	actor = s.Players["a"]
	setSettingFlag(&actor.Body, playerDetectInvisibleFlag, true)
	s.Players["a"] = actor
	p = zapMustPlan(t, s, "healing", 1, "", 1, 100, 1, 1)
	if p.ItemID != "hidden" {
		t.Fatalf("PDINVI did not reveal hidden inventory root proposal=%+v", p)
	}

	hiddenReady := zapNamedWand("Ready Healing Hidden", [3]string{"", "", ""})
	setObjectFlag(&hiddenReady.Flags, objectInvisibleFlag, true)
	visibleReady := zapNamedWand("Ready Healing Visible", [3]string{"", "", ""})
	s = zapState(zapWand("unused", 1, 1), false)
	actor = s.Players["a"]
	actor.Items = &ItemCollection{
		Items: map[string]Item{
			"hidden-ready":  {Object: hiddenReady},
			"visible-ready": {Object: visibleReady},
		},
	}
	actor.Items.Ready[2] = "hidden-ready"
	actor.Items.Ready[5] = "visible-ready"
	s.Players["a"] = actor
	p = zapMustPlan(t, s, "ready", 1, "", 1, 100, 1, 1)
	if p.ItemID != "visible-ready" || p.ReadySlot != 5 {
		t.Fatalf("ordinary actor exposed hidden ready root proposal=%+v", p)
	}
	actor = s.Players["a"]
	setSettingFlag(&actor.Body, playerDetectInvisibleFlag, true)
	s.Players["a"] = actor
	p = zapMustPlan(t, s, "ready", 1, "", 1, 100, 1, 1)
	if p.ItemID != "hidden-ready" || p.ReadySlot != 2 {
		t.Fatalf("PDINVI did not reveal hidden ready root proposal=%+v", p)
	}
}
