package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
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

func lastTokenItemMutationFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.Items = &world.ItemCollection{Items: map[string]world.Item{
		"floor-sword": {Object: world.LegacyObject{Name: "검"}},
	}, Inventory: []string{"floor-sword"}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Players["a"] = player
	return s
}

func collectionHasInventoryName(c *world.ItemCollection, name string) bool {
	if c == nil {
		return false
	}
	for _, id := range c.Inventory {
		if c.Items[id].Object.Name == name {
			return true
		}
	}
	return false
}

func TestParseItemMutationLineAdmitsPrefixAndLastTokenForms(t *testing.T) {
	prefixTake, ok := parseItemMutationLine("주워 검")
	if !ok || prefixTake.verb != "take" || prefixTake.name != "검" || prefixTake.occurrence != 1 || prefixTake.all || prefixTake.nested {
		t.Fatalf("prefix take=%+v ok=%v", prefixTake, ok)
	}
	suffixTake, ok := parseItemMutationLine("검 주워")
	if !ok || suffixTake.verb != "take" || suffixTake.name != "검" || suffixTake.occurrence != 1 || suffixTake.all || suffixTake.nested {
		t.Fatalf("suffix take=%+v ok=%v", suffixTake, ok)
	}
	for _, line := range []string{"검 주", "검 가져", "검 꺼내"} {
		got, ok := parseItemMutationLine(line)
		if !ok || got.verb != "take" || got.name != "검" {
			t.Fatalf("%q=%+v ok=%v", line, got, ok)
		}
	}
	prefixDrop, ok := parseItemMutationLine("버려 검")
	if !ok || prefixDrop.verb != "drop" || prefixDrop.name != "검" || prefixDrop.occurrence != 1 {
		t.Fatalf("prefix drop=%+v ok=%v", prefixDrop, ok)
	}
	suffixDrop, ok := parseItemMutationLine("검 버려")
	if !ok || suffixDrop.verb != "drop" || suffixDrop.name != "검" || suffixDrop.occurrence != 1 {
		t.Fatalf("suffix drop=%+v ok=%v", suffixDrop, ok)
	}
	put, ok := parseItemMutationLine("검 넣어")
	if !ok || put.verb != "drop" || put.name != "검" {
		t.Fatalf("suffix put=%+v ok=%v", put, ok)
	}
	allTake, ok := parseItemMutationLine("모두 주워")
	if !ok || allTake.verb != "take" || !allTake.all || allTake.name != "모두" {
		t.Fatalf("suffix all take=%+v ok=%v", allTake, ok)
	}
	allDrop, ok := parseItemMutationLine("모두 버려")
	if !ok || allDrop.verb != "drop" || !allDrop.all {
		t.Fatalf("suffix all drop=%+v ok=%v", allDrop, ok)
	}
	nestedTake, ok := parseItemMutationLine("가방 보석 꺼내")
	if !ok || nestedTake.verb != "take" || !nestedTake.nested || nestedTake.container != "가방" || nestedTake.name != "보석" {
		t.Fatalf("suffix nested take=%+v ok=%v", nestedTake, ok)
	}
	nestedDrop, ok := parseItemMutationLine("보석 가방 넣어")
	if !ok || nestedDrop.verb != "drop" || !nestedDrop.nested || nestedDrop.container != "가방" || nestedDrop.name != "보석" {
		t.Fatalf("suffix nested drop=%+v ok=%v", nestedDrop, ok)
	}
	occ, ok := parseItemMutationLine("검 2 버려")
	if !ok || occ.verb != "drop" || occ.name != "검" || occ.occurrence != 2 {
		t.Fatalf("suffix occ=%+v ok=%v", occ, ok)
	}
	both, ok := parseItemMutationLine("주워 버려")
	if !ok || both.verb != "drop" || both.name != "주워" {
		t.Fatalf("last-token wins=%+v ok=%v", both, ok)
	}
	extraDrop, ok := parseItemMutationLine("동 junk extra 버려")
	if !ok || extraDrop.verb != "drop" || extraDrop.name != "동" || !extraDrop.nested || extraDrop.container != "junk" || extraDrop.occurrence != 1 {
		t.Fatalf("extra last-token drop=%+v ok=%v", extraDrop, ok)
	}
	extraTake, ok := parseItemMutationLine("북 foo extra 주워")
	if !ok || extraTake.verb != "take" || extraTake.name != "foo" || !extraTake.nested || extraTake.container != "북" {
		t.Fatalf("extra last-token take=%+v ok=%v", extraTake, ok)
	}
	fourTake, ok := parseItemMutationLine("검 junk extra 주워")
	if !ok || fourTake.verb != "take" || !fourTake.nested || fourTake.container != "검" || fourTake.name != "junk" || fourTake.occurrence != 1 {
		t.Fatalf("four-token last-token take=%+v ok=%v", fourTake, ok)
	}
	occNested, ok := parseItemMutationLine("검 2 extra 버려")
	if !ok || occNested.verb != "drop" || !occNested.nested || occNested.name != "검" || occNested.occurrence != 2 || occNested.container != "extra" {
		t.Fatalf("occ nested last-token drop=%+v ok=%v", occNested, ok)
	}
	cardinalDrop, ok := parseItemMutationLine("동 버려")
	if !ok || cardinalDrop.verb != "drop" || cardinalDrop.name != "동" || cardinalDrop.nested {
		t.Fatalf("cardinal last-token drop=%+v ok=%v", cardinalDrop, ok)
	}
	for _, line := range []string{"", "검", "검 버려 extra", "동 버려 extra", "주워 검 extra junk", "검\n버려", "꺼내 가방 보석 2"} {
		if _, ok := parseItemMutationLine(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
}

func executeParsedItemMutationLine(t *testing.T, owners *Ownership, store *departureStore, lease SessionLease, commandID, line string) storage.WorldReceipt {
	t.Helper()
	parsed, err := ParseCommand(line)
	if err != nil {
		t.Fatalf("ParseCommand(%q) err=%v", line, err)
	}
	if parsed.Kind != CommandItemMutation {
		t.Fatalf("ParseCommand(%q)=%+v want CommandItemMutation", line, parsed)
	}
	receipt, execErr := owners.ExecuteItemMutationLine(context.Background(), store, "w", commandID, lease, line)
	if execErr != nil {
		t.Fatalf("ExecuteItemMutationLine(%q) err=%v", line, execErr)
	}
	return receipt
}

func extraTokenItemMutationFixture() world.State {
	s, err := world.DecodeState(directionalCardinalCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.Items = &world.ItemCollection{Items: map[string]world.Item{
		"floor-sword": {Object: world.LegacyObject{Name: "검"}},
	}, Inventory: []string{"floor-sword"}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Players["a"] = player
	return s
}

func executeParsedItemMutationOrDirectional(t *testing.T, owners *Ownership, store *departureStore, lease SessionLease, commandID, line string) (storage.WorldReceipt, error) {
	t.Helper()
	parsed, err := ParseCommand(line)
	if err != nil {
		t.Fatalf("ParseCommand(%q) err=%v", line, err)
	}
	switch parsed.Kind {
	case CommandItemMutation:
		return owners.ExecuteItemMutationLine(context.Background(), store, "w", commandID, lease, line)
	case CommandDirectional:
		receipt, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", commandID, lease, line, 100, 12, world.SceneOptions{}, nil, nil, nil)
		if execErr != nil {
			return receipt, execErr
		}
		t.Fatalf("ParseCommand(%q)=CommandDirectional", line)
		return receipt, execErr
	case CommandUnknown:
		return storage.WorldReceipt{}, ErrUnsupportedItemMutationLine
	default:
		t.Fatalf("ParseCommand(%q)=%+v want CommandItemMutation, CommandUnknown, or not CommandDirectional", line, parsed)
		return storage.WorldReceipt{}, ErrUnsupportedItemMutationLine
	}
}

func TestExecuteItemMutationLineLastTokenExtraTokensWithoutMoving(t *testing.T) {
	state, err := jsonStateFromWorldFixture(extraTokenItemMutationFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		line string
		id   string
		kind CommandKind
	}{
		{"동 버려", "item-east-drop", CommandItemMutation},
		{"동 junk 버려", "item-east-junk-drop", CommandItemMutation},
		{"동 junk extra 버려", "item-east-extra-drop", CommandItemMutation},
		{"북 foo extra 주워", "item-north-extra-take", CommandItemMutation},
	} {
		parsed, parseErr := ParseCommand(tt.line)
		if parseErr != nil || parsed.Kind != tt.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want kind=%d", tt.line, parsed, parseErr, tt.kind)
		}
		receipt, execErr := executeParsedItemMutationOrDirectional(t, &owners, store, lease, tt.id, tt.line)
		if parsed.Kind == CommandItemMutation && execErr == nil && store.commits != 0 {
			t.Fatalf("%q committed missing cardinal item: %+v commits=%d", tt.line, receipt, store.commits)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("%q moved actor: %+v err=%v exec=%v", tt.line, saved.Players["a"], decodeErr, execErr)
		}
		if !collectionHasInventoryName(saved.Rooms[1].Items, "검") {
			t.Fatalf("%q mutated floor sword: room=%+v", tt.line, saved.Rooms[1].Items)
		}
	}

	if _, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-extra-take", lease, "검 junk extra 주워"); err == nil || store.commits != 0 {
		t.Fatalf("four-token last-token floor-took 검 err=%v commits=%d", err, store.commits)
	}
	untouched, err := world.DecodeState(store.state)
	if err != nil || untouched.Players["a"].Body.RoomID != 1 {
		t.Fatalf("four-token last-token moved actor: %+v err=%v", untouched.Players["a"], err)
	}
	if !collectionHasInventoryName(untouched.Rooms[1].Items, "검") || collectionHasInventoryName(untouched.Players["a"].Items, "검") {
		t.Fatalf("four-token last-token mutated floor sword room=%+v player=%+v", untouched.Rooms[1].Items, untouched.Players["a"].Items)
	}
}

func TestExecuteItemMutationLineLastTokenFourPlusTokensNestedAndReplay(t *testing.T) {
	state, err := jsonStateFromWorldFixture(itemMutationFixtureForSession())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	parsed, parseErr := ParseCommand("가방 보석 extra 꺼내")
	if parseErr != nil || parsed.Kind != CommandItemMutation {
		t.Fatalf("ParseCommand four-token nested=%+v err=%v", parsed, parseErr)
	}
	action, ok := parseItemMutationLine("가방 보석 extra 꺼내")
	if !ok || action.verb != "take" || !action.nested || action.container != "가방" || action.name != "보석" {
		t.Fatalf("parse four-token nested=%+v ok=%v", action, ok)
	}

	first := executeParsedItemMutationLine(t, &owners, store, lease, "item-nested-extra-1", "가방 보석 extra 꺼내")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") || !strings.Contains(string(first.Response), "보석") {
		t.Fatalf("nested extra take=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	taken, err := world.DecodeState(store.state)
	if err != nil || taken.Players["a"].Body.RoomID != 1 {
		t.Fatalf("nested extra take moved actor: %+v err=%v", taken.Players["a"], err)
	}
	if !collectionHasInventoryName(taken.Players["a"].Items, "보석") || collectionHasInventoryName(taken.Rooms[1].Items, "보석") {
		t.Fatalf("nested extra take locations room=%+v player=%+v", taken.Rooms[1].Items, taken.Players["a"].Items)
	}
	replay := executeParsedItemMutationLine(t, &owners, store, lease, "item-nested-extra-1", "가방 보석 extra 꺼내")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("nested extra replay=%+v commits=%d", replay, store.commits)
	}

	if _, execErr := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-nested-extra-drop", lease, "보석 extra junk 버려"); execErr == nil || store.commits != 1 {
		t.Fatalf("four-token last-token floor-dropped 보석 err=%v commits=%d", execErr, store.commits)
	}
	held, err := world.DecodeState(store.state)
	if err != nil || !collectionHasInventoryName(held.Players["a"].Items, "보석") || collectionHasInventoryName(held.Rooms[1].Items, "보석") {
		t.Fatalf("four-token last-token mutated gem room=%+v player=%+v err=%v", held.Rooms[1].Items, held.Players["a"].Items, err)
	}
	if _, execErr := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-occ-nested-drop", lease, "검 2 extra 버려"); execErr == nil || store.commits != 1 {
		t.Fatalf("occ nested drop committed floor item err=%v commits=%d", execErr, store.commits)
	}
}

