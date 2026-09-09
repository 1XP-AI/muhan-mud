package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func marriageTestState() State {
	room := LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "결혼식장"}}
	room.Flags[MarriageRoomFlag/8] |= 1 << (MarriageRoomFlag % 8)
	dayOfAge := int32(7 * 86400)
	alice := LegacyMonster{Name: "Alice", Type: 0, RoomID: 1}
	alice.Timers[MarriageHoursTimerIndex].Interval = dayOfAge
	alice.Flags[MarriageMaleFlag/8] |= 1 << (MarriageMaleFlag % 8)
	bob := LegacyMonster{Name: "Bob", Type: 0, RoomID: 1}
	bob.Timers[MarriageHoursTimerIndex].Interval = dayOfAge
	return State{
		Version: 1,
		Rooms:   map[int16]RoomState{1: {Resource: room, PlayerIDs: []string{"alice", "bob"}}},
		Players: map[string]PlayerState{
			"alice": {Body: alice, Online: true},
			"bob":   {Body: bob, Online: true},
		},
	}
}

func marriageFlag(body LegacyMonster, bit uint) bool {
	return body.Flags[bit/8]&(1<<(bit%8)) != 0
}

func editMarriagePlayer(s *State, id string, edit func(*LegacyMonster)) {
	player := s.Players[id]
	edit(&player.Body)
	s.Players[id] = player
}

func TestPlanAndApplyMarriageRequestAcceptsAndEmitsCommittedProjections(t *testing.T) {
	state := marriageTestState()
	before := state
	request, err := state.PlanMarriage("alice", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	if request.Action != MarriageRequest || request.TargetID != "bob" || request.Response != "당신은 Bob님에게 결혼을 신청하였습니다.\r\n" {
		t.Fatalf("request=%+v", request)
	}
	next, result, err := state.ApplyMarriage(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("planning mutated the source state")
	}
	if result.Action != MarriageRequest || !result.Changed || result.Broadcast || len(result.Events) != 1 || result.Events[0].RecipientID != "bob" {
		t.Fatalf("request result=%+v", result)
	}
	if !marriageFlag(next.Players["alice"].Body, MarriagePendingFlag) || next.Players["alice"].Body.Keys[MarriageSpouseKeyIndex] != "mBob" {
		t.Fatalf("pending proposal not persisted: %+v", next.Players["alice"].Body)
	}

	accept, err := next.PlanMarriage("bob", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if accept.Action != MarriageAccept || accept.TargetID != "alice" {
		t.Fatalf("accept proposal=%+v", accept)
	}
	married, result, err := next.ApplyMarriage(accept)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != MarriageAccept || !result.Broadcast || result.BroadcastText == "" || len(result.Events) != 1 || result.Events[0].RecipientID != "alice" {
		t.Fatalf("accept result=%+v", result)
	}
	for _, id := range []string{"alice", "bob"} {
		body := married.Players[id].Body
		if !marriageFlag(body, MarriageActiveFlag) || marriageFlag(body, MarriagePendingFlag) {
			t.Fatalf("marriage flags for %s=%08b", id, body.Flags)
		}
	}
	if married.Players["alice"].Body.Keys[MarriageSpouseKeyIndex] != "mBob" || married.Players["bob"].Body.Keys[MarriageSpouseKeyIndex] != "mAlice" {
		t.Fatalf("spouse keys: alice=%q bob=%q", married.Players["alice"].Body.Keys[MarriageSpouseKeyIndex], married.Players["bob"].Body.Keys[MarriageSpouseKeyIndex])
	}
	if !strings.Contains(result.BroadcastText, "Alice님과 Bob님") {
		t.Fatalf("broadcast=%q", result.BroadcastText)
	}
}

func TestPlanMarriagePendingActorCancelsBeforeTargetResolution(t *testing.T) {
	state := marriageTestState()
	editMarriagePlayer(&state, "alice", func(body *LegacyMonster) {
		body.Flags[MarriagePendingFlag/8] |= 1 << (MarriagePendingFlag % 8)
		body.Keys[MarriageSpouseKeyIndex] = "mBob"
	})
	proposal, err := state.PlanMarriage("alice", "nobody")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != MarriageCancel || proposal.TargetID != "" {
		t.Fatalf("cancel proposal=%+v", proposal)
	}
	next, result, err := state.ApplyMarriage(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != MarriageCancel || !result.Changed || result.Response != MarriageCancelResponse {
		t.Fatalf("cancel result=%+v", result)
	}
	if marriageFlag(next.Players["alice"].Body, MarriagePendingFlag) {
		t.Fatal("pending flag remains after cancellation")
	}
}

func TestPlanMarriageFailsClosedAtCanonicalGates(t *testing.T) {
	tests := []struct {
		name string
		edit func(*State)
		want error
	}{
		{name: "not wedding hall", edit: func(s *State) {
			room := s.Rooms[1]
			room.Resource.Flags[MarriageRoomFlag/8] &^= 1 << (MarriageRoomFlag % 8)
			s.Rooms[1] = room
		}, want: ErrMarriageNotWeddingHall},
		{name: "actor too young", edit: func(s *State) {
			editMarriagePlayer(s, "alice", func(body *LegacyMonster) { body.Timers[MarriageHoursTimerIndex].Interval = 6 * 86400 })
		}, want: ErrMarriageActorTooYoung},
		{name: "target required", edit: func(*State) {}, want: ErrMarriageTargetRequired},
		{name: "same sex", edit: func(s *State) {
			editMarriagePlayer(s, "bob", func(body *LegacyMonster) { body.Flags[MarriageMaleFlag/8] |= 1 << (MarriageMaleFlag % 8) })
		}, want: ErrMarriageSameSex},
		{name: "target too young", edit: func(s *State) {
			editMarriagePlayer(s, "bob", func(body *LegacyMonster) { body.Timers[MarriageHoursTimerIndex].Interval = 6 * 86400 })
		}, want: ErrMarriageTargetTooYoung},
		{name: "target married", edit: func(s *State) {
			editMarriagePlayer(s, "bob", func(body *LegacyMonster) { body.Flags[MarriageActiveFlag/8] |= 1 << (MarriageActiveFlag % 8) })
		}, want: ErrMarriageTargetMarried},
		{name: "target invisible", edit: func(s *State) {
			editMarriagePlayer(s, "bob", func(body *LegacyMonster) { body.Flags[MarriageInvisibleFlag/8] |= 1 << (MarriageInvisibleFlag % 8) })
		}, want: ErrMarriageTargetInvisible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := marriageTestState()
			tt.edit(&state)
			selector := "Bob"
			if tt.name == "target required" {
				selector = ""
			}
			_, err := state.PlanMarriage("alice", selector)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err=%v, want %v", err, tt.want)
			}
		})
	}
}

func TestApplyMarriageRejectsStaleProposal(t *testing.T) {
	state := marriageTestState()
	proposal, err := state.PlanMarriage("alice", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	editMarriagePlayer(&state, "alice", func(body *LegacyMonster) { body.Gold++ })
	if _, _, err := state.ApplyMarriage(proposal); !errors.Is(err, ErrMarriageStaleProposal) {
		t.Fatalf("err=%v", err)
	}
}
