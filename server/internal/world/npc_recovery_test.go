package world

import (
	"reflect"
	"testing"
)

func TestRecoverOfflineClearsPlayerNPCTargetsButPreservesNPCOrder(t *testing.T) {
	s := npcCanonicalFixture()
	s.Players["offline"] = PlayerState{Body: LegacyMonster{Name: "Offline", RoomID: 2}}
	npc := s.NPCs["npc-a"]
	npc.Enemies = []NPCEnemy{
		{Target: EntityRef{Kind: "player", ID: "player"}, Damage: -7},
		{Target: EntityRef{Kind: "npc", ID: "npc-b"}, Damage: 42},
		{Target: EntityRef{Kind: "player", ID: "offline"}, Damage: 3},
	}
	s.NPCs["npc-a"] = npc
	b := s.NPCs["npc-b"]
	b.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "player"}, Damage: 0}}
	s.NPCs["npc-b"] = b
	next, ids, err := s.RecoverOffline()
	want := []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "npc-b"}, Damage: 42}}
	if err != nil || !reflect.DeepEqual(ids, []string{"player"}) || !reflect.DeepEqual(next.NPCs["npc-a"].Enemies, want) || next.NPCs["npc-b"].Enemies == nil || len(next.NPCs["npc-b"].Enemies) != 0 {
		t.Fatalf("recovery relationships %+v %v", next.NPCs, err)
	}
	if !reflect.DeepEqual(next.Rooms[1].NPCIDs, s.Rooms[1].NPCIDs) || !reflect.DeepEqual(next.NPCs["npc-a"].Body, s.NPCs["npc-a"].Body) || len(s.NPCs["npc-a"].Enemies) != 3 || len(s.NPCs["npc-b"].Enemies) != 1 {
		t.Fatal("recovery mutated source or gameplay body")
	}
	again, ids, err := next.RecoverOffline()
	if err != nil || len(ids) != 0 || !reflect.DeepEqual(again, next) {
		t.Fatal("NPC recovery not idempotent")
	}
}

func TestRecoverOfflineDoesNotInventUnresolvedNPCRelations(t *testing.T) {
	s := npcCanonicalFixture()
	next, _, err := s.RecoverOffline()
	if err != nil || next.NPCs["npc-a"].Enemies != nil || next.NPCs["npc-b"].Enemies != nil {
		t.Fatal("unknown relations became peaceful")
	}
}
