package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func stealTestFlag(bits *[8]byte, number int) {
	bits[number/8] |= 1 << (number % 8)
}

func stealTestItem(name string) Item {
	return Item{Object: LegacyObject{Name: name, Type: 1}}
}

func stealTestFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "방"}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"npc-1"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: stealThiefClass, Level: 20, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
		},
		NPCs: map[string]NPCState{
			"npc-1": {
				Body:    LegacyMonster{Name: "고블린", RoomID: 1, Type: 1, Class: 4, Level: 1},
				Items:   &ItemCollection{Items: map[string]Item{"pouch": {Object: LegacyObject{Name: "주머니", Type: 1}, Contents: []string{"gem"}}, "gem": stealTestItem("보석")}, Inventory: []string{"pouch"}},
				Enemies: []NPCEnemy{},
			},
		},
	}
}

func stealTestPlayerTarget(s *State) {
	s.Players["target"] = PlayerState{
		Body:   LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Level: 1, Flags: [8]byte{3: 1 << (stealPlayerChaos % 8)}},
		Online: true,
		Items:  &ItemCollection{Items: map[string]Item{"ring": stealTestItem("반지")}, Inventory: []string{"ring"}},
	}
	room := s.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "target")
	s.Rooms[1] = room
}

func TestPlanApplyStealSuccessMovesCanonicalSubtreeAndReveals(t *testing.T) {
	s := stealTestFixture()
	actor := s.Players["actor"]
	stealTestFlag(&actor.Body.Flags, stealPlayerHidden)
	stealTestFlag(&actor.Body.Flags, stealPlayerInvisible)
	s.Players["actor"] = actor
	original := s.clone()

	proposal, err := s.PlanSteal("actor", "주머니", "고블린", 100, func(lo, hi int) int {
		if lo != 1 || hi != 100 {
			t.Fatalf("unexpected steal roll %d..%d", lo, hi)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Succeeded || !proposal.Attempted || proposal.TargetID != "npc-1" || proposal.ItemID != "pouch" || !proposal.Reveal || !proposal.Broadcast || proposal.ExpectedEvent == nil {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the authoritative snapshot")
	}

	next, result, err := s.ApplySteal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || !result.Attempted || result.Roll != 1 || !result.Changed || result.Event == nil || len(result.Event.Texts) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if got := next.Players["actor"].Items.Inventory; !reflect.DeepEqual(got, []string{"pouch"}) {
		t.Fatalf("actor inventory=%v", got)
	}
	if _, ok := next.Players["actor"].Items.Items["gem"]; !ok {
		t.Fatal("nested item did not move with stolen root")
	}
	if len(next.NPCs["npc-1"].Items.Inventory) != 0 || len(next.NPCs["npc-1"].Items.Items) != 0 {
		t.Fatalf("npc inventory=%+v", next.NPCs["npc-1"].Items)
	}
	updatedActor := next.Players["actor"].Body
	if flag(updatedActor.Flags[:], stealPlayerHidden) || flag(updatedActor.Flags[:], stealPlayerInvisible) || updatedActor.Timers[stealTimerIndex] != (LegacyTimer{LastTime: 100, Interval: stealCooldownSeconds}) {
		t.Fatalf("actor side effects=%+v timers=%+v", updatedActor.Flags, updatedActor.Timers[stealTimerIndex])
	}
	if len(next.NPCs["npc-1"].Enemies) != 0 {
		t.Fatalf("successful steal made npc hostile: %+v", next.NPCs["npc-1"].Enemies)
	}
}

func TestPlanApplyStealFailureAddsNPCEnemyAndPersistsReceiptData(t *testing.T) {
	s := stealTestFixture()
	proposal, err := s.PlanSteal("actor", "주머니", "고블린", 200, func(lo, hi int) int {
		if lo != 1 || hi != 100 {
			t.Fatalf("unexpected steal roll %d..%d", lo, hi)
		}
		return 100
	})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplySteal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded || !result.Attempted || !result.EnemyAdded || result.Event == nil || len(result.Event.Texts) != 1 || result.Event.TargetID != "npc-1" {
		t.Fatalf("result=%+v", result)
	}
	if len(next.NPCs["npc-1"].Enemies) != 1 || next.NPCs["npc-1"].Enemies[0].Target != (EntityRef{Kind: "player", ID: "actor"}) {
		t.Fatalf("npc hostility=%+v", next.NPCs["npc-1"].Enemies)
	}
	if len(next.NPCs["npc-1"].Items.Inventory) != 1 || next.Players["actor"].Body.Timers[stealTimerIndex].LastTime != 200 {
		t.Fatalf("failed state=%+v actor=%+v", next.NPCs["npc-1"].Items, next.Players["actor"].Body)
	}
}

func TestApplyStealCooldownClearsHiddenWithoutReroll(t *testing.T) {
	s := stealTestFixture()
	actor := s.Players["actor"]
	stealTestFlag(&actor.Body.Flags, stealPlayerHidden)
	actor.Body.Timers[stealTimerIndex] = LegacyTimer{LastTime: 100, Interval: stealCooldownSeconds}
	s.Players["actor"] = actor
	proposal, err := s.PlanSteal("actor", "주머니", "고블린", 102, func(int, int) int {
		t.Fatal("cooldown consumed random source")
		return 0
	})
	if err != nil || !proposal.Cooldown || proposal.ClearInvisible || proposal.WaitSeconds != 3 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplySteal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	cooldownActor := next.Players["actor"].Body
	if !result.Cooldown || result.Attempted || !result.Changed || flag(cooldownActor.Flags[:], stealPlayerHidden) || cooldownActor.Timers[stealTimerIndex] != actor.Body.Timers[stealTimerIndex] {
		t.Fatalf("cooldown result=%+v actor=%+v", result, next.Players["actor"])
	}
}

func TestPlanApplyStealRejectedAttemptStillRevealsAndStartsCooldown(t *testing.T) {
	s := stealTestFixture()
	actor := s.Players["actor"]
	stealTestFlag(&actor.Body.Flags, stealPlayerInvisible)
	s.Players["actor"] = actor
	npc := s.NPCs["npc-1"]
	stealTestFlag(&npc.Body.Flags, stealNPCNoHarm)
	s.NPCs["npc-1"] = npc

	proposal, err := s.PlanSteal("actor", "주머니", "고블린", 250, func(int, int) int {
		t.Fatal("rejected steal consumed random")
		return 0
	})
	if err != nil || proposal.Succeeded || proposal.Attempted || !proposal.Reveal || !strings.HasPrefix(proposal.Response, "당신의 모습이 서서히 드러납니다") || proposal.ExpectedEvent == nil {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplySteal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	updatedActor := next.Players["actor"].Body
	if result.Attempted || result.Succeeded || !result.Changed || flag(updatedActor.Flags[:], stealPlayerInvisible) || updatedActor.Timers[stealTimerIndex].LastTime != 250 {
		t.Fatalf("result=%+v actor=%+v", result, next.Players["actor"])
	}
}

func TestPlanApplyStealPlayerSuccessMovesItemAndSetsPlayerKillTimer(t *testing.T) {
	s := stealTestFixture()
	stealTestPlayerTarget(&s)
	actor := s.Players["actor"]
	stealTestFlag(&actor.Body.Flags, stealPlayerChaos)
	actor.Items = &ItemCollection{Items: map[string]Item{}}
	s.Players["actor"] = actor

	calls := 0
	proposal, err := s.PlanSteal("actor", "반지", "Bob", 300, func(lo, hi int) int {
		calls++
		switch calls {
		case 1:
			if lo != 1 || hi != 100 {
				t.Fatalf("unexpected chance roll %d..%d", lo, hi)
			}
			return 1
		case 2:
			if lo != 7 || hi != 10 {
				t.Fatalf("unexpected player-kill roll %d..%d", lo, hi)
			}
			return 9
		default:
			t.Fatal("too many random draws")
			return 0
		}
	})
	if err != nil || !proposal.Succeeded || proposal.PlayerKillRoll != 9 || proposal.PlayerKillInterval != 9*86400 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplySteal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || len(next.Players["target"].Items.Inventory) != 0 || len(next.Players["target"].Items.Items) != 0 || !reflect.DeepEqual(next.Players["actor"].Items.Inventory, []string{"ring"}) {
		t.Fatalf("player transfer result=%+v actor=%+v target=%+v", result, next.Players["actor"].Items, next.Players["target"].Items)
	}
	if got := next.Players["target"].Body.Timers[stealPlayerKillTimer]; got != (LegacyTimer{LastTime: 300, Interval: 9 * 86400}) {
		t.Fatalf("player kill timer=%+v", got)
	}
}

func TestStealRejectsRestrictionsAndLegacyInventoryWithoutUsingFallback(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*State)
		want  string
	}{
		{name: "safe room", setup: func(s *State) {
			stealTestPlayerTarget(s)
			room := s.Rooms[1]
			stealTestFlag(&room.Resource.Flags, stealRoomNoPlayerKill)
			s.Rooms[1] = room
			actor := s.Players["actor"]
			stealTestFlag(&actor.Body.Flags, stealPlayerChaos)
			s.Players["actor"] = actor
		}, want: "이 방에서는 훔칠 수 없습니다."},
		{name: "blind", setup: func(s *State) {
			actor := s.Players["actor"]
			stealTestFlag(&actor.Body.Flags, stealPlayerBlind)
			s.Players["actor"] = actor
		}, want: "눈이 멀어"},
		{name: "legacy inventory unresolved", setup: func(s *State) {
			npc := s.NPCs["npc-1"]
			npc.Items = nil
			npc.Body.Inventory = []LegacyObject{{Name: "주머니"}}
			s.NPCs["npc-1"] = npc
		}, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := stealTestFixture()
			tc.setup(&s)
			if tc.name == "legacy inventory unresolved" {
				_, err := s.PlanSteal("actor", "주머니", "고블린", 1, func(int, int) int { t.Fatal("unresolved inventory consumed random"); return 1 })
				if !errors.Is(err, ErrStealInventoryUnresolved) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			proposal, err := s.PlanSteal("actor", "반지", "Bob", 1, func(int, int) int { t.Fatal("rejected steal consumed random"); return 1 })
			if tc.name == "safe room" {
				if err != nil || proposal.Response == "" || !containsText(proposal.Response, tc.want) {
					t.Fatalf("proposal=%+v err=%v", proposal, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if proposal.TargetID != "" {
				t.Fatalf("blind player fallback unexpectedly selected target: %+v", proposal)
			}
		})
	}
}

func containsText(value, want string) bool {
	return len(want) == 0 || strings.Contains(value, want)
}
