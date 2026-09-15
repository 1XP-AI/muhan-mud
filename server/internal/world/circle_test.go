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

func circleSelectorNPC(name string, keys [3]string) NPCState {
	return NPCState{
		Body: LegacyMonster{
			Name: name, Keys: keys, Type: 1, RoomID: 1,
			HPMax: 100, HPCurrent: 100,
		},
		Enemies: []NPCEnemy{},
	}
}

func circleSelectorPlayer(name string, keys [3]string) PlayerState {
	return PlayerState{
		Body: LegacyMonster{
			Name: name, Keys: keys, Type: 0, Class: CircleFighterClass,
			RoomID: 1, HPMax: 100, HPCurrent: 100,
		},
		Online: true,
	}
}

func TestSelectCircleTargetUsesLegacyCreaturePrefixes(t *testing.T) {
	tests := []struct {
		name  string
		query string
		keys  [3]string
	}{
		{name: "display", query: "DiSp", keys: [3]string{"unused-display-key", "", ""}},
		{name: "key0", query: "KEYZERO", keys: [3]string{"keyzero-alias", "", ""}},
		{name: "key1", query: "keyone", keys: [3]string{"", "keyone-alias", ""}},
		{name: "key2", query: "KeYtWo", keys: [3]string{"", "", "keytwo-alias"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := circleFixture()
			s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, PlayerIDs: []string{"actor"}, NPCIDs: []string{tt.name}}
			s.NPCs = map[string]NPCState{tt.name: circleSelectorNPC("DisplayTarget", tt.keys)}

			got, err := s.selectCircleTarget("actor", tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if got.targetID != tt.name || got.targetKind != CircleTargetNPC || got.target.Name != "DisplayTarget" {
				t.Fatalf("target=%+v", got)
			}
		})
	}
}

func TestSelectCircleTargetCountsMixedNPCFieldsOnce(t *testing.T) {
	s := circleFixture()
	s.Rooms[1] = RoomState{Resource: s.Rooms[1].Resource, PlayerIDs: []string{"actor"}, NPCIDs: []string{"mixed", "later"}}
	s.NPCs = map[string]NPCState{
		"mixed": circleSelectorNPC("UnrelatedDisplay", [3]string{"mix-zero", "MIX-one", "mix-two"}),
		"later": circleSelectorNPC("LaterDisplay", [3]string{"mix-later", "", ""}),
	}

	got, err := s.selectCircleTarget("actor", "MiX")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "mixed" {
		t.Fatalf("mixed-field root was not selected first: %+v", got)
	}
}

func TestSelectCircleTargetKeepsMonsterFirstPlayerFallbackAndActorExclusion(t *testing.T) {
	s := circleFixture()
	actor := s.Players["actor"]
	actor.Body.Keys = [3]string{"actor-alias", "", ""}
	s.Players["actor"] = actor
	s.Rooms[1] = RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"actor", "target"},
		NPCIDs:    []string{"monster"},
	}
	s.NPCs = map[string]NPCState{
		"monster": circleSelectorNPC("SharedTarget", [3]string{"shared-monster", "", ""}),
	}
	s.Players["target"] = circleSelectorPlayer("PlayerTarget", [3]string{"player-fallback", "", ""})

	got, err := s.selectCircleTarget("actor", "SHARED")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "monster" || got.targetKind != CircleTargetNPC {
		t.Fatalf("monster-first target=%+v", got)
	}

	room := s.Rooms[1]
	room.NPCIDs = nil
	s.Rooms[1] = room
	got, err = s.selectCircleTarget("actor", "PLAYER")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "target" || got.targetKind != CircleTargetPlayer {
		t.Fatalf("player fallback target=%+v", got)
	}

	got, err = s.selectCircleTarget("actor", "ACTOR")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "" {
		t.Fatalf("actor was selectable through own display/key: %+v", got)
	}
}

func TestSelectCircleTargetPreservesVisibilityAndShortPrefixBoundaries(t *testing.T) {
	s := circleFixture()
	hiddenNPC := circleSelectorNPC("HiddenNPC", [3]string{"hidden-npc", "", ""})
	setSettingFlag(&hiddenNPC.Body, circlePlayerInvisible, true)
	s.NPCs["hidden"] = hiddenNPC
	room := s.Rooms[1]
	room.NPCIDs = []string{"hidden"}
	s.Rooms[1] = room

	got, err := s.selectCircleTarget("actor", "HIDDEN")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "" {
		t.Fatalf("ordinary invisible NPC was visible without PDINVI: %+v", got)
	}
	actor := s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerDetect, true)
	s.Players["actor"] = actor
	got, err = s.selectCircleTarget("actor", "HIDDEN")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "hidden" {
		t.Fatalf("PDINVI did not reveal ordinary invisible NPC: %+v", got)
	}

	caretaker := circleSelectorNPC("Caretaker", [3]string{"caretaker", "", ""})
	caretaker.Body.Class = circleCaretakerClass
	setSettingFlag(&caretaker.Body, circlePlayerDMInvisible, true)
	s.NPCs["caretaker"] = caretaker
	room.NPCIDs = []string{"caretaker"}
	s.Rooms[1] = room
	got, err = s.selectCircleTarget("actor", "CARETAKER")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "" {
		t.Fatalf("caretaker PDMINV NPC was visible with PDINVI: %+v", got)
	}

	room.NPCIDs = nil
	room.PlayerIDs = []string{"actor", "hidden-player", "short-player"}
	s.Rooms[1] = room
	hiddenPlayer := circleSelectorPlayer("HiddenPlayer", [3]string{"hidden-player", "", ""})
	setSettingFlag(&hiddenPlayer.Body, circlePlayerInvisible, true)
	s.Players["hidden-player"] = hiddenPlayer
	s.Players["short-player"] = circleSelectorPlayer("Beta", [3]string{"", "", ""})
	actor = s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerDetect, false)
	s.Players["actor"] = actor
	got, err = s.selectCircleTarget("actor", "HIDDEN")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "" {
		t.Fatalf("ordinary invisible player was visible without PDINVI: %+v", got)
	}
	actor = s.Players["actor"]
	setSettingFlag(&actor.Body, circlePlayerDetect, true)
	s.Players["actor"] = actor
	got, err = s.selectCircleTarget("actor", "HIDDEN")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "hidden-player" {
		t.Fatalf("PDINVI did not reveal ordinary invisible player: %+v", got)
	}

	room.PlayerIDs = []string{"actor"}
	room.NPCIDs = []string{"short-npc"}
	s.Rooms[1] = room
	s.NPCs["short-npc"] = circleSelectorNPC("Alpha", [3]string{"alpha-key", "", ""})
	got, err = s.selectCircleTarget("actor", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "short-npc" {
		t.Fatalf("one-byte NPC prefix should be admitted: %+v", got)
	}

	room.NPCIDs = nil
	room.PlayerIDs = []string{"actor", "short-player"}
	s.Rooms[1] = room
	got, err = s.selectCircleTarget("actor", "b")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "" {
		t.Fatalf("one-byte player prefix should be rejected after fallback: %+v", got)
	}
	got, err = s.selectCircleTarget("actor", "be")
	if err != nil {
		t.Fatal(err)
	}
	if got.targetID != "short-player" {
		t.Fatalf("two-byte player prefix should be admitted: %+v", got)
	}
}
