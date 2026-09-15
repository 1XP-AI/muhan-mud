package world

import (
	"errors"
	"fmt"
	"testing"
)

type forgeObjectCatalog map[int16]LegacyObject

func (forgeObjectCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{}, errors.New("monster lookup not used")
}

func (c forgeObjectCatalog) Object(id int16) (LegacyObject, error) {
	object, ok := c[id]
	if !ok {
		return LegacyObject{}, fmt.Errorf("unmigrated object %d", id)
	}
	return object, nil
}

func forgeWeaponCatalog() forgeObjectCatalog {
	return forgeObjectCatalog{
		900: {Name: "무명도", Weight: 1},
		901: {Name: "무명검", Weight: 1},
		902: {Name: "무명봉", Weight: 1},
		903: {Name: "무명창", Weight: 1},
		904: {Name: "무명궁", Weight: 1},
	}
}

func forgeSelectArmState(gold int32) State {
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = gold
	actor.Body.Flags = forgeWithFlag(actor.Body.Flags, ForgeReadingFlag, true)
	s.Players["actor"] = actor
	return s
}

func emptyForgeItems() *ItemCollection {
	return &ItemCollection{Items: map[string]Item{}}
}

func forgeMaterialState(gold int32, class byte) State {
	s := forgeSelectArmState(gold)
	actor := s.Players["actor"]
	actor.Body.Class = class
	actor.Items = emptyForgeItems()
	s.Players["actor"] = actor
	return s
}

func forgeDiamondFlagsSet(flags [8]byte) bool {
	return forgeFlag(flags, ForgeClassSelectFlag) &&
		forgeFlag(flags, ForgeAssassinOnlyFlag) &&
		forgeFlag(flags, ForgeBarbarianOnlyFlag) &&
		forgeFlag(flags, ForgeFighterOnlyFlag) &&
		forgeFlag(flags, ForgeRangerOnlyFlag) &&
		forgeFlag(flags, ForgeThiefOnlyFlag) &&
		!forgeFlag(flags, 34) && !forgeFlag(flags, 36) && !forgeFlag(flags, 37)
}

func forgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[ForgeRoomFlag/8] |= 1 << (ForgeRoomFlag % 8)
	}
	return flags
}

func forgeState(rforge bool) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "대장간", Flags: forgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
		},
	}
}

func TestPlanApplyForgeStartsSelectArmWeaponTypePrompt(t *testing.T) {
	s := forgeState(true)
	proposal, err := s.PlanForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgePrompt || !proposal.Changed || proposal.Continuation != ForgeSelectArmPhase ||
		proposal.Response != ForgePromptResponse || proposal.RoomID != 1 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if forgeFlag(proposal.BeforeFlags, ForgeNoBroadcastFlag) || forgeFlag(proposal.BeforeFlags, ForgeReadingFlag) {
		t.Fatal("start flags already set")
	}
	if forgeFlag(proposal.AfterFlags, ForgeNoBroadcastFlag) || !forgeFlag(proposal.AfterFlags, ForgeReadingFlag) {
		t.Fatalf("select_arm flags=%+v want PREADI without persisted PNOBRD", proposal.AfterFlags)
	}
	next, result, err := s.ApplyForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ForgePrompt || !result.Changed || result.Continuation != ForgeSelectArmPhase ||
		result.Response != ForgePromptResponse || result.NoBroadcast || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
		t.Fatalf("applied flags=%+v want PREADI without PNOBRD", body.Flags)
	}
	if PlayerFlagSet(s.Players["actor"].Body, ForgeNoBroadcastFlag) || PlayerFlagSet(s.Players["actor"].Body, ForgeReadingFlag) {
		t.Fatal("plan/apply mutated the original snapshot")
	}
	if _, _, err := next.ApplyForge(proposal); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	again, result, err := next.Forge("actor")
	if err != nil || result.Action != ForgePrompt || !result.Changed || again.Players["actor"].Body.Flags != body.Flags {
		t.Fatalf("idempotent start result=%+v err=%v", result, err)
	}
	if PlayerFlagSet(again.Players["actor"].Body, ForgeNoBroadcastFlag) || !PlayerFlagSet(again.Players["actor"].Body, ForgeReadingFlag) {
		t.Fatalf("second start flags=%+v", again.Players["actor"].Body.Flags)
	}
}

func TestPlanApplyForgeClearsExistingPNOBRDAfterSelectArmPrompt(t *testing.T) {
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Flags = forgeWithFlag(actor.Body.Flags, ForgeNoBroadcastFlag, true)
	s.Players["actor"] = actor
	proposal, err := s.PlanForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !forgeFlag(proposal.BeforeFlags, ForgeNoBroadcastFlag) || forgeFlag(proposal.AfterFlags, ForgeNoBroadcastFlag) ||
		!forgeFlag(proposal.AfterFlags, ForgeReadingFlag) || !proposal.Changed {
		t.Fatalf("proposal flags before=%+v after=%+v", proposal.BeforeFlags, proposal.AfterFlags)
	}
	next, result, err := s.ApplyForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["actor"].Body
	if result.NoBroadcast || !result.Reading || PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
		t.Fatalf("result=%+v flags=%+v", result, body.Flags)
	}
	if !PlayerFlagSet(s.Players["actor"].Body, ForgeNoBroadcastFlag) {
		t.Fatal("plan/apply mutated the original snapshot")
	}
}