func TestParseCommandItemMutationVerbInMiddleDoesNotExecuteDirectionalOrMove(t *testing.T) {
	state, err := jsonStateFromWorldFixture(extraTokenItemMutationFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line string
		id   string
	}{
		{"동 버려 extra", "item-mid-east"},
		{"북 주워 junk", "item-mid-north"},
	} {
		parsed, parseErr := ParseCommand(tt.line)
		if parseErr != nil || parsed.Kind == CommandDirectional {
			t.Fatalf("ParseCommand(%q)=%+v err=%v want fail-closed unknown or item mutation", tt.line, parsed, parseErr)
		}
		if parsed.Kind != CommandUnknown && parsed.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v want CommandUnknown or CommandItemMutation", tt.line, parsed)
		}
		_, execErr := executeParsedItemMutationOrDirectional(t, &owners, store, lease, tt.id, tt.line)
		if parsed.Kind == CommandItemMutation && execErr == nil && store.commits != 0 {
			t.Fatalf("mid-verb %q committed: commits=%d", tt.line, store.commits)
		}
		saved, decodeErr := world.DecodeState(store.state)
		if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
			t.Fatalf("mid-verb %q moved actor: %+v err=%v exec=%v", tt.line, saved.Players["a"], decodeErr, execErr)
		}
	}
}

