package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func dmFollowCommandFixture(t *testing.T) []byte {
	t.Helper()
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm"},
				NPCIDs:    []string{"wolf-1"},
			},
		},
		Players: map[string]world.PlayerState{
			"dm": {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf-1": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}},
		},
		ActiveNPCIDs: []string{},
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

func TestParseDMFollowLineAdmitsSuffixForms(t *testing.T) {
	bare, ok := ParseDMFollowLine("  *따르기  ")
	if !ok || bare.Verb != "*따르기" || bare.NPCName != "" || bare.Occurrence != 1 || !IsDMFollowLine("*따르기") {
		t.Fatalf("bare=%+v ok=%t", bare, ok)
	}
	named, ok := ParseDMFollowLine("늑대 *따르기")
	if !ok || named.NPCName != "늑대" || named.Occurrence != 1 || named.Verb != "*따르기" {
		t.Fatalf("named=%+v ok=%t", named, ok)
	}
	occ, ok := ParseDMFollowLine("오크 2 *cfollow")
	if !ok || occ.NPCName != "오크" || occ.Occurrence != 2 || occ.Verb != "*cfollow" {
		t.Fatalf("occurrence=%+v ok=%t", occ, ok)
	}
	for _, line := range []string{
		"*따르기 늑대", "*따", "늑대", "오크 0 *따르기", "오크 -1 *cfollow", "*따르기\n", string([]byte{0xff}),
	} {
		if _, ok := ParseDMFollowLine(line); ok || IsDMFollowLine(line) {
			t.Fatalf("accepted %q", line)
		}
	}
}

func TestParseCommandClassifiesDMFollow(t *testing.T) {
	for _, line := range []string{"*따르기", "늑대 *따르기", "오크 2 *cfollow"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDMFollow {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
	parsed, err := ParseCommand("*따르기 늑대")
	if err != nil || parsed.Kind != CommandUnknown {
		t.Fatalf("prefix=%+v err=%v", parsed, err)
	}
}

func TestExecuteDMFollowLineAttachesDetachesAndReplays(t *testing.T) {
	store := &departureStore{state: dmFollowCommandFixture(t)}
	owners := &Ownership{}
	lease, err := owners.Acquire("dm")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-1", lease, "늑대 *따르기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMFollowResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMFollowAttach || result.NPCID != "wolf-1" || !result.Changed {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := saved.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "dm" || !world.PlayerFlagSet(npc.Body, 46) {
		t.Fatalf("saved npc=%+v", npc)
	}
	replay, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-1", lease, "늑대 *따르기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	second, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-2", lease, "늑대 *cfollow")
	if err != nil || second.Replayed || store.commits != 2 {
		t.Fatalf("detach=%+v err=%v commits=%d", second, err, store.commits)
	}
	if err := json.Unmarshal(second.Response, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != world.DMFollowDetach || result.Changed == false {
		t.Fatalf("detach result=%+v", result)
	}
	if _, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-bad", lease, "*따르기 늑대"); !errors.Is(err, ErrUnsupportedDMFollowLine) {
		t.Fatalf("unsupported err=%v", err)
	}
}

func TestExecuteDMFollowDepartClearsMDMFOLAndReplays(t *testing.T) {
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm"},
				NPCIDs:    []string{"wolf-1"},
			},
		},
		Players: map[string]world.PlayerState{
			"dm": {Body: world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf-1": {Body: world.LegacyMonster{Name: "늑대", Type: 1, RoomID: 1}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{},
	}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &departureStore{state: raw}
	owners := &Ownership{}
	lease, err := owners.Acquire("dm")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	first, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-logout-1", lease, "늑대 *따르기")
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("attach=%+v err=%v commits=%d", first, err, store.commits)
	}
	replay, err := owners.ExecuteDMFollowLine(context.Background(), store, "w", "dm-follow-logout-1", lease, "늑대 *따르기")
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}

	depart, err := owners.Depart(context.Background(), store, "w", "dm-follow-depart-1", lease)
	if err != nil || depart.Replayed || store.commits != 2 || owners.Owns(lease) {
		t.Fatalf("depart=%+v err=%v commits=%d", depart, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := saved.NPCs["wolf-1"]
	if npc.FollowingPlayerID != "" || world.PlayerFlagSet(npc.Body, 46) || saved.Players["dm"].Online || saved.Players["dm"].NPCFollowerIDs != nil {
		t.Fatalf("logout state npc=%+v player=%+v", npc, saved.Players["dm"])
	}

	next, err := owners.Acquire("dm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owners.Depart(context.Background(), store, "w", "dm-follow-depart-1", lease); err == nil || !owners.Owns(next) || store.commits != 2 {
		t.Fatalf("stale depart recommitted commits=%d err=%v", store.commits, err)
	}
}
