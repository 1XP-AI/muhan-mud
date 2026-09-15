package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/1XP-Inc/muhan-mud/server/internal/world"
)

func poisonCommandFixture(t *testing.T) []byte {
	t.Helper()
	s := world.State{
		Version: 1,
		Rooms: map[int16]world.RoomState{1: {
			Resource:  world.LegacyRoom{LegacyRoomHeader: world.LegacyRoomHeader{ID: 1}},
			PlayerIDs: []string{"alice"},
			NPCIDs:    []string{"wolf"},
		}},
		Players: map[string]world.PlayerState{
			"alice": {Body: world.LegacyMonster{
				Name: "자객", Type: world.PoisonPlayerType, Class: world.PoisonAssassinClass,
				Level: 20, RoomID: 1, Stats: [5]byte{10, 20, 10, 10, 10}, HPMax: 100, HPCurrent: 100,
			}, Online: true},
		},
		NPCs: map[string]world.NPCState{
			"wolf": {Body: world.LegacyMonster{
				Name: "늑대", Keys: [3]string{"wolf", "짐승", ""}, Type: world.PoisonMonsterType,
				Level: 4, Class: 4, RoomID: 1, HPMax: 99, HPCurrent: 99,
			}, Enemies: []world.NPCEnemy{}},
		},
		ActiveNPCIDs: []string{"wolf"},
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func admitPoisonActor(t *testing.T, owners *Ownership) SessionLease {
	t.Helper()
	lease, err := owners.Acquire("alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := owners.Admit(lease, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestParsePoisonLineAdmitsOnlyAliasAndOneExactToken(t *testing.T) {
	for _, line := range []string{"독살포 늑대", " 독살포 늑대 "} {
		command, ok := ParsePoisonLine(line)
		if !ok || command.Target != "늑대" {
			t.Fatalf("ParsePoisonLine(%q)=%+v ok=%v", line, command, ok)
		}
	}
	for _, line := range []string{"독살포", "독살포 늑대 extra", "독 살포 늑대", "독살포 늑 대", "독살포 늑대\n"} {
		if IsPoisonLine(line) {
			t.Fatalf("unsupported poison line accepted: %q", line)
		}
	}
}

func TestExecutePoisonPersistsAndReplaysWithoutReroll(t *testing.T) {
	store := &departureStore{state: poisonCommandFixture(t)}
	var owners Ownership
	lease := admitPoisonActor(t, &owners)
	calls := 0
	roll := func(low, high int) int {
		calls++
		switch calls {
		case 1:
			if low != 1 || high != 100 {
				t.Fatalf("chance range=%d..%d", low, high)
			}
			return 1
		case 2:
			if low != 10 || high != 100 {
				t.Fatalf("damage range=%d..%d", low, high)
			}
			return 10
		default:
			if low != 1 || high != 6 {
				t.Fatalf("duration range=%d..%d", low, high)
			}
			return 2
		}
	}
	first, err := owners.ExecutePoisonLine(context.Background(), store, "world", "poison-1", lease, "독살포 늑대", 1000, roll)
	if err != nil || first.Replayed || store.commits != 1 || calls != 4 {
		t.Fatalf("first=%+v err=%v commits=%d calls=%d", first, err, store.commits, calls)
	}
	var result world.PoisonResult
	if err := json.Unmarshal(first.Response, &result); err != nil || result.Action != "poison" || !result.Authorized || !result.TargetFound || !result.Succeeded || result.Damage != 33 || result.SpellInterval != 10 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	if saved.NPCs["wolf"].Body.HPCurrent != 66 || !hasPoisonFlag(saved.NPCs["wolf"].Body) || len(saved.NPCs["wolf"].Enemies) != 1 {
		t.Fatalf("saved=%+v", saved.NPCs["wolf"])
	}
	replay, err := owners.ExecutePoisonLine(context.Background(), store, "world", "poison-1", lease, "독살포 늑대", 9999, func(int, int) int {
		t.Fatal("poison replay rerolled")
		return 100
	})
	if err != nil || !replay.Replayed || store.commits != 1 || string(replay.Response) != string(first.Response) {
		t.Fatalf("replay=%+v err=%v commits=%d", replay, err, store.commits)
	}
}

func hasPoisonFlag(body world.LegacyMonster) bool {
	return body.Flags[world.PoisonNPCPoisonedFlag/8]&(1<<(world.PoisonNPCPoisonedFlag%8)) != 0
}

func TestExecutePoisonDoesNotReceiptFailClosedOrMalformedLine(t *testing.T) {
	store := &departureStore{state: poisonCommandFixture(t)}
	var owners Ownership
	lease := admitPoisonActor(t, &owners)
	if _, err := owners.ExecutePoisonLine(context.Background(), store, "world", "poison-bad-line", lease, "독살포 늑대 extra", 1000, func(int, int) int { return 1 }); !errors.Is(err, ErrUnsupportedPoisonLine) || store.commits != 0 {
		t.Fatalf("malformed line reached receipt: err=%v commits=%d", err, store.commits)
	}
	// A canonical NPC with unresolved enemy relations cannot be safely
	// composed with add_enm_crt/add_enm_dmg, so no draw or receipt is allowed.
	s, err := world.DecodeState(store.state)
	if err != nil {
		t.Fatal(err)
	}
	npc := s.NPCs["wolf"]
	npc.Enemies = nil
	s.NPCs["wolf"] = npc
	store.state, err = json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	if _, err := owners.ExecutePoisonLine(context.Background(), store, "world", "poison-unresolved", lease, "독살포 늑대", 1000, func(int, int) int { calls++; return 1 }); !errors.Is(err, world.ErrPoisonCombatSideEffectPending) || store.commits != 0 || calls != 0 {
		t.Fatalf("unresolved poison was admitted: err=%v commits=%d calls=%d", err, store.commits, calls)
	}
}
