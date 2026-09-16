package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func castSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[0] = 1 << 0 // SVIGOR / 회복
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"a"},
		}},
		Players: map[string]world.PlayerState{"a": {
			Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: world.ClericClass, Level: 8, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18},
				HPMax: 100, HPCurrent: 10, MPMax: 50, MPCurrent: 30,
				Spells: spells,
			},
			Online: true,
		}},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestParseCastLineKeepsSelfTargetBoundary(t *testing.T) {
	tests := []struct {
		line      string
		kind      CommandKind
		spellName string
		target    string
	}{
		{line: "주문", kind: CommandCast},
		{line: "주문 회복", kind: CommandCast, spellName: "회복"},
		{line: "주문 원기회복", kind: CommandCast, spellName: "원기회복"},
		{line: "주문 천리안", kind: CommandCast, spellName: "천리안"},
		{line: "주문 천리안 Bob", kind: CommandCast, spellName: "천리안", target: "Bob"},
		{line: "주문 천리 bob", kind: CommandCast, spellName: "천리", target: "bob"},
		{line: "주문 소환", kind: CommandCast, spellName: "소환"},
		{line: "주문 소환 Bob", kind: CommandCast, spellName: "소환", target: "Bob"},
		{line: "주문 소 bob", kind: CommandCast, spellName: "소", target: "bob"},
		{line: "주문 귀환", kind: CommandCast, spellName: "귀환"},
		{line: "주문 귀", kind: CommandCast, spellName: "귀"},
		{line: "주문 귀환 Bob", kind: CommandCast, spellName: "귀환", target: "Bob"},
		{line: "주문 귀 Bob", kind: CommandCast, spellName: "귀", target: "Bob"},
	}
	for _, tc := range tests {
		parsed, err := ParseCommand(tc.line)
		if err != nil || parsed.Kind != tc.kind {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", tc.line, parsed, err)
		}
		command, ok := ParseCastLine(tc.line)
		if !ok || command.SpellName != tc.spellName || command.Target != tc.target || (command.Target != "" && command.Occurrence != 1) {
			t.Fatalf("ParseCastLine(%q)=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{"주문 회복 Alice", "주문 완치 Bob", "주문 천리안 Bob extra", "주문\n회복", "주문\x00"} {
		if IsCastLine(line) {
			t.Fatalf("unsupported cast form accepted: %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandUnknown {
			t.Fatalf("malformed cast form=%+v err=%v", parsed, err)
		}
	}
}

func TestParseCastLineRecallTargetOccurrenceIsBounded(t *testing.T) {
	tests := []struct {
		line       string
		target     string
		occurrence int
	}{
		{line: "주문 귀환 Bob", target: "Bob", occurrence: 1},
		{line: "주문 귀 Bob 2", target: "Bob", occurrence: 2},
		{line: "주문 귀환 Bob 2147483647", target: "Bob", occurrence: 2147483647},
	}
	for _, tc := range tests {
		command, ok := ParseCastLine(tc.line)
		if !ok || command.SpellName == "" || command.Target != tc.target || command.Occurrence != tc.occurrence {
			t.Fatalf("ParseCastLine(%q)=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{
		"주문 귀환 Bob 0",
		"주문 귀환 Bob -1",
		"주문 귀환 Bob +1",
		"주문 귀환 Bob 1.0",
		"주문 귀환 Bob nope",
		"주문 귀환 Bob 2147483648",
		"주문 귀환 Bob 999999999999999999999999",
		"주문 귀환 2",
		"주문 귀환 -1",
		"주문 귀환",
		"주문 귀환 Bob 2 extra",
		`주문 귀환 "Bob Foo"`,
		`주문 귀환 "Bob Foo" 2`,
		"주문 귀환 Bob\n2",
		"주문 귀환 Bob\x00 2",
		"주문 귀환 Bob\x1b 2",
	} {
		if line == "주문 귀환" {
			if command, ok := ParseCastLine(line); !ok || command.Target != "" {
				t.Fatalf("self recall boundary changed: %+v ok=%v", command, ok)
			}
			continue
		}
		if command, ok := ParseCastLine(line); ok {
			t.Fatalf("invalid recall occurrence accepted: %q -> %+v", line, command)
		}
		store := &departureStore{state: recallSessionFixture(t)}
		var owners Ownership
		lease, err := owners.Acquire("a")
		if err != nil {
			t.Fatal(err)
		}
		if err := owners.Admit(lease, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := owners.ExecuteCastLine(context.Background(), store, "w", "recall-invalid-"+line, lease, line, 100, 12, nil); !errors.Is(err, ErrUnsupportedCastLine) || store.commits != 0 {
			t.Fatalf("invalid recall reached receipt line=%q err=%v commits=%d", line, err, store.commits)
		}
	}
}

func TestExecuteCastLinePersistsResponseAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: castSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-1", lease, "주문 회복", 100, 12, func(low, high int) int {
		calls++
		if low != 1 || high < low {
			t.Fatalf("unexpected cast random bounds %d..%d", low, high)
		}
		return high
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 2 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "회복" || result.HPDelta <= 0 || !strings.Contains(result.Response, "회복 주문") {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.MPCurrent != 28 || saved.Players["a"].Body.HPCurrent != 22 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex].LastTime != 100 {
		t.Fatalf("saved body=%+v", saved.Players["a"].Body)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-1", lease, "주문 회복", 200, 0, func(int, int) int {
		t.Fatal("cast RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteCastLineRejectsTargetFormBeforeReceipt(t *testing.T) {
	store := &departureStore{state: castSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteCastLine(context.Background(), store, "w", "cast-target", lease, "주문 회복 Alice", 100, 12, func(int, int) int { return 1 }); err == nil || store.commits != 0 {
		t.Fatalf("target form reached receipt: err=%v commits=%d", err, store.commits)
	}
}

func TestParseCastLineKnowAlignmentAliasAndOccurrence(t *testing.T) {
	for _, tc := range []struct {
		line       string
		spell      string
		target     string
		occurrence int
	}{
		{line: "주문 선악감지 Bob", spell: "선악감지", target: "Bob", occurrence: 1},
		{line: "주문 선악 Bob 2", spell: "선악", target: "Bob", occurrence: 2},
		{line: "주문 선악감지 Bob 2147483647", spell: "선악감지", target: "Bob", occurrence: 2147483647},
	} {
		command, ok := ParseCastLine(tc.line)
		if !ok || command.SpellName != tc.spell || command.Target != tc.target || command.Occurrence != tc.occurrence {
			t.Fatalf("ParseCastLine(%q)=%+v ok=%v", tc.line, command, ok)
		}
	}
	for _, line := range []string{
		"주문 선악감지 2",
		"주문 선악감지 Bob 0",
		"주문 선악감지 Bob -1",
		"주문 선악감지 Bob +1",
		"주문 선악감지 Bob nope",
		"주문 선악감지 Bob 2147483648",
		"주문 선악감지 Bob 2 extra",
		`주문 선악감지 "Bob Foo"`,
	} {
		if command, ok := ParseCastLine(line); ok {
			t.Fatalf("invalid know-alignment target accepted: %q -> %+v", line, command)
		}
	}
}

func knowAlignmentSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[41/8] |= 1 << uint(41%8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"a", "b", "c"},
		}},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{
				Name: "Alice", Type: 0, Class: world.ClericClass, Level: 8, RoomID: 1,
				Stats: [5]byte{12, 12, 12, 18, 18}, MPMax: 50, MPCurrent: 30, Spells: spells,
			}, Online: true},
			"b": {Body: world.LegacyMonster{Name: "Bob", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
			"c": {Body: world.LegacyMonster{Name: "Bobby", Type: 0, Class: 4, Level: 4, RoomID: 1}, Online: true},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteCastLineKnowAlignmentPersistsAndReplaysWithoutRNG(t *testing.T) {
	store := &departureStore{state: knowAlignmentSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "know-1", lease, "주문 선악 Bob 2", 100, 12, func(int, int) int {
		t.Fatal("know-alignment consumed RNG")
		return 0
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.TargetID != "c" || result.TargetName != "Bobby" || result.TargetText == "" || result.Event == nil || result.Event.ExcludeTargetID != "c" {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.MPCurrent != 24 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex] != (world.LegacyTimer{LastTime: 100, Interval: 3}) || !world.PlayerFlagSet(saved.Players["c"].Body, 33) || saved.Players["c"].Body.Timers[27] != (world.LegacyTimer{LastTime: 100, Interval: 2400}) {
		t.Fatalf("saved actor=%+v target=%+v", saved.Players["a"].Body, saved.Players["c"].Body)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "know-1", lease, "주문 선악 Bob 2", 200, 0, func(int, int) int {
		t.Fatal("know-alignment replay consumed RNG")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func locateSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[46/8] |= 1 << (46 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"a"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "광장"}}, PlayerIDs: []string{"b"}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 5, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 50, MPCurrent: 30,
					Spells: spells,
				},
				Online: true,
			},
			"b": {
				Body: world.LegacyMonster{
					Name: "Bob", Type: 0, Class: 4, Level: 8, RoomID: 2,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 80, HPCurrent: 80,
				},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteCastLineLocatePlayerPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: locateSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "locate-1", lease, "주문 천리안 Bob", 100, 12, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected locate bounds %d..%d", low, high)
		}
		return 1
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 3 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "천리안" || result.TargetID != "b" || !result.LocateLinked || result.Event == nil || result.Hour != 12 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, "마음을 Bob에게 집중") || !strings.Contains(result.Response, "광장") {
		t.Fatalf("response=%q", result.Response)
	}
	if !strings.Contains(result.TargetText, "주위를 보고 있습니다") {
		t.Fatalf("target text=%q", result.TargetText)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.MPCurrent != 15 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex].LastTime != 100 {
		t.Fatalf("saved body=%+v", saved.Players["a"].Body)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "locate-1", lease, "주문 천리안 Bob", 200, 0, func(int, int) int {
		t.Fatal("locate RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func locateRDARKNSessionFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(locateSessionFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	room := s.Rooms[2]
	room.Resource.Flags[1] |= 1 << 1 // RF 9 / RDARKN
	s.Rooms[2] = room
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteCastLinePassesHourIntoLocateRDARKN(t *testing.T) {
	store := &departureStore{state: locateRDARKNSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteCastLine(context.Background(), store, "w", "locate-night", lease, "주문 천리안 Bob", 100, 0, func(int, int) int { return 1 }); !errors.Is(err, world.ErrCastSpellUnavailable) || store.commits != 0 {
		t.Fatalf("hour 0 err=%v commits=%d", err, store.commits)
	}

	day := &departureStore{state: locateRDARKNSessionFixture(t)}
	var dayOwners Ownership
	dayLease, err := dayOwners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := dayOwners.Admit(dayLease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := dayOwners.ExecuteCastLine(context.Background(), day, "w", "locate-day", dayLease, "주문 천리안 Bob", 100, 12, func(int, int) int { return 1 })
	if err != nil || first.Replayed || day.commits != 1 {
		t.Fatalf("hour 12 first=%+v err=%v commits=%d", first, err, day.commits)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Hour != 12 || !result.Succeeded || !strings.Contains(result.Response, "광장") || strings.Contains(result.Response, "너무 어두워서") {
		t.Fatalf("result=%+v response=%q", result, result.Response)
	}
	if result.TargetID != "b" || !strings.Contains(result.TargetText, "주위를 보고 있습니다") {
		t.Fatalf("target=%q text=%q", result.TargetID, result.TargetText)
	}
}

func summonSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[17/8] |= 1 << (17 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"a"}},
			2: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 2, Name: "광장"}}, PlayerIDs: []string{"b"}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: 5, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 80, MPCurrent: 80,
					Spells: spells,
				},
				Online: true,
			},
			"b": {
				Body: world.LegacyMonster{
					Name: "Bob", Type: 0, Class: 4, Level: 8, RoomID: 2,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 80, HPCurrent: 80,
				},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteCastLineSummonPersistsMoveAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: summonSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "summon-1", lease, "주문 소환 Bob", 100, 12, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("unexpected summon bounds %d..%d", low, high)
		}
		return 51
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 || calls != 1 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "소환" || result.TargetID != "b" || result.Event == nil || result.MPDelta != -50 {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, "Bob을 소환") || !strings.Contains(result.TargetText, "Alice이 당신앞에") {
		t.Fatalf("response=%q target=%q", result.Response, result.TargetText)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["b"].Body.RoomID != 1 || saved.Players["a"].Body.MPCurrent != 30 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex].LastTime != 100 {
		t.Fatalf("saved a=%+v b=%+v", saved.Players["a"].Body, saved.Players["b"].Body)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "summon-1", lease, "주문 소환 Bob", 200, 0, func(int, int) int {
		t.Fatal("summon RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func recallSessionFixture(t *testing.T) []byte {
	t.Helper()
	var spells [16]byte
	spells[16/8] |= 1 << (16 % 8)
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1:    {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "숲"}}, PlayerIDs: []string{"a"}},
			1001: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1001, Name: "광장"}}, PlayerIDs: []string{}},
		},
		Players: map[string]world.PlayerState{
			"a": {
				Body: world.LegacyMonster{
					Name: "Alice", Type: 0, Class: world.ClericClass, Level: 8, RoomID: 1,
					Stats: [5]byte{12, 12, 12, 18, 18},
					HPMax: 100, HPCurrent: 100, MPMax: 80, MPCurrent: 40,
					Spells: spells,
				},
				Online: true,
			},
		},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecuteCastLineRecallPersistsMoveAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: recallSessionFixture(t)}
	var owners Ownership
	lease, err := owners.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteCastLine(context.Background(), store, "w", "recall-1", lease, "주문 귀환", 100, 12, func(int, int) int {
		t.Fatal("recall RNG invoked")
		return 0
	})
	if err != nil || first.Replayed || first.Revision != 1 || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.CastResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Succeeded || result.SpellName != "귀환" || result.MPDelta != -30 || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Response, "귀환 주문을 외웠습니다") || !strings.Contains(result.Response, "광장") {
		t.Fatalf("response=%q", result.Response)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Players["a"].Body.RoomID != 1001 || saved.Players["a"].Body.MPCurrent != 10 || saved.Players["a"].Body.Timers[world.CastSpellTimerIndex].LastTime != 100 {
		t.Fatalf("saved=%+v", saved.Players["a"].Body)
	}
	if !containsPlayer(saved.Rooms[1001].PlayerIDs, "a") || containsPlayer(saved.Rooms[1].PlayerIDs, "a") {
		t.Fatalf("occupancy dest=%v source=%v", saved.Rooms[1001].PlayerIDs, saved.Rooms[1].PlayerIDs)
	}
	replay, err := owners.ExecuteCastLine(context.Background(), store, "w", "recall-1", lease, "주문 귀환", 200, 0, func(int, int) int {
		t.Fatal("recall RNG replayed")
		return 0
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func containsPlayer(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
