package world

import (
	"reflect"
	"testing"
)

func TestRecoverOfflinePreservesGameStateAndIsIdempotent(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Gold = 412
	p.Body.Inventory = []LegacyObject{{Name: "bag", Contents: []LegacyObject{{Name: "coin"}}}}
	s.Players["a"] = p
	next, disconnected, err := s.RecoverOffline()
	if err != nil || !reflect.DeepEqual(disconnected, []string{"a"}) || next.Players["a"].Online || len(next.Rooms[1].PlayerIDs) != 0 || !reflect.DeepEqual(next.Players["a"].Body, p.Body) {
		t.Fatalf("%+v %v %v", next, disconnected, err)
	}
	again, ids, err := next.RecoverOffline()
	if err != nil || len(ids) != 0 || !reflect.DeepEqual(again, next) {
		t.Fatal("recovery is not idempotent")
	}
	next.Players["a"].Body.Inventory[0].Contents[0].Name = "changed"
	if !s.Players["a"].Online || len(s.Rooms[1].PlayerIDs) != 1 || s.Players["a"].Body.Inventory[0].Contents[0].Name != "coin" {
		t.Fatal("recovery mutated original")
	}
}

func TestRecoverOfflineRejectsCorruptionInsteadOfRepairingIt(t *testing.T) {
	s := stateFixture()
	r := s.Rooms[2]
	r.PlayerIDs = []string{"a"}
	s.Rooms[2] = r
	next, ids, err := s.RecoverOffline()
	if err == nil || !reflect.DeepEqual(next, State{}) || ids != nil {
		t.Fatal("corrupt snapshot silently repaired")
	}
}

func TestRecoverOfflineOrdersDisconnectedIDs(t *testing.T) {
	s := stateFixture()
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2}, Online: true}
	r := s.Rooms[2]
	r.PlayerIDs = []string{"b"}
	s.Rooms[2] = r
	_, ids, err := s.RecoverOffline()
	if err != nil || !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Fatalf("%v %v", ids, err)
	}
}

func TestRecoverOfflineClearsMDMFOLFlagsAndReciprocalEdges(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil || attached.NPCs["wolf-1"].FollowingPlayerID != "dm" || !PlayerFlagSet(attached.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatalf("attached=%+v err=%v", attached.NPCs["wolf-1"], err)
	}

	next, disconnected, err := attached.RecoverOffline()
	if err != nil {
		t.Fatal(err)
	}
	npc := next.NPCs["wolf-1"]
	dm := next.Players["dm"]
	if npc.FollowingPlayerID != "" || PlayerFlagSet(npc.Body, npcDMFollowFlag) || dm.NPCFollowerIDs != nil || dm.FollowerRefs != nil || dm.Online {
		t.Fatalf("recovery left MDMFOL lock npc=%+v dm=%+v", npc, dm)
	}
	if !reflect.DeepEqual(disconnected, []string{"caretaker", "dm", "mortal"}) {
		t.Fatalf("disconnected=%v", disconnected)
	}
	if attached.NPCs["wolf-1"].FollowingPlayerID != "dm" || !PlayerFlagSet(attached.NPCs["wolf-1"].Body, npcDMFollowFlag) || !attached.Players["dm"].Online {
		t.Fatal("RecoverOffline mutated the attached snapshot")
	}

	again, ids, err := next.RecoverOffline()
	if err != nil || len(ids) != 0 || !reflect.DeepEqual(again, next) {
		t.Fatal("recovery replay recommitted MDMFOL cleanup")
	}
	if _, _, err := next.ApplyDMFollow(proposal); err == nil {
		t.Fatal("stale MDMFOL proposal recommitted after recovery")
	}
}

func TestRecoverOfflineClearsOrphanedMDMFOLWithoutFollowEdge(t *testing.T) {
	s := dmFollowFixture(t)
	npc := s.NPCs["wolf-1"]
	npc.Body = setNPCFlag(npc.Body, npcDMFollowFlag, true)
	s.NPCs["wolf-1"] = npc

	next, _, err := s.RecoverOffline()
	if err != nil {
		t.Fatal(err)
	}
	got := next.NPCs["wolf-1"]
	if got.FollowingPlayerID != "" || PlayerFlagSet(got.Body, npcDMFollowFlag) {
		t.Fatalf("orphaned MDMFOL survived StartWorld recovery npc=%+v", got)
	}
	if s.NPCs["wolf-1"].FollowingPlayerID != "" || !PlayerFlagSet(s.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatal("RecoverOffline mutated the leftover snapshot")
	}
	again, ids, err := next.RecoverOffline()
	if err != nil || len(ids) != 0 || !reflect.DeepEqual(again, next) {
		t.Fatal("orphaned MDMFOL recovery replay recommitted")
	}
}

func TestRecoverOfflineRejectsUnmigratedMDMFOLEnemiesAndActiveOrder(t *testing.T) {
	s := resolveDMFollowLogoutDomain(dmFollowFixture(t))
	proposal, err := s.PlanDMFollow("dm", "*따르기", "늑대", 1)
	if err != nil {
		t.Fatal(err)
	}
	attached, _, err := s.ApplyDMFollow(proposal)
	if err != nil {
		t.Fatal(err)
	}

	missingEnemies := attached.clone()
	npc := missingEnemies.NPCs["wolf-1"]
	npc.Enemies = nil
	missingEnemies.NPCs["wolf-1"] = npc
	got, ids, err := missingEnemies.RecoverOffline()
	if err == nil || !reflect.DeepEqual(got, State{}) || ids != nil ||
		!missingEnemies.Players["dm"].Online || !PlayerFlagSet(missingEnemies.NPCs["wolf-1"].Body, npcDMFollowFlag) ||
		missingEnemies.NPCs["wolf-1"].FollowingPlayerID != "dm" {
		t.Fatalf("unmigrated enemies next=%+v ids=%v err=%v", got, ids, err)
	}

	missingActive := attached.clone()
	missingActive.ActiveNPCIDs = nil
	got, ids, err = missingActive.RecoverOffline()
	if err == nil || !reflect.DeepEqual(got, State{}) || ids != nil ||
		!missingActive.Players["dm"].Online || missingActive.NPCs["wolf-1"].FollowingPlayerID != "dm" ||
		!PlayerFlagSet(missingActive.NPCs["wolf-1"].Body, npcDMFollowFlag) {
		t.Fatalf("unmigrated active next=%+v ids=%v err=%v", got, ids, err)
	}
}
