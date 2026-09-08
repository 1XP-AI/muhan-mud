package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func lookAtTargetFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b"},
			},
			2: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2, Name: "외곽"}},
				PlayerIDs: []string{"c"},
			},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}, Online: true},
			"b": {Body: LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}, Online: true},
			"c": {Body: LegacyMonster{Name: "Carol", Type: 0, RoomID: 2}, Online: true},
		},
	}
}

func canonicalLookAtRoomState() State {
	s := lookAtTargetFixture()
	room := s.Rooms[1]
	room.Items = &ItemCollection{
		Items: map[string]Item{
			"floor-sword": {Object: LegacyObject{Name: "검", Description: "빛나는 검."}, Contents: []string{"nested-gem"}},
			"nested-gem":  {Object: LegacyObject{Name: "보석"}},
		},
		Inventory: []string{"floor-sword"},
	}
	room.Resource.Exits = []LegacyExit{{Name: "동", Destination: 2}}
	s.Rooms[1] = room
	return s
}

func TestPlanLookAtTargetAdmitsCanonicalRoomRootAndExitOnly(t *testing.T) {
	s := canonicalLookAtRoomState()
	for _, tc := range []struct {
		name, kind, id, response, event string
	}{
		{"검", "object", "floor-sword", "당신은 검을 봅니다.\r\n", "Alice님이 검을 봅니다."},
		{"동", "exit", "exit:1:0", "당신은 동을 봅니다.\r\n", "Alice님이 동을 봅니다."},
	} {
		next, result, err := s.PlanLookAtTarget("a", tc.name)
		if err != nil || result.TargetKind != tc.kind || result.TargetID != tc.id || result.Response != tc.response || result.Event == nil || !strings.Contains(result.Event.Text, tc.event) {
			t.Fatalf("target=%q next=%+v result=%+v err=%v", tc.name, next, result, err)
		}
		if result.Event.TargetText != "" {
			t.Fatalf("non-player target received private projection: %+v", result.Event)
		}
	}
}

func TestPlanLookAtTargetRejectsObjectFallbackPrefixOccurrenceAndNestedRoots(t *testing.T) {
	s := canonicalLookAtRoomState()
	for _, name := range []string{"검의", "보", "보석"} {
		if _, _, err := s.PlanLookAtTarget("a", name); err == nil {
			t.Fatalf("unsupported object target accepted: %q", name)
		}
	}
	room := s.Rooms[1]
	room.Items.Items["second-sword"] = Item{Object: LegacyObject{Name: "검"}}
	room.Items.Inventory = append(room.Items.Inventory, "second-sword")
	s.Rooms[1] = room
	if _, _, err := s.PlanLookAtTarget("a", "검"); !errors.Is(err, ErrAmbiguousCanonicalRoomObject) {
		t.Fatalf("duplicate exact root did not fail closed: %v", err)
	}

	s = lookAtTargetFixture()
	room = s.Rooms[1]
	room.Resource.Objects = []LegacyObject{{Name: "유물"}}
	s.Rooms[1] = room
	if _, _, err := s.PlanLookAtTarget("a", "유물"); !errors.Is(err, ErrCanonicalRoomObjectUnavailable) {
		t.Fatalf("legacy linked-list object fallback accepted: %v", err)
	}
	room.Resource.Exits = []LegacyExit{{Name: "동", Destination: 2}}
	s.Rooms[1] = room
	if _, result, err := s.PlanLookAtTarget("a", "동"); err != nil || result.TargetKind != "exit" {
		t.Fatalf("canonical exit was coupled to unrelated legacy object fallback: result=%+v err=%v", result, err)
	}
}

