package world

import (
	"errors"
	"reflect"
	"testing"
)

func magicStopTestState(class byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf-1", "wolf-2"},
			},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: LegacyMonster{
					Name: "수호자", Type: 0, Class: class, Level: 20, RoomID: 1,
					Stats: [5]byte{10, 20, 10, 10, 10}, MPMax: 90, MPCurrent: 17,
					Timers: [45]LegacyTimer{{}, {}, {}, {Interval: 7}},
				},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf-1": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf", "", ""}, Type: 1, Level: 4, Class: 4, RoomID: 1, HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 31}},
			"wolf-2": {Body: LegacyMonster{Name: "늑대", Keys: [3]string{"wolf", "", ""}, Type: 1, Level: 4, Class: 4, RoomID: 1, HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 31}},
		},
		ActiveNPCIDs: []string{"wolf-1", "wolf-2"},
	}
}

func TestPlanMagicStopFailsClosedBeforeCombatContinuation(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	before := s.clone()
	calls := 0
	_, err := s.PlanMagicStopWithOccurrence("actor", "늑대", 2, 1000, func(low, high int) int {
		calls++
		t.Fatalf("combat continuation consumed RNG: call=%d range=%d..%d", calls, low, high)
		return 1
	})
	if err == nil || !errors.Is(err, ErrMagicStopCombatSideEffectPending) {
		t.Fatalf("ordinary target was not fail-closed: err=%v", err)
	}
	if calls != 0 || !reflect.DeepEqual(s, before) {
		t.Fatalf("fail-closed plan mutated state or consumed RNG: calls=%d", calls)
	}
}

func TestMagicStopClassVisibilityCooldownAndOccurrenceAreFailClosed(t *testing.T) {
	unauthorized := magicStopTestState(4)
	p, err := unauthorized.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("unauthorized magic_stop consumed RNG")
		return 1
	})
	if err != nil || p.Authorized || !p.NoOp || p.TargetFound || p.Response == "" {
		t.Fatalf("unauthorized proposal=%+v err=%v", p, err)
	}
	next, result, err := unauthorized.ApplyMagicStop(p)
	if err != nil || result.Changed || !reflect.DeepEqual(next, unauthorized) {
		t.Fatalf("unauthorized apply result=%+v err=%v", result, err)
	}

	s := magicStopTestState(MagicStopRangerClass)
	npc := s.NPCs["wolf-1"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] |= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-1"] = npc
	npc = s.NPCs["wolf-2"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] |= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-2"] = npc
	actor := s.Players["actor"]
	actor.Body.Flags[MagicStopActorInvisibleFlag/8] |= 1 << (MagicStopActorInvisibleFlag % 8)
	s.Players["actor"] = actor
	p, err = s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("invisible target should not consume RNG")
		return 1
	})
	if err != nil || !p.NoOp || p.TargetFound || p.ClearInvisible || p.Response != "그런 괴물은 존재하지 않습니다.\n" {
		t.Fatalf("hidden target proposal=%+v err=%v", p, err)
	}
	if next, result, err := s.ApplyMagicStop(p); err != nil || result.Changed || !reflect.DeepEqual(next, s) {
		t.Fatalf("hidden target apply result=%+v err=%v", result, err)
	}

	npc = s.NPCs["wolf-1"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] &^= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-1"] = npc
	npc = s.NPCs["wolf-2"]
	npc.Body.Flags[MagicStopTargetInvisibleFlag/8] &^= 1 << (MagicStopTargetInvisibleFlag % 8)
	s.NPCs["wolf-2"] = npc
	actor = s.Players["actor"]
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20}
	s.Players["actor"] = actor
	p, err = s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int {
		t.Fatal("cooldown consumed RNG")
		return 1
	})
	if err != nil || !p.Cooldown || !p.NoOp || !p.TargetFound || !p.ClearInvisible || p.WaitSeconds != 15 || p.Broadcast == false {
		t.Fatalf("cooldown proposal=%+v err=%v", p, err)
	}
	next, result, err = s.ApplyMagicStop(p)
	nextActor := next.Players["actor"].Body
	if err != nil || !result.Cooldown || !result.Changed || !result.Broadcast || flag(nextActor.Flags[:], MagicStopActorInvisibleFlag) {
		t.Fatalf("cooldown apply result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.Timers[MagicStopCooldownTimerIndex] != actor.Body.Timers[MagicStopCooldownTimerIndex] {
		t.Fatal("cooldown rewrote timer")
	}
}

func TestMagicStopRejectsStaleAndUnresolvedState(t *testing.T) {
	s := magicStopTestState(MagicStopRangerClass)
	actor := s.Players["actor"]
	actor.Body.Timers[MagicStopCooldownTimerIndex] = LegacyTimer{LastTime: 995, Interval: 20}
	s.Players["actor"] = actor
	p, err := s.PlanMagicStop("actor", "늑대", 1000, func(int, int) int { return 100 })
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	changedActor := changed.Players["actor"]
	changedActor.Body.MPCurrent++
	changed.Players["actor"] = changedActor
	if _, _, err := changed.ApplyMagicStop(p); err == nil {
		t.Fatal("stale magic_stop proposal applied")
	}
	unresolved := State{Version: 1, Rooms: map[int16]RoomState{1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}, NPCIDs: []string{"wolf"}}}, Players: map[string]PlayerState{"actor": {Body: LegacyMonster{Name: "수호자", Type: 0, Class: MagicStopRangerClass, RoomID: 1}, Online: true}}, NPCs: nil}
	if _, err := unresolved.PlanMagicStop("actor", "늑대", 1000, func(int, int) int { return 1 }); err == nil {
		t.Fatal("unresolved canonical NPC state admitted")
	}
}
