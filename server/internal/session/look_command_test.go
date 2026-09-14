package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/storage"
	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func lookCommandFixture() world.State {
	return world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{
					ID: 1, Name: "광장",
					Exits: []world.LegacyExit{{Name: "동", Destination: 2}, {Name: "동굴", Destination: 3}, {Name: "북", Destination: 4}},
				}},
				PlayerIDs: []string{"a"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "동쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			3: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3, Name: "동굴방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
			4: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 4, Name: "북쪽방"}}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
	}
}

func TestParseLookLineAcceptsBareAndExitTarget(t *testing.T) {
	bare, ok := ParseLookLine("  봐  ")
	if !ok || bare.Target != "" || bare.Occurrence != 0 {
		t.Fatalf("bare=%+v ok=%v", bare, ok)
	}
	exit, ok := ParseLookLine("조사 동")
	if !ok || exit.Target != "동" || exit.Occurrence != 1 {
		t.Fatalf("exit=%+v ok=%v", exit, ok)
	}
	occurrence, ok := ParseLookLine(`보다 "동굴" 2`)
	if !ok || occurrence.Target != "동굴" || occurrence.Occurrence != 2 {
		t.Fatalf("occurrence=%+v ok=%v", occurrence, ok)
	}
	suffix, ok := ParseLookLine("동 봐")
	if !ok || suffix.Target != "동" || suffix.Occurrence != 1 || !IsLookLine("동 봐") {
		t.Fatalf("suffix=%+v ok=%v", suffix, ok)
	}
	north, ok := ParseLookLine("북 보다")
	if !ok || north.Target != "북" || north.Occurrence != 1 {
		t.Fatalf("north suffix=%+v ok=%v", north, ok)
	}
	suffixOcc, ok := ParseLookLine("동굴 2 조사")
	if !ok || suffixOcc.Target != "동굴" || suffixOcc.Occurrence != 2 {
		t.Fatalf("suffix occ=%+v ok=%v", suffixOcc, ok)
	}
	self, ok := ParseLookLine("봐 나")
	if !ok || self.Target != "나" || self.Occurrence != 1 {
		t.Fatalf("self=%+v ok=%v", self, ok)
	}
	selfSuffix, ok := ParseLookLine("나 봐")
	if !ok || selfSuffix.Target != "나" || selfSuffix.Occurrence != 1 {
		t.Fatalf("self suffix=%+v ok=%v", selfSuffix, ok)
	}
	ply, ok := ParseLookLine("봐 Bob")
	if !ok || ply.Target != "Bob" || ply.Occurrence != 1 {
		t.Fatalf("ply=%+v ok=%v", ply, ok)
	}
	plySuffix, ok := ParseLookLine("Bob 봐")
	if !ok || plySuffix.Target != "Bob" || plySuffix.Occurrence != 1 {
		t.Fatalf("ply suffix=%+v ok=%v", plySuffix, ok)
	}
	both, ok := ParseLookLine("봐 보다")
	if !ok || both.Target != "봐" || both.Occurrence != 1 {
		t.Fatalf("last-token wins=%+v ok=%v", both, ok)
	}
	extra, ok := ParseLookLine("동 junk 봐")
	if !ok || extra.Target != "동" || extra.Occurrence != 1 {
		t.Fatalf("extra last-token=%+v ok=%v", extra, ok)
	}
	northExtra, ok := ParseLookLine("북 foo 보다")
	if !ok || northExtra.Target != "북" || northExtra.Occurrence != 1 {
		t.Fatalf("north extra last-token=%+v ok=%v", northExtra, ok)
	}
	for _, line := range []string{"", "보아 동", "봐 동 extra", "봐 동 0", "봐 동 -1", "봐\n동", "봐 동\x00", "동 봐 extra", "동 0 봐", "동 -1 봐"} {
		if _, ok := ParseLookLine(line); ok {
			t.Fatalf("unsupported look line accepted: %q", line)
		}
	}
}

func TestExecuteLookLineReturnsCurrentRoomThroughReceipt(t *testing.T) {
	s := lookCommandFixture()
	raw, _ := json.Marshal(s)
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	first, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-1", lease, "봐", 12)
	if err != nil {
		t.Fatal(err)
	}
	var scene string
	if err := json.Unmarshal(first.Response, &scene); err != nil || !strings.Contains(scene, "광장") {
		t.Fatalf("%q %v", scene, err)
	}
	again, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-1", lease, "봐", 12)
	if err != nil || !again.Replayed || store.commits != 1 {
		t.Fatal("look receipt reexecuted")
	}
	if string(store.state) != string(raw) {
		t.Fatal("look mutated world")
	}
	for _, line := range []string{"검 조사", "북", "봐\n봐"} {
		if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "other", lease, line, 12); err == nil {
			t.Fatalf("unsupported command accepted %q", line)
		}
	}
}