func TestPlanForgeMissingRForgeIsTypedNoOp(t *testing.T) {
	s := forgeState(false)
	named := s.Rooms[1]
	named.Resource.Name = "대장간"
	s.Rooms[1] = named
	proposal, err := s.PlanForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != ForgeNotForgeResponse || proposal.AfterFlags != proposal.BeforeFlags {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != ForgeNotForge || result.Response != ForgeNotForgeResponse ||
		result.Continuation != 0 || result.NoBroadcast || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
		t.Fatal("not-forge mutated player flags")
	}
}

func TestPlanForgeFailClosedWithoutCanonicalRoomFlags(t *testing.T) {
	s := forgeState(true)
	delete(s.Rooms, 1)
	if _, err := s.PlanForge("actor"); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated err=%v", err)
	}
	s = forgeState(true)
	if _, err := s.PlanForge(""); !errors.Is(err, ErrForgeActorAbsent) {
		t.Fatalf("empty actor err=%v", err)
	}
	offline := s.clone()
	actor := offline.Players["actor"]
	actor.Online = false
	offline.Players["actor"] = actor
	room := offline.Rooms[1]
	room.PlayerIDs = nil
	offline.Rooms[1] = room
	if _, err := offline.PlanForge("actor"); !errors.Is(err, ErrForgeActorAbsent) {
		t.Fatalf("offline err=%v", err)
	}
}

func TestApplyForgeRejectsTamperedPrompt(t *testing.T) {
	s := forgeState(true)
	proposal, err := s.PlanForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "weapon?"
	if _, _, err := s.ApplyForge(proposal); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	if _, _, err := s.ApplyForge(ForgeProposal{ActorID: "actor", Action: ForgePrompt, Changed: true}); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
}