func TestPlanLookAtTargetObjectAndExitVisibilityUsesDetectFlag(t *testing.T) {
	s := canonicalLookAtRoomState()
	room := s.Rooms[1]
	item := room.Items.Items["floor-sword"]
	item.Object.Flags[objectInvisibleFlag/8] |= 1 << (objectInvisibleFlag % 8)
	room.Items.Items["floor-sword"] = item
	room.Resource.Exits[0].Flags[searchExitInvisibleFlag/8] |= 1 << (searchExitInvisibleFlag % 8)
	s.Rooms[1] = room
	for _, name := range []string{"검", "동"} {
		if _, _, err := s.PlanLookAtTarget("a", name); err == nil {
			t.Fatalf("invisible target accepted without detection: %q", name)
		}
	}
	actor := s.Players["a"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["a"] = actor
	for _, name := range []string{"검", "동"} {
		_, result, err := s.PlanLookAtTarget("a", name)
		if err != nil || result.TargetName != name {
			t.Fatalf("detected invisible target rejected: %q result=%+v err=%v", name, result, err)
		}
	}
}

func TestApplyLookAtTargetRejectsStaleCanonicalIdentity(t *testing.T) {
	s := canonicalLookAtRoomState()
	proposal, err := s.PlanLookAtTargetProposal("a", "검")
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[1]
	item := room.Items.Items["floor-sword"]
	item.Object.Name = "바뀐검"
	room.Items.Items["floor-sword"] = item
	s.Rooms[1] = room
	if _, _, err := s.ApplyLookAtTarget(proposal); err == nil {
		t.Fatal("stale object proposal applied")
	}
}

func TestPlanLookAtTargetUsesNPCFirstExactSameRoomOrder(t *testing.T) {
	s := lookAtTargetFixture()
	s.NPCs = map[string]NPCState{
		"npc-bob": {Body: LegacyMonster{Name: "Bob", Type: 1, RoomID: 1}},
	}
	r := s.Rooms[1]
	r.NPCIDs = []string{"npc-bob"}
	s.Rooms[1] = r

	original := s
	next, result, err := s.PlanLookAtTarget("a", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetKind != "npc" || result.TargetID != "npc-bob" || result.TargetName != "Bob" {
		t.Fatalf("target=%+v", result)
	}
	if result.Response != "당신은 Bob를 봅니다.\r\n" || result.Event == nil || result.Event.TargetText != "" || !strings.Contains(result.Event.Text, "Alice님이 Bob를 봅니다.") {
		t.Fatalf("result=%+v", result)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("look-at target mutated input snapshot")
	}
	if !reflect.DeepEqual(next, s) {
		t.Fatal("look-at target changed state beyond PHIDDN, which was already clear")
	}

	for _, name := range []string{"Bo", "Carol", "Nobody"} {
		if _, _, err := s.PlanLookAtTarget("a", name); err == nil {
			t.Fatalf("unproven/non-room target accepted: %q", name)
		}
	}
}

func TestPlanLookAtTargetBuildsDeterministicPlayerProjection(t *testing.T) {
	s := lookAtTargetFixture()
	original := s
	next, result, err := s.PlanLookAtTarget("a", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetKind != "player" || result.TargetID != "b" || result.Response != "당신은 Bob님을 봅니다.\r\n" || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	if result.Event == nil || result.Event.TargetText != "\nAlice님이 당신을 봅니다.\r\n" || result.Event.Text != "\nAlice님이 Bob님을 봅니다.\r\n" || result.Event.ExcludeActorID != "a" || result.Event.ExcludeTargetID != "b" {
		t.Fatalf("event=%+v", result.Event)
	}
	nextActor := next.Players["a"]
	originalActor := s.Players["a"]
	if !reflect.DeepEqual(s, original) || nextActor.Body.Flags != originalActor.Body.Flags {
		t.Fatal("target look mutated a state field unexpectedly")
	}

	hidden := s.Players["a"]
	hidden.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = hidden
	next, result, err = s.PlanLookAtTarget("a", "Bob")
	nextActor = next.Players["a"]
	if err != nil || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) || result.Response != "당신은 Bob님을 봅니다.\r\n" {
		t.Fatalf("PHIDDN result=%+v err=%v", result, err)
	}
	inputActor := s.Players["a"]
	if !flag(inputActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatal("input PHIDDN was mutated")
	}
}

func TestPlanLookAtTargetClearsPHIDDNBeforeSilenceAndTargetLookup(t *testing.T) {
	s := lookAtTargetFixture()
	actor := s.Players["a"]
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	actor.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	s.Players["a"] = actor
	original := s

	// C returns from action() before find_crt when PSILNC is set.  The Go
	// reducer keeps that ordering: an absent target does not turn silence into
	// a target error, while PHIDDN is still cleared in the committed clone.
	next, result, err := s.PlanLookAtTarget("a", "not-present")
	if err != nil || result.Response != "한마디도 할수 없습니다!\r\n" || result.Broadcast || result.Event != nil {
		t.Fatalf("silent result=%+v err=%v", result, err)
	}
	nextActor := next.Players["a"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) || !flag(nextActor.Body.Flags[:], playerSilentStateFlag) {
		t.Fatalf("silence/PHIDDN ordering lost: %+v", next.Players["a"].Body.Flags)
	}
	originalActor := original.Players["a"]
	if !flag(originalActor.Body.Flags[:], playerHiddenStateFlag) || !reflect.DeepEqual(s, original) {
		t.Fatal("silent plan mutated input snapshot")
	}
}

func TestPlanLookAtTargetUsesSourceInvisibleGate(t *testing.T) {
	s := lookAtTargetFixture()
	target := s.Players["b"]
	target.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	s.Players["b"] = target
	if _, _, err := s.PlanLookAtTarget("a", "Bob"); err == nil {
		t.Fatal("invisible target accepted without PDINVI")
	}
	actor := s.Players["a"]
	actor.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	s.Players["a"] = actor
	_, result, err := s.PlanLookAtTarget("a", "Bob")
	if err != nil || result.Response != "당신은 Bob(*)님을 봅니다.\r\n" {
		t.Fatalf("detected invisible target result=%+v err=%v", result, err)
	}
}
