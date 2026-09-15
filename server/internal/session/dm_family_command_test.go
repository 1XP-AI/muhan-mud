package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

type dmFamilySessionCatalog struct{}

func (dmFamilySessionCatalog) Object(id int16) (world.LegacyObject, error) {
	return world.LegacyObject{Name: "초인의 돌", Weight: 1, ShotsMax: 10, ShotsCurrent: 10}, nil
}

func (dmFamilySessionCatalog) Monster(int16) (world.LegacyMonster, error) {
	return world.LegacyMonster{
		Name: "침략자", Type: 1, Class: 4, Level: 1, Armor: 100,
		HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4,
	}, nil
}

func TestParseDMFamilyLine(t *testing.T) {
	if command, ok := ParseDMFamilyLine("*떨어져라"); !ok || command.Action != world.DMFamilyMoonstone {
		t.Fatalf("moonstone=%+v ok=%t", command, ok)
	}
	if command, ok := ParseDMFamilyLine("*침공"); !ok || command.Action != world.DMFamilyInvasion {
		t.Fatalf("invasion=%+v ok=%t", command, ok)
	}
	if _, ok := ParseDMFamilyLine("*떨어"); ok || IsDMFamilyLine("떨어져라") {
		t.Fatal("prefix accepted")
	}
}

func TestParseCommandClassifiesDMFamily(t *testing.T) {
	for _, line := range []string{"*떨어져라", "*침공"} {
		parsed, err := ParseCommand(line)
		if err != nil || parsed.Kind != CommandDMFamily {
			t.Fatalf("ParseCommand(%q)=%+v err=%v", line, parsed, err)
		}
	}
}

func TestExecuteDMFamilyMoonstoneReplays(t *testing.T) {
	dm := world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
			PlayerIDs: []string{"dm"},
			Items:     &world.ItemCollection{Items: map[string]world.Item{}},
		}},
		Players: map[string]world.PlayerState{"dm": {Body: dm, Online: true}},
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
	n := 0
	opts := DMFamilyOptions{
		Catalog: dmFamilySessionCatalog{},
		Roll:    func(lo, hi int) int { return lo },
		Allocate: func() (string, error) {
			n++
			return "stone-1", nil
		},
	}
	first, err := owners.ExecuteDMFamilyLine(context.Background(), store, "w", "dm-moon-1", lease, "*떨어져라", opts)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMFamilyResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.ItemID != "stone-1" {
		t.Fatalf("result=%+v", result)
	}
	replay, err := owners.ExecuteDMFamilyLine(context.Background(), store, "w", "dm-moon-1", lease, "*떨어져라", opts)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteDMFamilyInvasionReplaysCombatReadyNPCs(t *testing.T) {
	dm := world.LegacyMonster{Name: "운영자", Type: 0, Class: 12, RoomID: 1}
	attacker := world.LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, Level: 1, RoomID: 3601,
		Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40,
		DiceCount: 1, DiceSides: 5,
	}
	state := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"dm"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
			3601: {
				Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 3601, Name: "무적존"}},
				PlayerIDs: []string{"fighter"},
				Items:     &world.ItemCollection{Items: map[string]world.Item{}},
			},
		},
		Players: map[string]world.PlayerState{
			"dm":      {Body: dm, Online: true},
			"fighter": {Body: attacker, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs:         map[string]world.NPCState{},
		ActiveNPCIDs: []string{},
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
	n := 0
	opts := DMFamilyOptions{
		Catalog: dmFamilySessionCatalog{},
		Roll:    func(lo, hi int) int { return lo },
		Allocate: func() (string, error) {
			n++
			return fmt.Sprintf("inv-%d", n), nil
		},
	}
	first, err := owners.ExecuteDMFamilyLine(context.Background(), store, "w", "dm-invade-1", lease, "*침공", opts)
	if err != nil || first.Replayed || store.commits != 1 {
		t.Fatalf("first=%+v err=%v commits=%d", first, err, store.commits)
	}
	var result world.DMFamilyResult
	if err := json.Unmarshal(first.Response, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.NPCIDs) != 10 {
		t.Fatalf("result=%+v", result)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range result.NPCIDs {
		npc, ok := saved.NPCs[id]
		if !ok || npc.Enemies == nil {
			t.Fatalf("persisted NPC %q enemies=%v ok=%t", id, npc.Enemies, ok)
		}
	}
	if _, _, err := saved.PlanNPCMeleeAttack("fighter", result.NPCIDs[0], func(_, hi int) int { return hi }); err != nil {
		t.Fatalf("melee against persisted invasion NPC: %v", err)
	}
	replay, err := owners.ExecuteDMFamilyLine(context.Background(), store, "w", "dm-invade-1", lease, "*침공", opts)
	if err != nil || !replay.Replayed || store.commits != 1 || !bytes.Equal(replay.Response, first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}