func TestPlanApplyForgeSelectArmLoadsWeaponTemplatesAndMaterialPrompt(t *testing.T) {
	catalog := forgeWeaponCatalog()
	names := map[int]string{1: "무명도", 2: "무명검", 3: "무명봉", 4: "무명창", 5: "무명궁"}
	for choice, name := range names {
		s := forgeSelectArmState(50000)
		input := string(rune('0' + choice))
		proposal, err := s.PlanForgeSelectArm("actor", input, catalog)
		if err != nil {
			t.Fatalf("choice %d: %v", choice, err)
		}
		if proposal.Action != ForgeMaterial || proposal.Changed || proposal.Continuation != ForgeMaterialPhase ||
			proposal.Response != ForgeMaterialResponse || proposal.WeaponChoice != choice ||
			proposal.ObjectID != ForgeWeaponObjectID(choice) || proposal.expectedObject.Name != name {
			t.Fatalf("choice %d proposal=%+v name=%q", choice, proposal, proposal.expectedObject.Name)
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("choice %d planned gold=%d", choice, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyForgeSelectArm(proposal, catalog)
		if err != nil {
			t.Fatalf("choice %d apply: %v", choice, err)
		}
		if result.Action != ForgeMaterial || result.Changed || result.Continuation != ForgeMaterialPhase ||
			result.Response != ForgeMaterialResponse || result.WeaponChoice != choice ||
			result.ObjectID != ForgeWeaponObjectID(choice) || result.ObjectName != name ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("choice %d result=%+v", choice, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("choice %d gold next=%d orig=%d", choice, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
			t.Fatalf("choice %d flags=%+v", choice, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("choice %d invented inventory", choice)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyForgeSelectArm(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
			t.Fatalf("choice %d gold-drift err=%v", choice, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("choice %d stale apply mutated gold", choice)
		}
	}
}

func TestPlanApplyForgeSelectArmInvalidDigitRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(50000)
	for _, input := range []string{"", "0", "6", "x", " 1", "도", "무기만들기"} {
		proposal, err := s.PlanForgeSelectArm("actor", input, catalog)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != ForgeReprompt || proposal.Changed || proposal.Continuation != ForgeSelectArmPhase ||
			proposal.Response != ForgeRepromptResponse || proposal.WeaponChoice != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyForgeSelectArm(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != ForgeReprompt || result.Changed || result.Continuation != ForgeSelectArmPhase ||
			result.Response != ForgeRepromptResponse || result.ObjectID != 0 || result.WeaponChoice != 0 ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", input, result)
		}
		if next.Players["actor"].Body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q committed gold next=%d orig=%d", input, next.Players["actor"].Body.Gold, s.Players["actor"].Body.Gold)
		}
		if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
			t.Fatalf("input %q mutated flags", input)
		}
	}
}

func TestPlanApplyForgeSelectArmAcceptsCFirstByteSuffix(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(12345)
	proposal, err := s.PlanForgeSelectArm("actor", "2 extra", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeMaterial || proposal.WeaponChoice != 2 || proposal.ObjectID != 901 ||
		proposal.expectedObject.Name != "무명검" || proposal.Changed {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyForgeSelectArm(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObjectName != "무명검" || next.Players["actor"].Body.Gold != 12345 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestPlanForgeSelectArmFailClosedWithoutCatalogOrTemplate(t *testing.T) {
	s := forgeSelectArmState(50000)
	if _, err := s.PlanForgeSelectArm("actor", "1", nil); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if s.Players["actor"].Body.Gold != 50000 {
		t.Fatal("nil catalog mutated gold")
	}
	partial := forgeWeaponCatalog()
	delete(partial, 902)
	if _, err := s.PlanForgeSelectArm("actor", "3", partial); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("missing 902 err=%v", err)
	}
	blank := forgeWeaponCatalog()
	blank[900] = LegacyObject{Name: ""}
	if _, err := s.PlanForgeSelectArm("actor", "1", blank); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("blank template err=%v", err)
	}
	if _, _, err := s.ForgeSelectArm("actor", "1", nil); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanForgeSelectArmFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectArm("actor", "1", catalog); !errors.Is(err, ErrForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = forgeSelectArmState(50000)
	delete(s.Rooms, 1)
	if _, err := s.PlanForgeSelectArm("actor", "1", catalog); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = forgeSelectArmState(50000)
	if _, err := s.PlanForgeSelectArm("actor", "1\n", catalog); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	if _, err := s.PlanForgeSelectArm("actor", string([]byte{0xff}), catalog); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("invalid utf8 err=%v", err)
	}
}

func TestApplyForgeSelectArmRejectsTamperedMaterialAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(50000)
	proposal, err := s.PlanForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "material?"
	if _, _, err := s.ApplyForgeSelectArm(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Changed = true
	if _, _, err := s.ApplyForgeSelectArm(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("changed err=%v", err)
	}
	proposal, err = s.PlanForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyForgeSelectArm(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}

func TestPlanApplyForgeSelectMaterialSetsSdiceAndSumWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input    string
		choice   int
		dice     int16
		sum      int32
		diamond  bool
		objectID int16
		name     string
	}{
		{"1", 1, ForgeSteelDice, ForgeSteelCost, false, 900, "무명도"},
		{"2 extra", 2, ForgePreciousDice, ForgePreciousCost, false, 901, "무명검"},
		{"3", 3, ForgeDiamondDice, ForgeDiamondCost, true, 902, "무명봉"},
	}
	for _, tt := range cases {
		s := forgeMaterialState(50000, 4)
		proposal, err := s.PlanForgeSelectMaterial("actor", tt.input, catalog, tt.objectID)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		if proposal.Action != ForgeQuench || proposal.Changed || proposal.Continuation != ForgeQuenchPhase ||
			proposal.Response != ForgeQuenchResponse || proposal.MaterialChoice != tt.choice ||
			proposal.DiceSides != tt.dice || proposal.Sum != tt.sum || proposal.ObjectID != tt.objectID ||
			proposal.expectedObject.Name != tt.name || proposal.expectedObject.DiceSides != tt.dice {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		if tt.diamond != forgeDiamondFlagsSet(proposal.expectedObject.Flags) {
			t.Fatalf("input %q diamond flags=%+v", tt.input, proposal.expectedObject.Flags)
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != ForgeQuench || result.Changed || result.Continuation != ForgeQuenchPhase ||
			result.Response != ForgeQuenchResponse || result.MaterialChoice != tt.choice ||
			result.DiceSides != tt.dice || result.Sum != tt.sum || result.ObjectID != tt.objectID ||
			result.ObjectName != tt.name || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items == nil ||
			len(next.Players["actor"].Items.Items) != 0 || len(next.Players["actor"].Items.Inventory) != 0 {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyForgeSelectMaterialInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeMaterialState(50000, 4)
	for _, input := range []string{"", "0", "4", "5", "6", "x", " 1", "도", "무기만들기"} {
		proposal, err := s.PlanForgeSelectMaterial("actor", input, catalog, 900)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != ForgeReprompt || proposal.Changed || proposal.Continuation != ForgeMaterialPhase ||
			proposal.Response != ForgeRepromptResponse || proposal.MaterialChoice != 0 ||
			proposal.DiceSides != 0 || proposal.Sum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != ForgeReprompt || result.Changed || result.Continuation != ForgeMaterialPhase ||
			result.Response != ForgeRepromptResponse || result.Sum != 0 || result.DiceSides != 0 ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", input, result)
		}
		if next.Players["actor"].Body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q committed gold next=%d orig=%d", input, next.Players["actor"].Body.Gold, s.Players["actor"].Body.Gold)
		}
		if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
			t.Fatalf("input %q mutated flags", input)
		}
	}
}

func TestPlanApplyForgeSelectMaterialDeniesRestrictedClassesWithoutCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	for _, class := range []byte{ForgeClericClass, ForgeMageClass, ForgePaladinClass} {
		s := forgeMaterialState(300000, class)
		proposal, err := s.PlanForgeSelectMaterial("actor", "3", catalog, 904)
		if err != nil {
			t.Fatalf("class %d: %v", class, err)
		}
		if proposal.Action != ForgeMaterialDenied || proposal.Changed || proposal.Continuation != ForgeMaterialPhase ||
			proposal.Response != ForgeMaterialDeniedResponse || proposal.MaterialChoice != 3 ||
			proposal.Sum != 0 || proposal.DiceSides != 0 || proposal.ObjectID != 904 ||
			proposal.expectedObject.DiceSides != 0 || forgeFlag(proposal.expectedObject.Flags, ForgeClassSelectFlag) {
			t.Fatalf("class %d proposal=%+v flags=%+v", class, proposal, proposal.expectedObject.Flags)
		}
		next, result, err := s.ApplyForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("class %d apply: %v", class, err)
		}
		if result.Action != ForgeMaterialDenied || result.Changed || result.Continuation != ForgeMaterialPhase ||
			result.Response != ForgeMaterialDeniedResponse || result.Sum != 0 || result.DiceSides != 0 ||
			!result.Reading {
			t.Fatalf("class %d result=%+v", class, result)
		}
		if next.Players["actor"].Body.Gold != 300000 || next.Players["actor"].Items == nil ||
			len(next.Players["actor"].Items.Items) != 0 {
			t.Fatalf("class %d mutated gold/items", class)
		}
	}
}

func TestPlanForgeSelectMaterialFailClosedWithoutGoldOrObjectGraph(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(50000)
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}
	s = forgeMaterialState(50000, 4)
	actor := s.Players["actor"]
	actor.Body.Gold = -1
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("negative gold err=%v", err)
	}
	s = forgeMaterialState(50000, 4)
	actor = s.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "유물"}}
	actor.Items = nil
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("legacy inventory err=%v", err)
	}
	s = forgeMaterialState(50000, 4)
	if _, err := s.PlanForgeSelectMaterial("actor", "1", nil, 900); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 899); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("bad object id err=%v", err)
	}
	if _, _, err := s.ForgeSelectMaterial("actor", "1", nil, 900); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanForgeSelectMaterialFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	actor.Items = emptyForgeItems()
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900); !errors.Is(err, ErrForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = forgeMaterialState(50000, 4)
	delete(s.Rooms, 1)
	if _, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = forgeMaterialState(50000, 4)
	if _, err := s.PlanForgeSelectMaterial("actor", "1\n", catalog, 900); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
}

