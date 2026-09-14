package world

import (
	"errors"
	"testing"
)

func newForgeRoomFlags(rforge bool) (flags [8]byte) {
	if rforge {
		flags[NewForgeRoomFlag/8] |= 1 << (NewForgeRoomFlag % 8)
	}
	return flags
}

func newForgeState(roomID int16, rforge bool) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			roomID: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: roomID, Name: "대장간", Flags: newForgeRoomFlags(rforge)}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: roomID}, Online: true},
		},
	}
}

func TestPlanApplyNewForgeStartsSelectNewarmWeaponTypePrompt(t *testing.T) {
	s := newForgeState(NewForgeRoomID, true)
	proposal, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgePrompt || !proposal.Changed || proposal.Continuation != NewForgeSelectArmPhase ||
		proposal.Response != NewForgePromptResponse || proposal.RoomID != NewForgeRoomID {
		t.Fatalf("proposal=%+v", proposal)
	}
	if forgeFlag(proposal.BeforeFlags, NewForgeNoBroadcastFlag) || forgeFlag(proposal.BeforeFlags, NewForgeReadingFlag) {
		t.Fatal("start flags already set")
	}
	if forgeFlag(proposal.AfterFlags, NewForgeNoBroadcastFlag) || !forgeFlag(proposal.AfterFlags, NewForgeReadingFlag) {
		t.Fatalf("select_newarm flags=%+v want PREADI without persisted PNOBRD", proposal.AfterFlags)
	}
	next, result, err := s.ApplyNewForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != NewForgePrompt || !result.Changed || result.Continuation != NewForgeSelectArmPhase ||
		result.Response != NewForgePromptResponse || result.NoBroadcast || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
		t.Fatalf("applied flags=%+v want PREADI without PNOBRD", body.Flags)
	}
	if PlayerFlagSet(s.Players["actor"].Body, NewForgeNoBroadcastFlag) || PlayerFlagSet(s.Players["actor"].Body, NewForgeReadingFlag) {
		t.Fatal("plan/apply mutated the original snapshot")
	}
	if _, _, err := next.ApplyNewForge(proposal); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	again, result, err := next.NewForge("actor")
	if err != nil || result.Action != NewForgePrompt || !result.Changed || again.Players["actor"].Body.Flags != body.Flags {
		t.Fatalf("idempotent start result=%+v err=%v", result, err)
	}
	if PlayerFlagSet(again.Players["actor"].Body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(again.Players["actor"].Body, NewForgeReadingFlag) {
		t.Fatalf("second start flags=%+v", again.Players["actor"].Body.Flags)
	}
}

func TestPlanApplyNewForgeClearsExistingPNOBRDAfterSelectNewarmPrompt(t *testing.T) {
	s := newForgeState(NewForgeRoomID, true)
	actor := s.Players["actor"]
	actor.Body.Flags = forgeWithFlag(actor.Body.Flags, NewForgeNoBroadcastFlag, true)
	s.Players["actor"] = actor
	proposal, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !forgeFlag(proposal.BeforeFlags, NewForgeNoBroadcastFlag) || forgeFlag(proposal.AfterFlags, NewForgeNoBroadcastFlag) ||
		!forgeFlag(proposal.AfterFlags, NewForgeReadingFlag) || !proposal.Changed {
		t.Fatalf("proposal flags before=%+v after=%+v", proposal.BeforeFlags, proposal.AfterFlags)
	}
	next, result, err := s.ApplyNewForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	body := next.Players["actor"].Body
	if result.NoBroadcast || !result.Reading || PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
		t.Fatalf("result=%+v flags=%+v", result, body.Flags)
	}
	if !PlayerFlagSet(s.Players["actor"].Body, NewForgeNoBroadcastFlag) {
		t.Fatal("plan/apply mutated the original snapshot")
	}
}

func TestPlanNewForgeMissingRForgeIsTypedNoOpEvenInRoom611(t *testing.T) {
	s := newForgeState(NewForgeRoomID, false)
	named := s.Rooms[NewForgeRoomID]
	named.Resource.Name = "대장간"
	s.Rooms[NewForgeRoomID] = named
	proposal, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeNotForgeResponse || proposal.AfterFlags != proposal.BeforeFlags {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeNotForge || result.Response != NewForgeNotForgeResponse ||
		result.Continuation != 0 || result.NoBroadcast || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
		t.Fatal("not-forge mutated player flags")
	}
}

