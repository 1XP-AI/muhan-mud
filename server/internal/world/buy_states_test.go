package world

import (
	"errors"
	"testing"
)

func buyStatesState(class byte, experience, gold int32, inventory []LegacyObject) State {
	actor := LegacyMonster{
		Name: "초인", Type: 0, Class: class, RoomID: 1,
		Experience: experience, Gold: gold, HPMax: 100, MPMax: 80,
		Stats: [5]byte{10, 10, 10, 10, 10},
	}
	if inventory != nil {
		actor.Inventory = inventory
	}
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {Body: actor, Online: true},
		},
	}
}

func TestPlanApplyBuyStatesPromptsCaretakerWithoutArgument(t *testing.T) {
	s := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, nil)
	proposal, err := s.PlanBuyStates("actor", "")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != BuyStatesPrompt || proposal.Changed || proposal.Response != BuyStatesPromptResponse {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBuyStates(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != BuyStatesPrompt || result.Changed || result.Response != BuyStatesPromptResponse {
		t.Fatalf("result=%+v", result)
	}
	if next.Players["actor"].Body.Gold != s.Players["actor"].Body.Gold ||
		next.Players["actor"].Body.Experience != s.Players["actor"].Body.Experience ||
		next.Players["actor"].Body.HPMax != s.Players["actor"].Body.HPMax {
		t.Fatal("prompt mutated gold or stats")
	}
	if _, _, err := next.ApplyBuyStates(proposal); err != nil {
		t.Fatalf("idempotent prompt err=%v", err)
	}
}

func TestPlanBuyStatesRejectsNonCaretakerBeforePrompt(t *testing.T) {
	for _, class := range []byte{4, 9, 11, 12} {
		s := buyStatesState(class, 101000000, 2000000, nil)
		proposal, err := s.PlanBuyStates("actor", "")
		if err != nil {
			t.Fatalf("class %d err=%v", class, err)
		}
		if proposal.Action != BuyStatesNotCaretaker || proposal.Changed ||
			proposal.Response != BuyStatesNotCaretakerResponse {
			t.Fatalf("class %d proposal=%+v", class, proposal)
		}
		next, result, err := s.ApplyBuyStates(proposal)
		if err != nil {
			t.Fatal(err)
		}
		if result.Changed || result.Response != BuyStatesNotCaretakerResponse ||
			next.Players["actor"].Body.Gold != 2000000 {
			t.Fatalf("class %d result=%+v gold=%d", class, result, next.Players["actor"].Body.Gold)
		}
	}
}

func TestPlanBuyStatesExperienceAndGoldGatesBeforeApply(t *testing.T) {
	lowExp := buyStatesState(BuyStatesCaretakerClass, 100000000, 5000000, nil)
	proposal, err := lowExp.PlanBuyStates("actor", "체력")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != BuyStatesExperience || proposal.Changed ||
		proposal.Response != BuyStatesExperienceResponse || proposal.Amount >= 1 {
		t.Fatalf("low exp proposal=%+v", proposal)
	}

	lowGold := buyStatesState(BuyStatesCaretakerClass, 101000000, 999999, nil)
	proposal, err = lowGold.PlanBuyStates("actor", "도력")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != BuyStatesGold || proposal.Changed ||
		proposal.Response != BuyStatesGoldResponse || proposal.Amount != 1 || proposal.Cost != BuyStatesGoldUnit {
		t.Fatalf("low gold proposal=%+v", proposal)
	}
	next, result, err := lowGold.ApplyBuyStates(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || next.Players["actor"].Body.Gold != 999999 {
		t.Fatalf("gold gate mutated result=%+v", result)
	}
}

func TestPlanBuyStatesFailClosedOnKnownStatApply(t *testing.T) {
	s := buyStatesState(BuyStatesCaretakerClass, 102000000, 3000000, nil)
	for _, stat := range []string{"체력", "도력", "힘", "민첩", "맷집", "지식", "신앙심"} {
		if _, err := s.PlanBuyStates("actor", stat); !errors.Is(err, ErrBuyStatesApplyPending) {
			t.Fatalf("stat %q err=%v", stat, err)
		}
		if _, _, err := s.BuyStates("actor", stat); !errors.Is(err, ErrBuyStatesApplyPending) {
			t.Fatalf("BuyStates %q err=%v", stat, err)
		}
		if s.Players["actor"].Body.Gold != 3000000 || s.Players["actor"].Body.Experience != 102000000 {
			t.Fatal("fail-closed apply mutated the snapshot")
		}
	}
}

func TestPlanBuyStatesUnknownStatAfterGoldGate(t *testing.T) {
	s := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, nil)
	proposal, err := s.PlanBuyStates("actor", "마법")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Action != BuyStatesUnknownStat || proposal.Changed ||
		proposal.Response != BuyStatesUnknownStatResponse || proposal.Amount != 1 {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyBuyStates(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || next.Players["actor"].Body.HPMax != 100 {
		t.Fatalf("unknown stat mutated result=%+v", result)
	}
}

func TestPlanBuyStatesFailClosedWithoutCanonicalGoldAndStats(t *testing.T) {
	s := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, []LegacyObject{{Name: "금화", Value: 10}})
	if _, err := s.PlanBuyStates("actor", "체력"); !errors.Is(err, ErrBuyStatesGoldUnresolved) || !errors.Is(err, ErrBuyStatesStatsUnresolved) {
		t.Fatalf("nested inventory err=%v", err)
	}
	if _, err := s.PlanBuyStates("actor", ""); err != nil {
		t.Fatalf("prompt should not inspect gold err=%v", err)
	}
	if _, err := s.PlanBuyStates("", "체력"); !errors.Is(err, ErrBuyStatesActorAbsent) {
		t.Fatalf("empty actor err=%v", err)
	}
	offline := s.clone()
	actor := offline.Players["actor"]
	actor.Online = false
	actor.Body.Inventory = nil
	offline.Players["actor"] = actor
	room := offline.Rooms[1]
	room.PlayerIDs = nil
	offline.Rooms[1] = room
	if _, err := offline.PlanBuyStates("actor", ""); !errors.Is(err, ErrBuyStatesActorAbsent) {
		t.Fatalf("offline err=%v", err)
	}
}

func TestApplyBuyStatesRejectsTamperedPrompt(t *testing.T) {
	s := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, nil)
	proposal, err := s.PlanBuyStates("actor", "")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Response = "which?"
	if _, _, err := s.ApplyBuyStates(proposal); !errors.Is(err, ErrBuyStatesStaleProposal) {
		t.Fatalf("tampered response err=%v", err)
	}
	proposal, err = s.PlanBuyStates("actor", "")
	if err != nil {
		t.Fatal(err)
	}
	proposal.Changed = true
	if _, _, err := s.ApplyBuyStates(proposal); !errors.Is(err, ErrBuyStatesStaleProposal) && !errors.Is(err, ErrBuyStatesInvalidProposal) {
		t.Fatalf("changed prompt err=%v", err)
	}
}