func TestApplyForgeSelectMaterialRejectsTamperedQuenchAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeMaterialState(50000, 4)
	proposal, err := s.PlanForgeSelectMaterial("actor", "1", catalog, 900)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "quench?"
	if _, _, err := s.ApplyForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanForgeSelectMaterial("actor", "1", catalog, 900)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanForgeSelectMaterial("actor", "1", catalog, 900)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}

func forgeQuenchState(gold int32, class byte) State {
	return forgeMaterialState(gold, class)
}

func TestPlanApplyForgeSelectQuenchSetsShotsAndSumWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input       string
		choice      int
		shots       int16
		quenchCost  int32
		materialSum int32
		dice        int16
		diamond     bool
		objectID    int16
		name        string
	}{
		{"1", 1, ForgeQuench100Shots, ForgeQuench100Cost, ForgeSteelCost, ForgeSteelDice, false, 900, "무명도"},
		{"2 extra", 2, ForgeQuench200Shots, ForgeQuench200Cost, ForgePreciousCost, ForgePreciousDice, false, 901, "무명검"},
		{"3", 3, ForgeQuench300Shots, ForgeQuench300Cost, ForgeDiamondCost, ForgeDiamondDice, true, 902, "무명봉"},
		{"4", 4, ForgeQuench400Shots, ForgeQuench400Cost, ForgeSteelCost, ForgeSteelDice, false, 903, "무명창"},
		{"5", 5, ForgeQuench500Shots, ForgeQuench500Cost, ForgeSteelCost, ForgeSteelDice, false, 904, "무명궁"},
	}
	for _, tt := range cases {
		s := forgeQuenchState(50000, 4)
		proposal, err := s.PlanForgeSelectQuench("actor", tt.input, catalog, tt.objectID, tt.materialSum)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		wantSum := tt.materialSum + tt.quenchCost
		if proposal.Action != ForgeName || proposal.Changed || proposal.Continuation != ForgeNamePhase ||
			proposal.Response != ForgeNameResponse || proposal.QuenchChoice != tt.choice ||
			proposal.ShotsMax != tt.shots || proposal.ShotsCurrent != tt.shots ||
			proposal.MaterialSum != tt.materialSum || proposal.Sum != wantSum ||
			proposal.ObjectID != tt.objectID || proposal.expectedObject.Name != tt.name ||
			proposal.expectedObject.ShotsMax != tt.shots || proposal.expectedObject.ShotsCurrent != tt.shots ||
			proposal.expectedObject.DiceSides != tt.dice {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		if tt.diamond != forgeDiamondFlagsSet(proposal.expectedObject.Flags) {
			t.Fatalf("input %q diamond flags=%+v", tt.input, proposal.expectedObject.Flags)
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyForgeSelectQuench(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != ForgeName || result.Changed || result.Continuation != ForgeNamePhase ||
			result.Response != ForgeNameResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != wantSum || result.ObjectID != tt.objectID || result.ObjectName != tt.name ||
			result.DiceSides != tt.dice || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items == nil ||
			len(next.Players["actor"].Items.Items) != 0 || len(next.Players["actor"].Items.Inventory) != 0 {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyForgeSelectQuenchInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeQuenchState(50000, 4)
	for _, input := range []string{"", "0", "6", "x", " 1", "도", "무기만들기"} {
		proposal, err := s.PlanForgeSelectQuench("actor", input, catalog, 900, ForgeSteelCost)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != ForgeReprompt || proposal.Changed || proposal.Continuation != ForgeQuenchPhase ||
			proposal.Response != ForgeRepromptResponse || proposal.QuenchChoice != 0 ||
			proposal.ShotsMax != 0 || proposal.ShotsCurrent != 0 || proposal.Sum != 0 ||
			proposal.MaterialSum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyForgeSelectQuench(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != ForgeReprompt || result.Changed || result.Continuation != ForgeQuenchPhase ||
			result.Response != ForgeRepromptResponse || result.Sum != 0 || result.ShotsMax != 0 ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", input, result)
		}
		if next.Players["actor"].Body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q committed gold next=%d orig=%d", input, next.Players["actor"].Body.Gold, s.Players["actor"].Body.Gold)
		}
		if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
			t.Fatalf("input %q mutated flags", input)
		}
	}
}

func TestPlanForgeSelectQuenchFailClosedWithoutGoldOrObjectGraph(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(50000)
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}
	s = forgeQuenchState(50000, 4)
	actor := s.Players["actor"]
	actor.Body.Gold = -1
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("negative gold err=%v", err)
	}
	s = forgeQuenchState(50000, 4)
	actor = s.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "유물"}}
	actor.Items = nil
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("legacy inventory err=%v", err)
	}
	s = forgeQuenchState(50000, 4)
	if _, err := s.PlanForgeSelectQuench("actor", "1", nil, 900, ForgeSteelCost); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 899, ForgeSteelCost); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("bad object id err=%v", err)
	}
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, 0); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, 7); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("unknown sum err=%v", err)
	}
	if _, _, err := s.ForgeSelectQuench("actor", "1", nil, 900, ForgeSteelCost); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanForgeSelectQuenchFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	actor.Items = emptyForgeItems()
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = forgeQuenchState(50000, 4)
	delete(s.Rooms, 1)
	if _, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = forgeQuenchState(50000, 4)
	if _, err := s.PlanForgeSelectQuench("actor", "1\n", catalog, 900, ForgeSteelCost); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
}

