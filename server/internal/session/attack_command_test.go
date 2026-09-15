package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func attackCommandFixture() []byte {
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{
			1: {Resource: world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"a"}, NPCIDs: []string{"wolf-id"}, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		Players: map[string]world.PlayerState{
			"a": {Body: world.LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Level: 1, Stats: [5]byte{10, 10, 10, 10, 10}, HPMax: 100, HPCurrent: 40, DiceCount: 1, DiceSides: 5}, Online: true, Items: &world.ItemCollection{Items: map[string]world.Item{}}},
		},
		NPCs: map[string]world.NPCState{
			"wolf-id": {Body: world.LegacyMonster{Name: "늑대", RoomID: 1, Type: 1, Class: 4, Level: 1, Armor: 100, HPMax: 50, HPCurrent: 50, DiceCount: 1, DiceSides: 4}, Enemies: []world.NPCEnemy{}},
		},
	}
	raw, _ := json.Marshal(s)
	return raw
}

func TestExecuteAttackLinePersistsNPCDamageAndReplays(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-1", lease, "공격 늑대", func(_, hi int) int { return hi })
	if err != nil || !strings.Contains(string(first.Response), "5 만큼의 피해") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.NPCs["wolf-id"].Body.HPCurrent != 45 || len(saved.NPCs["wolf-id"].Enemies) != 1 {
		t.Fatalf("state=%+v err=%v", saved.NPCs["wolf-id"], err)
	}
	replay, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-1", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func lethalAttackCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf-id"]
	npc.Body.HPCurrent = 1
	npc.Body.Experience = 100
	s.NPCs["wolf-id"] = npc
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func lethalDropAttackCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(lethalAttackCommandFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	r := s.Rooms[1]
	r.Items = &world.ItemCollection{Items: map[string]world.Item{}}
	s.Rooms[1] = r
	npc := s.NPCs["wolf-id"]
	npc.Body.Inventory = []world.LegacyObject{{Name: "검", Value: 7}}
	npc.Body.Gold = 12
	s.NPCs["wolf-id"] = npc
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func weaponDropAttackCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	p.Items = &world.ItemCollection{
		Items: map[string]world.Item{
			"sword": {Object: world.LegacyObject{Name: "검", Type: 0, Wear: 20, DiceCount: 1, DiceSides: 4, ShotsMax: 5, ShotsCurrent: 5}},
		},
		Ready: [20]string{19: "sword"},
	}
	s.Players["a"] = p
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func powerDamageAttackCommandFixture(t *testing.T) []byte {
	t.Helper()
	s, err := world.DecodeState(attackCommandFixture())
	if err != nil {
		t.Fatal(err)
	}
	p := s.Players["a"]
	p.Body.Class = 10
	p.Body.Level = 128
	p.Body.Flags[59/8] |= 1 << (59 % 8) // PUPDMG
	s.Players["a"] = p
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func powerDamageRoll(t *testing.T) func(int, int) int {
	t.Helper()
	return func(lo, hi int) int {
		switch {
		case lo == 0 && hi == 3:
			return 0
		case lo == 1 && hi == 4:
			return 2
		case lo == 1 && hi == 30, lo == 1 && hi == 5:
			return hi
		case lo == 1 && hi == 100:
			return 100
		default:
			t.Fatalf("unexpected attack random request %d..%d", lo, hi)
			return 0
		}
	}
}

func weaponDropRoll(t *testing.T) func(int, int) int {
	t.Helper()
	calls := 0
	return func(lo, hi int) int {
		switch {
		case lo == 1 && hi == 30:
			return hi
		case lo == 1 && hi == 4:
			return hi
		case lo == 1 && hi == 100:
			calls++
			if calls == 1 {
				return 100
			}
			return 1
		default:
			t.Fatalf("unexpected attack random request %d..%d", lo, hi)
			return 0
		}
	}
}

func TestExecuteAttackLineCommitsLethalNPCDeathAndReplays(t *testing.T) {
	store := &departureStore{state: lethalAttackCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-lethal", lease, "공격 늑대", func(_, hi int) int { return hi })
	if err != nil || !strings.Contains(string(first.Response), "죽였습니다") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || len(saved.Rooms[1].NPCIDs) != 0 {
		t.Fatalf("saved state=%+v err=%v", saved.Rooms[1].NPCIDs, err)
	}
	if _, exists := saved.NPCs["wolf-id"]; exists {
		t.Fatal("dead NPC persisted")
	}
	replay, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-lethal", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteAttackLineCommitsWeaponDropAndReplays(t *testing.T) {
	store := &departureStore{state: weaponDropAttackCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteAttackLineWithOptions(context.Background(), store, "w", "attack-drop", lease, "공격 늑대", weaponDropRoll(t), AttackOptions{})
	if err != nil || !strings.Contains(string(first.Response), "떨어뜨렸습니다") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.Players["a"].Items.Ready[19] != "" || len(saved.Players["a"].Items.Inventory) != 1 || saved.NPCs["wolf-id"].Body.HPCurrent != 50 {
		t.Fatalf("saved player=%+v npc=%+v err=%v", saved.Players["a"].Items, saved.NPCs["wolf-id"].Body, err)
	}
	replay, err := owners.ExecuteAttackLineWithOptions(context.Background(), store, "w", "attack-drop", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 }, AttackOptions{})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteAttackLineCommitsPowerDamageSequenceAndReplays(t *testing.T) {
	store := &departureStore{state: powerDamageAttackCommandFixture(t)}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-power", lease, "공격 늑대", powerDamageRoll(t))
	if err != nil || !strings.Contains(string(first.Response), "10 만큼의 피해") || store.commits != 1 {
		t.Fatalf("first=%q err=%v commits=%d", first.Response, err, store.commits)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil || saved.NPCs["wolf-id"].Body.HPCurrent != 40 || saved.NPCs["wolf-id"].Enemies[0].Damage != 10 {
		t.Fatalf("saved npc=%+v err=%v", saved.NPCs["wolf-id"], err)
	}
	replay, err := owners.ExecuteAttackLine(context.Background(), store, "w", "attack-power", lease, "공격 늑대", func(int, int) int { t.Fatal("attack RNG replayed"); return 0 })
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func TestExecuteAttackLineRejectsUnsupportedWithoutCommit(t *testing.T) {
	store := &departureStore{state: attackCommandFixture()}
	var owners Ownership
	lease, _ := owners.Acquire("a")
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := owners.ExecuteAttackLine(context.Background(), store, "w", "unsupported", lease, "봐", nil); err == nil || store.commits != 0 {
		t.Fatalf("unsupported attack accepted: err=%v commits=%d", err, store.commits)
	}
}
