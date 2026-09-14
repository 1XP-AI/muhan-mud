package world

import (
	"errors"
	"testing"
)

func suicideState() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Gold: 50}, Online: true},
		},
	}
}

func TestPlanApplySuicideStartsPasswordPrompt(t *testing.T) {
	s := suicideState()
	proposal, err := s.PlanSuicide("actor")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != SuicidePrompt || !proposal.Changed || proposal.Continuation != SuicidePasswordPhase ||
		proposal.Response != SuicidePromptResponse || proposal.RoomID != 1 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if suicideFlag(proposal.BeforeFlags, SuicideReadingFlag) || !suicideFlag(proposal.AfterFlags, SuicideReadingFlag) {
		t.Fatalf("select flags before=%+v after=%+v want PREADI", proposal.BeforeFlags, proposal.AfterFlags)
	}
	next, result, err := s.ApplySuicide(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != SuicidePrompt || !result.Changed || result.Continuation != SuicidePasswordPhase ||
		result.Response != SuicidePromptResponse || !result.Reading {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if !PlayerFlagSet(body, SuicideReadingFlag) || body.Gold != 50 || body.Name != "Alice" {
		t.Fatalf("applied body=%+v", body)
	}
	if _, ok := next.Players["actor"]; !ok {
		t.Fatal("apply deleted the actor")
	}
	if PlayerFlagSet(s.Players["actor"].Body, SuicideReadingFlag) {
		t.Fatal("plan/apply mutated the original snapshot")
	}
	if _, _, err := next.ApplySuicide(proposal); !errors.Is(err, ErrSuicideStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	again, result, err := next.Suicide("actor")
	if err != nil || result.Action != SuicidePrompt || !result.Changed || again.Players["actor"].Body.Flags != body.Flags {
		t.Fatalf("idempotent start result=%+v err=%v", result, err)
	}
	if !PlayerFlagSet(again.Players["actor"].Body, SuicideReadingFlag) {
		t.Fatalf("second start flags=%+v", again.Players["actor"].Body.Flags)
	}
	if len(again.Players) != len(s.Players) {
		t.Fatalf("player count changed: %d", len(again.Players))
	}
}

func TestPlanSuicideFailClosedWithoutCanonicalActor(t *testing.T) {
	s := suicideState()
	delete(s.Rooms, 1)
	if _, err := s.PlanSuicide("actor"); !errors.Is(err, ErrSuicideActorAbsent) {
		t.Fatalf("unmigrated err=%v", err)
	}
	s = suicideState()
	if _, err := s.PlanSuicide(""); !errors.Is(err, ErrSuicideActorAbsent) {
		t.Fatalf("empty actor err=%v", err)
	}
	offline := s.clone()
	actor := offline.Players["actor"]
	actor.Online = false
	offline.Players["actor"] = actor
	room := offline.Rooms[1]
	room.PlayerIDs = nil
	offline.Rooms[1] = room
	if _, err := offline.PlanSuicide("actor"); !errors.Is(err, ErrSuicideActorAbsent) {
		t.Fatalf("offline err=%v", err)
	}
	padded := suicideState()
	actor = padded.Players["actor"]
	actor.Body.Name = "Alice "
	padded.Players["actor"] = actor
	if _, err := padded.PlanSuicide("actor"); !errors.Is(err, ErrSuicideNameInvalid) {
		t.Fatalf("padded name err=%v", err)
	}
}

func TestApplySuicideRejectsTamperedPromptAndNeverDeletes(t *testing.T) {
	s := suicideState()
	proposal, err := s.PlanSuicide("actor")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "delete?"
	if _, _, err := s.ApplySuicide(proposal); !errors.Is(err, ErrSuicideStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	if _, _, err := s.ApplySuicide(SuicideProposal{ActorID: "actor", Action: SuicidePrompt, Changed: true}); !errors.Is(err, ErrSuicideStaleProposal) && !errors.Is(err, ErrSuicideInvalidProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
	if _, ok := s.Players["actor"]; !ok {
		t.Fatal("reject path deleted the actor")
	}
}

func TestPlanApplySuicidePasswordRejectsMismatchAndClearsReading(t *testing.T) {
	started, _, err := suicideState().Suicide("actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := started.PlanSuicidePassword("actor", false); err != nil {
		t.Fatal(err)
	}
	proposal, err := started.PlanSuicidePassword("actor", false)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != SuicideReject || !proposal.Changed || proposal.Continuation != 0 ||
		proposal.PasswordMatched || proposal.Response != SuicideRejectResponse {
		t.Fatalf("proposal=%+v", proposal)
	}
	if !suicideFlag(proposal.BeforeFlags, SuicideReadingFlag) || suicideFlag(proposal.AfterFlags, SuicideReadingFlag) {
		t.Fatalf("select flags before=%+v after=%+v want PREADI cleared", proposal.BeforeFlags, proposal.AfterFlags)
	}
	next, result, err := started.ApplySuicidePassword(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != SuicideReject || !result.Changed || result.Continuation != 0 ||
		result.Response != SuicideRejectResponse || result.Reading {
		t.Fatalf("result=%+v", result)
	}
	body := next.Players["actor"].Body
	if PlayerFlagSet(body, SuicideReadingFlag) || body.Gold != 50 || body.Name != "Alice" {
		t.Fatalf("applied body=%+v", body)
	}
	if _, ok := next.Players["actor"]; !ok {
		t.Fatal("mismatch apply deleted the actor")
	}
	if !PlayerFlagSet(started.Players["actor"].Body, SuicideReadingFlag) {
		t.Fatal("password apply mutated the original snapshot")
	}
	if _, _, err := next.ApplySuicidePassword(proposal); !errors.Is(err, ErrSuicideNotReading) && !errors.Is(err, ErrSuicideStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	if _, err := next.PlanSuicidePassword("actor", false); !errors.Is(err, ErrSuicideNotReading) {
		t.Fatalf("cleared reading err=%v", err)
	}
}

func TestPlanSuicidePasswordMatchFailsClosedWithoutConfirmOrDelete(t *testing.T) {
	started, _, err := suicideState().Suicide("actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := started.PlanSuicidePassword("actor", true); !errors.Is(err, ErrSuicideConfirmUnmigrated) {
		t.Fatalf("match err=%v", err)
	}
	if _, _, err := started.SuicidePassword("actor", true); !errors.Is(err, ErrSuicideConfirmUnmigrated) {
		t.Fatalf("convenience match err=%v", err)
	}
	body := started.Players["actor"].Body
	if !PlayerFlagSet(body, SuicideReadingFlag) || body.Gold != 50 {
		t.Fatalf("match mutated body=%+v", body)
	}
	if _, ok := started.Players["actor"]; !ok {
		t.Fatal("match path deleted the actor")
	}
}

func TestPlanSuicidePasswordRequiresReadingFlag(t *testing.T) {
	s := suicideState()
	if _, err := s.PlanSuicidePassword("actor", false); !errors.Is(err, ErrSuicideNotReading) {
		t.Fatalf("unarmed err=%v", err)
	}
	if _, err := s.PlanSuicidePassword("", false); !errors.Is(err, ErrSuicideActorAbsent) {
		t.Fatalf("empty actor err=%v", err)
	}
}

func TestApplySuicidePasswordRejectsTamperedMismatchAndNeverDeletes(t *testing.T) {
	started, _, err := suicideState().Suicide("actor")
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := started.PlanSuicidePassword("actor", false)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "deleted"
	if _, _, err := started.ApplySuicidePassword(proposal); !errors.Is(err, ErrSuicideStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	if _, _, err := started.ApplySuicidePassword(SuicideProposal{ActorID: "actor", Action: SuicideReject, Changed: true}); !errors.Is(err, ErrSuicideStaleProposal) && !errors.Is(err, ErrSuicideInvalidProposal) {
		t.Fatalf("empty proposal err=%v", err)
	}
	if _, ok := started.Players["actor"]; !ok {
		t.Fatal("reject path deleted the actor")
	}
	if started.Players["actor"].Body.Gold != 50 {
		t.Fatal("tamper path mutated gold")
	}
}
