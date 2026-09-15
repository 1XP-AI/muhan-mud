package world

import (
	"reflect"
	"strings"
	"testing"
)

func npcMovementFixture(t *testing.T) (State, TransferInput) {
	t.Helper()
	s, in := canonicalTransferFixture()
	p := s.Players["a"]
	p.Body.Class = 4
	s.Players["a"] = p
	room := s.Rooms[1]
	room.Resource.Monsters = []LegacyMonster{{Name: "guard", Type: 1, Description: "guard stands"}}
	s.Rooms[1] = room
	var err error
	s, err = s.ImportNPCs(func() (string, error) { return "guard-id", nil })
	if err != nil {
		t.Fatal(err)
	}
	return s, in
}

func TestCanonicalNPCMovementUsesOrderedEnemyAuthority(t *testing.T) {
	for _, mode := range []string{"unknown", "peaceful", "negative", "fighting"} {
		t.Run(mode, func(t *testing.T) {
			s, in := npcMovementFixture(t)
			npc := s.NPCs["guard-id"]
			switch mode {
			case "peaceful":
				npc.Enemies = []NPCEnemy{}
			case "negative":
				npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: -1}}
			case "fighting":
				npc.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}
			}
			s.NPCs["guard-id"] = npc
			in.Movement.Traversal.Fighting = mode != "fighting"
			next, result, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
			if mode == "unknown" {
				if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, TransferProposal{}) {
					t.Fatal("unknown enemy state became peaceful")
				}
				return
			}
			if err != nil || result.Movement.Moved != (mode != "fighting") {
				t.Fatalf("movement %+v %v", result.Movement, err)
			}
			if !reflect.DeepEqual(next.NPCs, s.NPCs) || !reflect.DeepEqual(next.Rooms[1].NPCIDs, s.Rooms[1].NPCIDs) || len(next.Rooms[1].Resource.Monsters) != 0 {
				t.Fatal("NPC authority lost or duplicated")
			}
		})
	}
}

func TestCanonicalNPCSceneAndGuardProjection(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Enemies = []NPCEnemy{}
	npc.Body.Flags[4] |= 64
	s.NPCs["guard-id"] = npc
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "guard") {
		t.Fatalf("NPC missing from scene %q %v", text, err)
	}
	room := s.Rooms[1]
	room.Resource.Exits[0].Flags[2] |= 4
	s.Rooms[1] = room
	next, result, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	if err != nil || result.Movement.Moved || result.Movement.Traversal.GuardIndex != 0 || len(next.NPCs) != 1 {
		t.Fatalf("guard ignored %+v %v", result, err)
	}
}

func TestCanonicalNPCSpawnWithoutAllocatorRejectsWholeMove(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Enemies = []NPCEnemy{}
	s.NPCs["guard-id"] = npc
	room := s.Rooms[2]
	room.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
	s.Rooms[2] = room
	next, result, err := s.DirectionalTransferWithIDs(in, refreshCatalog{}, func(lo, hi int) int { return lo }, nil)
	if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, TransferProposal{}) || s.Players["a"].Body.RoomID != 1 || len(s.NPCs) != 1 {
		t.Fatalf("unidentified spawn leaked: %v", err)
	}
}

type npcSpawnCatalog struct{ refreshCatalog }

func (npcSpawnCatalog) Monster(int16) (LegacyMonster, error) {
	return LegacyMonster{Name: "늑대", Type: 1}, nil
}

func TestCanonicalNPCSpawnPreservesExistingTiedNameIdentity(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Enemies = []NPCEnemy{}
	s.NPCs["guard-id"] = npc
	room := s.Rooms[2]
	room.NPCIDs = []string{"wolf-old"}
	room.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
	s.Rooms[2] = room
	s.NPCs["wolf-old"] = NPCState{Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 2, HPCurrent: 77}, Enemies: []NPCEnemy{}}
	calls := 0
	next, result, err := s.DirectionalTransferWithIDs(in, npcSpawnCatalog{}, func(lo, hi int) int { return lo }, func() (string, error) { calls++; return "wolf-new", nil })
	if err != nil || !result.Movement.Moved || calls != 1 || !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"wolf-old", "wolf-new"}) {
		t.Fatalf("spawn IDs %+v %v calls%d", next.Rooms[2].NPCIDs, err, calls)
	}
	if next.NPCs["wolf-old"].Body.HPCurrent != 77 || next.NPCs["wolf-new"].Enemies == nil || len(next.Rooms[2].Resource.Monsters) != 0 || len(s.NPCs) != 2 {
		t.Fatal("NPC spawn aliased or replaced existing instance")
	}
	room = next.Rooms[2]
	room.Resource.Exits = []LegacyExit{{Name: "남", Destination: 1}}
	next.Rooms[2] = room
	back := in
	dest := next.Rooms[1].Resource
	back.Movement.Prefix = "남"
	back.Movement.Destination = &dest
	backState, _, err := next.DirectionalTransferWithIDs(back, npcSpawnCatalog{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := backState.DirectionalTransferWithIDs(in, npcSpawnCatalog{}, nil, func() (string, error) { t.Fatal("existing spawn reallocated"); return "", nil })
	if err != nil || !reflect.DeepEqual(again.Rooms[2].NPCIDs, []string{"wolf-old", "wolf-new"}) || len(again.NPCs) != 3 {
		t.Fatalf("repeat entry %v", err)
	}
}

func TestCanonicalNPCSpawnFailuresReturnNoPartialState(t *testing.T) {
	for _, id := range []string{"", "guard-id", "fresh"} {
		s, in := npcMovementFixture(t)
		npc := s.NPCs["guard-id"]
		npc.Enemies = []NPCEnemy{}
		s.NPCs["guard-id"] = npc
		room := s.Rooms[2]
		room.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 1}
		if id == "fresh" {
			room.Resource.PermanentObjects[0] = LegacyTimer{Misc: 2}
		}
		s.Rooms[2] = room
		next, result, err := s.DirectionalTransferWithIDs(in, npcSpawnCatalog{refreshCatalog{failObject: true}}, func(lo, hi int) int { return lo }, func() (string, error) { return id, nil })
		if err == nil || !reflect.DeepEqual(next, State{}) || !reflect.DeepEqual(result, TransferProposal{}) || len(s.NPCs) != 1 || s.Players["a"].Body.RoomID != 1 {
			t.Fatalf("partial NPC failure id%q %v", id, err)
		}
	}
}
