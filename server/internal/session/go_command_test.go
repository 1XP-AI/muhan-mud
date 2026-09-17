package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func goCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "출발지",
					Exits: []world.LegacyExit{{Name: "동굴", Destination: 2}},
				}},
				PlayerIDs: []string{"actor"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "도착지"}},
				Items:    &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {
				Body:   world.LegacyMonster{Name: "Alice", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
				Online: true,
				Items:  &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseGoLineAdmitsPrefixAndSuffixForms(t *testing.T) {
	bare, ok := ParseGoLine("  가  ")
	if !ok || bare.Verb != "가" || bare.ExitName != "" || bare.Occurrence != 1 || !IsGoLine("들어가") {
		t.Fatalf("bare=%+v ok=%t", bare, ok)
	}
	prefix, ok := ParseGoLine("가 동굴")
	if !ok || prefix.Verb != "가" || prefix.ExitName != "동굴" || prefix.Occurrence != 1 {
		t.Fatalf("prefix=%+v ok=%t", prefix, ok)
	}
	enter, ok := ParseGoLine("들어가 동굴")
	if !ok || enter.Verb != "들어가" || enter.ExitName != "동굴" {
		t.Fatalf("enter=%+v ok=%t", enter, ok)
	}
	suffix, ok := ParseGoLine("동굴 가")
	if !ok || suffix.Verb != "가" || suffix.ExitName != "동굴" {
		t.Fatalf("suffix=%+v ok=%t", suffix, ok)
	}
	occ, ok := ParseGoLine("동굴 2 들어가")
	if !ok || occ.ExitName != "동굴" || occ.Occurrence != 2 || occ.Verb != "들어가" {
		t.Fatalf("suffix occ=%+v ok=%t", occ, ok)
	}
	prefixOcc, ok := ParseGoLine(`가 "비밀 통로" 2`)
	if !ok || prefixOcc.ExitName != "비밀 통로" || prefixOcc.Occurrence != 2 {
		t.Fatalf("prefix occ=%+v ok=%t", prefixOcc, ok)
	}
	both, ok := ParseGoLine("들어가 가")
	if !ok || both.Verb != "가" || both.ExitName != "들어가" {
		t.Fatalf("last-token wins=%+v ok=%t", both, ok)
	}
	for _, line := range []string{
		"가 동굴 2 extra", "가\n동굴", "동굴 0 가", "동굴 -1 가", string([]byte{0xff}),
	} {
		if _, ok := ParseGoLine(line); ok || IsGoLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesGo(t *testing.T) {
	for _, line := range []string{"가", "들어가", "가 동굴", "들어가 동굴", "동굴 가", "동굴 2 들어가"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandGo {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	parsed, err := ParseCommand("북")
	if err != nil || parsed.Kind != CommandDirectional {
		t.Fatalf("cardinal=%+v err=%v", parsed, err)
	}
}

func TestExecuteGoLineCommitsCanonicalMoveAndReplays(t *testing.T) {
	store := &departureStore{state: goCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-1", lease, "가 동굴", 100, 12, nil, nil, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "도착지") {
		t.Fatalf("response=%q", text)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["actor"].Body.RoomID != 2 {
		t.Fatalf("saved=%+v err=%v", saved.Players["actor"], err)
	}
	replay, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-1", lease, "가 동굴", 100, 12, nil, nil, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	suffix, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-suffix", lease, "동굴 가", 100, 12, nil, nil, nil)
	if err != nil || suffix.Replayed {
		t.Fatalf("suffix from dest=%+v err=%v", suffix, err)
	}
	var suffixText string
	if err := json.Unmarshal(suffix.Response, &suffixText); err != nil {
		t.Fatal(err)
	}
	if suffixText != world.GoMissingResponse {
		t.Fatalf("no exit in dest=%q", suffixText)
	}
	if _, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-bad", lease, "봐 동굴", 100, 12, nil, nil, nil); !errors.Is(err, ErrUnsupportedGoLine) {
		t.Fatalf("unsupported err=%v", err)
	}
}

func TestExecuteGoLineIncludesLeaderArrivalTrapActorTextAndEphemeralEvent(t *testing.T) {
	s, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	destination := s.Rooms[2]
	destination.Resource.Trap = world.TrapDart
	s.Rooms[2] = destination
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-trap-output", lease, "가 동굴", 100, 12, nil, func(low, high int) int {
		calls++
		if calls == 1 && (low != 1 || high != 100) {
			t.Fatalf("unexpected trigger roll %d..%d", low, high)
		}
		if calls == 2 && (low != 1 || high != 10) {
			t.Fatalf("unexpected dart roll %d..%d", low, high)
		}
		return []int{100, 7}[calls-1]
	}, nil)
	if err != nil || first.Replayed || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var response string
	if err := json.Unmarshal(first.Response, &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response, "당신은 숨겨진 독화살에 맞았습니다!\n") || first.ArrivalTrapEvent == nil || first.ArrivalTrapEvent.RoomID != 2 || first.ArrivalTrapEvent.Trap != world.TrapDart {
		t.Fatalf("response=%q event=%+v", response, first.ArrivalTrapEvent)
	}
	if encoded, encodeErr := json.Marshal(first); encodeErr != nil || strings.Contains(string(encoded), "ArrivalTrapEvent") || strings.Contains(string(encoded), "ActorText") {
		t.Fatalf("arrival trap metadata leaked into receipt JSON: %s err=%v", encoded, encodeErr)
	}
	replay, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-trap-output", lease, "가 동굴", 100, 12, nil, func(int, int) int {
		t.Fatal("arrival trap replay rerolled")
		return 0
	}, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || replay.ArrivalTrapEvent != nil {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteGoLineCarriesFollowerArrivalTrapEventsAndSuppressesReplay(t *testing.T) {
	s, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	leader := s.Players["actor"]
	leader.FollowerIDs = []string{"follower"}
	leader.Body.Stats[1] = 1
	s.Players["actor"] = leader
	s.Players["follower"] = world.PlayerState{
		Body:        world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 30, HPCurrent: 30, Stats: [5]byte{10, 1, 10, 10, 10}},
		Online:      true,
		FollowingID: "actor",
		Items:       &world.ItemCollection{Items: map[string]world.Item{}},
	}
	source := s.Rooms[1]
	source.PlayerIDs = []string{"actor", "follower"}
	s.Rooms[1] = source
	destination := s.Rooms[2]
	destination.Resource.Trap = world.TrapDart
	s.Rooms[2] = destination
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	rolls := []int{100, 7, 100, 8}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-follower-trap", lease, "가 동굴", 100, 12, nil, func(low, high int) int {
		if low != 1 || (high != 100 && high != 10) {
			t.Fatalf("unexpected trap roll %d..%d", low, high)
		}
		value := rolls[0]
		rolls = rolls[1:]
		return value
	}, nil)
	if err != nil || first.Replayed || store.commits != 1 || len(rolls) != 0 {
		t.Fatalf("first=%+v err=%v commits=%d rolls=%v", first, err, store.commits, rolls)
	}
	if len(first.FollowerArrivalTrapEvents) != 1 || first.FollowerArrivalTrapEvents[0].ActorID != "follower" || first.FollowerArrivalTrapEvents[0].ActorName != "Bob" || first.FollowerArrivalTrapEvents[0].RoomID != 2 || first.FollowerArrivalTrapEvents[0].Trap != world.TrapDart {
		t.Fatalf("missing follower trap event=%+v", first.FollowerArrivalTrapEvents)
	}
	var response string
	if err := json.Unmarshal(first.Response, &response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response, "8점의 피해") || strings.Contains(response, "7점의 피해") {
		t.Fatalf("leader response included follower output=%q", response)
	}
	if encoded, encodeErr := json.Marshal(first); encodeErr != nil || strings.Contains(string(encoded), "FollowerArrivalTrapEvents") || strings.Contains(string(encoded), "ActorText") || strings.Contains(string(encoded), "RoomText") {
		t.Fatalf("follower trap metadata leaked into receipt JSON: %s err=%v", encoded, encodeErr)
	}
	replay, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-follower-trap", lease, "가 동굴", 100, 12, nil, func(int, int) int {
		t.Fatal("follower arrival trap replay rerolled")
		return 0
	}, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || replay.FollowerArrivalTrapEvents != nil {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteGoLineGatesMissingLockSilentAndCombat(t *testing.T) {
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	missingStore := &departureStore{state: goCommandFixture(t)}
	missing, err := owners.ExecuteGoLine(context.Background(), missingStore, "w", "go-missing", lease, "가 없는문", 100, 12, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var missingText string
	if err := json.Unmarshal(missing.Response, &missingText); err != nil || missingText != world.GoMissingResponse || missingStore.commits != 1 {
		t.Fatalf("missing=%q err=%v commits=%d", missingText, err, missingStore.commits)
	}

	lockedState, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	room := lockedState.Rooms[1]
	room.Resource.Exits[0].Flags[2/8] |= 1 << (2 % 8)
	lockedState.Rooms[1] = room
	lockedRaw, err := json.Marshal(lockedState)
	if err != nil {
		t.Fatal(err)
	}
	lockedStore := &departureStore{state: lockedRaw}
	locked, err := owners.ExecuteGoLine(context.Background(), lockedStore, "w", "go-lock", lease, "들어가 동굴", 100, 12, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var lockedText string
	if err := json.Unmarshal(locked.Response, &lockedText); err != nil || lockedText != world.GoLockedResponse {
		t.Fatalf("locked=%q err=%v", lockedText, err)
	}
	lockedSaved, err := world.DecodeState(lockedStore.state)
	if err != nil || lockedSaved.Players["actor"].Body.RoomID != 1 {
		t.Fatal("lock moved actor")
	}

	silentState, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	actor := silentState.Players["actor"]
	actor.Body.Flags[44/8] |= 1 << (44 % 8)
	silentState.Players["actor"] = actor
	silentRaw, err := json.Marshal(silentState)
	if err != nil {
		t.Fatal(err)
	}
	silentStore := &departureStore{state: silentRaw}
	silent, err := owners.ExecuteGoLine(context.Background(), silentStore, "w", "go-silent", lease, "가 동굴", 100, 12, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var silentText string
	if err := json.Unmarshal(silent.Response, &silentText); err != nil || silentText != world.GoSilentResponse {
		t.Fatalf("silent=%q err=%v", silentText, err)
	}

	combatState, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	src := combatState.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	combatState.Rooms[1] = src
	combatState.NPCs = map[string]world.NPCState{
		"wolf": {
			Body:    world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10},
			Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: 0}},
		},
	}
	combatRaw, err := json.Marshal(combatState)
	if err != nil {
		t.Fatal(err)
	}
	combatStore := &departureStore{state: combatRaw}
	combat, err := owners.ExecuteGoLine(context.Background(), combatStore, "w", "go-combat", lease, "가 동굴", 100, 12, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var combatText string
	if err := json.Unmarshal(combat.Response, &combatText); err != nil || combatText != world.GoCombatResponse {
		t.Fatalf("combat=%q err=%v", combatText, err)
	}

	unresolvedState, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	unresolvedRoom := unresolvedState.Rooms[1]
	unresolvedRoom.Resource.Exits[0].Destination = 99
	unresolvedState.Rooms[1] = unresolvedRoom
	unresolvedRaw, err := json.Marshal(unresolvedState)
	if err != nil {
		t.Fatal(err)
	}
	unresolvedStore := &departureStore{state: unresolvedRaw}
	if _, err := owners.ExecuteGoLine(context.Background(), unresolvedStore, "w", "go-unresolved", lease, "가 동굴", 100, 12, nil, nil, nil); !errors.Is(err, world.ErrGoDestinationUnresolved) || unresolvedStore.commits != 0 {
		t.Fatalf("unresolved err=%v commits=%d", err, unresolvedStore.commits)
	}
	unresolvedSaved, err := world.DecodeState(unresolvedStore.state)
	if err != nil || unresolvedSaved.Players["actor"].Body.RoomID != 1 || unresolvedSaved.Rooms[1].Resource.Track != "" {
		t.Fatalf("missing dest mutated snapshot: actor=%+v track=%q err=%v", unresolvedSaved.Players["actor"], unresolvedSaved.Rooms[1].Resource.Track, err)
	}
}

func TestExecuteGoLineClosedFlyTimeSexGatesAndReplay(t *testing.T) {
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id       string
		line     string
		hour     int
		want     string
		setup    func(world.State) world.State
		passFlag uint
	}{
		{
			id: "go-closed", line: "가 동굴", hour: 12, want: world.GoClosedResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[3/8] |= 1 << (3 % 8)
				s.Rooms[1] = room
				return s
			},
		},
		{
			id: "go-fly", line: "들어가 동굴", hour: 12, want: world.GoFlyResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[11/8] |= 1 << (11 % 8)
				s.Rooms[1] = room
				return s
			},
			passFlag: 31,
		},
		{
			id: "go-night", line: "동굴 가", hour: 12, want: world.GoNightResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[16/8] |= 1 << (16 % 8)
				s.Rooms[1] = room
				return s
			},
		},
		{
			id: "go-day", line: "가 동굴", hour: 21, want: world.GoDayResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[17/8] |= 1 << (17 % 8)
				s.Rooms[1] = room
				return s
			},
		},
		{
			id: "go-female", line: "가 동굴", hour: 12, want: world.GoFemaleResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[12/8] |= 1 << (12 % 8)
				s.Rooms[1] = room
				actor := s.Players["actor"]
				actor.Body.Flags[12/8] |= 1 << (12 % 8)
				s.Players["actor"] = actor
				return s
			},
		},
		{
			id: "go-male", line: "가 동굴", hour: 12, want: world.GoMaleResponse,
			setup: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[13/8] |= 1 << (13 % 8)
				s.Rooms[1] = room
				return s
			},
		},
	} {
		t.Run(tc.id, func(t *testing.T) {
			state, err := world.DecodeState(goCommandFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.setup(state))
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			first, err := owners.ExecuteGoLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, tc.hour, nil, nil, nil)
			if err != nil || first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
			}
			var text string
			if err := json.Unmarshal(first.Response, &text); err != nil || text != tc.want {
				t.Fatalf("response=%q err=%v", text, err)
			}
			saved, err := world.DecodeState(store.state)
			if err != nil || saved.Players["actor"].Body.RoomID != 1 {
				t.Fatalf("gate moved actor=%+v err=%v", saved.Players["actor"], err)
			}
			replay, err := owners.ExecuteGoLine(context.Background(), store, "w", tc.id, lease, tc.line, 100, tc.hour, nil, nil, nil)
			if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
				t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
			}
			if tc.passFlag == 0 {
				return
			}
			allowedState, err := world.DecodeState(raw)
			if err != nil {
				t.Fatal(err)
			}
			actor := allowedState.Players["actor"]
			actor.Body.Flags[tc.passFlag/8] |= 1 << (tc.passFlag % 8)
			allowedState.Players["actor"] = actor
			allowedRaw, err := json.Marshal(allowedState)
			if err != nil {
				t.Fatal(err)
			}
			allowedStore := &departureStore{state: allowedRaw}
			allowed, err := owners.ExecuteGoLine(context.Background(), allowedStore, "w", tc.id+"-pass", lease, tc.line, 100, tc.hour, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			allowedSaved, err := world.DecodeState(allowedStore.state)
			if err != nil || allowedSaved.Players["actor"].Body.RoomID != 2 || allowed.Replayed {
				t.Fatalf("pass did not move actor=%+v err=%v", allowedSaved.Players["actor"], err)
			}
		})
	}
}

