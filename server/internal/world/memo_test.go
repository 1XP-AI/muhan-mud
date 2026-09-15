package world

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func memoWorldFixture(imported, targetOnline bool) State {
	state := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"actor":  {Body: LegacyMonster{Name: "Alice", RoomID: 1}, Online: true},
			"target": {Body: LegacyMonster{Name: "Bob", RoomID: 2}, Online: targetOnline},
		},
	}
	if targetOnline {
		room := state.Rooms[2]
		room.PlayerIDs = []string{"target"}
		state.Rooms[2] = room
	}
	if imported {
		state.Memos = map[string][]CharacterMemo{}
	}
	return state
}

func TestMemoStateNilIsPreMigrationAndDoesNotMaterialize(t *testing.T) {
	state := memoWorldFixture(false, false)
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := state.PlanMemo("actor", "Bob", "hello", time.Unix(10, 0)); !errors.Is(err, ErrMemoStateUnresolved) {
		t.Fatalf("PlanMemo err=%v, want unresolved", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeState(raw)
	if err != nil || decoded.Memos != nil {
		t.Fatalf("decoded memos=%#v err=%v", decoded.Memos, err)
	}
}

func TestMemoAppendsToOfflineCanonicalTargetAndPreservesIdentity(t *testing.T) {
	state := memoWorldFixture(true, false)
	now := time.Date(2026, 9, 10, 1, 2, 3, 0, time.FixedZone("KST", 9*60*60))
	proposal, err := state.PlanMemoWithOptions("actor", "bOB", "hello 세계", MemoOptions{Now: now, ID: "memo-1"})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := state.ApplyMemo(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if result.Response != MemoResponse || result.TargetID != "target" || result.TargetName != "Bob" || !result.Changed {
		t.Fatalf("result=%+v", result)
	}
	records := next.Memos["target"]
	if len(records) != 1 || records[0].SenderID != "actor" || records[0].SenderName != "Alice" || records[0].Body != "hello 세계" || !records[0].CreatedAt.Equal(now.UTC()) {
		t.Fatalf("records=%+v", records)
	}
	if state.Memos != nil && len(state.Memos["target"]) != 0 {
		t.Fatal("planning mutated source state")
	}
}

func TestMemoResolvesOnlyExactCanonicalNameAndRejectsDuplicates(t *testing.T) {
	state := memoWorldFixture(true, false)
	if _, err := state.ResolveMemoTargetID("Bo"); !errors.Is(err, ErrMemoTargetUnavailable) {
		t.Fatalf("prefix err=%v", err)
	}
	duplicate := state.Players["target"]
	duplicate.Body.Name = "Bob"
	state.Players["other"] = duplicate
	if _, err := state.ResolveMemoTargetID("bob"); !errors.Is(err, ErrMemoTargetAmbiguous) {
		t.Fatalf("duplicate err=%v", err)
	}
}

func TestMemoRequiresOnlineCanonicalActorButAllowsOnlineTarget(t *testing.T) {
	state := memoWorldFixture(true, true)
	actor := state.Players["actor"]
	actor.Online = false
	state.Players["actor"] = actor
	state.Rooms[1] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}}
	if _, err := state.PlanMemo("actor", "Bob", "hello", time.Unix(10, 0)); !errors.Is(err, ErrMemoActorAbsent) {
		t.Fatalf("offline actor err=%v", err)
	}
}

func TestMemoBodyBoundsRejectInvalidText(t *testing.T) {
	for name, body := range map[string]string{
		"empty":        "",
		"whitespace":   "   ",
		"control":      "hello\x1b[31m",
		"line feed":    "hello\nworld",
		"too long":     strings.Repeat("a", MaxMemoBodyBytes+1),
		"invalid utf8": string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateMemoBody(body); err == nil {
				t.Fatalf("ValidateMemoBody(%q) accepted", body)
			}
		})
	}
	if err := ValidateMemoBody(strings.Repeat("가", MaxMemoBodyBytes/3)); err != nil {
		t.Fatalf("valid UTF-8 body rejected: %v", err)
	}
}

func TestMemoValidationRejectsDanglingOrDuplicateRecords(t *testing.T) {
	base := memoWorldFixture(true, false)
	base.Memos["target"] = []CharacterMemo{{ID: "memo-1", SenderID: "actor", SenderName: "Alice", Body: "hello", CreatedAt: time.Unix(1, 0)}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*State){
		"dangling recipient":      func(s *State) { s.Memos["missing"] = nil },
		"dangling sender":         func(s *State) { s.Memos["target"][0].SenderID = "missing" },
		"sender display mismatch": func(s *State) { s.Memos["target"][0].SenderName = "Mallory" },
		"duplicate id":            func(s *State) { s.Memos["target"] = append(s.Memos["target"], s.Memos["target"][0]) },
		"unsafe body":             func(s *State) { s.Memos["target"][0].Body = "bad\x00body" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := memoWorldFixture(true, false)
			bad.Memos["target"] = append([]CharacterMemo(nil), base.Memos["target"]...)
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatal("invalid memo state accepted")
			}
		})
	}
}

func TestMemoApplyRejectsStaleProposalWithoutMutation(t *testing.T) {
	state := memoWorldFixture(true, false)
	proposal, err := state.PlanMemoWithOptions("actor", "Bob", "hello", MemoOptions{Now: time.Unix(10, 0), ID: "memo-1"})
	if err != nil {
		t.Fatal(err)
	}
	state.Memos["target"] = []CharacterMemo{{ID: "other", SenderID: "actor", SenderName: "Alice", Body: "other", CreatedAt: time.Unix(2, 0)}}
	before := state.Memos["target"]
	if _, _, err := state.ApplyMemo(proposal); !errors.Is(err, ErrMemoStaleProposal) {
		t.Fatalf("stale err=%v", err)
	}
	if !reflect.DeepEqual(state.Memos["target"], before) {
		t.Fatal("stale apply mutated source")
	}
}
