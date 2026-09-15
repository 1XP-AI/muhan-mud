package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func sessionBuyStatesFixture(t *testing.T, class byte, experience, gold int32, inventory []world.LegacyObject) []byte {
	t.Helper()
	actor := world.LegacyMonster{
		Name: "초인", Type: 0, Class: class, RoomID: 1,
		Experience: experience, Gold: gold, HPMax: 100, MPMax: 80,
		Stats: [5]byte{10, 10, 10, 10, 10},
	}
	if inventory != nil {
		actor.Inventory = inventory
	}
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: actor, Online: true},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitBuyStatesOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseBuyStatesLineAdmitsSuffixPrompt(t *testing.T) {
	command, ok := ParseBuyStatesLine("  향상  ")
	if !ok || command.Stat != "" || !IsBuyStatesLine("향상") {
		t.Fatalf("command=%+v ok=%t", command, ok)
	}
	named, ok := ParseBuyStatesLine("체력 향상")
	if !ok || named.Stat != "체력" {
		t.Fatalf("named=%+v ok=%t", named, ok)
	}
	quoted, ok := ParseBuyStatesLine(`"도력" 향상`)
	if !ok || quoted.Stat != "도력" {
		t.Fatalf("quoted=%+v ok=%t", quoted, ok)
	}
	for _, line := range []string{
		"향상 체력", "체력 2 향상", "buy_states", "향상\n", "향상\x00", string([]byte{0xff}),
	} {
		if _, ok := ParseBuyStatesLine(line); ok || IsBuyStatesLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesBuyStates(t *testing.T) {
	for _, line := range []string{"향상", "체력 향상", "도력 향상"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandBuyStates {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	for _, line := range []string{"향상 체력", "체력 2 향상"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteBuyStatesLinePromptsAndReplaysWithoutRecommit(t *testing.T) {
	initial := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 101000000, 2000000, nil)
	store := &departureStore{state: initial}
	owners, lease := admitBuyStatesOwner(t)
	first, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-1", lease, "향상")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.BuyStatesResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.BuyStatesPrompt || result.Changed || result.Response != world.BuyStatesPromptResponse {
		t.Fatalf("result=%+v", result)
	}
	if !bytes.Equal(store.state, initial) {
		t.Fatal("prompt mutated committed state")
	}
	replay, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-1", lease, "향상")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBuyStatesLineNonCaretakerAndGoldGateDoNotMutate(t *testing.T) {
	initial := sessionBuyStatesFixture(t, 4, 101000000, 2000000, nil)
	store := &departureStore{state: initial}
	owners, lease := admitBuyStatesOwner(t)
	first, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-fighter", lease, "향상")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.BuyStatesResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.BuyStatesNotCaretaker || result.Response != world.BuyStatesNotCaretakerResponse {
		t.Fatalf("result=%+v", result)
	}

	lowGold := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 101000000, 1, nil)
	store = &departureStore{state: lowGold}
	first, err = owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-gold", lease, "체력 향상")
	if err != nil || store.commits != 1 {
		t.Fatalf("gold first=%+v err=%v commits=%d", first, err, store.commits)
	}
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.BuyStatesGold || result.Changed || result.Response != world.BuyStatesGoldResponse {
		t.Fatalf("gold result=%+v", result)
	}
	replay, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-gold", lease, "체력 향상")
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("gold replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteBuyStatesLineRejectsUnsupportedAndUnmigratedBeforeReceipt(t *testing.T) {
	initial := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 101000000, 2000000, nil)
	store := &departureStore{state: initial}
	owners, lease := admitBuyStatesOwner(t)
	if _, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-bad", lease, "향상 체력"); !errors.Is(err, ErrUnsupportedBuyStatesLine) || store.commits != 0 {
		t.Fatalf("prefix err=%v commits=%d", err, store.commits)
	}
	unmigrated := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 101000000, 2000000, []world.LegacyObject{{Name: "금화"}})
	store = &departureStore{state: unmigrated}
	if _, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-unmigrated", lease, "체력 향상"); !errors.Is(err, world.ErrBuyStatesGoldUnresolved) || !errors.Is(err, world.ErrBuyStatesStatsUnresolved) || store.commits != 0 {
		t.Fatalf("unmigrated err=%v commits=%d", err, store.commits)
	}
	store = &departureStore{state: initial}
	if _, err := owners.ExecuteBuyStatesLine(context.Background(), store, "w", "buy-states-apply", lease, "체력 향상"); !errors.Is(err, world.ErrBuyStatesApplyPending) || store.commits != 0 {
		t.Fatalf("apply pending err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteBuyStatesLineWithOptionsPersistsRollsAndReplaysWithoutSecondRoll(t *testing.T) {
	initial := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 102000000, 5000000, nil)
	state, err := world.DecodeState(initial)
	if err != nil {
		t.Fatal(err)
	}
	actor := state.Players["actor"]
	actor.Body.HPMax, actor.Body.HPCurrent = 100, 42
	actor.Body.MPMax, actor.Body.MPCurrent = 80, 21
	state.Players["actor"] = actor
	initial, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: initial}
	owners, lease := admitBuyStatesOwner(t)
	calls := 0
	first, err := owners.ExecuteBuyStatesLineWithOptions(context.Background(), store, "w", "buy-states-apply", lease, "체력 향상", BuyStatesOptions{Roll: func(low, high int) int {
		calls++
		if low != 0 || high != 3 {
			t.Fatalf("vitality roll bounds %d..%d", low, high)
		}
		return []int{1, 3}[calls-1]
	}})
	if err != nil || first.Replayed || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.BuyStatesResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.BuyStatesApplied || !result.Changed || result.Amount != 2 || result.Cost != 2000000 || result.Gain != 9 || result.ExperienceAfter != 100000000 || result.GoldAfter != 3000000 {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Rolls) != 2 || len(result.GrowthRolls) != 2 || result.Rolls[0] != 1 || result.Rolls[1] != 3 {
		t.Fatalf("result roll evidence=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.HPMax != 109 || saved.Players["actor"].Body.HPCurrent != 109 || saved.Players["actor"].Body.MPCurrent != 21 {
		t.Fatalf("saved=%+v", saved.Players["actor"].Body)
	}
	replay, err := owners.ExecuteBuyStatesLineWithOptions(context.Background(), store, "w", "buy-states-apply", lease, "체력 향상", BuyStatesOptions{Roll: func(int, int) int {
		t.Fatal("buy_states replay rerolled")
		return 0
	}})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) || calls != 2 {
		t.Fatalf("replay=%+v err=%v commits=%d calls=%d", replay, err, store.commits, calls)
	}
}

func TestExecuteBuyStatesLineWithOptionsRejectsInvalidRollBeforeReceipt(t *testing.T) {
	initial := sessionBuyStatesFixture(t, world.BuyStatesCaretakerClass, 101000000, 2000000, nil)
	store := &departureStore{state: initial}
	owners, lease := admitBuyStatesOwner(t)
	if _, err := owners.ExecuteBuyStatesLineWithOptions(context.Background(), store, "w", "buy-states-invalid-roll", lease, "도력 향상", BuyStatesOptions{Roll: func(low, high int) int {
		if low != 0 || high != 3 {
			t.Fatalf("invalid roll bounds %d..%d", low, high)
		}
		return 9
	}}); !errors.Is(err, world.ErrBuyStatesRandom) || store.commits != 0 {
		t.Fatalf("invalid roll err=%v commits=%d", err, store.commits)
	}
	if string(store.state) != string(initial) {
		t.Fatal("invalid roll changed stored state")
	}
}
