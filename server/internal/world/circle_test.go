package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func circleFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name:      "아린",
					Type:      0,
					Class:     CircleFighterClass,
					Level:     10,
					Stats:     [5]byte{10, 10, 10, 10, 10},
					HPMax:     100,
					HPCurrent: 100,
					RoomID:    1,
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf": {
				Body: LegacyMonster{
					Name:      "늑대",
					Type:      1,
					Class:     0,
					Level:     10,
					Stats:     [5]byte{10, 10, 10, 10, 10},
					HPMax:     100,
					HPCurrent: 100,
					RoomID:    1,
				},
				Enemies: []NPCEnemy{},
			},
		},
	}
}

func circleRollScript(t *testing.T, values ...int) func(int, int) int {
	t.Helper()
	index := 0
	return func(low, high int) int {
		if index >= len(values) {
			t.Fatalf("unexpected circle random request %d..%d", low, high)
		}
		value := values[index]
		index++
		if value < low || value > high {
			t.Fatalf("circle random value %d outside %d..%d", value, low, high)
		}
		return value
	}
}

func TestPlanApplyCircleSuccessAddsHostilityAndBefuddle(t *testing.T) {
	s := circleFixture()
	original := s.clone()
	proposal, err := s.PlanCircle("actor", "늑대", 100, circleRollScript(t, 1, 8))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "wolf" || proposal.TargetKind != CircleTargetNPC || proposal.Chance != 50 || proposal.Roll != 1 || proposal.Delay != 8 || proposal.Interval != 2 || !proposal.Attempted || !proposal.Succeeded || !proposal.EnemyAdded || !proposal.Befuddled {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("planning mutated the input snapshot")
	}

	next, result, err := s.ApplyCircle(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != "circle" || !result.Changed || !result.Broadcast || !result.Attempted || !result.Succeeded || result.Delay != 8 {
		t.Fatalf("result=%+v", result)
	}
	npc := next.NPCs["wolf"]
	if len(npc.Enemies) != 1 || npc.Enemies[0].Target != (EntityRef{Kind: "player", ID: "actor"}) || npc.Enemies[0].Damage != 0 {
		t.Fatalf("npc enemies=%+v", npc.Enemies)
	}
	if !flag(npc.Body.Flags[:], circleNPCBefuddled) || npc.Body.Timers[CircleBefuddleTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 8}) {
		t.Fatalf("npc body=%+v", npc.Body)
	}
	if next.Players["actor"].Body.Timers[CircleTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 2}) {
		t.Fatalf("actor timer=%+v", next.Players["actor"].Body.Timers[CircleTimerIndex])
	}
	if result.Event == nil || len(result.Event.Texts) != 1 || !strings.Contains(result.Event.Texts[0], "뱅글뱅글") || result.Event.TargetText != "" {
		t.Fatalf("event=%+v", result.Event)
	}
}