func TestExecuteGoLineMovesFollowers(t *testing.T) {
	state, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	leader := state.Players["actor"]
	leader.FollowerIDs = []string{"follower"}
	state.Players["actor"] = leader
	state.Players["follower"] = world.PlayerState{
		Body:        world.LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPMax: 20, HPCurrent: 20, Stats: [5]byte{10, 10, 10, 10, 10}},
		Online:      true,
		FollowingID: "actor",
		Items:       &world.ItemCollection{Items: map[string]world.Item{}},
	}
	src := state.Rooms[1]
	src.PlayerIDs = []string{"actor", "follower"}
	state.Rooms[1] = src
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	receipt, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-follow", lease, "가 동굴", 100, 12, nil, nil, nil)
	if err != nil || receipt.Replayed || store.commits != 1 {
		t.Fatalf("receipt=%+v err=%v commits=%d", receipt, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["actor"].Body.RoomID != 2 || saved.Players["follower"].Body.RoomID != 2 {
		t.Fatalf("followers stayed behind actor=%+v follower=%+v", saved.Players["actor"].Body, saved.Players["follower"].Body)
	}
}

func TestExecuteGoLineLethalClimbRespawnsWithoutSealing(t *testing.T) {
	state, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	state.War = &world.FamilyWar{}
	src := state.Rooms[1]
	src.Resource.Exits[0].Flags[1] |= 1
	state.Rooms[1] = src
	state.Rooms[1008] = world.RoomState{
		Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1008, Name: "부활"}},
		Items:    &world.ItemCollection{Items: map[string]world.Item{}},
	}
	actor := state.Players["actor"]
	actor.Body.HPCurrent = 5
	state.Players["actor"] = actor
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	receipt, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-die", lease, "가 동굴", 100, 12, nil, func(low, high int) int { return low }, nil)
	if err != nil || store.commits != 1 {
		t.Fatalf("receipt=%+v err=%v commits=%d", receipt, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	actor = saved.Players["actor"]
	if actor.Body.RoomID != 1008 || actor.Body.HPCurrent != actor.Body.HPMax || !actor.Online {
		t.Fatalf("lethal climb ghost actor=%+v", actor.Body)
	}
}

func goChaseCommandState(t *testing.T, enemies []world.NPCEnemy, active []string) world.State {
	t.Helper()
	state, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	src := state.Rooms[1]
	src.NPCIDs = []string{"wolf"}
	src.PlayerIDs = []string{"actor"}
	state.Rooms[1] = src
	wolf := world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	wolf.Flags[9/8] |= 1 << (9 % 8)
	state.NPCs = map[string]world.NPCState{"wolf": {Body: wolf, Enemies: enemies}}
	state.ActiveNPCIDs = active
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestExecuteGoLineChasesMFOLLOAndReplaysWithoutRecommit(t *testing.T) {
	raw, err := json.Marshal(goChaseCommandState(t, []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}, []string{}))
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-chase", lease, "가 동굴", 100, 12, nil, func(int, int) int { return 1 }, nil)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	if !reflect.DeepEqual(first.NPCChaseIDs, []string{"wolf"}) {
		t.Fatalf("committed chase IDs=%v", first.NPCChaseIDs)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, world.NPCGoChaseActorText("늑대")) {
		t.Fatalf("response=%q", text)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.NPCs["wolf"].Body.RoomID != 2 {
		t.Fatalf("wolf stayed behind: %+v err=%v", saved.NPCs, err)
	}
	replay, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-chase", lease, "가 동굴", 100, 12, nil, func(int, int) int { t.Fatal("replay rerolled chase"); return 1 }, nil)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if replay.NPCChaseIDs != nil {
		t.Fatalf("replay carried ephemeral chase IDs=%v", replay.NPCChaseIDs)
	}
}

