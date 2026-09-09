package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func compareCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	p.Items = &world.ItemCollection{Items: map[string]world.Item{
		"sword-1":      {Object: world.LegacyObject{Name: "검", Type: 0, DiceCount: 3, DiceSides: 10, DicePlus: 2}},
		"sword-2":      {Object: world.LegacyObject{Name: "검", Type: 0, DiceCount: 1, DiceSides: 1, DicePlus: 1}},
		"potion":       {Object: world.LegacyObject{Name: "물약", Type: 6}},
		"nested":       {Object: world.LegacyObject{Name: "가방", Type: 9}, Contents: []string{"nested-child"}},
		"nested-child": {Object: world.LegacyObject{Name: "검", Type: 0}},
	}, Inventory: []string{"sword-1", "sword-2", "potion", "nested"}}
	s.Players["a"] = p
	r := s.Rooms[1]
	r.Items = nil
	s.Rooms[1] = r
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitCompareOwner(t *testing.T) (*Ownership, SessionLease) {
	t.Helper()
	owners := &Ownership{}
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return owners, lease
}

func TestParseCompareLineAdmitsExactAliasAndPositiveOccurrence(t *testing.T) {
	for _, line := range []string{"비교 검", "비교 검 2", `비교 "긴 검" 3`, "비교"} {
		command, ok := ParseCompareLine(line)
		if !ok || command.Alias != "비교" || command.Occurrence < 1 {
			t.Fatalf("line=%q command=%+v ok=%t", line, command, ok)
		}
	}
	if command, ok := ParseCompareLine("비교 검 2"); !ok || command.Name != "검" || command.Occurrence != 2 {
		t.Fatalf("parsed=%+v ok=%t", command, ok)
	}
	for _, line := range []string{"감정 검", "compare 검", "비교", "비교 검 0", "비교 검 -1", "비교 검 x", "비교 검 1 extra", "비교\n검"} {
		if line == "비교" {
			continue
		}
		if _, ok := ParseCompareLine(line); ok {
			t.Fatalf("unsupported compare line accepted: %q", line)
		}
	}
}

func TestExecuteCompareLinePersistsTypedReadOnlyResultAndReplays(t *testing.T) {
	store := &departureStore{state: compareCommandFixture(t)}
	owners, lease := admitCompareOwner(t)
	before := append([]byte(nil), store.state...)

	first, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-1", lease, "비교 검 2")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.CompareResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "compare" || !result.Success || result.Kind != world.CompareWeapon || result.ItemID != "sword-2" || result.Occurrence != 2 || result.CheckDamage != -5 {
		t.Fatalf("result=%+v", result)
	}
	if result.RequiredLevel != 0 || !result.Universal || !strings.Contains(result.Response, "누구나 무장") {
		t.Fatalf("result=%+v", result)
	}
	if string(store.state) != string(before) {
		t.Fatal("compare changed the world snapshot")
	}

	replay, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-1", lease, "비교 검 2")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteCompareLinePersistsSourceFailureResponsesAndBarePrompt(t *testing.T) {
	store := &departureStore{state: compareCommandFixture(t)}
	owners, lease := admitCompareOwner(t)
	missing, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-missing", lease, "비교 없음")
	if err != nil || store.commits != 1 {
		t.Fatalf("missing=%+v err=%v commits=%d", missing, err, store.commits)
	}
	var missingResult world.CompareResult
	if err := json.Unmarshal(missing.Response, &missingResult); err != nil || missingResult.Success || missingResult.ErrorCode != world.CompareItemNotFound || !strings.Contains(missingResult.Response, "갖고 있지 않습니다") {
		t.Fatalf("missing result=%+v err=%v", missingResult, err)
	}

	unsupported, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-unsupported", lease, "비교 물약")
	if err != nil || store.commits != 2 {
		t.Fatalf("unsupported=%+v err=%v commits=%d", unsupported, err, store.commits)
	}
	var unsupportedResult world.CompareResult
	if err := json.Unmarshal(unsupported.Response, &unsupportedResult); err != nil || unsupportedResult.Success || unsupportedResult.ErrorCode != world.CompareUnsupportedItem || !strings.Contains(unsupportedResult.Response, "무기나 방어구") {
		t.Fatalf("unsupported result=%+v err=%v", unsupportedResult, err)
	}

	prompt, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-prompt", lease, "비교")
	if err != nil || store.commits != 3 {
		t.Fatalf("prompt=%+v err=%v commits=%d", prompt, err, store.commits)
	}
	var promptResult world.CompareResult
	if err := json.Unmarshal(prompt.Response, &promptResult); err != nil || promptResult.ErrorCode != world.CompareMissingItem || !strings.Contains(promptResult.Response, "무엇을 비교") {
		t.Fatalf("prompt result=%+v err=%v", promptResult, err)
	}

	if _, err := owners.ExecuteCompareLine(context.Background(), store, "w", "compare-bad", lease, "감정 검"); err == nil || store.commits != 3 {
		t.Fatalf("malformed line committed: err=%v commits=%d", err, store.commits)
	}
}