func TestPlanNewForgeRForgeOutsideRoom611IsTypedNoOp(t *testing.T) {
	s := newForgeState(1, true)
	proposal, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeWrongRoom || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeWrongRoomResponse || proposal.AfterFlags != proposal.BeforeFlags ||
		proposal.RoomID != 1 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForge(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeWrongRoom || result.Response != NewForgeWrongRoomResponse ||
		result.Continuation != 0 || result.NoBroadcast || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Flags != s.Players["actor"].Body.Flags {
		t.Fatal("wrong-room mutated player flags")
	}
}

func TestPlanNewForgeFailClosedWithoutCanonicalRoomFlags(t *testing.T) {
	s := newForgeState(NewForgeRoomID, true)
	delete(s.Rooms, NewForgeRoomID)
	if _, err := s.PlanNewForge("actor"); !errors.Is(err, ErrNewForgeFlagsUnresolved) {
		t.Fatalf("unmigrated err=%v", err)
	}
	s = newForgeState(NewForgeRoomID, true)
	if _, err := s.PlanNewForge(""); !errors.Is(err, ErrNewForgeActorAbsent) {
		t.Fatalf("empty actor err=%v", err)
	}
	offline := s.clone()
	actor := offline.Players["actor"]
	actor.Online = false
	offline.Players["actor"] = actor
	room := offline.Rooms[NewForgeRoomID]
	room.PlayerIDs = nil
	offline.Rooms[NewForgeRoomID] = room
	if _, err := offline.PlanNewForge("actor"); !errors.Is(err, ErrNewForgeActorAbsent) {
		t.Fatalf("offline err=%v", err)
	}
}

func TestApplyNewForgeRejectsTamperedPrompt(t *testing.T) {
	s := newForgeState(NewForgeRoomID, true)
	proposal, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "weapon?"
	if _, _, err := s.ApplyNewForge(proposal); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	if _, _, err := s.ApplyNewForge(NewForgeProposal{ActorID: "actor", Action: NewForgePrompt, Changed: true}); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
}

func TestPlanNewForgeIsDistinctFromPlanForge(t *testing.T) {
	s := newForgeState(NewForgeRoomID, true)
	newforge, err := s.PlanNewForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	forge, err := s.PlanForge("actor")
	if err != nil {
		t.Fatal(err)
	}
	if newforge.Action != NewForgePrompt || forge.Action != ForgePrompt {
		t.Fatalf("newforge=%+v forge=%+v", newforge, forge)
	}
	if newforge.Continuation != NewForgeSelectArmPhase || forge.Continuation != ForgeSelectArmPhase {
		t.Fatalf("continuations newforge=%d forge=%d", newforge.Continuation, forge.Continuation)
	}
}

func newForgeSelectArmState(gold int32) State {
	s := newForgeState(NewForgeRoomID, true)
	actor := s.Players["actor"]
	actor.Body.Gold = gold
	actor.Body.Flags = forgeWithFlag(actor.Body.Flags, NewForgeReadingFlag, true)
	s.Players["actor"] = actor
	return s
}

func TestPlanApplyNewForgeSelectArmLoadsWeaponTemplatesAndMaterialPrompt(t *testing.T) {
	catalog := forgeWeaponCatalog()
	names := map[int]string{1: "무명도", 2: "무명검", 3: "무명봉", 4: "무명창", 5: "무명궁"}
	for choice, name := range names {
		s := newForgeSelectArmState(50000)
		input := string(rune('0' + choice))
		proposal, err := s.PlanNewForgeSelectArm("actor", input, catalog)
		if err != nil {
			t.Fatalf("choice %d: %v", choice, err)
		}
		if proposal.Action != NewForgeMaterial || proposal.Changed || proposal.Continuation != NewForgeMaterialPhase ||
			proposal.Response != NewForgeMaterialResponse || proposal.WeaponChoice != choice ||
			proposal.ObjectID != NewForgeWeaponObjectID(choice) || proposal.expectedObject.Name != name {
			t.Fatalf("choice %d proposal=%+v name=%q", choice, proposal, proposal.expectedObject.Name)
		}
		if proposal.Response == ForgeMaterialResponse {
			t.Fatal("newforge reused 제련 material prompt")
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("choice %d planned gold=%d", choice, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyNewForgeSelectArm(proposal, catalog)
		if err != nil {
			t.Fatalf("choice %d apply: %v", choice, err)
		}
		if result.Action != NewForgeMaterial || result.Changed || result.Continuation != NewForgeMaterialPhase ||
			result.Response != NewForgeMaterialResponse || result.WeaponChoice != choice ||
			result.ObjectID != NewForgeWeaponObjectID(choice) || result.ObjectName != name ||
			result.NoBroadcast || !result.Reading {
			t.Fatalf("choice %d result=%+v", choice, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("choice %d gold next=%d orig=%d", choice, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
			t.Fatalf("choice %d flags=%+v", choice, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("choice %d invented inventory", choice)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyNewForgeSelectArm(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
			t.Fatalf("choice %d gold-drift err=%v", choice, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("choice %d stale apply mutated gold", choice)
		}
	}
}

func TestPlanApplyNewForgeSelectArmInvalidDigitRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	for _, input := range []string{"", "0", "6", "x", " 1", "도", "제련"} {
		proposal, err := s.PlanNewForgeSelectArm("actor", input, catalog)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != NewForgeReprompt || proposal.Changed || proposal.Continuation != NewForgeSelectArmPhase ||
			proposal.Response != NewForgeRepromptResponse || proposal.WeaponChoice != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyNewForgeSelectArm(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != NewForgeReprompt || result.Changed || result.Continuation != NewForgeSelectArmPhase ||
			result.Response != NewForgeRepromptResponse || result.ObjectID != 0 || result.WeaponChoice != 0 ||
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

func TestPlanApplyNewForgeSelectArmAcceptsCFirstByteSuffix(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(12345)
	proposal, err := s.PlanNewForgeSelectArm("actor", "2 extra", catalog)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeMaterial || proposal.WeaponChoice != 2 || proposal.ObjectID != 901 ||
		proposal.expectedObject.Name != "무명검" || proposal.Changed || proposal.Response != NewForgeMaterialResponse {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectArm(proposal, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObjectName != "무명검" || next.Players["actor"].Body.Gold != 12345 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestPlanNewForgeSelectArmFailClosedWithoutCatalogOrTemplate(t *testing.T) {
	s := newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectArm("actor", "1", nil); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if s.Players["actor"].Body.Gold != 50000 {
		t.Fatal("nil catalog mutated gold")
	}
	partial := forgeWeaponCatalog()
	delete(partial, 902)
	if _, err := s.PlanNewForgeSelectArm("actor", "3", partial); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("missing 902 err=%v", err)
	}
	blank := forgeWeaponCatalog()
	blank[900] = LegacyObject{Name: ""}
	if _, err := s.PlanNewForgeSelectArm("actor", "1", blank); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("blank template err=%v", err)
	}
	if _, _, err := s.NewForgeSelectArm("actor", "1", nil); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanNewForgeSelectArmFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeState(NewForgeRoomID, true)
	actor := s.Players["actor"]
	actor.Body.Gold = 50000
	s.Players["actor"] = actor
	if _, err := s.PlanNewForgeSelectArm("actor", "1", catalog); !errors.Is(err, ErrNewForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = newForgeSelectArmState(50000)
	delete(s.Rooms, NewForgeRoomID)
	if _, err := s.PlanNewForgeSelectArm("actor", "1", catalog); !errors.Is(err, ErrNewForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectArm("actor", "1\n", catalog); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectArm("actor", string([]byte{0xff}), catalog); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("invalid utf8 err=%v", err)
	}
}

func TestPlanNewForgeSelectArmMissingRForgeIsTypedNoOp(t *testing.T) {
	s := newForgeSelectArmState(50000)
	room := s.Rooms[NewForgeRoomID]
	room.Resource.Flags = newForgeRoomFlags(false)
	s.Rooms[NewForgeRoomID] = room
	proposal, err := s.PlanNewForgeSelectArm("actor", "1", forgeWeaponCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeNotForgeResponse || proposal.ObjectID != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectArm(proposal, forgeWeaponCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeNotForge || next.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestApplyNewForgeSelectArmRejectsTamperedMaterialAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	proposal, err := s.PlanNewForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "material?"
	if _, _, err := s.ApplyNewForgeSelectArm(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	proposal.ObjectID = 900
	if _, _, err := s.ApplyNewForgeSelectArm(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered object err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectArm("actor", "4", catalog)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.RoomID = 1
	moved.Players["actor"] = actor
	moved.Rooms[1] = RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "elsewhere", Flags: newForgeRoomFlags(true)}},
		PlayerIDs: []string{"actor"},
	}
	delete(moved.Rooms, NewForgeRoomID)
	if _, _, err := moved.ApplyNewForgeSelectArm(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("moved actor err=%v", err)
	}
}

func TestPlanApplyNewForgeSelectMaterialSetsDiceEnchantAndForge2Sum(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input    string
		choice   int
		dice     int16
		sum      int32
		objectID int16
		name     string
	}{
		{"1", 1, NewForgeEmeraldDiceCount, NewForgeEmeraldCost, 900, "무명도"},
		{"2 extra", 2, NewForgeTitaniumDiceCount, NewForgeTitaniumCost, 901, "무명검"},
		{"3", 3, NewForgeIllusionDiceCount, NewForgeIllusionCost, 902, "무명봉"},
	}
	for _, tt := range cases {
		s := newForgeSelectArmState(50000)
		proposal, err := s.PlanNewForgeSelectMaterial("actor", tt.input, catalog, tt.objectID)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		if proposal.Action != NewForgeQuench || proposal.Changed || proposal.Continuation != NewForgeQuenchPhase ||
			proposal.Response != NewForgeQuenchResponse || proposal.MaterialChoice != tt.choice ||
			proposal.DiceCount != tt.dice || proposal.DiceSides != NewForgeMaterialDiceSides ||
			proposal.DicePlus != NewForgeMaterialDicePlus || proposal.Sum != tt.sum ||
			proposal.ObjectID != tt.objectID || proposal.expectedObject.Name != tt.name ||
			proposal.expectedObject.DiceCount != tt.dice ||
			!forgeFlag(proposal.expectedObject.Flags, NewForgeEnchantedFlag) {
			t.Fatalf("input %q proposal=%+v flags=%+v", tt.input, proposal, proposal.expectedObject.Flags)
		}
		if proposal.Response == ForgeQuenchResponse {
			t.Fatal("newforge reused 제련 quench prompt")
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyNewForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != NewForgeQuench || result.Changed || result.Continuation != NewForgeQuenchPhase ||
			result.Response != NewForgeQuenchResponse || result.MaterialChoice != tt.choice ||
			result.DiceCount != tt.dice || result.DiceSides != NewForgeMaterialDiceSides ||
			result.DicePlus != NewForgeMaterialDicePlus || result.Sum != tt.sum ||
			result.ObjectID != tt.objectID || result.ObjectName != tt.name ||
			!result.Enchanted || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyNewForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyNewForgeSelectMaterialInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	for _, input := range []string{"", "0", "4", "5", "6", "x", " 1", "도", "제련"} {
		proposal, err := s.PlanNewForgeSelectMaterial("actor", input, catalog, 900)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != NewForgeReprompt || proposal.Changed || proposal.Continuation != NewForgeMaterialPhase ||
			proposal.Response != NewForgeRepromptResponse || proposal.MaterialChoice != 0 ||
			proposal.DiceCount != 0 || proposal.Sum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyNewForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != NewForgeReprompt || result.Changed || result.Continuation != NewForgeMaterialPhase ||
			result.Response != NewForgeRepromptResponse || result.Sum != 0 || result.DiceCount != 0 ||
			result.Enchanted || result.NoBroadcast || !result.Reading {
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

func TestPlanApplyNewForgeSelectMaterialAllowsRestrictedForgeClasses(t *testing.T) {
	catalog := forgeWeaponCatalog()
	for _, class := range []byte{ForgeClericClass, ForgeMageClass, ForgePaladinClass} {
		s := newForgeSelectArmState(3000000)
		actor := s.Players["actor"]
		actor.Body.Class = class
		s.Players["actor"] = actor
		proposal, err := s.PlanNewForgeSelectMaterial("actor", "3", catalog, 904)
		if err != nil {
			t.Fatalf("class %d: %v", class, err)
		}
		if proposal.Action != NewForgeQuench || proposal.MaterialChoice != 3 ||
			proposal.DiceCount != NewForgeIllusionDiceCount || proposal.Sum != NewForgeIllusionCost ||
			proposal.ObjectID != 904 || !forgeFlag(proposal.expectedObject.Flags, NewForgeEnchantedFlag) {
			t.Fatalf("class %d proposal=%+v flags=%+v", class, proposal, proposal.expectedObject.Flags)
		}
		next, result, err := s.ApplyNewForgeSelectMaterial(proposal, catalog)
		if err != nil {
			t.Fatalf("class %d apply: %v", class, err)
		}
		if result.Action != NewForgeQuench || result.Sum != NewForgeIllusionCost || !result.Enchanted {
			t.Fatalf("class %d result=%+v", class, result)
		}
		if next.Players["actor"].Body.Gold != 3000000 {
			t.Fatalf("class %d charged gold=%d", class, next.Players["actor"].Body.Gold)
		}
	}
}

func TestPlanNewForgeSelectMaterialFailClosedWithoutCatalogOrPREADI(t *testing.T) {
	s := newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectMaterial("actor", "1", nil, 900); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if s.Players["actor"].Body.Gold != 50000 {
		t.Fatal("nil catalog mutated gold")
	}
	if _, err := s.PlanNewForgeSelectMaterial("actor", "1", forgeWeaponCatalog(), 0); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("object 0 err=%v", err)
	}
	partial := forgeWeaponCatalog()
	delete(partial, 901)
	if _, err := s.PlanNewForgeSelectMaterial("actor", "2", partial, 901); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("missing 901 err=%v", err)
	}
	blank := forgeWeaponCatalog()
	blank[900] = LegacyObject{Name: ""}
	if _, err := s.PlanNewForgeSelectMaterial("actor", "1", blank, 900); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("blank template err=%v", err)
	}
	idle := newForgeState(NewForgeRoomID, true)
	actor := idle.Players["actor"]
	actor.Body.Gold = 50000
	idle.Players["actor"] = actor
	if _, err := idle.PlanNewForgeSelectMaterial("actor", "1", forgeWeaponCatalog(), 900); !errors.Is(err, ErrNewForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectMaterial("actor", "1\n", forgeWeaponCatalog(), 900); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	if _, _, err := s.NewForgeSelectMaterial("actor", "1", nil, 900); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanNewForgeSelectMaterialMissingRForgeIsTypedNoOp(t *testing.T) {
	s := newForgeSelectArmState(50000)
	room := s.Rooms[NewForgeRoomID]
	room.Resource.Flags = newForgeRoomFlags(false)
	s.Rooms[NewForgeRoomID] = room
	proposal, err := s.PlanNewForgeSelectMaterial("actor", "1", forgeWeaponCatalog(), 900)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeNotForgeResponse || proposal.ObjectID != 0 || proposal.Sum != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectMaterial(proposal, forgeWeaponCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeNotForge || next.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestApplyNewForgeSelectMaterialRejectsTamperedQuenchAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	proposal, err := s.PlanNewForgeSelectMaterial("actor", "1", catalog, 903)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "quench?"
	if _, _, err := s.ApplyNewForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectMaterial("actor", "1", catalog, 903)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = NewForgeTitaniumCost
	if _, _, err := s.ApplyNewForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectMaterial("actor", "1", catalog, 903)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.RoomID = 1
	moved.Players["actor"] = actor
	moved.Rooms[1] = RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "elsewhere", Flags: newForgeRoomFlags(true)}},
		PlayerIDs: []string{"actor"},
	}
	delete(moved.Rooms, NewForgeRoomID)
	if _, _, err := moved.ApplyNewForgeSelectMaterial(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("moved actor err=%v", err)
	}
}

func TestPlanApplyNewForgeSelectQuenchSetsShotsAndSumWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input       string
		choice      int
		shots       int16
		quenchCost  int32
		materialSum int32
		dice        int16
		objectID    int16
		name        string
	}{
		{"1", 1, NewForgeQuench100Shots, NewForgeQuench100Cost, NewForgeEmeraldCost, NewForgeEmeraldDiceCount, 900, "무명도"},
		{"2 extra", 2, NewForgeQuench200Shots, NewForgeQuench200Cost, NewForgeTitaniumCost, NewForgeTitaniumDiceCount, 901, "무명검"},
		{"3", 3, NewForgeQuench300Shots, NewForgeQuench300Cost, NewForgeIllusionCost, NewForgeIllusionDiceCount, 902, "무명봉"},
		{"4", 4, NewForgeQuench400Shots, NewForgeQuench400Cost, NewForgeEmeraldCost, NewForgeEmeraldDiceCount, 903, "무명창"},
		{"5", 5, NewForgeQuench500Shots, NewForgeQuench500Cost, NewForgeEmeraldCost, NewForgeEmeraldDiceCount, 904, "무명궁"},
	}
	for _, tt := range cases {
		s := newForgeSelectArmState(50000)
		proposal, err := s.PlanNewForgeSelectQuench("actor", tt.input, catalog, tt.objectID, tt.materialSum)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		wantSum := tt.materialSum + tt.quenchCost
		if proposal.Action != NewForgeName || proposal.Changed || proposal.Continuation != NewForgeNamePhase ||
			proposal.Response != NewForgeNameResponse || proposal.QuenchChoice != tt.choice ||
			proposal.ShotsMax != tt.shots || proposal.ShotsCurrent != tt.shots ||
			proposal.MaterialSum != tt.materialSum || proposal.Sum != wantSum ||
			proposal.ObjectID != tt.objectID || proposal.expectedObject.Name != tt.name ||
			proposal.expectedObject.ShotsMax != tt.shots || proposal.expectedObject.ShotsCurrent != tt.shots ||
			proposal.DiceCount != tt.dice || proposal.DiceSides != NewForgeMaterialDiceSides ||
			proposal.DicePlus != NewForgeMaterialDicePlus ||
			!forgeFlag(proposal.expectedObject.Flags, NewForgeEnchantedFlag) {
			t.Fatalf("input %q proposal=%+v flags=%+v", tt.input, proposal, proposal.expectedObject.Flags)
		}
		if tt.choice == 2 && proposal.ShotsMax == 300 {
			t.Fatal("case 4 used the printed 300번 shots instead of C switch shots=200")
		}
		if proposal.QuenchChoice == 1 && proposal.Sum == tt.materialSum+ForgeQuench100Cost {
			t.Fatal("newforge reused 제련 quench cost")
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyNewForgeSelectQuench(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != NewForgeName || result.Changed || result.Continuation != NewForgeNamePhase ||
			result.Response != NewForgeNameResponse || result.QuenchChoice != tt.choice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != wantSum || result.MaterialSum != tt.materialSum ||
			result.ObjectID != tt.objectID || result.ObjectName != tt.name ||
			result.DiceCount != tt.dice || !result.Enchanted || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyNewForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyNewForgeSelectQuenchInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	for _, input := range []string{"", "0", "6", "x", " 1", "도", "제련"} {
		proposal, err := s.PlanNewForgeSelectQuench("actor", input, catalog, 900, NewForgeEmeraldCost)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != NewForgeReprompt || proposal.Changed || proposal.Continuation != NewForgeQuenchPhase ||
			proposal.Response != NewForgeRepromptResponse || proposal.QuenchChoice != 0 ||
			proposal.ShotsMax != 0 || proposal.ShotsCurrent != 0 || proposal.Sum != 0 ||
			proposal.MaterialSum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", input, proposal)
		}
		next, result, err := s.ApplyNewForgeSelectQuench(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != NewForgeReprompt || result.Changed || result.Continuation != NewForgeQuenchPhase ||
			result.Response != NewForgeRepromptResponse || result.Sum != 0 || result.ShotsMax != 0 ||
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

func TestPlanNewForgeSelectQuenchFailClosedWithoutCatalogOrPREADI(t *testing.T) {
	s := newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectQuench("actor", "1", nil, 900, NewForgeEmeraldCost); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if s.Players["actor"].Body.Gold != 50000 {
		t.Fatal("nil catalog mutated gold")
	}
	if _, err := s.PlanNewForgeSelectQuench("actor", "1", forgeWeaponCatalog(), 0, NewForgeEmeraldCost); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("object 0 err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectQuench("actor", "1", forgeWeaponCatalog(), 900, 0); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectQuench("actor", "1", forgeWeaponCatalog(), 900, ForgeSteelCost); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("제련 forge2 sum err=%v", err)
	}
	partial := forgeWeaponCatalog()
	delete(partial, 901)
	if _, err := s.PlanNewForgeSelectQuench("actor", "2", partial, 901, NewForgeTitaniumCost); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("missing 901 err=%v", err)
	}
	idle := newForgeState(NewForgeRoomID, true)
	actor := idle.Players["actor"]
	actor.Body.Gold = 50000
	idle.Players["actor"] = actor
	if _, err := idle.PlanNewForgeSelectQuench("actor", "1", forgeWeaponCatalog(), 900, NewForgeEmeraldCost); !errors.Is(err, ErrNewForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectQuench("actor", "1\n", forgeWeaponCatalog(), 900, NewForgeEmeraldCost); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	if _, _, err := s.NewForgeSelectQuench("actor", "1", nil, 900, NewForgeEmeraldCost); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanNewForgeSelectQuenchMissingRForgeIsTypedNoOp(t *testing.T) {
	s := newForgeSelectArmState(50000)
	room := s.Rooms[NewForgeRoomID]
	room.Resource.Flags = newForgeRoomFlags(false)
	s.Rooms[NewForgeRoomID] = room
	proposal, err := s.PlanNewForgeSelectQuench("actor", "1", forgeWeaponCatalog(), 900, NewForgeEmeraldCost)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeNotForgeResponse || proposal.ObjectID != 0 || proposal.Sum != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectQuench(proposal, forgeWeaponCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeNotForge || next.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestApplyNewForgeSelectQuenchRejectsTamperedNameAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	proposal, err := s.PlanNewForgeSelectQuench("actor", "1", catalog, 903, NewForgeEmeraldCost)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "name?"
	if _, _, err := s.ApplyNewForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectQuench("actor", "1", catalog, 903, NewForgeEmeraldCost)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyNewForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectQuench("actor", "1", catalog, 903, NewForgeEmeraldCost)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.RoomID = 1
	moved.Players["actor"] = actor
	moved.Rooms[1] = RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "elsewhere", Flags: newForgeRoomFlags(true)}},
		PlayerIDs: []string{"actor"},
	}
	delete(moved.Rooms, NewForgeRoomID)
	if _, _, err := moved.ApplyNewForgeSelectQuench(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("moved actor err=%v", err)
	}
}

func TestPlanApplyNewForgeSelectNameSetsWeaponNameWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	cases := []struct {
		input        string
		objectID     int16
		materialSum  int32
		quenchChoice int
		shots        int16
		quenchCost   int32
		dice         int16
		template     string
	}{
		{"abc", 900, NewForgeEmeraldCost, 1, NewForgeQuench100Shots, NewForgeQuench100Cost, NewForgeEmeraldDiceCount, "무명도"},
		{"불의검", 901, NewForgeTitaniumCost, 2, NewForgeQuench200Shots, NewForgeQuench200Cost, NewForgeTitaniumDiceCount, "무명검"},
		{"12345678901234567890", 902, NewForgeIllusionCost, 3, NewForgeQuench300Shots, NewForgeQuench300Cost, NewForgeIllusionDiceCount, "무명봉"},
		{"무명창이름", 903, NewForgeEmeraldCost, 4, NewForgeQuench400Shots, NewForgeQuench400Cost, NewForgeEmeraldDiceCount, "무명창"},
		{"xyz", 904, NewForgeEmeraldCost, 5, NewForgeQuench500Shots, NewForgeQuench500Cost, NewForgeEmeraldDiceCount, "무명궁"},
	}
	for _, tt := range cases {
		s := newForgeSelectArmState(50000)
		proposal, err := s.PlanNewForgeSelectName("actor", tt.input, catalog, tt.objectID, tt.materialSum, tt.quenchChoice)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		wantSum := tt.materialSum + tt.quenchCost
		if proposal.Action != NewForgeConfirm || proposal.Changed || proposal.Continuation != NewForgeConfirmPhase ||
			proposal.Response != NewForgeConfirmResponse || proposal.QuenchChoice != tt.quenchChoice ||
			proposal.ShotsMax != tt.shots || proposal.ShotsCurrent != tt.shots ||
			proposal.MaterialSum != tt.materialSum || proposal.Sum != wantSum ||
			proposal.ObjectID != tt.objectID || proposal.expectedObject.Name != tt.input ||
			proposal.expectedObject.ShotsMax != tt.shots || proposal.expectedObject.ShotsCurrent != tt.shots ||
			proposal.DiceCount != tt.dice || proposal.DiceSides != NewForgeMaterialDiceSides ||
			proposal.DicePlus != NewForgeMaterialDicePlus ||
			!forgeFlag(proposal.expectedObject.Flags, NewForgeEnchantedFlag) {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		if proposal.expectedObject.Name == tt.template {
			t.Fatalf("input %q kept catalog name %q", tt.input, tt.template)
		}
		if proposal.QuenchChoice == 1 && proposal.Sum == tt.materialSum+ForgeQuench100Cost {
			t.Fatal("newforge reused 제련 quench cost")
		}
		if proposal.expectedActor.Body.Gold != 50000 {
			t.Fatalf("input %q planned gold=%d", tt.input, proposal.expectedActor.Body.Gold)
		}
		next, result, err := s.ApplyNewForgeSelectName(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != NewForgeConfirm || result.Changed || result.Continuation != NewForgeConfirmPhase ||
			result.Response != NewForgeConfirmResponse || result.QuenchChoice != tt.quenchChoice ||
			result.ShotsMax != tt.shots || result.ShotsCurrent != tt.shots ||
			result.Sum != wantSum || result.MaterialSum != tt.materialSum ||
			result.ObjectID != tt.objectID || result.ObjectName != tt.input ||
			result.DiceCount != tt.dice || !result.Enchanted || result.NoBroadcast || !result.Reading {
			t.Fatalf("input %q result=%+v", tt.input, result)
		}
		body := next.Players["actor"].Body
		if body.Gold != 50000 || s.Players["actor"].Body.Gold != 50000 {
			t.Fatalf("input %q gold next=%d orig=%d", tt.input, body.Gold, s.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(body, NewForgeNoBroadcastFlag) || !PlayerFlagSet(body, NewForgeReadingFlag) {
			t.Fatalf("input %q flags=%+v", tt.input, body.Flags)
		}
		if len(body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("input %q invented inventory", tt.input)
		}
		drift := next.clone()
		moved := drift.Players["actor"]
		moved.Body.Gold = 7
		drift.Players["actor"] = moved
		if _, _, err := drift.ApplyNewForgeSelectName(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
			t.Fatalf("input %q gold-drift err=%v", tt.input, err)
		}
		if drift.Players["actor"].Body.Gold != 7 {
			t.Fatalf("input %q stale apply mutated gold", tt.input)
		}
	}
}

func TestPlanApplyNewForgeSelectNameInvalidRepromptsWithoutGoldCommit(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	cases := []struct {
		input    string
		response string
	}{
		{"", NewForgeNameShortResponse},
		{"ab", NewForgeNameShortResponse},
		{"a", NewForgeNameShortResponse},
		{"123456789012345678901", NewForgeNameLongResponse},
		{"한글일곱글자임", NewForgeNameLongResponse},
		{"ab(c", NewForgeNameParenResponse},
		{"검)", NewForgeNameParenResponse},
		{"(불의검)", NewForgeNameParenResponse},
	}
	for _, tt := range cases {
		proposal, err := s.PlanNewForgeSelectName("actor", tt.input, catalog, 900, NewForgeEmeraldCost, 1)
		if err != nil {
			t.Fatalf("input %q: %v", tt.input, err)
		}
		if proposal.Action != NewForgeReprompt || proposal.Changed || proposal.Continuation != NewForgeNamePhase ||
			proposal.Response != tt.response || proposal.QuenchChoice != 0 ||
			proposal.ShotsMax != 0 || proposal.ShotsCurrent != 0 || proposal.Sum != 0 ||
			proposal.MaterialSum != 0 || proposal.ObjectID != 0 {
			t.Fatalf("input %q proposal=%+v", tt.input, proposal)
		}
		next, result, err := s.ApplyNewForgeSelectName(proposal, catalog)
		if err != nil {
			t.Fatalf("input %q apply: %v", tt.input, err)
		}
		if result.Action != NewForgeReprompt || result.Changed || result.Continuation != NewForgeNamePhase ||
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
		if len(next.Players["actor"].Body.Inventory) != 0 || next.Players["actor"].Items != nil {
			t.Fatalf("input %q invented inventory", tt.input)
		}
	}
}

func TestPlanNewForgeSelectNameFailClosedWithoutCatalogOrPREADI(t *testing.T) {
	s := newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectName("actor", "abc", nil, 900, NewForgeEmeraldCost, 1); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if s.Players["actor"].Body.Gold != 50000 {
		t.Fatal("nil catalog mutated gold")
	}
	if _, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 0, NewForgeEmeraldCost, 1); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("object 0 err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, 0, 1); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, ForgeSteelCost, 1); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("제련 forge2 sum err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, NewForgeEmeraldCost, 0); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("zero quench err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, NewForgeEmeraldCost, 6); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("bad quench err=%v", err)
	}
	partial := forgeWeaponCatalog()
	delete(partial, 901)
	if _, err := s.PlanNewForgeSelectName("actor", "abc", partial, 901, NewForgeTitaniumCost, 2); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("missing 901 err=%v", err)
	}
	idle := newForgeState(NewForgeRoomID, true)
	actor := idle.Players["actor"]
	actor.Body.Gold = 50000
	idle.Players["actor"] = actor
	if _, err := idle.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, NewForgeEmeraldCost, 1); !errors.Is(err, ErrNewForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = newForgeSelectArmState(50000)
	if _, err := s.PlanNewForgeSelectName("actor", "abc\n", forgeWeaponCatalog(), 900, NewForgeEmeraldCost, 1); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	if _, _, err := s.NewForgeSelectName("actor", "abc", nil, 900, NewForgeEmeraldCost, 1); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanNewForgeSelectNameMissingRForgeIsTypedNoOp(t *testing.T) {
	s := newForgeSelectArmState(50000)
	room := s.Rooms[NewForgeRoomID]
	room.Resource.Flags = newForgeRoomFlags(false)
	s.Rooms[NewForgeRoomID] = room
	proposal, err := s.PlanNewForgeSelectName("actor", "abc", forgeWeaponCatalog(), 900, NewForgeEmeraldCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeNotForgeResponse || proposal.ObjectID != 0 || proposal.Sum != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectName(proposal, forgeWeaponCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Action != NewForgeNotForge || next.Players["actor"].Body.Gold != 50000 {
		t.Fatalf("result=%+v gold=%d", result, next.Players["actor"].Body.Gold)
	}
}

func TestApplyNewForgeSelectNameRejectsTamperedNameAndGold(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeSelectArmState(50000)
	proposal, err := s.PlanNewForgeSelectName("actor", "불의검", catalog, 900, NewForgeEmeraldCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "name?"
	if _, _, err := s.ApplyNewForgeSelectName(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectName("actor", "불의검", catalog, 900, NewForgeEmeraldCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.expectedObject.Name = "변조"
	if _, _, err := s.ApplyNewForgeSelectName(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered object name err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectName("actor", "불의검", catalog, 900, NewForgeEmeraldCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyNewForgeSelectName(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectName("actor", "불의검", catalog, 900, NewForgeEmeraldCost, 1)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyNewForgeSelectName(proposal, catalog); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}

func emptyNewForgeItems() *ItemCollection {
	return &ItemCollection{Items: map[string]Item{}}
}

func newForgeConfirmState(gold int32) State {
	s := newForgeSelectArmState(gold)
	actor := s.Players["actor"]
	actor.Items = emptyNewForgeItems()
	s.Players["actor"] = actor
	return s
}

func newForgeTestAllocate(id string) func() (string, error) {
	return func() (string, error) { return id, nil }
}

func TestPlanApplyNewForgeSelectConfirmChargesGoldAndAddsWeapon(t *testing.T) {
	catalog := forgeWeaponCatalog()
	const gold int32 = 2000000
	s := newForgeConfirmState(gold)
	allocate := newForgeTestAllocate("newforge-weapon-1")
	proposal, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	wantSum := NewForgeEmeraldCost + NewForgeQuench100Cost
	if proposal.Action != NewForgeGive || !proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeGiveResponse || proposal.Sum != wantSum ||
		proposal.ItemID != "newforge-weapon-1" || proposal.GoldAfter != gold-wantSum ||
		proposal.expectedObject.Name != "불의검" || proposal.WeaponName != "불의검" ||
		proposal.ObjectID != 900 || proposal.DiceCount != NewForgeEmeraldDiceCount ||
		len(proposal.Events) != 1 ||
		proposal.Events[0].Text != NewForgeBroadcastText("Alice") ||
		proposal.Events[0].ExcludeActorID != "actor" {
		t.Fatalf("proposal=%+v events=%+v", proposal, proposal.Events)
	}
	if proposal.Sum == ForgeSteelCost+ForgeQuench100Cost {
		t.Fatal("newforge reused 제련 confirm cost")
	}
	if forgeFlag(proposal.AfterFlags, NewForgeReadingFlag) || !forgeFlag(proposal.BeforeFlags, NewForgeReadingFlag) {
		t.Fatalf("PREADI after=%+v", proposal.AfterFlags)
	}
	item, ok := proposal.expectedAfterItems.Items["newforge-weapon-1"]
	if !ok || item.Object.Name != "불의검" || item.Object.DiceCount != NewForgeEmeraldDiceCount ||
		item.Object.DiceSides != NewForgeMaterialDiceSides || item.Object.DicePlus != NewForgeMaterialDicePlus ||
		item.Object.ShotsMax != NewForgeQuench100Shots || item.Object.ShotsCurrent != NewForgeQuench100Shots ||
		!containsString(proposal.expectedAfterItems.Inventory, "newforge-weapon-1") {
		t.Fatalf("after items=%+v", proposal.expectedAfterItems)
	}
	next, result, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != NewForgeGive || !result.Changed || result.Continuation != 0 ||
		result.Response != NewForgeGiveResponse || result.ItemID != "newforge-weapon-1" ||
		result.GoldAfter != gold-wantSum || result.ObjectName != "불의검" ||
		result.Reading || result.NoBroadcast || result.Sum != wantSum {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if body.Gold != gold-wantSum || s.Players["actor"].Body.Gold != gold {
		t.Fatalf("gold next=%d orig=%d", body.Gold, s.Players["actor"].Body.Gold)
	}
	if PlayerFlagSet(body, NewForgeReadingFlag) {
		t.Fatalf("flags next=%+v still PREADI", body.Flags)
	}
	if !PlayerFlagSet(s.Players["actor"].Body, NewForgeReadingFlag) {
		t.Fatal("plan/apply mutated original flags")
	}
	got, ok := next.Players["actor"].Items.Items["newforge-weapon-1"]
	if !ok || got.Object.Name != "불의검" || len(next.Players["actor"].Items.Inventory) != 1 {
		t.Fatalf("inventory=%+v", next.Players["actor"].Items)
	}
	if len(s.Players["actor"].Items.Items) != 0 {
		t.Fatal("plan/apply mutated original inventory")
	}
	if _, _, err := next.ApplyNewForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrNewForgeNotReading) && !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("second apply err=%v", err)
	}
	if next.Players["actor"].Body.Gold != gold-wantSum {
		t.Fatal("stale second apply mutated gold")
	}
}

func TestPlanApplyNewForgeSelectConfirmYesPrefixCharges(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeConfirmState(2000000)
	allocate := newForgeTestAllocate("newforge-weapon-prefix")
	proposal, err := s.PlanNewForgeSelectConfirm("actor", "예스", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeGive || proposal.ItemID != "newforge-weapon-prefix" {
		t.Fatalf("prefix proposal=%+v", proposal)
	}
	next, result, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil || result.Action != NewForgeGive || next.Players["actor"].Body.Gold != 0 {
		t.Fatalf("result=%+v gold=%d err=%v", result, next.Players["actor"].Body.Gold, err)
	}
}

func TestPlanApplyNewForgeSelectConfirmInsufficientGoldDoesNotCharge(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeConfirmState(50000)
	called := 0
	allocate := func() (string, error) {
		called++
		return "should-not-allocate", nil
	}
	proposal, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeTooPoor || !proposal.Changed || proposal.Continuation != 0 ||
		proposal.Response != NewForgeTooPoorResponse || proposal.ItemID != "" ||
		proposal.GoldAfter != 50000 || called != 0 || len(proposal.Events) != 0 {
		t.Fatalf("proposal=%+v called=%d", proposal, called)
	}
	next, result, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != NewForgeTooPoor || result.ItemID != "" || result.GoldAfter != 50000 ||
		result.Reading || next.Players["actor"].Body.Gold != 50000 ||
		len(next.Players["actor"].Items.Items) != 0 {
		t.Fatalf("result=%+v gold=%d items=%+v", result, next.Players["actor"].Body.Gold, next.Players["actor"].Items)
	}
	if PlayerFlagSet(next.Players["actor"].Body, NewForgeReadingFlag) {
		t.Fatal("too-poor left PREADI set")
	}
}

func TestPlanApplyNewForgeSelectConfirmCancelDoesNotCharge(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeConfirmState(2000000)
	called := 0
	allocate := func() (string, error) {
		called++
		return "should-not-allocate", nil
	}
	for _, input := range []string{"아니오", "", "무기만들기", "제련", "no"} {
		proposal, err := s.PlanNewForgeSelectConfirm("actor", input, catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if proposal.Action != NewForgeCancel || !proposal.Changed || proposal.Continuation != 0 ||
			proposal.Response != NewForgeCancelResponse || proposal.ItemID != "" ||
			proposal.GoldAfter != 2000000 || called != 0 {
			t.Fatalf("input %q proposal=%+v called=%d", input, proposal, called)
		}
		next, result, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate)
		if err != nil {
			t.Fatalf("input %q apply: %v", input, err)
		}
		if result.Action != NewForgeCancel || next.Players["actor"].Body.Gold != 2000000 ||
			len(next.Players["actor"].Items.Items) != 0 || result.Reading {
			t.Fatalf("input %q result=%+v gold=%d", input, result, next.Players["actor"].Body.Gold)
		}
		if PlayerFlagSet(next.Players["actor"].Body, NewForgeReadingFlag) {
			t.Fatalf("input %q left PREADI set", input)
		}
	}
}

func TestPlanNewForgeSelectConfirmFailClosedWithoutGoldOrObjectGraph(t *testing.T) {
	catalog := forgeWeaponCatalog()
	allocate := newForgeTestAllocate("newforge-weapon-1")
	s := newForgeSelectArmState(2000000)
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("nil items err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	actor := s.Players["actor"]
	actor.Body.Gold = -1
	s.Players["actor"] = actor
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("negative gold err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	actor = s.Players["actor"]
	actor.Body.Inventory = []LegacyObject{{Name: "유물"}}
	actor.Items = nil
	s.Players["actor"] = actor
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("legacy inventory err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", nil, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("nil catalog err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 899, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("bad object id err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, 0, 1, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("zero sum err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, ForgeSteelCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("제련 forge2 sum err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 0, "abc", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("zero quench err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "ab", allocate); !errors.Is(err, ErrNewForgeGoldObjectUnmigrated) {
		t.Fatalf("short name err=%v", err)
	}
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", nil); !errors.Is(err, ErrNewForgeItemAllocatorUnavailable) {
		t.Fatalf("nil allocate err=%v", err)
	}
	if _, _, err := s.NewForgeSelectConfirm("actor", "예", nil, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeCatalogUnmigrated) {
		t.Fatalf("convenience err=%v", err)
	}
}

func TestPlanNewForgeSelectConfirmFailClosedWithoutPREADIOrCanonicalRoom(t *testing.T) {
	catalog := forgeWeaponCatalog()
	allocate := newForgeTestAllocate("newforge-weapon-1")
	s := newForgeState(NewForgeRoomID, true)
	actor := s.Players["actor"]
	actor.Body.Gold = 2000000
	actor.Items = emptyNewForgeItems()
	s.Players["actor"] = actor
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeNotReading) {
		t.Fatalf("idle err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	delete(s.Rooms, NewForgeRoomID)
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeFlagsUnresolved) {
		t.Fatalf("unmigrated room err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	if _, err := s.PlanNewForgeSelectConfirm("actor", "예\n", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate); !errors.Is(err, ErrNewForgeSelectArmInput) {
		t.Fatalf("control input err=%v", err)
	}
	s = newForgeConfirmState(2000000)
	named := s.Rooms[NewForgeRoomID]
	named.Resource.Flags = newForgeRoomFlags(false)
	s.Rooms[NewForgeRoomID] = named
	proposal, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeNotForge || proposal.Changed || proposal.Response != NewForgeNotForgeResponse {
		t.Fatalf("missing RFORGE proposal=%+v", proposal)
	}
	s = newForgeConfirmState(2000000)
	wrong := newForgeState(1, true)
	actor = s.Players["actor"]
	actor.Body.RoomID = 1
	s.Players["actor"] = actor
	s.Rooms[1] = wrong.Rooms[1]
	delete(s.Rooms, NewForgeRoomID)
	proposal, err = s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "abc", allocate)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != NewForgeWrongRoom || proposal.Changed || proposal.Response != NewForgeWrongRoomResponse {
		t.Fatalf("wrong room proposal=%+v", proposal)
	}
}

func TestApplyNewForgeSelectConfirmRejectsTamperedAndGoldDrift(t *testing.T) {
	catalog := forgeWeaponCatalog()
	s := newForgeConfirmState(2000000)
	allocate := newForgeTestAllocate("newforge-weapon-1")
	proposal, err := s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "give?"
	if _, _, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Sum = 1
	if _, _, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered sum err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	proposal.ItemID = "forged"
	if _, _, err := s.ApplyNewForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrNewForgeStaleProposal) && !errors.Is(err, ErrNewForgeInvalidProposal) {
		t.Fatalf("tampered item id err=%v", err)
	}
	proposal, err = s.PlanNewForgeSelectConfirm("actor", "예", catalog, 900, NewForgeEmeraldCost, 1, "불의검", allocate)
	if err != nil {
		t.Fatal(err)
	}
	moved := s.clone()
	actor := moved.Players["actor"]
	actor.Body.Gold = 1
	moved.Players["actor"] = actor
	if _, _, err := moved.ApplyNewForgeSelectConfirm(proposal, catalog, allocate); !errors.Is(err, ErrNewForgeStaleProposal) {
		t.Fatalf("gold drift err=%v", err)
	}
	if moved.Players["actor"].Body.Gold != 1 {
		t.Fatal("stale apply mutated gold")
	}
}