func TestExecuteItemMutationLineLastTokenTakeAndDropWithoutMoving(t *testing.T) {
	state, err := jsonStateFromWorldFixture(lastTokenItemMutationFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first := executeParsedItemMutationLine(t, &owners, store, lease, "item-take-suffix", "검 주워")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") {
		t.Fatalf("take=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	taken, err := world.DecodeState(store.state)
	if err != nil || taken.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take moved actor: %+v err=%v", taken.Players["a"], err)
	}
	if collectionHasInventoryName(taken.Rooms[1].Items, "검") || !collectionHasInventoryName(taken.Players["a"].Items, "검") {
		t.Fatalf("take locations room=%+v player=%+v", taken.Rooms[1].Items, taken.Players["a"].Items)
	}

	second := executeParsedItemMutationLine(t, &owners, store, lease, "item-drop-suffix", "검 버려")
	if second.Replayed || store.commits != 2 || !strings.Contains(string(second.Response), "버렸습니다") || !strings.Contains(string(second.Response), "검") {
		t.Fatalf("drop=%q commits=%d replayed=%t", second.Response, store.commits, second.Replayed)
	}
	dropped, err := world.DecodeState(store.state)
	if err != nil || dropped.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop moved actor: %+v err=%v", dropped.Players["a"], err)
	}
	if !collectionHasInventoryName(dropped.Rooms[1].Items, "검") || collectionHasInventoryName(dropped.Players["a"].Items, "검") {
		t.Fatalf("drop locations room=%+v player=%+v", dropped.Rooms[1].Items, dropped.Players["a"].Items)
	}
}