func TestCircleFailureUsesThreeSecondCooldown(t *testing.T) {
	s := circleFixture()
	proposal, err := s.PlanCircle("actor", "늑대", 100, circleRollScript(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.Attempted || proposal.Succeeded || proposal.Interval != 3 || proposal.Delay != 0 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyCircle(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded || result.Befuddled || !result.EnemyAdded || next.NPCs["wolf"].Body.Timers[CircleBefuddleTimerIndex] != (LegacyTimer{}) {
		t.Fatalf("result=%+v npc=%+v", result, next.NPCs["wolf"])
	}
	if next.Players["actor"].Body.Timers[CircleTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 3}) {
		t.Fatalf("actor timer=%+v", next.Players["actor"].Body.Timers[CircleTimerIndex])
	}
}

func TestCircleCooldownDoesNotRevealOrConsumeRandomness(t *testing.T) {
	s := circleFixture()
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerHidden, true)
	setSettingFlag(&actor.Body, circlePlayerInvisible, true)
	actor.Body.Timers[CircleTimerIndex] = LegacyTimer{LastTime: 100, Interval: 2}
	s.Players["actor"] = actor
	original := s.clone()
	proposal, err := s.PlanCircle("actor", "늑대", 101, func(int, int) int {
		t.Fatal("circle cooldown consumed RNG")
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.NoOp || !proposal.Cooldown || proposal.WaitSeconds != 1 || proposal.Response != "1초만 기다리세요.\r\n" {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyCircle(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.Broadcast || result.Event != nil || !reflect.DeepEqual(next, original) {
		t.Fatalf("result=%+v next=%+v", result, next)
	}
}

func TestCircleNoHarmStillRevealsWithoutHostilityOrTimer(t *testing.T) {
	s := circleFixture()
	npc := s.NPCs["wolf"]
	setSettingFlag(&npc.Body, circleNPCNoHarm, true)
	s.NPCs["wolf"] = npc
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerHidden, true)
	setSettingFlag(&actor.Body, circlePlayerInvisible, true)
	s.Players["actor"] = actor

	proposal, err := s.PlanCircle("actor", "늑대", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !proposal.NoOp || !proposal.NoHarm || proposal.Chance != 50 || proposal.Attempted || !proposal.ClearInvisible || proposal.ExpectedEvent == nil {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyCircle(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoHarm || !result.Changed || !result.Broadcast || result.Attempted || result.Event == nil || len(result.Event.Texts) != 1 {
		t.Fatalf("result=%+v", result)
	}
	nextActor := next.Players["actor"]
	if flag(nextActor.Body.Flags[:], circlePlayerHidden) || flag(nextActor.Body.Flags[:], circlePlayerInvisible) || nextActor.Body.Timers[CircleTimerIndex] != (LegacyTimer{}) || len(next.NPCs["wolf"].Enemies) != 0 {
		t.Fatalf("next actor=%+v npc=%+v", nextActor.Body, next.NPCs["wolf"])
	}
}

func TestCirclePlayerTargetWritesBefuddleAndPrivateProjection(t *testing.T) {
	s := circleFixture()
	s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, PlayerIDs: []string{"actor", "target"}}
	s.NPCs = map[string]NPCState{}
	target := PlayerState{Body: LegacyMonster{
		Name:      "민수",
		Type:      0,
		Class:     CircleFighterClass,
		Level:     10,
		Stats:     [5]byte{10, 10, 10, 10, 10},
		HPMax:     100,
		HPCurrent: 100,
		RoomID:    1,
	}, Online: true}
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerChaos, true)
	target.Body.Flags[3] |= 1 << (circlePlayerChaos % 8)
	s.Players["actor"] = actor
	s.Players["target"] = target

	proposal, err := s.PlanCircle("actor", "민수", 100, circleRollScript(t, 1, 7))
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetKind != CircleTargetPlayer || proposal.TargetID != "target" || !proposal.Succeeded {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyCircle(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if next.Players["target"].Body.Timers[CircleBefuddleTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 7}) || result.Event == nil || result.Event.TargetText == "" {
		t.Fatalf("result=%+v target=%+v", result, next.Players["target"].Body)
	}
}

func TestCirclePermissionAndCharmStateFailClosed(t *testing.T) {
	s := circleFixture()
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerChaos, true)
	s.Players["actor"] = actor
	target := PlayerState{Body: LegacyMonster{
		Name: "민수", Type: 0, Class: CircleFighterClass, Level: 10,
		Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 100, RoomID: 1,
		Flags: [8]byte{circlePlayerCharm / 8: 1 << (circlePlayerCharm % 8)},
	}, Online: true}
	setSettingFlag(&target.Body, circlePlayerChaos, true)
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.PlayerIDs = []string{"actor", "target"}
	s.Rooms[1] = room
	s.NPCs = map[string]NPCState{}
	s.Players["target"] = target
	if _, err := s.PlanCircle("actor", "민수", 100, func(int, int) int { return 1 }); !errors.Is(err, ErrCircleCharmStateUnresolved) {
		t.Fatalf("charm err=%v", err)
	}

	actor = s.Players["actor"]
	actor.Body.Flags[3] &^= 1 << (circlePlayerChaos % 8)
	setSettingFlag(&actor.Body, circlePlayerFamily, true)
	target = s.Players["target"]
	setSettingFlag(&target.Body, circlePlayerFamily, true)
	s.Players["actor"], s.Players["target"] = actor, target
	if _, err := s.PlanCircle("actor", "민수", 100, func(int, int) int { return 1 }); !errors.Is(err, ErrCircleFamilyWarUnresolved) {
		t.Fatalf("war err=%v", err)
	}
}

func TestCircleRejectsDeadAndInvalidRandomInputs(t *testing.T) {
	s := circleFixture()
	target := s.NPCs["wolf"]
	target.Body.HPCurrent = 0
	s.NPCs["wolf"] = target
	if _, err := s.PlanCircle("actor", "늑대", 100, func(int, int) int { return 1 }); !errors.Is(err, ErrCircleTargetDead) {
		t.Fatalf("dead target err=%v", err)
	}

	s = circleFixture()
	if _, err := s.PlanCircle("actor", "늑대", 100, nil); err == nil {
		t.Fatal("nil circle RNG admitted")
	}
	if _, err := s.PlanCircle("actor", "늑대", 100, func(int, int) int { return 101 }); err == nil {
		t.Fatal("out-of-range circle RNG admitted")
	}
}

func TestCircleSourceGateOrderingAndChanceOverrides(t *testing.T) {
	s := circleFixture()
	actor := s.Players["actor"]
	actor.Body.Class = 3
	s.Players["actor"] = actor
	p, err := s.PlanCircle("actor", "", 100, nil)
	if err != nil || p.Response != circleNoArgumentResponse() {
		t.Fatalf("no-argument ordering p=%+v err=%v", p, err)
	}
	p, err = s.PlanCircle("actor", "없는대상", 100, nil)
	if err != nil || p.Response != circleUnauthorizedResponse() {
		t.Fatalf("authorization p=%+v err=%v", p, err)
	}

	s = circleFixture()
	target := s.NPCs["wolf"]
	setSettingFlag(&target.Body, circleNPCNoCircle, true)
	s.NPCs["wolf"] = target
	if got, err := CircleChance(s.Players["actor"].Body, target.Body); err != nil || got != 1 {
		t.Fatalf("MNOCIR chance=%d err=%v", got, err)
	}
	actor = s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerBlind, true)
	if got, err := CircleChance(actor.Body, target.Body); err != nil || got != 1 {
		t.Fatalf("PBLIND chance=%d err=%v", got, err)
	}
}
