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

func dmEnemySessionFixture(t *testing.T, class byte) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor", "alice"},
				NPCIDs:    []string{"orc-first", "orc-second", "guardian"},
			},
		},
		Players: map[string]world.PlayerState{
			"actor": {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: class, RoomID: 1}, Online: true},
			"alice": {Body: world.LegacyMonster{Name: "Alice", Type: 0, Class: 4, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"orc-first": {
				Body:    world.LegacyMonster{Name: "오크", Type: 1, RoomID: 1},
				Enemies: []world.NPCEnemy{{Target: world.EntityRef{Kind: "player", ID: "alice"}}, {Target: world.EntityRef{Kind: "npc", ID: "guardian"}}},
			},
			"orc-second": {Body: world.LegacyMonster{Name: "오크", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
			"guardian":   {Body: world.LegacyMonster{Name: "수호자", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{},
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

func admitDMEnemySessionOwner(t *testing.T) (*Ownership, SessionLease) {
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

func TestParseDMEnemyLineAdmitsOnlyExactSuffixAliasesAndOccurrence(t *testing.T) {
	for _, test := range []struct {
		line       string
		verb       string
		target     string
		occurrence int
	}{
		{line: "오크 *enemy", verb: "*enemy", target: "오크", occurrence: 1},
		{line: "  오크 2 *적  ", verb: "*적", target: "오크", occurrence: 2},
	} {
		command, ok := ParseDMEnemyLine(test.line)
		if !ok || command.Verb != test.verb || command.Target != test.target || command.Occurrence != test.occurrence || !IsDMEnemyLine(test.line) {
			t.Fatalf("ParseDMEnemyLine(%q)=%+v ok=%t", test.line, command, ok)
		}
		parsed, err := ParseCommand(test.line)
		if err != nil || parsed.Kind != CommandDMEnemy {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", test.line, parsed, err)
		}
	}
	for _, line := range []string{
		"*enemy", "*적", "오크", "*enemy 오크", "오크 *ene", "오크 *enemy extra", "extra 오크 *enemy",
		"오크 0 *enemy", "오크 -1 *enemy", "오크 +1 *enemy", "오크 1 2 *enemy", "오크 ' *enemy", "\"오크\" *enemy",
		"오크 *enemy\n", "오크 *enemy\u00a0", string([]byte{0xff}),
	} {
		if _, ok := ParseDMEnemyLine(line); ok || IsDMEnemyLine(line) {
			t.Fatalf("accepted malformed DM enemy line %q", line)
		}
		parsed, err := ParseCommand(line)
		if err != nil {
			continue
		}
		if parsed.Kind == CommandDMEnemy {
			t.Fatalf("ParseCommand(%q) routed malformed line: %+v", line, parsed)
		}
	}
	parsed, err := ParseCommand("*active")
	if err != nil || parsed.Kind != CommandDMActive {
		t.Fatalf("DMActive regression: %+v err=%v", parsed, err)
	}
}

func TestExecuteDMEnemyLineIsReadOnlyAndReplaysExactly(t *testing.T) {
	store := &departureStore{state: dmEnemySessionFixture(t, 12)}
	initial := append([]byte(nil), store.state...)
	owners, lease := admitDMEnemySessionOwner(t)

	first, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-1", lease, "오크 *enemy")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("first=%+v err=%v commits=%d stateChanged=%t", first, err, store.commits, !bytes.Equal(store.state, initial))
	}
	var result world.DMEnemyResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMEnemyList || result.TargetID != "orc-first" || !reflect.DeepEqual(result.EnemyNames, []string{"Alice", "수호자"}) || result.Response != "오크의 적들:\nAlice.\n수호자.\n" || result.Changed {
		t.Fatalf("result=%+v", result)
	}

	replay, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-1", lease, "오크 *enemy")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) || !bytes.Equal(store.state, initial) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDMEnemyLineUnauthorizedIsUnknownAndMalformedFailsBeforeReceipt(t *testing.T) {
	store := &departureStore{state: dmEnemySessionFixture(t, 4)}
	initial := append([]byte(nil), store.state...)
	owners, lease := admitDMEnemySessionOwner(t)

	first, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-mortal", lease, "오크 *적")
	if err != nil || first.Replayed || store.commits != 1 || !bytes.Equal(store.state, initial) {
		t.Fatalf("unauthorized=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMEnemyResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMEnemyUnknown || result.Response != world.DMEnemyUnknownResponse("*적") || result.TargetID != "" || len(result.EnemyNames) != 0 {
		t.Fatalf("unauthorized result=%+v", result)
	}
	replay, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-mortal", lease, "오크 *적")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("unauthorized replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
	if _, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-bad", lease, "오크 *enemy extra"); !errors.Is(err, ErrUnsupportedDMEnemyLine) || store.commits != 1 {
		t.Fatalf("malformed err=%v commits=%d", err, store.commits)
	}
}

func TestExecuteDMEnemyLineRejectsMissingTargetAndUnresolvedRelationsBeforeReceipt(t *testing.T) {
	owners, lease := admitDMEnemySessionOwner(t)
	for _, test := range []struct {
		name  string
		line  string
		state func(t *testing.T) []byte
	}{
		{name: "missing target", line: "*enemy", state: func(t *testing.T) []byte { return dmEnemySessionFixture(t, 12) }},
		{name: "unresolved relation", line: "오크 *enemy", state: func(t *testing.T) []byte {
			raw := dmEnemySessionFixture(t, 12)
			var s world.State
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatal(err)
			}
			npc := s.NPCs["orc-first"]
			npc.Enemies = nil
			s.NPCs["orc-first"] = npc
			out, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &departureStore{state: test.state(t)}
			_, err := owners.ExecuteDMEnemyLine(context.Background(), store, "w", "dm-enemy-fail-"+test.name, lease, test.line)
			if test.name == "missing target" {
				if !errors.Is(err, ErrUnsupportedDMEnemyLine) {
					t.Fatalf("missing target err=%v", err)
				}
			} else if err == nil {
				t.Fatal("unresolved relation unexpectedly committed")
			}
			if store.commits != 0 || store.receipt != nil {
				t.Fatalf("failed command left receipt commits=%d receipt=%+v", store.commits, store.receipt)
			}
		})
	}

	// Keep the direct parser contract explicit: quoted/ambiguous input never
	// reaches ExecuteGame, so a store cannot observe a receipt attempt.
	if _, ok := ParseDMEnemyLine(strings.TrimSpace("오크 1 *enemy extra")); ok {
		t.Fatal("extra token accepted")
	}
}