func TestExecuteItemMutationLineLastTokenReplaysWithoutRecommit(t *testing.T) {
	state, err := jsonStateFromWorldFixture(lastTokenItemMutationFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first := executeParsedItemMutationLine(t, &owners, store, lease, "item-suffix-1", "검 주워")
	if first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v commits=%d", first, store.commits)
	}
	replay := executeParsedItemMutationLine(t, &owners, store, lease, "item-suffix-1", "검 주워")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v commits=%d", replay, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 || !collectionHasInventoryName(saved.Players["a"].Items, "검") {
		t.Fatalf("replay mutated=%+v err=%v", saved.Players["a"], err)
	}
}

func TestExecuteItemMutationLineLastTokenFailsClosedForMissingAndUnmigrated(t *testing.T) {
	state, err := jsonStateFromWorldFixture(lastTokenItemMutationFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCommand("없는검 주워")
	if err != nil || parsed.Kind != CommandItemMutation {
		t.Fatalf("ParseCommand missing=%+v err=%v", parsed, err)
	}
	if _, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "missing-1", lease, "없는검 주워"); err == nil || store.commits != 0 {
		t.Fatalf("missing item committed err=%v commits=%d", err, store.commits)
	}
	missing, err := world.DecodeState(store.state)
	if err != nil || missing.Players["a"].Body.RoomID != 1 || !collectionHasInventoryName(missing.Rooms[1].Items, "검") {
		t.Fatalf("missing mutated room=%+v actor=%+v err=%v", missing.Rooms[1].Items, missing.Players["a"], err)
	}

	unmigrated := lastTokenItemMutationFixture()
	player := unmigrated.Players["a"]
	player.Items = nil
	unmigrated.Players["a"] = player
	raw, err := jsonStateFromWorldFixture(unmigrated)
	if err != nil {
		t.Fatal(err)
	}
	unmigratedStore := &departureStore{state: raw}
	if _, err := owners.ExecuteItemMutationLine(context.Background(), unmigratedStore, "w", "unmigrated-player", lease, "검 주워"); err == nil || unmigratedStore.commits != 0 {
		t.Fatalf("unmigrated player committed err=%v commits=%d", err, unmigratedStore.commits)
	}

	roomUnmigrated := lastTokenItemMutationFixture()
	room := roomUnmigrated.Rooms[1]
	room.Items = nil
	roomUnmigrated.Rooms[1] = room
	raw, err = jsonStateFromWorldFixture(roomUnmigrated)
	if err != nil {
		t.Fatal(err)
	}
	roomStore := &departureStore{state: raw}
	if _, err := owners.ExecuteItemMutationLine(context.Background(), roomStore, "w", "unmigrated-room", lease, "검 주워"); err == nil || roomStore.commits != 0 {
		t.Fatalf("unmigrated room committed err=%v commits=%d", err, roomStore.commits)
	}
	saved, err := world.DecodeState(roomStore.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("unmigrated room moved actor=%+v err=%v", saved.Players["a"], err)
	}
}

func collectionHasInventoryID(c *world.ItemCollection, id string) bool {
	if c == nil {
		return false
	}
	for _, got := range c.Inventory {
		if got == id {
			return true
		}
	}
	return false
}

func collectionHasContainedID(c *world.ItemCollection, containerID, childID string) bool {
	if c == nil {
		return false
	}
	item, ok := c.Items[containerID]
	if !ok {
		return false
	}
	for _, got := range item.Contents {
		if got == childID {
			return true
		}
	}
	return false
}

func twoGemBagFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a", "b"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 2}, Contents: []string{"floor-gem-1", "floor-gem-2"}},
			"floor-gem-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag"}},
	}
	s.NPCs = nil
	return s
}

func twoBagFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a", "b"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag-1": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-1"}},
			"floor-bag-2": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 1}, Contents: []string{"floor-gem-2"}},
			"floor-gem-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag-1", "floor-bag-2"}},
	}
	s.NPCs = nil
	return s
}

func twoBagDropFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"bag-1": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 0}},
		"bag-2": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 2, ShotsCurrent: 0}},
		"gem":   {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"bag-1", "bag-2", "gem"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func twoBagThreeGemFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	s.Rooms[1] = world.RoomState{
		Resource:  s.Rooms[1].Resource,
		PlayerIDs: []string{"a", "b"},
		Items: &world.ItemCollection{Items: map[string]world.Item{
			"floor-bag-1":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 1}, Contents: []string{"floor-gem-1-1"}},
			"floor-bag-2":   {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 3}, Contents: []string{"floor-gem-2-1", "floor-gem-2-2", "floor-gem-2-3"}},
			"floor-gem-1-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-1": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-2": {Object: world.LegacyObject{Name: "보석"}},
			"floor-gem-2-3": {Object: world.LegacyObject{Name: "보석"}},
		}, Inventory: []string{"floor-bag-1", "floor-bag-2"}},
	}
	s.NPCs = nil
	return s
}

func threeGemTwoBagDropFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"bag-1": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 0}},
		"bag-2": {Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: 0}},
		"gem-1": {Object: world.LegacyObject{Name: "보석"}},
		"gem-2": {Object: world.LegacyObject{Name: "보석"}},
		"gem-3": {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"bag-1", "bag-2", "gem-1", "gem-2", "gem-3"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func admitItemMutation(t *testing.T, s world.State) (*Ownership, *departureStore, SessionLease) {
	t.Helper()
	state, err := jsonStateFromWorldFixture(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: state}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &owners, store, lease
}

func TestParseItemMutationLineLastTokenAppliesCVal2AndTakeContainerOccurrence(t *testing.T) {
	for _, line := range []string{"가방 보석 2 꺼내", "가방 보석 2 주워"} {
		got, ok := parseItemMutationLine(line)
		if !ok || got.verb != "take" || !got.nested || got.container != "가방" || got.name != "보석" || got.occurrence != 2 || got.containerOccurrence != 1 {
			t.Fatalf("%q=%+v ok=%v want take 보석#2 from 가방#1", line, got, ok)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	fromSecond, ok := parseItemMutationLine("가방 2 보석 꺼내")
	if !ok || fromSecond.verb != "take" || !fromSecond.nested || fromSecond.container != "가방" || fromSecond.name != "보석" || fromSecond.occurrence != 1 || fromSecond.containerOccurrence != 2 {
		t.Fatalf("take case 3=%+v ok=%v want take 보석#1 from 가방#2", fromSecond, ok)
	}
	drop, ok := parseItemMutationLine("보석 가방 2 버려")
	if !ok || drop.verb != "drop" || !drop.nested || drop.container != "가방" || drop.name != "보석" || drop.occurrence != 1 || drop.containerOccurrence != 2 {
		t.Fatalf("drop val[2]=%+v ok=%v want drop 보석#1 into 가방#2", drop, ok)
	}
	itemOccDrop, ok := parseItemMutationLine("보석 2 가방 버려")
	if !ok || itemOccDrop.verb != "drop" || !itemOccDrop.nested || itemOccDrop.container != "가방" || itemOccDrop.name != "보석" || itemOccDrop.occurrence != 2 || itemOccDrop.containerOccurrence != 1 {
		t.Fatalf("drop val[1]=%+v ok=%v want drop 보석#2 into 가방#1", itemOccDrop, ok)
	}
}

func TestExecuteItemMutationLineTakesNthContainedItemAndReplays(t *testing.T) {
	for _, line := range []string{"가방 보석 2 꺼내", "가방 보석 2 주워"} {
		owners, store, lease := admitItemMutation(t, twoGemBagFixture())
		first := executeParsedItemMutationLine(t, owners, store, lease, "item-val2-take-1", line)
		if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") || !strings.Contains(string(first.Response), "보석") {
			t.Fatalf("%q take=%q commits=%d replayed=%t", line, first.Response, store.commits, first.Replayed)
		}
		taken, err := world.DecodeState(store.state)
		if err != nil || taken.Players["a"].Body.RoomID != 1 {
			t.Fatalf("%q moved actor=%+v err=%v", line, taken.Players["a"], err)
		}
		if collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-1") || !collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-2") {
			t.Fatalf("%q took first gem player=%+v", line, taken.Players["a"].Items)
		}
		if !collectionHasContainedID(taken.Rooms[1].Items, "floor-bag", "floor-gem-1") || collectionHasContainedID(taken.Rooms[1].Items, "floor-bag", "floor-gem-2") {
			t.Fatalf("%q bag contents=%+v", line, taken.Rooms[1].Items.Items["floor-bag"])
		}
		replay := executeParsedItemMutationLine(t, owners, store, lease, "item-val2-take-1", line)
		if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
			t.Fatalf("%q replay=%+v commits=%d", line, replay, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || collectionHasInventoryID(replayed.Players["a"].Items, "floor-gem-1") || !collectionHasInventoryID(replayed.Players["a"].Items, "floor-gem-2") {
			t.Fatalf("%q replay mutated player=%+v err=%v", line, replayed.Players["a"].Items, err)
		}
	}
}

func TestExecuteItemMutationLineTakesFromNthContainerAndReplays(t *testing.T) {
	owners, store, lease := admitItemMutation(t, twoBagFixture())
	first := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-container-1", "가방 2 보석 꺼내")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") {
		t.Fatalf("take from 2nd bag=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	taken, err := world.DecodeState(store.state)
	if err != nil || taken.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take from 2nd bag moved actor=%+v err=%v", taken.Players["a"], err)
	}
	if collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-1") || !collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-2") {
		t.Fatalf("took from first bag player=%+v", taken.Players["a"].Items)
	}
	if !collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-1", "floor-gem-1") || collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-2", "floor-gem-2") {
		t.Fatalf("2nd bag still held gem room=%+v", taken.Rooms[1].Items)
	}
	replay := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-container-1", "가방 2 보석 꺼내")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("take from 2nd bag replay=%+v commits=%d", replay, store.commits)
	}
}

