package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func itemMutationFixtureForSession() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a", "b"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem"}},
			"floor-gem": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag"}},
	}
	s.NPCs = nil
	return s
}

func TestExecuteItemMutationLineSupportsContainedRoots(t *testing.T) {
	state, err := jsonStateFromWorldFixture(itemMutationFixtureForSession())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "nested-1", lease, "꺼내 가방 보석")
	if err != nil || !strings.Contains(string(first.Response), "주웠습니다") || store.commits != 1 {
		t.Fatalf("take contained=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	// The player now owns the floor bag root only after explicitly taking it;
	// this mirrors the C path where a floor container can be selected for get
	// and then becomes a normal inventory root.
	second, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "nested-2", lease, "주워 가방")
	if err != nil || !strings.Contains(string(second.Response), "주웠습니다") || store.commits != 2 {
		t.Fatalf("take bag=%q err=%v commits=%d", second.Response, err, store.commits)
	}
	third, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "nested-3", lease, "넣어 보석 가방")
	if err != nil || !strings.Contains(string(third.Response), "버렸습니다") || store.commits != 3 {
		t.Fatalf("drop contained=%q err=%v commits=%d", third.Response, err, store.commits)
	}
}

func jsonStateFromWorldFixture(s world.State) ([]byte, error) { return json.Marshal(s) }

func TestExecuteItemMutationLinePersistsAndReplays(t *testing.T) {
	state, err := jsonStateFromWorldFixture(itemMutationFixtureForSession())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-1", lease, "주워 가방")
	if err != nil || !strings.Contains(string(first.Response), "주웠습니다") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	replay, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-1", lease, "주워 가방")
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteItemMutationLineSupportsAllRoots(t *testing.T) {
	state, err := jsonStateFromWorldFixture(itemMutationFixtureForSession())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "all-1", lease, "주워 모두")
	if err != nil || !strings.Contains(string(first.Response), "주웠습니다") || store.commits != 1 {
		t.Fatalf("take all=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	second, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "all-2", lease, "버려 모두")
	if err != nil || !strings.Contains(string(second.Response), "버렸습니다") || store.commits != 2 {
		t.Fatalf("drop all=%q err=%v commits=%d", second.Response, err, store.commits)
	}
}

func TestParseItemMutationLineSupportsCanonicalContainerForms(t *testing.T) {
	take, ok := parseItemMutationLine("꺼내 가방 보석")
	if !ok || take.verb != "take" || !take.nested || take.container != "가방" || take.name != "보석" || take.occurrence != 1 {
		t.Fatalf("take=%+v ok=%v", take, ok)
	}
	drop, ok := parseItemMutationLine("넣어 보석 가방")
	if !ok || drop.verb != "drop" || !drop.nested || drop.container != "가방" || drop.name != "보석" {
		t.Fatalf("drop=%+v ok=%v", drop, ok)
	}
	for _, line := range []string{"꺼내 가방 보석 2", "넣어 모두 가방"} {
		if _, ok := parseItemMutationLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}
