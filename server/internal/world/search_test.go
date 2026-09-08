package world

import (
	"reflect"
	"strings"
	"testing"
)

func searchStateFixture() State {
	var hiddenPlayer, invisibleHidden LegacyMonster
	hiddenPlayer = LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1}
	hiddenPlayer.Flags[1/8] |= 1 << (1 % 8)
	invisibleHidden = LegacyMonster{Name: "Carol", RoomID: 1, Type: 0, Class: 4, Level: 1}
	invisibleHidden.Flags[1/8] |= 1 << (1 % 8)
	invisibleHidden.Flags[2/8] |= 1 << (2 % 8)
	actor := LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 15}}
	actor.Flags[1/8] |= 1 << (1 % 8)
	npcBody := LegacyMonster{Name: "Goblin", RoomID: 1, Type: 1, Class: 4, Level: 1}
	npcBody.Flags[1/8] |= 1 << (1 % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a", "b", "c"},
			NPCIDs:    []string{"goblin"},
		}},
		Players: map[string]PlayerState{
			"a": {Body: actor, Online: true},
			"b": {Body: hiddenPlayer, Online: true},
			"c": {Body: invisibleHidden, Online: true},
		},
		NPCs: map[string]NPCState{"goblin": {Body: npcBody}},
	}
}

func TestPlanAndApplySearchDetectsCanonicalHiddenTargetsInSourceOrder(t *testing.T) {
	before := searchStateFixture()
	rolls := []int{1, 1, 1}
	calls := 0
	proposal, err := before.PlanSearch("a", 100, func(low, high int) int {
		calls++
		if low != 1 || high != 100 || calls > len(rolls) {
			t.Fatalf("unexpected search random call %d..%d number=%d", low, high, calls)
		}
		return rolls[calls-1]
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || proposal.Chance != 22 || !proposal.ClearHidden || !proposal.Broadcast || len(proposal.Targets) != 2 || proposal.Targets[0].ID != "b" || proposal.Targets[1].ID != "goblin" {
		t.Fatalf("proposal=%+v calls=%d", proposal, calls)
	}
	if !before.Players["a"].Online || before.Players["a"].Body.Flags[0]&(1<<1) == 0 {
		t.Fatal("planning mutated source actor")
	}
	next, result, err := before.ApplySearch(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Broadcast || len(result.Targets) != 2 || !strings.Contains(result.Response, "Bob") || !strings.Contains(result.Response, "Goblin") {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["a"].Body.Flags[0]&(1<<1) != 0 || next.Players["a"].Body.Timers[searchTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 7}) {
		t.Fatalf("applied actor=%+v", next.Players["a"])
	}
	if !reflect.DeepEqual(before.Players["b"].Body.Flags, next.Players["b"].Body.Flags) || !reflect.DeepEqual(before.NPCs["goblin"].Body.Flags, next.NPCs["goblin"].Body.Flags) {
		t.Fatal("search changed target hidden state")
	}
	event, ok, err := next.RoomSearchEvent("a", result.Targets)
	if err != nil || !ok || len(event.Texts) != 2 || !strings.Contains(event.Texts[0], "Alice") || !strings.Contains(event.Texts[1], "그녀") {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestSearchChancePreservesLegacyOverrideOrdering(t *testing.T) {
	blind := LegacyMonster{Class: searchRangerClass, Stats: [5]byte{0, 0, 0, 0, 63}}
	blind.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	chance, err := SearchChance(blind)
	if err != nil || chance != 20 {
		t.Fatalf("ranger blind chance=%d err=%v", chance, err)
	}
	blind.Class = searchCaretakerClass
	chance, err = SearchChance(blind)
	if err != nil || chance != 100 {
		t.Fatalf("caretaker blind chance=%d err=%v", chance, err)
	}
}

func TestSearchCooldownIsPureNoOpAndDoesNotConsumeRandomness(t *testing.T) {
	s := searchStateFixture()
	p := s.Players["a"]
	p.Body.Timers[searchTimerIndex] = LegacyTimer{LastTime: 100, Interval: 7}
	s.Players["a"] = p
	proposal, err := s.PlanSearch("a", 105, func(int, int) int { t.Fatal("cooldown consumed random"); return 1 })
	if err != nil || !proposal.Cooldown || proposal.ClearHidden || proposal.Broadcast || proposal.WaitSeconds != 2 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplySearch(proposal)
	if err != nil || result.Broadcast || result.Response != "2초동안 기다리세요.\n" || !reflect.DeepEqual(s, next) {
		t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
	}
}

func TestSearchFailsClosedForMissingRandomSourceAndStaleTarget(t *testing.T) {
	s := searchStateFixture()
	if _, err := s.PlanSearch("a", 100, nil); err == nil {
		t.Fatal("missing random source accepted for hidden target")
	}
	proposal, err := s.PlanSearch("a", 100, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	target := s.Players["b"]
	target.Body.Flags[1/8] &^= 1 << (1 % 8)
	s.Players["b"] = target
	if _, _, err := s.ApplySearch(proposal); err == nil {
		t.Fatal("stale visible target was accepted")
	}
}
