package world

import (
	"reflect"
	"strings"
	"testing"
)

func trackStateFixture(class, dex, level byte, roomTrack string) State {
	actor := PlayerState{Body: LegacyMonster{
		Name:   "Ranger",
		RoomID: 1,
		Type:   0,
		Class:  class,
		Level:  level,
		Stats:  [5]byte{10, dex, 10, 10, 10},
	}, Online: true}
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{1: {
			Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}, Track: roomTrack},
			PlayerIDs: []string{"ranger"},
		}},
		Players: map[string]PlayerState{"ranger": actor},
	}
}

func TestPlanAndApplyTrackSuccessClearsHiddenAndStoresRoomTrace(t *testing.T) {
	before := trackStateFixture(trackRangerClass, 15, 4, "동")
	proposal, err := before.PlanTrack("ranger", 100, func(low, high int) int {
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Cooldown || !proposal.ClearHidden || proposal.Interval != 4 || proposal.Chance != 35 || !proposal.Broadcast || !strings.Contains(proposal.Response, "동쪽") {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := before.ApplyTrack(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result != (TrackResult{Response: proposal.Response, Broadcast: true}) {
		t.Fatalf("result=%+v", result)
	}
	nextActor := next.Players["ranger"]
	if flag(nextActor.Body.Flags[:], playerHiddenStateFlag) || nextActor.Body.Timers[trackTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 4}) {
		t.Fatalf("next=%+v", next.Players["ranger"])
	}
	event, ok, err := next.RoomTrackEvent("ranger")
	if err != nil || !ok || event.ExcludeActorID != "ranger" || !strings.Contains(event.Text, "Ranger") {
		t.Fatalf("event=%+v ok=%v err=%v", event, ok, err)
	}
}

func TestTrackCooldownClearsHiddenWithoutTimerOrRandomness(t *testing.T) {
	s := trackStateFixture(trackRangerClass, 15, 4, "동")
	p := s.Players["ranger"]
	p.Body.Timers[trackTimerIndex] = LegacyTimer{LastTime: 100, Interval: 4}
	s.Players["ranger"] = p
	proposal, err := s.PlanTrack("ranger", 102, func(int, int) int {
		t.Fatal("cooldown consumed random")
		return 1
	})
	if err != nil || !proposal.Cooldown || !proposal.ClearHidden || proposal.Broadcast || proposal.WaitSeconds != 2 {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyTrack(proposal)
	nextActor := next.Players["ranger"]
	if err != nil || result.Broadcast || !reflect.DeepEqual(nextActor.Body.Timers[trackTimerIndex], s.Players["ranger"].Body.Timers[trackTimerIndex]) || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["ranger"], result, err)
	}
}

func TestTrackBlindAndEmptyTraceUseTimerButDoNotRollOrBroadcast(t *testing.T) {
	blind := trackStateFixture(trackRangerClass, 15, 4, "동")
	p := blind.Players["ranger"]
	p.Body.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	blind.Players["ranger"] = p
	proposal, err := blind.PlanTrack("ranger", 100, func(int, int) int {
		t.Fatal("blind track consumed random")
		return 1
	})
	if err != nil || proposal.Broadcast || !strings.Contains(proposal.Response, "눈이 멀어") {
		t.Fatalf("blind proposal=%+v err=%v", proposal, err)
	}
	next, result, err := blind.ApplyTrack(proposal)
	nextActor := next.Players["ranger"]
	if err != nil || result.Broadcast || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) || nextActor.Body.Timers[trackTimerIndex].LastTime != 100 {
		t.Fatalf("blind next=%+v result=%+v err=%v", next.Players["ranger"], result, err)
	}

	empty := trackStateFixture(trackRangerClass, 15, 4, "")
	proposal, err = empty.PlanTrack("ranger", 100, func(int, int) int { return 1 })
	if err != nil || proposal.Broadcast || !strings.Contains(proposal.Response, "흔적이 남아있지") {
		t.Fatalf("empty proposal=%+v err=%v", proposal, err)
	}
}

func TestTrackRejectsUnauthorizedAndInvalidRandomSource(t *testing.T) {
	unauthorized := trackStateFixture(4, 15, 4, "동")
	before := unauthorized
	proposal, err := unauthorized.PlanTrack("ranger", 100, func(int, int) int { return 1 })
	if err != nil || proposal.Broadcast || !strings.Contains(proposal.Response, "포졸만") {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, _, err := unauthorized.ApplyTrack(proposal)
	if err != nil || !reflect.DeepEqual(next, before) {
		t.Fatalf("unauthorized changed state: next=%+v before=%+v err=%v", next, before, err)
	}

	if _, err := trackStateFixture(trackRangerClass, 15, 4, "동").PlanTrack("ranger", 100, nil); err == nil {
		t.Fatal("missing random source accepted")
	}
}

func TestApplyTrackRejectsTamperedCommittedResponse(t *testing.T) {
	s := trackStateFixture(trackRangerClass, 15, 4, "동")
	proposal, err := s.PlanTrack("ranger", 100, func(int, int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "추적 실패!\r\n"
	if _, _, err := s.ApplyTrack(proposal); err == nil {
		t.Fatal("tampered track response accepted")
	}
}
