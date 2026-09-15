package world

import (
	"errors"
	"reflect"
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

func TestPlanApplyBuyStatesVitalityBranchesMatchCOrderAndReceiptEvidence(t *testing.T) {
	for _, tc := range []struct {
		stat          string
		wantHPMax     int16
		wantHPCurrent int16
		wantMPMax     int16
		wantMPCurrent int16
	}{
		{stat: "체력", wantHPMax: 105, wantHPCurrent: 105, wantMPMax: 80, wantMPCurrent: 29},
		{stat: "도력", wantHPMax: 100, wantHPCurrent: 37, wantMPMax: 85, wantMPCurrent: 85},
	} {
		s := buyStatesState(BuyStatesCaretakerClass, 102000000, 5000000, nil)
		actor := s.Players["actor"]
		actor.Body.HPMax, actor.Body.HPCurrent = 100, 37
		actor.Body.MPMax, actor.Body.MPCurrent = 80, 29
		actor.Body.DiceCount, actor.Body.DiceSides, actor.Body.DicePlus = 9, 9, 11
		s.Players["actor"] = actor

		rollCalls := 0
		proposal, err := s.PlanBuyStatesWithOptions("actor", tc.stat, BuyStatesOptions{Roll: func(low, high int) int {
			rollCalls++
			if low != 0 || high != 3 {
				t.Fatalf("C vitality roll bounds %d..%d", low, high)
			}
			return 0
		}})
		if err != nil {
			t.Fatalf("%s plan err=%v", tc.stat, err)
		}
		if rollCalls != 2 || len(proposal.Rolls) != 2 || len(proposal.GrowthRolls) != 2 || proposal.Amount != 2 || proposal.Cost != 2000000 || proposal.Gain != 5 {
			t.Fatalf("%s proposal=%+v calls=%d", tc.stat, proposal, rollCalls)
		}
		next, result, err := s.ApplyBuyStates(proposal)
		if err != nil {
			t.Fatalf("%s apply err=%v", tc.stat, err)
		}
		body := next.Players["actor"].Body
		if body.HPMax != tc.wantHPMax || body.HPCurrent != tc.wantHPCurrent || body.MPMax != tc.wantMPMax || body.MPCurrent != tc.wantMPCurrent || body.Experience != 100000000 || body.Gold != 3000000 {
			t.Fatalf("%s body=%+v", tc.stat, body)
		}
		if body.DiceCount != 4 || body.DiceSides != 4 || body.DicePlus != 4 {
			t.Fatalf("%s dice=%d/%d/%d", tc.stat, body.DiceCount, body.DiceSides, body.DicePlus)
		}
		if result.Action != BuyStatesApplied || !result.Changed || result.Response != BuyStatesSuccessResponse || result.Gain != 5 || !reflect.DeepEqual(result.Rolls, []int{0, 0}) || !reflect.DeepEqual(result.GrowthRolls, []int{0, 0}) {
			t.Fatalf("%s result=%+v", tc.stat, result)
		}
		if result.HPCurrentBefore != 37 || result.MPCurrentBefore != 29 || result.HPCurrentAfter != body.HPCurrent || result.MPCurrentAfter != body.MPCurrent {
			t.Fatalf("%s current evidence=%+v", tc.stat, result)
		}
	}
}

func TestPlanApplyBuyStatesBasicStatsUseOneRollAndBranchSpecificCosts(t *testing.T) {
	stats := []struct {
		name  string
		index int
		roll  int
	}{
		{name: "힘", index: 0, roll: 1},
		{name: "민첩", index: 1, roll: 0},
		{name: "맷집", index: 2, roll: 1},
		{name: "지식", index: 3, roll: 0},
		{name: "신앙심", index: 4, roll: 1},
	}
	for _, tc := range stats {
		s := buyStatesState(BuyStatesCaretakerClass, 104000000, 5000000, nil)
		actor := s.Players["actor"]
		actor.Body.HPMax, actor.Body.HPCurrent = 100, 41
		actor.Body.MPMax, actor.Body.MPCurrent = 80, 23
		actor.Body.Stats[tc.index] = 10
		s.Players["actor"] = actor
		calls := 0
		proposal, err := s.PlanBuyStatesWithOptions("actor", tc.name, BuyStatesOptions{Roll: func(low, high int) int {
			calls++
			if low != 0 || high != 1 {
				t.Fatalf("%s roll bounds %d..%d", tc.name, low, high)
			}
			return tc.roll
		}})
		if err != nil {
			t.Fatalf("%s plan err=%v", tc.name, err)
		}
		if calls != 1 || proposal.Amount != 4 || proposal.Cost != BuyStatesBasicGoldCost || proposal.ExperienceAfter != 102000000 || proposal.GoldAfter != 4000000 || proposal.StatRoll != tc.roll {
			t.Fatalf("%s proposal=%+v calls=%d", tc.name, proposal, calls)
		}
		next, result, err := s.ApplyBuyStates(proposal)
		if err != nil {
			t.Fatalf("%s apply err=%v", tc.name, err)
		}
		body := next.Players["actor"].Body
		if body.Experience != 102000000 || body.Gold != 4000000 || body.Stats[tc.index] != 11 || body.HPCurrent != 41 || body.MPCurrent != 23 {
			t.Fatalf("%s body=%+v", tc.name, body)
		}
		if tc.roll == 1 && (body.HPMax != 104 || body.MPMax != 80) {
			t.Fatalf("%s HP choice body=%+v", tc.name, body)
		}
		if tc.roll == 0 && (body.HPMax != 100 || body.MPMax != 83) {
			t.Fatalf("%s MP choice body=%+v", tc.name, body)
		}
		if body.DiceCount != 4 || body.DiceSides != 4 || body.DicePlus != 4 || result.Gain != int64(3+tc.roll) {
			t.Fatalf("%s result=%+v body=%+v", tc.name, result, body)
		}
		if !reflect.DeepEqual(result.Rolls, []int{tc.roll}) || result.StatRoll != tc.roll || result.Response != BuyStatesSuccessResponse {
			t.Fatalf("%s roll evidence=%+v", tc.name, result)
		}
	}
}

func TestBuyStatesCapLevelAndBasicStatBoundaryDoNotConsumeRNG(t *testing.T) {
	hpCap := buyStatesState(BuyStatesCaretakerClass, 102000000, 5000000, nil)
	actor := hpCap.Players["actor"]
	actor.Body.Level = 127
	actor.Body.HPMax = 2000
	hpCap.Players["actor"] = actor
	proposal, err := hpCap.PlanBuyStatesWithOptions("actor", "체력", BuyStatesOptions{Roll: func(int, int) int { t.Fatal("HP cap consumed RNG"); return 0 }})
	if err != nil || proposal.Action != BuyStatesVitalityCap || proposal.Changed || proposal.Response != BuyStatesHealthCapResponse {
		t.Fatalf("HP cap proposal=%+v err=%v", proposal, err)
	}
	next, result, err := hpCap.ApplyBuyStates(proposal)
	if err != nil || result.Changed || next.Players["actor"].Body.HPMax != 2000 {
		t.Fatalf("HP cap apply result=%+v err=%v", result, err)
	}

	mpCap := buyStatesState(BuyStatesCaretakerClass, 102000000, 5000000, nil)
	actor = mpCap.Players["actor"]
	actor.Body.Level = 127
	actor.Body.MPMax = 2000
	mpCap.Players["actor"] = actor
	proposal, err = mpCap.PlanBuyStatesWithOptions("actor", "도력", BuyStatesOptions{Roll: func(int, int) int {
		t.Fatal("MP cap consumed RNG")
		return 0
	}})
	if err != nil || proposal.Action != BuyStatesVitalityCap || proposal.Changed || proposal.Response != BuyStatesManaCapResponse {
		t.Fatalf("MP cap proposal=%+v err=%v", proposal, err)
	}
	next, result, err = mpCap.ApplyBuyStates(proposal)
	if err != nil || result.Changed || next.Players["actor"].Body.MPMax != 2000 {
		t.Fatalf("MP cap apply result=%+v err=%v", result, err)
	}

	levelBoundary := hpCap
	actor = levelBoundary.Players["actor"]
	actor.Body.Level = 128
	levelBoundary.Players["actor"] = actor
	if _, err := levelBoundary.PlanBuyStates("actor", "체력"); !errors.Is(err, ErrBuyStatesApplyPending) {
		t.Fatalf("level 128 nil roll err=%v", err)
	}
	if _, err := levelBoundary.PlanBuyStatesWithOptions("actor", "체력", BuyStatesOptions{Roll: func(low, high int) int {
		if low != 0 || high != 3 {
			t.Fatalf("level boundary bounds %d..%d", low, high)
		}
		return 0
	}}); err != nil {
		t.Fatalf("level 128 supplied roll err=%v", err)
	}

	statCap := buyStatesState(BuyStatesCaretakerClass, 102000000, 5000000, nil)
	actor = statCap.Players["actor"]
	actor.Body.Stats[2] = 45
	statCap.Players["actor"] = actor
	proposal, err = statCap.PlanBuyStatesWithOptions("actor", "맷집", BuyStatesOptions{Roll: func(int, int) int { t.Fatal("basic stat cap consumed RNG"); return 0 }})
	if err != nil || proposal.Action != BuyStatesStatCap || proposal.Changed || proposal.Response != BuyStatesStatCapResponse {
		t.Fatalf("basic cap proposal=%+v err=%v", proposal, err)
	}
	if _, result, err = statCap.ApplyBuyStates(proposal); err != nil || result.Changed {
		t.Fatalf("basic cap apply result=%+v err=%v", result, err)
	}

	stat44 := statCap
	actor = stat44.Players["actor"]
	actor.Body.Stats[2] = 44
	stat44.Players["actor"] = actor
	if _, err := stat44.PlanBuyStatesWithOptions("actor", "맷집", BuyStatesOptions{Roll: func(low, high int) int {
		if low != 0 || high != 1 {
			t.Fatalf("stat 44 bounds %d..%d", low, high)
		}
		return 0
	}}); err != nil {
		t.Fatalf("stat 44 should apply err=%v", err)
	}
}

func TestBuyStatesRejectedRollAndOverflowNeverMutateState(t *testing.T) {
	invalidRoll := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, nil)
	original := invalidRoll
	if _, err := invalidRoll.PlanBuyStatesWithOptions("actor", "체력", BuyStatesOptions{Roll: func(low, high int) int {
		if low != 0 || high != 3 {
			t.Fatalf("invalid roll bounds %d..%d", low, high)
		}
		return 4
	}}); !errors.Is(err, ErrBuyStatesRandom) {
		t.Fatalf("invalid roll err=%v", err)
	}
	if !reflect.DeepEqual(invalidRoll, original) {
		t.Fatal("invalid roll mutated source state")
	}

	overflow := buyStatesState(BuyStatesCaretakerClass, 101000000, 2000000, nil)
	actor := overflow.Players["actor"]
	actor.Body.Level = 128
	actor.Body.HPMax = 32767
	overflow.Players["actor"] = actor
	original = overflow
	if _, err := overflow.PlanBuyStatesWithOptions("actor", "체력", BuyStatesOptions{Roll: func(int, int) int { return 0 }}); !errors.Is(err, ErrBuyStatesNumeric) {
		t.Fatalf("HP overflow err=%v", err)
	}
	if !reflect.DeepEqual(overflow, original) {
		t.Fatal("HP overflow mutated source state")
	}

	basicOverflow := buyStatesState(BuyStatesCaretakerClass, 102000000, 2000000, nil)
	actor = basicOverflow.Players["actor"]
	actor.Body.HPMax = 32767
	basicOverflow.Players["actor"] = actor
	original = basicOverflow
	if _, err := basicOverflow.PlanBuyStatesWithOptions("actor", "힘", BuyStatesOptions{Roll: func(int, int) int { return 1 }}); !errors.Is(err, ErrBuyStatesNumeric) {
		t.Fatalf("basic HP overflow err=%v", err)
	}
	if !reflect.DeepEqual(basicOverflow, original) {
		t.Fatal("basic overflow mutated source state")
	}
}