func executeParsedLookLine(t *testing.T, owners *Ownership, store *departureStore, lease SessionLease, commandID, line string) storage.WorldReceipt {
	t.Helper()
	parsed, err := ParseCommand(line)
	if err != nil {
		t.Fatalf("ParseCommand(%q) err=%v", line, err)
	}
	switch parsed.Kind {
	case CommandLook:
		receipt, execErr := owners.ExecuteLookLine(context.Background(), store, "w", commandID, lease, line, 12)
		if execErr != nil {
			t.Fatalf("ExecuteLookLine(%q) err=%v", line, execErr)
		}
		return receipt
	case CommandDirectional:
		receipt, execErr := owners.ExecuteDirectionalLine(context.Background(), store, "w", commandID, lease, line, 100, 12, world.SceneOptions{}, nil, nil, nil)
		if execErr != nil {
			t.Fatalf("ExecuteDirectionalLine(%q) err=%v", line, execErr)
		}
		return receipt
	default:
		t.Fatalf("ParseCommand(%q)=%+v want CommandLook", line, parsed)
		return storage.WorldReceipt{}
	}
}

func TestParseCommandDirectionThenLookVerbPeeksWithoutMoving(t *testing.T) {
	for _, tt := range []struct {
		line string
		want string
		id   string
	}{
		{"동 봐", "동쪽방", "look-east-suffix"},
		{"북 보다", "북쪽방", "look-north-suffix"},
		{"동 junk 봐", "동쪽방", "look-east-extra"},
		{"북 foo 보다", "북쪽방", "look-north-extra"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			s := lookCommandFixture()
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			var owners Ownership
			lease, err := owners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseCommand(tt.line)
			if err != nil || parsed.Kind != CommandLook {
				t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandLook", tt.line, parsed, err)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var scene string
			if err := json.Unmarshal(first.Response, &scene); err != nil || !strings.Contains(scene, tt.want) || strings.Contains(scene, "== 광장 ==") {
				t.Fatalf("scene=%q err=%v want %q", scene, err, tt.want)
			}
			saved, err := world.DecodeState(store.state)
			if err != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("last-token look moved actor: %+v err=%v", saved.Players["a"], err)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestParseCommandLastTokenInvalidOccurrenceDoesNotPeekOrMove(t *testing.T) {
	for _, tt := range []struct {
		line, id string
	}{
		{"동 0 봐", "look-east-zero"},
		{"동 -1 봐", "look-east-neg"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			s := lookCommandFixture()
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			store := &departureStore{state: raw}
			var owners Ownership
			lease, err := owners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			if err := owners.Admit(lease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseCommand(tt.line)
			if err != nil || parsed.Kind != CommandLook {
				t.Fatalf("ParseCommand(%q)=%+v err=%v want CommandLook", tt.line, parsed, err)
			}
			command, ok := ParseLookLine(tt.line)
			if ok && command.Target == "동" && command.Occurrence == 1 {
				t.Fatalf("invalid occurrence peeked as 1: %+v", command)
			}
			first, execErr := owners.ExecuteLookLine(context.Background(), store, "w", tt.id, lease, tt.line, 12)
			if !errors.Is(execErr, ErrUnsupportedLookLine) || store.commits != 0 {
				var scene string
				_ = json.Unmarshal(first.Response, &scene)
				t.Fatalf("ExecuteLookLine(%q) peeked or committed: receipt=%+v scene=%q err=%v commits=%d", tt.line, first, scene, execErr, store.commits)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("invalid occurrence moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay, replayErr := owners.ExecuteLookLine(context.Background(), store, "w", tt.id, lease, tt.line, 12)
			if !errors.Is(replayErr, ErrUnsupportedLookLine) || replay.Replayed || store.commits != 0 {
				t.Fatalf("replay committed: receipt=%+v err=%v commits=%d", replay, replayErr, store.commits)
			}
		})
	}
}

func TestExecuteLookLinePeeksExitDestinationAndReplays(t *testing.T) {
	s := lookCommandFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-east", lease, "봐 동", 12)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var scene string
	if err := json.Unmarshal(first.Response, &scene); err != nil || !strings.Contains(scene, "동쪽방") || strings.Contains(scene, "== 광장 ==") {
		t.Fatalf("scene=%q err=%v", scene, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Body.RoomID != 1 {
		t.Fatalf("look-through moved actor: %+v err=%v", saved.Players["a"], err)
	}
	replay, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-east", lease, "봐 동", 12)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteLookLineClosedExitPersistsAndReplays(t *testing.T) {
	s := lookCommandFixture()
	room := s.Rooms[1]
	room.Resource.Exits[0].Flags[0] |= 8
	s.Rooms[1] = room
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	first, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-closed", lease, "보다 동", 12)
	if err != nil || store.commits != 1 {
		t.Fatalf("first err=%v commits=%d", err, store.commits)
	}
	var text string
	if err := json.Unmarshal(first.Response, &text); err != nil || text != world.LookClosedResponse {
		t.Fatalf("text=%q err=%v", text, err)
	}
	replay, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-closed", lease, "보다 동", 12)
	if err != nil || !replay.Replayed || store.commits != 1 {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteLookLineLastTokenExtraMatchesPrefixClosedBlindNomap(t *testing.T) {
	for _, tt := range []struct {
		name string
		id   string
		prep func(world.State) world.State
		want string
	}{
		{
			name: "closed",
			id:   "look-closed-extra",
			prep: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Flags[0] |= 8
				s.Rooms[1] = room
				return s
			},
			want: world.LookClosedResponse,
		},
		{
			name: "blind",
			id:   "look-blind-extra",
			prep: func(s world.State) world.State {
				actor := s.Players["a"]
				actor.Body.Flags[42/8] |= 1 << (42 % 8) // PBLIND
				s.Players["a"] = actor
				return s
			},
			want: world.LookBlindResponse,
		},
		{
			name: "nomap",
			id:   "look-nomap-extra",
			prep: func(s world.State) world.State {
				room := s.Rooms[1]
				room.Resource.Exits[0].Destination = 1
				s.Rooms[1] = room
				return s
			},
			want: world.LookNoMapResponse,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.prep(lookCommandFixture())
			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			prefixStore := &departureStore{state: append([]byte(nil), raw...)}
			extraStore := &departureStore{state: append([]byte(nil), raw...)}
			var prefixOwners, extraOwners Ownership
			prefixLease, err := prefixOwners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			if err := prefixOwners.Admit(prefixLease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			extraLease, err := extraOwners.Acquire("a")
			if err != nil {
				t.Fatal(err)
			}
			if err := extraOwners.Admit(extraLease, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			prefix := executeParsedLookLine(t, &prefixOwners, prefixStore, prefixLease, tt.id+"-prefix", "보다 동")
			extra := executeParsedLookLine(t, &extraOwners, extraStore, extraLease, tt.id, "동 junk 봐")
			if prefix.Replayed || extra.Replayed || prefixStore.commits != 1 || extraStore.commits != 1 {
				t.Fatalf("first prefix=%+v extra=%+v prefixCommits=%d extraCommits=%d", prefix, extra, prefixStore.commits, extraStore.commits)
			}
			var prefixText, extraText string
			if err := json.Unmarshal(prefix.Response, &prefixText); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(extra.Response, &extraText); err != nil {
				t.Fatal(err)
			}
			if prefixText != tt.want || extraText != tt.want || extraText != prefixText {
				t.Fatalf("prefix=%q extra=%q want %q", prefixText, extraText, tt.want)
			}
			saved, decodeErr := world.DecodeState(extraStore.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("extra-token look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &extraOwners, extraStore, extraLease, tt.id, "동 junk 봐")
			if !replay.Replayed || extraStore.commits != 1 || string(replay.Response) != string(extra.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, extraStore.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedTargets(t *testing.T) {
	s := lookCommandFixture()
	room := s.Rooms[1]
	room.Resource.Exits = append(room.Resource.Exits, world.LegacyExit{Name: "허공", Destination: 99})
	s.Rooms[1] = room
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"봐 늑대", "조사 검", "봐 허공"} {
		store := &departureStore{state: raw}
		var owners Ownership
		lease, _ := owners.Acquire("a")
		owners.Admit(lease, func() error { return nil })
		if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-bad", lease, line, 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
			t.Fatalf("line=%q err=%v commits=%d", line, err, store.commits)
		}
	}
}

func lookObjectCreatureCommandFixture() world.State {
	s := lookCommandFixture()
	room := s.Rooms[1]
	room.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"floor-sword": {Object: world.LegacyObject{Name: "검", Description: "빛나는 검.", Type: 13}},
		},
		Inventory: []string{"floor-sword"},
	}
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: &world.ItemCollection{Items: map[string]world.Item{}}, Enemies: []world.NPCEnemy{}},
	}
	return s
}

func TestExecuteLookLineInspectsRoomObjectAndCreatureWithoutMoving(t *testing.T) {
	s := lookObjectCreatureCommandFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, want, id string
	}{
		{"봐 검", "빛나는 검.", "look-sword"},
		{"조사 검", "빛나는 검.", "look-inspect-sword"},
		{"보다 늑대", "당신은 늑대를 봅니다.", "look-wolf"},
		{"늑대 봐", "당신은 늑대를 봅니다.", "look-wolf-suffix"},
		{"검 조사", "빛나는 검.", "look-sword-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			parsed, parseErr := ParseCommand(tt.line)
			if parseErr != nil || parsed.Kind != CommandLook {
				t.Fatalf("ParseCommand(%q)=%+v err=%v", tt.line, parsed, parseErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, tt.want) || strings.Contains(text, "== 광장 ==") {
				t.Fatalf("text=%q err=%v want %q", text, unmarshalErr, tt.want)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("object/creature look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineInspectsInventoryAndReadyWithoutMoving(t *testing.T) {
	s := lookObjectCreatureCommandFixture()
	actor := s.Players["a"]
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"inv-gem":   {Object: world.LegacyObject{Name: "보석", Description: "품은 보석.", Type: 13}},
			"wear-ring": {Object: world.LegacyObject{Name: "반지", Description: "낀 반지.", Type: 13}},
			"inv-sword": {Object: world.LegacyObject{Name: "검", Description: "품은 검.", Type: 13}},
		},
		Inventory: []string{"inv-gem", "inv-sword"},
		Ready:     [20]string{8: "wear-ring"},
	}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, want, id string
	}{
		{"봐 보석", "품은 보석.", "look-inv-gem"},
		{"보석 조사", "품은 보석.", "look-inv-gem-suffix"},
		{"보다 반지", "낀 반지.", "look-ready-ring"},
		{"봐 검", "품은 검.", "look-inv-beats-floor"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			parsed, parseErr := ParseCommand(tt.line)
			if parseErr != nil || parsed.Kind != CommandLook {
				t.Fatalf("ParseCommand(%q)=%+v err=%v", tt.line, parsed, parseErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, tt.want) || strings.Contains(text, "빛나는 검.") || strings.Contains(text, "== 광장 ==") {
				t.Fatalf("text=%q err=%v want %q", text, unmarshalErr, tt.want)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("inventory/ready look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineMissingRoomTargetStillFailsClosed(t *testing.T) {
	s := lookObjectCreatureCommandFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-missing", lease, "봐 유령", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("missing target err=%v commits=%d", err, store.commits)
	}
}

func lookCombatNoticeCommandFixture() world.State {
	s := lookCommandFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}}},
	}
	return s
}

func TestExecuteLookLineIncludesDisplayRomCombatNoticeAndReplays(t *testing.T) {
	s := lookCombatNoticeCommandFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, acquireErr := owners.Acquire("a")
	if acquireErr != nil {
		t.Fatal(acquireErr)
	}
	if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
		t.Fatal(admitErr)
	}
	first, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-combat-1", lease, "봐", 12)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var scene string
	if err := json.Unmarshal(first.Response, &scene); err != nil || !strings.Contains(scene, "늑대가 당신과 싸우고 있습니다.\n") {
		t.Fatalf("scene=%q err=%v", scene, err)
	}
	replay, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-combat-1", lease, "봐", 12)
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if string(store.state) != string(raw) {
		t.Fatal("look combat receipt mutated world")
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedCombat(t *testing.T) {
	s := lookCombatNoticeCommandFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-combat-nil", lease, "봐", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("unmigrated combat err=%v commits=%d", err, store.commits)
	}
}

func lookSelfPlyCommandFixture() world.State {
	s := lookCommandFixture()
	actor := s.Players["a"]
	actor.Body.Description = "바르게 "
	s.Players["a"] = actor
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	s.Rooms[1] = room
	s.Players["b"] = world.PlayerState{Body: world.LegacyMonster{Name: "Bob", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	return s
}

func TestExecuteLookLineInspectsSelfAndFirstPlyWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, want, id string
	}{
		{"봐 나", world.LookSelfMirrorResponse, "look-self"},
		{"나 봐", world.LookSelfMirrorResponse, "look-self-suffix"},
		{"보다 나", world.LookSelfMirrorResponse, "look-self-보다"},
		{"봐 Bob", "당신은 Bob님을 봅니다.", "look-bob"},
		{"Bob 봐", "당신은 Bob님을 봅니다.", "look-bob-suffix"},
		{"조사 Bob", "당신은 Bob님을 봅니다.", "look-bob-조사"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			parsed, parseErr := ParseCommand(tt.line)
			if parseErr != nil || parsed.Kind != CommandLook {
				t.Fatalf("ParseCommand(%q)=%+v err=%v", tt.line, parsed, parseErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, strings.TrimRight(tt.want, "\r\n")) || strings.Contains(text, "== 광장 ==") {
				t.Fatalf("text=%q err=%v want %q", text, unmarshalErr, tt.want)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("self/first_ply look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineAppendsConsiderAndEquipListWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {
			Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100},
			Items: &world.ItemCollection{
				Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}},
				Ready: [20]string{19: "sword"},
			},
			Enemies: []world.NPCEnemy{},
		},
	}
	actor := s.Players["a"]
	actor.Body.Level = 4
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-consider", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"나 봐", "look-self-equip-suffix", []string{"그녀는 바르게 서 있습니다", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-consider", []string{"당신은 늑대를 봅니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-equip-suffix", []string{"회색 늑대다.", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-equip", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"꼭 맞는", "한방에", "쨉도"}},
		{"Bob 봐", "look-bob-equip-suffix", []string{"그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"꼭 맞는", "한방에"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("consider/equip look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForNilInspectItems(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Items = nil
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-nil-items", lease, "봐 Bob", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("nil first_ply items err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedPlayer(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.Name = "Bob "
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-ghost", lease, "봐 Bob", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("unmigrated first_ply err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineAppendsHPBandsWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {
			Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPCurrent: 50, HPMax: 100},
			Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
			Enemies: []world.NPCEnemy{},
		},
	}
	actor := s.Players["a"]
	actor.Body.Level = 4
	actor.Body.HPCurrent = 89
	actor.Body.HPMax = 100
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Body.HPCurrent = 10
	bob.Body.HPMax = 100
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-hp", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"나 봐", "look-self-hp-suffix", []string{"그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-hp", []string{"당신은 늑대를 봅니다.", "그녀는 많은 상처를 입었습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-hp-suffix", []string{"회색 늑대다.", "그녀는 많은 상처를 입었습니다.", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-hp", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "[  몸  ]  갑옷"}, []string{"여러군데", "많은 상처", "심각한", "죽기 직전"}},
		{"Bob 봐", "look-bob-hp-suffix", []string{"그녀는 바르게 서 있습니다", "그녀는 가벼운 상처를 입었습니다.", "[  몸  ]  갑옷"}, []string{"죽기 직전"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("hp-band look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedHPMax(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	actor := s.Players["a"]
	actor.Body.HPMax = 0
	actor.Body.HPCurrent = 10
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-hpmax", lease, "봐 나", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("self hpmax=0 err=%v commits=%d", err, store.commits)
	}

	s = lookObjectCreatureCommandFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.HPMax = 0
	wolf.Body.HPCurrent = 10
	s.NPCs["npc-wolf"] = wolf
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	owners = Ownership{}
	lease, _ = owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-wolf-hpmax", lease, "봐 늑대", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("creature hpmax=0 err=%v commits=%d", err, store.commits)
	}

	s = lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.HPMax = 0
	bob.Body.HPCurrent = 10
	s.Players["b"] = bob
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	owners = Ownership{}
	lease, _ = owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-bob-hpmax", lease, "봐 Bob", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("first_ply hpmax=0 err=%v commits=%d", err, store.commits)
	}
}

func lookSetCommandMarried(body *world.LegacyMonster, spouse string, male bool) {
	body.Flags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	if male {
		body.Flags[world.MarriageMaleFlag/8] |= 1 << (world.MarriageMaleFlag % 8)
	}
	body.Keys[world.MarriageSpouseKeyIndex] = world.MarriageSpouseKeyPrefix + spouse
}

func TestExecuteLookLineAppendsFirstPlyMarriageWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.HPCurrent = 100
	bob.Body.HPMax = 100
	lookSetCommandMarried(&bob.Body, "Carol", true)
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 Bob", "look-bob-marriage"},
		{"Bob 봐", "look-bob-marriage-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			want := []string{"당신은 Bob님을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "[  몸  ]  갑옷"}
			for _, line := range want {
				if !strings.Contains(text, line) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, line)
				}
			}
			for _, not := range []string{"상처", "죽기 직전", "꼭 맞는", "광채"} {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("marriage look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedFirstPlySpouse(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.Flags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-married-ghost", lease, "봐 Bob", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("PMARRI without spouse err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineAppendsFirstPlyStandingDescriptionWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	lookSetCommandMarried(&bob.Body, "Carol", true)
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 Bob", "look-bob-standing"},
		{"Bob 봐", "look-bob-standing-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			want := []string{"당신은 Bob님을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "[  몸  ]  갑옷"}
			for _, line := range want {
				if !strings.Contains(text, line) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, line)
				}
			}
			for _, not := range []string{"특별한 것은 보이지 않습니다", "상처", "죽기 직전", "꼭 맞는", "광채"} {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 || saved.Players["b"].Body.RoomID != 1 {
				t.Fatalf("standing look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineAppendsSelfStandingDescriptionWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	actor := s.Players["a"]
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 나", "look-self-standing"},
		{"나 봐", "look-self-standing-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			want := []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}
			for _, line := range want {
				if !strings.Contains(text, line) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, line)
				}
			}
			for _, not := range []string{"특별한 것은 보이지 않습니다", "그는 서 있습니다"} {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("self standing look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedSelfDescription(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	actor := s.Players["a"]
	actor.Body.Description = ""
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-self-desc-empty", lease, "봐 나", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("empty self description err=%v commits=%d", err, store.commits)
	}

	s = lookSelfPlyCommandFixture()
	actor = s.Players["a"]
	actor.Body.Description = "바르게"
	s.Players["a"] = actor
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	owners = Ownership{}
	lease, _ = owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-self-desc-unmigrated", lease, "나 봐", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("unmigrated self description err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineAppendsSelfMarriageWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	actor := s.Players["a"]
	lookSetCommandMarried(&actor.Body, "Carol", true)
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
	}{
		{"봐 나", "look-self-marriage"},
		{"나 봐", "look-self-marriage-suffix"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			want := []string{"당신은 거울을 들고 자신을 봅니다.", "그는 Carol님과 결혼한 기혼자입니다.", "그는 바르게 서 있습니다", "그는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}
			for _, line := range want {
				if !strings.Contains(text, line) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, line)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("self marriage look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedSelfSpouse(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	actor := s.Players["a"]
	actor.Body.Flags[world.MarriageActiveFlag/8] |= 1 << (world.MarriageActiveFlag % 8)
	s.Players["a"] = actor
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-self-married-ghost", lease, "봐 나", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("self PMARRI without spouse err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedFirstPlyDescription(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.Description = ""
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-desc-empty", lease, "봐 Bob", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("empty first_ply description err=%v commits=%d", err, store.commits)
	}

	s = lookSelfPlyCommandFixture()
	bob = s.Players["b"]
	bob.Body.Description = "바르게"
	s.Players["b"] = bob
	raw, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store = &departureStore{state: raw}
	owners = Ownership{}
	lease, _ = owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-desc-unmigrated", lease, "Bob 봐", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("unmigrated first_ply description err=%v commits=%d", err, store.commits)
	}
}

func lookSetCommandKnowAlignment(body *world.LegacyMonster) {
	body.Flags[33/8] |= 1 << (33 % 8)
}

func lookSetCommandMale(body *world.LegacyMonster) {
	body.Flags[12/8] |= 1 << (12 % 8)
}

func TestExecuteLookLineAppendsPKNOWAGlowWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	wolfBody := world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPCurrent: 50, HPMax: 100, Alignment: 40}
	lookSetCommandMale(&wolfBody)
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {
			Body:    wolfBody,
			Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
			Enemies: []world.NPCEnemy{},
		},
	}
	actor := s.Players["a"]
	actor.Body.Level = 4
	actor.Body.HPCurrent = 89
	actor.Body.HPMax = 100
	actor.Body.Alignment = -50
	lookSetCommandKnowAlignment(&actor.Body)
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.Body.HPCurrent = 10
	bob.Body.HPMax = 100
	bob.Body.Alignment = -50
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-pknowa", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"나 봐", "look-self-pknowa-suffix", []string{"그녀는 바르게 서 있습니다", "그녀에게서 붉은 광채가 뻗어 나오고 있습니다.", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-pknowa", []string{"당신은 늑대를 봅니다.", "그에게서 푸른 광채가 뻗어 나오고 있습니다.", "그는 많은 상처를 입었습니다.", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-pknowa-suffix", []string{"회색 늑대다.", "그에게서 푸른 광채가 뻗어 나오고 있습니다.", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-pknowa", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "그녀에게서 푸른 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다.", "[  몸  ]  갑옷"}, []string{"붉은", "꼭 맞는", "죽기 직전"}},
		{"Bob 봐", "look-bob-pknowa-suffix", []string{"그녀에게서 푸른 광채가 뻗어 나오고 있습니다.", "그녀는 가벼운 상처를 입었습니다."}, []string{"붉은"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("pknowa look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineInspectBroadcastsOnFirstCommitAndReplays(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b"}
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: &world.ItemCollection{Items: map[string]world.Item{}}, Enemies: []world.NPCEnemy{}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id, actor, room string
	}{
		{"봐 나", "look-self-broadcast", "당신은 거울을 들고 자신을 봅니다.", "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"},
		{"나 봐", "look-self-broadcast-suffix", "당신은 거울을 들고 자신을 봅니다.", "\nAlice님이 거울을 들고 자신을 바라 봅니다.\r\n"},
		{"봐 늑대", "look-wolf-broadcast", "당신은 늑대를 봅니다.", "\nAlice님이 늑대를 봅니다.\r\n"},
		{"늑대 봐", "look-wolf-broadcast-suffix", "당신은 늑대를 봅니다.", "\nAlice님이 늑대를 봅니다.\r\n"},
		{"봐 Bob", "look-bob-broadcast", "당신은 Bob님을 봅니다.", "\nAlice님이 Bob님을 봅니다.\r\n"},
		{"Bob 봐", "look-bob-broadcast-suffix", "당신은 Bob님을 봅니다.", "\nAlice님이 Bob님을 봅니다.\r\n"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, tt.actor) || strings.Contains(text, "바라 봅니다") {
				t.Fatalf("actor text=%q err=%v", text, unmarshalErr)
			}
			command, ok := ParseLookLine(tt.line)
			if !ok {
				t.Fatalf("ParseLookLine(%q)", tt.line)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("inspect moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			event, eventOK, eventErr := saved.RoomLookInspectEvent("a", command.Target, command.Occurrence, 12)
			if eventErr != nil || !eventOK || event.Text != tt.room || event.ExcludeActorID != "a" || event.RoomID != 1 {
				t.Fatalf("event=%+v ok=%v err=%v want %q", event, eventOK, eventErr, tt.room)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineInspectBroadcastsPINVISPerViewerAndReplays(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	alice := s.Players["a"]
	alice.Body.Flags[2/8] |= 1 << (2 % 8) // PINVIS
	s.Players["a"] = alice
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a", "b", "c"}
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	var carolFlags [8]byte
	carolFlags[21/8] |= 1 << (21 % 8) // PDINVI
	s.Players["c"] = world.PlayerState{Body: world.LegacyMonster{Name: "Carol", Description: "바르게 ", RoomID: 1, HPMax: 100, HPCurrent: 100, Flags: carolFlags}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}}
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {Body: world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}, Items: &world.ItemCollection{Items: map[string]world.Item{}}, Enemies: []world.NPCEnemy{}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id, actor, hidden, detected string
	}{
		{"봐 나", "look-self-pinvis", "당신은 거울을 들고 자신을 봅니다.", "\n누군가가 거울을 들고 자신을 바라 봅니다.\r\n", "\nAlice(*)님이 거울을 들고 자신을 바라 봅니다.\r\n"},
		{"봐 늑대", "look-wolf-pinvis", "당신은 늑대를 봅니다.", "\n누군가가 늑대를 봅니다.\r\n", "\nAlice(*)님이 늑대를 봅니다.\r\n"},
		{"봐 Bob", "look-bob-pinvis", "당신은 Bob님을 봅니다.", "\n누군가가 Bob님을 봅니다.\r\n", "\nAlice(*)님이 Bob님을 봅니다.\r\n"},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil || !strings.Contains(text, tt.actor) || strings.Contains(text, "바라 봅니다") {
				t.Fatalf("actor text=%q err=%v", text, unmarshalErr)
			}
			command, ok := ParseLookLine(tt.line)
			if !ok {
				t.Fatalf("ParseLookLine(%q)", tt.line)
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("inspect moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			event, eventOK, eventErr := saved.RoomLookInspectEvent("a", command.Target, command.Occurrence, 12)
			if eventErr != nil || !eventOK || event.ExcludeActorID != "a" {
				t.Fatalf("event=%+v ok=%v err=%v", event, eventOK, eventErr)
			}
			if got := event.TextFor(saved.Players["b"].Body); got != tt.hidden || strings.Contains(got, "Alice") {
				t.Fatalf("bob=%q want %q", got, tt.hidden)
			}
			if got := event.TextFor(saved.Players["c"].Body); got != tt.detected {
				t.Fatalf("carol=%q want %q", got, tt.detected)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForUnmigratedInspectOccupant(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	bob := s.Players["b"]
	bob.Body.Name = "Bob "
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-ghost-occupant", lease, "봐 나", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("unmigrated occupant err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteLookLineAppendsEnemyLinesWithoutMoving(t *testing.T) {
	s := lookSelfPlyCommandFixture()
	room := s.Rooms[1]
	room.NPCIDs = []string{"npc-wolf"}
	s.Rooms[1] = room
	s.NPCs = map[string]world.NPCState{
		"npc-wolf": {
			Body:    world.LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1, Level: 4, HPMax: 100, HPCurrent: 100},
			Items:   &world.ItemCollection{Items: map[string]world.Item{"sword": {Object: world.LegacyObject{Name: "검"}}}, Ready: [20]string{19: "sword"}},
			Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "a"}, Damage: 0}},
		},
	}
	actor := s.Players["a"]
	actor.Body.Level = 4
	actor.PlayerEnemies = []string{"a"}
	actor.Items = &world.ItemCollection{
		Items: map[string]world.Item{"helm": {Object: world.LegacyObject{Name: "투구"}}},
		Ready: [20]string{6: "helm"},
	}
	s.Players["a"] = actor
	bob := s.Players["b"]
	bob.PlayerEnemies = []string{"a"}
	bob.Items = &world.ItemCollection{
		Items: map[string]world.Item{"armor": {Object: world.LegacyObject{Name: "갑옷"}}},
		Ready: [20]string{0: "armor"},
	}
	s.Players["b"] = bob
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		line, id string
		want     []string
		not      []string
	}{
		{"봐 나", "look-self-enm", []string{"당신은 거울을 들고 자신을 봅니다.", "그녀는 바르게 서 있습니다", "그녀는 당신에게 매우 화가 난것 같습니다.", "그녀는 당신과 싸우고 있습니다.", "그녀는 당신과 꼭 맞는 상대입니다!", "[ 머리 ]  투구"}, nil},
		{"나 봐", "look-self-enm-suffix", []string{"그녀는 당신에게 매우 화가 난것 같습니다.", "그녀는 당신과 싸우고 있습니다.", "[ 머리 ]  투구"}, nil},
		{"봐 늑대", "look-wolf-enm", []string{"당신은 늑대를 봅니다.", "그녀는 당신에게 매우 화가 난것 같습니다.", "그녀는 당신과 싸우고 있습니다.", "[ 무기 ]  검"}, nil},
		{"늑대 봐", "look-wolf-enm-suffix", []string{"회색 늑대다.", "그녀는 당신과 싸우고 있습니다.", "[ 무기 ]  검"}, nil},
		{"봐 Bob", "look-bob-enm", []string{"당신은 Bob님을 봅니다.", "그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"화가", "싸우고"}},
		{"Bob 봐", "look-bob-enm-suffix", []string{"그녀는 바르게 서 있습니다", "[  몸  ]  갑옷"}, []string{"화가", "싸우고"}},
	} {
		t.Run(tt.line, func(t *testing.T) {
			store := &departureStore{state: append([]byte(nil), raw...)}
			var owners Ownership
			lease, acquireErr := owners.Acquire("a")
			if acquireErr != nil {
				t.Fatal(acquireErr)
			}
			if admitErr := owners.Admit(lease, func() error { return nil }); admitErr != nil {
				t.Fatal(admitErr)
			}
			first := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if first.Replayed || store.commits != 1 {
				t.Fatalf("first=%+v commits=%d", first, store.commits)
			}
			var text string
			if unmarshalErr := json.Unmarshal(first.Response, &text); unmarshalErr != nil {
				t.Fatalf("text=%q err=%v", text, unmarshalErr)
			}
			for _, want := range tt.want {
				if !strings.Contains(text, want) || strings.Contains(text, "== 광장 ==") {
					t.Fatalf("text=%q missing %q", text, want)
				}
			}
			for _, not := range tt.not {
				if strings.Contains(text, not) {
					t.Fatalf("text=%q has forbidden %q", text, not)
				}
			}
			saved, decodeErr := world.DecodeState(store.state)
			if decodeErr != nil || saved.Players["a"].Body.RoomID != 1 {
				t.Fatalf("enm look moved actor: %+v err=%v", saved.Players["a"], decodeErr)
			}
			replay := executeParsedLookLine(t, &owners, store, lease, tt.id, tt.line)
			if !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
				t.Fatalf("replay=%+v commits=%d", replay, store.commits)
			}
		})
	}
}

func TestExecuteLookLineFailsClosedWithoutReceiptForNilEnemies(t *testing.T) {
	s := lookObjectCreatureCommandFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	owners.Admit(lease, func() error { return nil })
	if _, err := owners.ExecuteLookLine(context.Background(), store, "w", "look-nil-enm", lease, "봐 늑대", 12); !errors.Is(err, ErrUnsupportedLookLine) || store.commits != 0 {
		t.Fatalf("nil Enemies err=%v commits=%d", err, store.commits)
	}
}