func TestExecuteGoLineCollectsOrderedNPCChaseIDs(t *testing.T) {
	state, err := world.DecodeState(goCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	source := state.Rooms[1]
	source.NPCIDs = []string{"zeta", "alpha"}
	state.Rooms[1] = source
	zeta := world.LegacyMonster{Name: "Zulu", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	alpha := world.LegacyMonster{Name: "Alpha", Type: 1, RoomID: 1, HPMax: 10, HPCurrent: 10, Stats: [5]byte{0, 5}}
	for id, body := range map[string]world.LegacyMonster{"zeta": zeta, "alpha": alpha} {
		value := body
		value.Flags[9/8] |= 1 << (9 % 8)
		if state.NPCs == nil {
			state.NPCs = map[string]world.NPCState{}
		}
		state.NPCs[id] = world.NPCState{
			Body:    value,
			Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: -1}},
		}
	}
	state.ActiveNPCIDs = []string{"zeta", "alpha"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-chase-ordered", lease, "가 동굴", 100, 12, nil, func(int, int) int { return 1 }, nil)
	if err != nil || first.Replayed || !reflect.DeepEqual(first.NPCChaseIDs, []string{"zeta", "alpha"}) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
}

func TestExecuteGoLineChaseReceiptUsesProposalMovesNotResponseNameMatching(t *testing.T) {
	state := goChaseCommandState(t, []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}, []string{})
	actor := state.Players["actor"]
	actor.NPCFollowerIDs = []string{"managed"}
	state.Players["actor"] = actor
	source := state.Rooms[1]
	source.NPCIDs = []string{"managed", "wolf"}
	state.Rooms[1] = source
	managed := world.NPCState{
		Body:              world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, Stats: [5]byte{0, 5}},
		Enemies:           []world.NPCEnemy{},
		FollowingPlayerID: "actor",
	}
	managed.Body.Flags[46/8] |= 1 << (46 % 8)
	state.NPCs["managed"] = managed
	state.ActiveNPCIDs = []string{"managed", "wolf"}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-chase-proposal-metadata", lease, "가 동굴", 100, 12, nil, func(int, int) int { return 1 }, nil)
	if err != nil || first.Replayed || !reflect.DeepEqual(first.NPCChaseIDs, []string{"wolf"}) {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	var response string
	if err := json.Unmarshal(first.Response, &response); err != nil || !strings.Contains(response, world.NPCGoChaseActorText("늑대")) {
		t.Fatalf("response=%q err=%v", response, err)
	}
}

func TestExecuteGoLineFailsClosedOnUnmigratedChaseRelations(t *testing.T) {
	owners := &Ownership{}
	lease, err := owners.Acquire("actor")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		enemies []world.NPCEnemy
		active  []string
	}{
		{name: "nil enemies"},
		{name: "nil active", enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "actor"}, Damage: -1}}, active: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(goChaseCommandState(t, tc.enemies, tc.active))
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			if _, err := owners.ExecuteGoLine(context.Background(), store, "w", "go-chase-"+tc.name, lease, "가 동굴", 100, 12, nil, func(int, int) int { return 1 }, nil); err == nil || store.commits != 0 {
				t.Fatalf("err=%v commits=%d", err, store.commits)
			}
		})
	}
}