func TestExecuteItemMutationLineDropsIntoNthContainerAndReplays(t *testing.T) {
	owners, store, lease := admitItemMutation(t, twoBagDropFixture())
	first := executeParsedItemMutationLine(t, owners, store, lease, "item-val2-drop-1", "보석 가방 2 버려")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "버렸습니다") {
		t.Fatalf("drop into 2nd bag=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	dropped, err := world.DecodeState(store.state)
	if err != nil || dropped.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop into 2nd bag moved actor=%+v err=%v", dropped.Players["a"], err)
	}
	if collectionHasInventoryID(dropped.Players["a"].Items, "gem") || collectionHasContainedID(dropped.Players["a"].Items, "bag-1", "gem") || !collectionHasContainedID(dropped.Players["a"].Items, "bag-2", "gem") {
		t.Fatalf("dropped into first bag player=%+v", dropped.Players["a"].Items)
	}
	replay := executeParsedItemMutationLine(t, owners, store, lease, "item-val2-drop-1", "보석 가방 2 버려")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("drop into 2nd bag replay=%+v commits=%d", replay, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || collectionHasContainedID(replayed.Players["a"].Items, "bag-1", "gem") || !collectionHasContainedID(replayed.Players["a"].Items, "bag-2", "gem") {
		t.Fatalf("drop replay mutated player=%+v err=%v", replayed.Players["a"].Items, err)
	}
}

func TestParseItemMutationLineSimultaneousContainerAndItemOccurrence(t *testing.T) {
	for _, line := range []string{"가방 2 보석 3 꺼내", "가방 2 보석 3 주워"} {
		got, ok := parseItemMutationLine(line)
		if !ok || got.verb != "take" || !got.nested || got.container != "가방" || got.name != "보석" || got.occurrence != 3 || got.containerOccurrence != 2 {
			t.Fatalf("%q=%+v ok=%v want take 보석#3 from 가방#2", line, got, ok)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandItemMutation {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	drop, ok := parseItemMutationLine("보석 3 가방 2 버려")
	if !ok || drop.verb != "drop" || !drop.nested || drop.container != "가방" || drop.name != "보석" || drop.occurrence != 3 || drop.containerOccurrence != 2 {
		t.Fatalf("drop val[1]+val[2]=%+v ok=%v want drop 보석#3 into 가방#2", drop, ok)
	}
	extra, ok := parseItemMutationLine("가방 2 보석 3 extra 꺼내")
	if !ok || extra.verb != "take" || !extra.nested || extra.container != "가방" || extra.name != "보석" || extra.occurrence != 3 || extra.containerOccurrence != 2 {
		t.Fatalf("extra after both vals=%+v ok=%v want take 보석#3 from 가방#2", extra, ok)
	}
}

func TestExecuteItemMutationLineTakesNthItemFromNthContainerAndReplays(t *testing.T) {
	for _, line := range []string{"가방 2 보석 3 꺼내", "가방 2 보석 3 주워"} {
		owners, store, lease := admitItemMutation(t, twoBagThreeGemFixture())
		first := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-val2-take-1", line)
		if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") || !strings.Contains(string(first.Response), "보석") {
			t.Fatalf("%q take=%q commits=%d replayed=%t", line, first.Response, store.commits, first.Replayed)
		}
		taken, err := world.DecodeState(store.state)
		if err != nil || taken.Players["a"].Body.RoomID != 1 {
			t.Fatalf("%q moved actor=%+v err=%v", line, taken.Players["a"], err)
		}
		if collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-1-1") || collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-2-1") || collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-2-2") || !collectionHasInventoryID(taken.Players["a"].Items, "floor-gem-2-3") {
			t.Fatalf("%q took wrong gem player=%+v", line, taken.Players["a"].Items)
		}
		if !collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-1", "floor-gem-1-1") || !collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-2", "floor-gem-2-1") || !collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-2", "floor-gem-2-2") || collectionHasContainedID(taken.Rooms[1].Items, "floor-bag-2", "floor-gem-2-3") {
			t.Fatalf("%q bag contents=%+v", line, taken.Rooms[1].Items)
		}
		replay := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-val2-take-1", line)
		if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
			t.Fatalf("%q replay=%+v commits=%d", line, replay, store.commits)
		}
		replayed, err := world.DecodeState(store.state)
		if err != nil || collectionHasInventoryID(replayed.Players["a"].Items, "floor-gem-2-1") || !collectionHasInventoryID(replayed.Players["a"].Items, "floor-gem-2-3") {
			t.Fatalf("%q replay mutated player=%+v err=%v", line, replayed.Players["a"].Items, err)
		}
	}
}