func TestApplyForgeSelectQuenchRejectsTamperedNameAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeQuenchState(50000, 4)
	proposal, err := s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "name?"
	if _, _, err := s.ApplyForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanForgeSelectQuench("actor", "1", catalog, 900, ForgeSteelCost)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}

func forgeNameState(gold int32, class byte) State {
	return forgeQuenchState(gold, class)
}

func TestPlanApplyForgeSelectNameSetsWeaponNameWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input        string
		objectID     int16
		materialSum  int32
		quenchChoice int
		shots        int16
		quenchCost   int32
		dice         int16
		diamond      bool
	}{
		{"abc", 900, ForgeSteelCost, 1, ForgeQuench100Shots, ForgeQuench100Cost, ForgeSteelDice, false},
		{"불의검", 901, ForgePreciousCost, 2, ForgeQuench200Shots, ForgeQuench200Cost, ForgePreciousDice, false},
		{"12345678901234567890", 902, ForgeDiamondCost, 3, ForgeQuench300Shots, ForgeQuench300Cost, ForgeDiamondDice, true},
		{"무명창이름", 903, ForgeSteelCost, 4, ForgeQuench400Shots, ForgeQuench400Cost, ForgeSteelDice, false},
		{"xyz", 904, ForgeSteelCost, 5, ForgeQuench500Shots, ForgeQuench500Cost, ForgeSteelDice, false},
	}
	for _, tt := range cases {
		s := forgeNameState(0, 4)
		proposal, err := s.PlanForgeSelectName("actor", tt.input, catalog, tt.objectID, tt.materialSum, tt.quenchChoice)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		wantSum := tt.materialSum + tt.quenchCost
		if proposal.Action != ForgeConfirm || proposal.Changed || proposal.Continuation != ForgeConfirmPhase ||
			proposal.Response != ForgeConfirmResponse || proposal.QuenchChoice != tt.quenchChoice ||
			proposal.ShotsMax != tt.shots || proposal.ShotsCurrent != tt.shots ||
			proposal.MaterialSum != tt.materialSum || proposal.Sum != wantSum ||
			proposal.ObjectID != tt.objectID || proposal.expectedObject.Name != tt.input ||
			proposal.expectedObject.ShotsMax != tt.shots || proposal.expectedObject.ShotsCurrent != tt.shots ||
			proposal.expectedObject.DiceSides != tt.dice {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		if tt.diamond != forgeDiamondFlagsSet(proposal.expectedObject.Flags) {
			t.Fatalf("input %q diamond flags=%+v", tt.input, proposal.expectedObject.Flags)
		}
		if proposal.expectedActor.Body.Gold != 0 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyForgeSelectName(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != ForgeConfirm || result.Changed || result.Continuation != ForgeConfirmPhase ||
			result.Response != ForgeConfirmResponse || result.QuenchChoice != tt.quenchChoice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != wantSum || result.MaterialSum != tt.materialSum ||
			result.ObjectID != tt.objectID || result.ObjectName != tt.input ||
			result.DiceSides != tt.dice || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 0 || s.Players["actor"].Body.Gold != 0 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, ForgeNoBroadcastFlag) || !PlayerFlagSet(body, ForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items == nil ||
			len(next.Players["actor"].Items.Items) != 0 || len(next.Players["actor"].Items.Inventory) != 0 {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyForgeSelectName(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyForgeSelectNameInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeNameState(50000, 4)
	cases := []struct {
		input    string
		response string
	}{
		{"", ForgeNameShortResponse},
		{"ab", ForgeNameShortResponse},
		{"a", ForgeNameShortResponse},
		{"123456789012345678901", ForgeNameLongResponse},
		{"한글일곱글자임", ForgeNameLongResponse},
		{"ab(c", ForgeNameParenResponse},
		{"검)", ForgeNameParenResponse},
		{"(불의검)", ForgeNameParenResponse},
	}
	for _, tt := range cases {
		proposal, err := s.PlanForgeSelectName("actor", tt.input, catalog, 900, ForgeSteelCost, 1)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		if proposal.Action != ForgeReprompt || proposal.Changed || proposal.Continuation != ForgeNamePhase ||
			proposal.Response != tt.response || proposal.QuenchChoice != 0 ||
			proposal.ShotsMax != 0 || proposal.ShotsCurrent != 0 || proposal.Sum != 0 ||
			proposal.MaterialSum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		next, result, err := s.ApplyForgeSelectName(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != ForgeReprompt || result.Changed || result.Continuation != ForgeNamePhase ||
			result.Response != tt.response || result.Sum != 0 || result.ShotsMax != 0 ||
			result.ObjectName != "" || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		if next.Players["actor"].Body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q committed gold next=%d orig=%d", tt.input, next.Players["actor"].Body.Gold, s.Players["actor"].Body.Gold)
		}
		if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
			t.Fatalf("input %q mutated flags", tt.input)
		}
		if len(next.Players["actor"].Items.Items) != 0 {
			t.Fatalf("input %q invented items", tt.input)
		}
	}
}

func TestPlanForgeSelectNameFailClosedWithoutGoldOrObjectGraph(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeSelectArmState(50000)
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}
	s = forgeNameState(50000, 4)
	actor := s.Players["actor"]
	actor.Body.Gold = -1
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("negative gold err=%v", err)
	}
	s = forgeNameState(50000, 4)
	actor = s.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "유물"}}
	actor.Items = nil
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("legacy inventory err=%v", err)
	}
	s = forgeNameState(50000, 4)
	if _, err := s.PlanForgeSelectName("actor", "abc", nil, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 899, ForgeSteelCost, 1); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("bad object id err=%v", err)
	}
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, 0, 1); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, 7, 1); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("unknown sum err=%v", err)
	}
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 0); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("zero quench err=%v", err)
	}
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 6); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("bad quench err=%v", err)
	}
	if _, _, err := s.ForgeSelectName("actor", "abc", nil, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanForgeSelectNameFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	actor.Items = emptyForgeItems()
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = forgeNameState(50000, 4)
	delete(s.Rooms, 1)
	if _, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = forgeNameState(50000, 4)
	if _, err := s.PlanForgeSelectName("actor", "abc\n", catalog, 900, ForgeSteelCost, 1); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	s = forgeNameState(50000, 4)
	named := s.Rooms[1]
	named.Resource.Flags = forgeRoomFlags(false)
	s.Rooms[1] = named
	proposal, err := s.PlanForgeSelectName("actor", "abc", catalog, 900, ForgeSteelCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeNotForge || proposal.Changed || proposal.Response != ForgeNotForgeResponse {
		t.Fatalf("missing RFORGE proposal=%+v", proposal)
	}
}

func TestApplyForgeSelectNameRejectsTamperedNameAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeNameState(50000, 4)
	proposal, err := s.PlanForgeSelectName("actor", "불의검", catalog, 900, ForgeSteelCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "name?"
	if _, _, err := s.ApplyForgeSelectName(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanForgeSelectName("actor", "불의검", catalog, 900, ForgeSteelCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.expectedObject.Name = "변조"
	if _, _, err := s.ApplyForgeSelectName(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered object name err=%v", err)
	}
	proposal, err = s.PlanForgeSelectName("actor", "불의검", catalog, 900, ForgeSteelCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyForgeSelectName(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanForgeSelectName("actor", "불의검", catalog, 900, ForgeSteelCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyForgeSelectName(proposal, catalog); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}

func forgeConfirmState(gold int32, class byte) State {
	return forgeNameState(gold, class)
}

func forgeTestAllocate(id string) func() (string, error) {
	return func() (string, error) { return id, nil }
}

func TestPlanApplyForgeSelectConfirmChargesGoldAndAddsWeapon(t *testing.T) {
	catalog := forgeWeaponCatalog()
	const gold int32 = 100000
	s := forgeConfirmState(gold, 4)
	allocate := forgeTestAllocate("forge-weapon-1")
	proposal, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	wantSum := ForgeSteelCost + ForgeQuench100Cost
	if proposal.Action != ForgeGive || !proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != ForgeGiveResponse || proposal.Sum != wantSum ||
		proposal.ItemID != "forge-weapon-1" || proposal.GoldAfter != gold-wantSum ||
		proposal.expectedObject.Name != "불의검" || proposal.WeaponName != "불의검" ||
		proposal.ObjectID != 900 || len(proposal.Events) != 1 ||
		proposal.Events[0].Text != ForgeBroadcastText("Alice") ||
		proposal.Events[0].ExcludeActorID != "actor" {
		t.Fatalf("proposal=%+v events=%+v", proposal, proposal.Events)
	}
	if forgeFlag(proposal.AfterFlags, ForgeReadingFlag) || !forgeFlag(proposal.BeforeFlags, ForgeReadingFlag) {
		t.Fatalf("PREADI after=%+v", proposal.AfterFlags)
	}
	item, ok := proposal.expectedAfterItems.Items["forge-weapon-1"]
	if !ok || item.Object.Name != "불의검" || item.Object.DiceSides != ForgeSteelDice ||
		item.Object.ShotsMax != ForgeQuench100Shots || item.Object.ShotsCurrent != ForgeQuench100Shots ||
		!containsString(proposal.expectedAfterItems.Inventory, "forge-weapon-1") {
		t.Fatalf("after items=%+v", proposal.expectedAfterItems)
	}
	next, result, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ForgeGive || !result.Changed || result.Continuation != 0 ||
		result.Response != ForgeGiveResponse || result.ItemID != "forge-weapon-1" ||
		result.GoldAfter != gold-wantSum || result.ObjectName != "불의검" ||
		result.Reading || result.NoBroadcast || result.Sum != wantSum {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if body.Gold != gold-wantSum || s.Players["actor"].Body.Gold != gold {
		t.Fatalf("gold next=%d orig=%d", body.Gold, s.Players["actor"].Body.Gold)
	}
	if PlayerFlagSet(body, ForgeReadingFlag) {
		t.Fatalf("flags next=%+v still PREADI", body.Flags)
	}
	if !PlayerFlagSet(s.Players["actor"].Body, ForgeReadingFlag) {
		t.Fatal("plan/apply mutated original flags")
	}
	got, ok := next.Players["actor"].Items.Items["forge-weapon-1"]
	if !ok || got.Object.Name != "불의검" || len(next.Players["actor"].Items.Inventory) != 1 {
		t.Fatalf("inventory=%+v", next.Players["actor"].Items)
	}
	if len(s.Players["actor"].Items.Items) != 0 {
		t.Fatal("plan/apply mutated original inventory")
	}
	if _, _, err := next.ApplyForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrForgeNotReading) && !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("second apply err=%v", err)
	}
	if next.Players["actor"].Body.Gold != gold-wantSum {
		t.Fatal("stale second apply mutated gold")
	}
}

func TestPlanApplyForgeSelectConfirmYesPrefixCharges(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeConfirmState(100000, 4)
	allocate := forgeTestAllocate("forge-weapon-prefix")
	proposal, err := s.PlanForgeSelectConfirm("actor", "예스", catalog, 900, ForgeSteelCost, 1, "abc", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeGive || proposal.ItemID != "forge-weapon-prefix" {
		t.Fatalf("prefix proposal=%+v", proposal)
	}
	next, result, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil || result.Action != ForgeGive || next.Players["actor"].Body.Gold != 0 {
		t.Fatalf("result=%+v gold=%d err=%v", result, next.Players["actor"].Body.Gold, err)
	}
}

func TestPlanApplyForgeSelectConfirmInsufficientGoldDoesNotCharge(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeConfirmState(50000, 4)
	called := 0
	allocate := func() (string, error) {
		called++
		return "should-not-allocate", nil
	}
	proposal, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeTooPoor || !proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != ForgeTooPoorResponse || proposal.ItemID != "" ||
		proposal.GoldAfter != 50000 || called != 0 || len(proposal.Events) != 0 {
		t.Fatalf("proposal=%+v called=%d", proposal, called)
	}
	next, result, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ForgeTooPoor || result.ItemID != "" || result.GoldAfter != 50000 ||
		result.Reading || next.Players["actor"].Body.Gold != 50000 ||
		len(next.Players["actor"].Items.Items) != 0 {
		t.Fatalf("result=%+v gold=%d items=%+v", result, next.Players["actor"].Body.Gold, next.Players["actor"].Items)
	}
	if PlayerFlagSet(next.Players["actor"].Body, ForgeReadingFlag) {
		t.Fatal("too-poor left PREADI set")
	}
}

func TestPlanApplyForgeSelectConfirmCancelDoesNotCharge(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeConfirmState(100000, 4)
	called := 0
	allocate := func() (string, error) {
		called++
		return "should-not-allocate", nil
	}
	for _, input := range []string{"아니오", "", "무기만들기", "제련", "no"} {
		proposal, err := s.PlanForgeSelectConfirm("actor", input, catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != ForgeCancel || !proposal.Changed || proposal.Continuation != 0 ||
			proposal.Response != ForgeCancelResponse || proposal.ItemID != "" ||
			proposal.GoldAfter != 100000 || called != 0 {
			t.Fatalf("input %q proposal=%+v called=%d", input, proposal, called)
		}
		next, result, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != ForgeCancel || next.Players["actor"].Body.Gold != 100000 ||
			len(next.Players["actor"].Items.Items) != 0 || result.Reading {
			t.Fatalf("input %q result=%+v gold=%d", input, result, next.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(next.Players["actor"].Body, ForgeReadingFlag) {
			t.Fatalf("input %q left PREADI set", input)
		}
	}
}

func TestPlanForgeSelectConfirmFailClosedWithoutGoldOrObjectGraph(t *testing.T) {
	catalog := forgeWeaponCatalog()
	allocate := forgeTestAllocate("forge-weapon-1")
	s := forgeSelectArmState(100000)
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	actor := s.Players["actor"]
	actor.Body.Gold = -1
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("negative gold err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	actor = s.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "유물"}}
	actor.Items = nil
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("legacy inventory err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	if _, err := s.PlanForgeSelectConfirm("actor", "예", nil, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 899, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("bad object id err=%v", err)
	}
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, 0, 1, "abc", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 0, "abc", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("zero quench err=%v", err)
	}
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "ab", allocate); !errors.Is(err, ErrForgeGoldObjectUnmigrated) {
		t.Fatalf("short name err=%v", err)
	}
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", nil); !errors.Is(err, ErrForgeItemAllocatorUnavailable) {
		t.Fatalf("nil allocate err=%v", err)
	}
	if _, _, err := s.ForgeSelectConfirm("actor", "예", nil, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanForgeSelectConfirmFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	allocate := forgeTestAllocate("forge-weapon-1")
	s := forgeState(true)
	actor := s.Players["actor"]
	actor.Body.Gold = 100000
	actor.Items = emptyForgeItems()
	s.Players["actor"] = actor
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	delete(s.Rooms, 1)
	if _, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	if _, err := s.PlanForgeSelectConfirm("actor", "예\n", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	s = forgeConfirmState(100000, 4)
	named := s.Rooms[1]
	named.Resource.Flags = forgeRoomFlags(false)
	s.Rooms[1] = named
	proposal, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != ForgeNotForge || proposal.Changed || proposal.Response != ForgeNotForgeResponse {
		t.Fatalf("missing RFORGE proposal=%+v", proposal)
	}
}

func TestApplyForgeSelectConfirmRejectsTamperedAndGoldDrift(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := forgeConfirmState(100000, 4)
	allocate := forgeTestAllocate("forge-weapon-1")
	proposal, err := s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "give?"
	if _, _, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.ItemID = "forged"
	if _, _, err := s.ApplyForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrForgeStaleProposal) && !errors.Is(err, ErrForgeInvalidProposal) {
		t.Fatalf("tampered item id err=%v", err)
	}
	proposal, err = s.PlanForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}