func TestExecuteItemMutationLineDropsNthItemIntoNthContainerAndReplays(t *testing.T) {
	owners, store, lease := admitItemMutation(t, threeGemTwoBagDropFixture())
	first := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-val2-drop-1", "보석 3 가방 2 버려")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "버렸습니다") {
		t.Fatalf("drop 보석#3 into 가방#2=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	dropped, err := world.DecodeState(store.state)
	if err != nil || dropped.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop 보석#3 into 가방#2 moved actor=%+v err=%v", dropped.Players["a"], err)
	}
	if !collectionHasInventoryID(dropped.Players["a"].Items, "gem-1") || !collectionHasInventoryID(dropped.Players["a"].Items, "gem-2") || collectionHasInventoryID(dropped.Players["a"].Items, "gem-3") {
		t.Fatalf("dropped wrong gem inventory=%+v", dropped.Players["a"].Items)
	}
	if collectionHasContainedID(dropped.Players["a"].Items, "bag-1", "gem-3") || !collectionHasContainedID(dropped.Players["a"].Items, "bag-2", "gem-3") {
		t.Fatalf("dropped into wrong bag player=%+v", dropped.Players["a"].Items)
	}
	replay := executeParsedItemMutationLine(t, owners, store, lease, "item-val1-val2-drop-1", "보석 3 가방 2 버려")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("drop 보석#3 into 가방#2 replay=%+v commits=%d", replay, store.commits)
	}
	replayed, err := world.DecodeState(store.state)
	if err != nil || collectionHasContainedID(replayed.Players["a"].Items, "bag-1", "gem-3") || !collectionHasContainedID(replayed.Players["a"].Items, "bag-2", "gem-3") || collectionHasInventoryID(replayed.Players["a"].Items, "gem-3") {
		t.Fatalf("drop replay mutated player=%+v err=%v", replayed.Players["a"].Items, err)
	}
}

func sessionContainerBag(contents []string) world.Item {
	return world.Item{Object: world.LegacyObject{Name: "가방", Flags: [8]byte{0: 1 << 6}, ShotsMax: 3, ShotsCurrent: int16(len(contents))}, Contents: append([]string(nil), contents...)}
}

func inventoryAndWornBagTakeFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"inv-bag":    sessionContainerBag([]string{"inv-gem-1", "inv-gem-2", "inv-gem-3"}),
		"worn-bag":   sessionContainerBag([]string{"worn-gem-1", "worn-gem-2", "worn-gem-3"}),
		"inv-gem-1":  {Object: world.LegacyObject{Name: "보석"}},
		"inv-gem-2":  {Object: world.LegacyObject{Name: "보석"}},
		"inv-gem-3":  {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-1": {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-2": {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-3": {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"inv-bag"}, Ready: [20]string{0: "worn-bag"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func inventoryAndWornBagDropFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"inv-bag":  sessionContainerBag(nil),
		"worn-bag": sessionContainerBag(nil),
		"gem-1":    {Object: world.LegacyObject{Name: "보석"}},
		"gem-2":    {Object: world.LegacyObject{Name: "보석"}},
		"gem-3":    {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"inv-bag", "gem-1", "gem-2", "gem-3"}, Ready: [20]string{0: "worn-bag"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func twoWornBagTakeFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"worn-bag-1":   sessionContainerBag([]string{"worn-gem-1-1"}),
		"worn-bag-2":   sessionContainerBag([]string{"worn-gem-2-1", "worn-gem-2-2", "worn-gem-2-3"}),
		"worn-gem-1-1": {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-2-1": {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-2-2": {Object: world.LegacyObject{Name: "보석"}},
		"worn-gem-2-3": {Object: world.LegacyObject{Name: "보석"}},
	}, Ready: [20]string{0: "worn-bag-1", 1: "worn-bag-2"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func twoWornBagDropFixture() world.State {
	s, err := world.DecodeState(followCommandFixture())
	if err != nil {
		panic(err)
	}
	room := s.Rooms[1]
	room.NPCIDs = nil
	room.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = room
	player := s.Players["a"]
	player.Items = &world.ItemCollection{Items: map[string]world.Item{
		"worn-bag-1": sessionContainerBag(nil),
		"worn-bag-2": sessionContainerBag(nil),
		"gem-1":      {Object: world.LegacyObject{Name: "보석"}},
		"gem-2":      {Object: world.LegacyObject{Name: "보석"}},
		"gem-3":      {Object: world.LegacyObject{Name: "보석"}},
	}, Inventory: []string{"gem-1", "gem-2", "gem-3"}, Ready: [20]string{0: "worn-bag-1", 1: "worn-bag-2"}}
	s.Players["a"] = player
	s.NPCs = nil
	return s
}

func TestExecuteItemMutationLineDoesNotCountWornAsInventoryOccurrence(t *testing.T) {
	owners, store, lease := admitItemMutation(t, inventoryAndWornBagTakeFixture())
	if _, err := owners.ExecuteItemMutationLine(context.Background(), store, "w", "item-worn-occ-take-miss", lease, "가방 2 보석 3 꺼내"); err == nil || store.commits != 0 {
		t.Fatalf("took worn as inventory #2 err=%v commits=%d", err, store.commits)
	}
	taken, err := world.DecodeState(store.state)
	if err != nil || taken.Players["a"].Body.RoomID != 1 || collectionHasInventoryID(taken.Players["a"].Items, "worn-gem-3") || collectionHasInventoryID(taken.Players["a"].Items, "inv-gem-3") {
		t.Fatalf("miss mutated player=%+v err=%v", taken.Players["a"].Items, err)
	}
	dropOwners, dropStore, dropLease := admitItemMutation(t, inventoryAndWornBagDropFixture())
	if _, err := dropOwners.ExecuteItemMutationLine(context.Background(), dropStore, "w", "item-worn-occ-drop-miss", dropLease, "보석 3 가방 2 버려"); err == nil || dropStore.commits != 0 {
		t.Fatalf("dropped into worn as inventory #2 err=%v commits=%d", err, dropStore.commits)
	}
}

func TestExecuteItemMutationLineTakesFromNthWornContainerAndReplays(t *testing.T) {
	owners, store, lease := admitItemMutation(t, twoWornBagTakeFixture())
	first := executeParsedItemMutationLine(t, owners, store, lease, "item-worn-val1-val2-take-1", "가방 2 보석 3 꺼내")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "주웠습니다") || !strings.Contains(string(first.Response), "보석") {
		t.Fatalf("take from 2nd worn=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	taken, err := world.DecodeState(store.state)
	if err != nil || taken.Players["a"].Body.RoomID != 1 {
		t.Fatalf("take from 2nd worn moved actor=%+v err=%v", taken.Players["a"], err)
	}
	if collectionHasInventoryID(taken.Players["a"].Items, "worn-gem-1-1") || collectionHasInventoryID(taken.Players["a"].Items, "worn-gem-2-1") || collectionHasInventoryID(taken.Players["a"].Items, "worn-gem-2-2") || !collectionHasInventoryID(taken.Players["a"].Items, "worn-gem-2-3") {
		t.Fatalf("took wrong worn gem player=%+v", taken.Players["a"].Items)
	}
	if !collectionHasContainedID(taken.Players["a"].Items, "worn-bag-1", "worn-gem-1-1") || collectionHasContainedID(taken.Players["a"].Items, "worn-bag-2", "worn-gem-2-3") {
		t.Fatalf("worn bags after take=%+v", taken.Players["a"].Items)
	}
	replay := executeParsedItemMutationLine(t, owners, store, lease, "item-worn-val1-val2-take-1", "가방 2 보석 3 꺼내")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("worn take replay=%+v commits=%d", replay, store.commits)
	}
}

func TestExecuteItemMutationLineDropsIntoNthWornContainerAndReplays(t *testing.T) {
	owners, store, lease := admitItemMutation(t, twoWornBagDropFixture())
	first := executeParsedItemMutationLine(t, owners, store, lease, "item-worn-val1-val2-drop-1", "보석 3 가방 2 버려")
	if first.Replayed || store.commits != 1 || !strings.Contains(string(first.Response), "버렸습니다") {
		t.Fatalf("drop into 2nd worn=%q commits=%d replayed=%t", first.Response, store.commits, first.Replayed)
	}
	dropped, err := world.DecodeState(store.state)
	if err != nil || dropped.Players["a"].Body.RoomID != 1 {
		t.Fatalf("drop into 2nd worn moved actor=%+v err=%v", dropped.Players["a"], err)
	}
	if !collectionHasInventoryID(dropped.Players["a"].Items, "gem-1") || !collectionHasInventoryID(dropped.Players["a"].Items, "gem-2") || collectionHasInventoryID(dropped.Players["a"].Items, "gem-3") {
		t.Fatalf("dropped wrong gem inventory=%+v", dropped.Players["a"].Items)
	}
	if collectionHasContainedID(dropped.Players["a"].Items, "worn-bag-1", "gem-3") || !collectionHasContainedID(dropped.Players["a"].Items, "worn-bag-2", "gem-3") {
		t.Fatalf("dropped into first worn bag player=%+v", dropped.Players["a"].Items)
	}
	replay := executeParsedItemMutationLine(t, owners, store, lease, "item-worn-val1-val2-drop-1", "보석 3 가방 2 버려")
	if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("worn drop replay=%+v commits=%d", replay, store.commits)
	}
}
