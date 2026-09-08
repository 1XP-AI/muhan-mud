package world

import (
	"reflect"
	"testing"
)

func TestFollowNPCToPlayerPreservesHeadOrderAndUnfollow(t *testing.T) {
	s, _ := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Enemies = []NPCEnemy{}
	s.NPCs["guard-id"] = npc
	with, err := s.FollowNPCToPlayer("guard-id", "a")
	if err != nil || !reflect.DeepEqual(with.Players["a"].NPCFollowerIDs, []string{"guard-id"}) || with.NPCs["guard-id"].FollowingPlayerID != "a" {
		t.Fatalf("follow state=%+v err=%v", with, err)
	}
	without, err := with.UnfollowNPCFromPlayer("guard-id")
	if err != nil || len(without.Players["a"].NPCFollowerIDs) != 0 || without.NPCs["guard-id"].FollowingPlayerID != "" {
		t.Fatalf("unfollow state=%+v err=%v", without, err)
	}
}

func TestDirectionalStepMovesMDMFOLMonsterFollowerWithoutDiePermTimer(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Body.Flags[npcDMFollowFlag/8] |= 1 << (npcDMFollowFlag % 8)
	npc.Body.Flags[npcPermanentFlag/8] |= 1 << (npcPermanentFlag % 8)
	npc.Enemies = []NPCEnemy{}
	npc.FollowingPlayerID = "a"
	npc.PermanentOrigin = &NPCPermanentOrigin{RoomID: 1, Slot: 0}
	s.NPCs["guard-id"] = npc
	player := s.Players["a"]
	player.NPCFollowerIDs = []string{"guard-id"}
	s.Players["a"] = player
	s.ActiveNPCIDs = []string{"guard-id"}
	source := s.Rooms[1]
	source.Resource.PermanentMonsters[0] = LegacyTimer{Misc: 77, LastTime: 1, Interval: 10}
	s.Rooms[1] = source
	in.Movement.Now = 100

	next, result, err := s.DirectionalStep(in, nil, nil, nil)
	if err != nil || result.NPCMonsterFollowers == nil || len(result.NPCMonsterFollowers.Moves) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["a"].Body.RoomID != 2 || next.NPCs["guard-id"].Body.RoomID != 2 || len(next.Rooms[1].NPCIDs) != 0 || !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"guard-id"}) {
		t.Fatalf("movement players=%+v rooms=%+v npcs=%+v", next.Players, next.Rooms, next.NPCs)
	}
	guard := next.NPCs["guard-id"]
	if flag(guard.Body.Flags[:], npcPermanentFlag) || next.Rooms[1].Resource.PermanentMonsters[0].LastTime != 1 || next.ActiveNPCIDs[0] != "guard-id" || guard.FollowingPlayerID != "a" {
		t.Fatalf("MDMFOL effects guard=%+v timer=%+v active=%v", guard, next.Rooms[1].Resource.PermanentMonsters[0], next.ActiveNPCIDs)
	}
}

func TestApplyNPCMonsterFollowersRejectsUnknownActiveOrderAtomically(t *testing.T) {
	s, in := npcMovementFixture(t)
	npc := s.NPCs["guard-id"]
	npc.Body.Flags[npcDMFollowFlag/8] |= 1 << (npcDMFollowFlag % 8)
	npc.FollowingPlayerID = "a"
	npc.Enemies = []NPCEnemy{}
	s.NPCs["guard-id"] = npc
	player := s.Players["a"]
	player.NPCFollowerIDs = []string{"guard-id"}
	s.Players["a"] = player
	s.ActiveNPCIDs = nil
	next, proposal, err := s.DirectionalTransferWithIDs(in, nil, nil, nil)
	if err != nil || !proposal.Movement.Moved {
		t.Fatal(err)
	}
	planned, err := next.PlanNPCMonsterFollowers(NPCMonsterFollowerInput{ActorID: "a", SourceRoomID: 1, Now: 100})
	if err == nil || !reflect.DeepEqual(planned, NPCMonsterFollowerProposal{}) {
		t.Fatalf("unknown active order planned=%+v err=%v", planned, err)
	}
	if len(next.Rooms[1].NPCIDs) != 1 || next.NPCs["guard-id"].Body.RoomID != 1 {
		t.Fatal("planning mutated source")
	}
}

func TestDirectionalStepReplaysMixedFirstFollowerOrder(t *testing.T) {
	s, in := npcMovementFixture(t)
	s.Players["a"] = s.Players["a"]
	s.Players["b"] = PlayerState{
		Body:        LegacyMonster{Name: "Bob", Type: 0, RoomID: 1, Class: 4, Level: 1, HPCurrent: 30, Stats: [5]byte{10, 10, 10, 10, 10}},
		Online:      true,
		FollowingID: "a",
		Items:       &ItemCollection{Items: map[string]Item{}},
	}
	room := s.Rooms[1]
	room.PlayerIDs = append(room.PlayerIDs, "b")
	s.Rooms[1] = room
	npc := s.NPCs["guard-id"]
	npc.Enemies = []NPCEnemy{}
	npc.FollowingPlayerID = "a"
	npc.Body.Flags[npcDMFollowFlag/8] |= 1 << (npcDMFollowFlag % 8)
	s.NPCs["guard-id"] = npc
	leader := s.Players["a"]
	leader.FollowerIDs = []string{"b"}
	leader.NPCFollowerIDs = []string{"guard-id"}
	leader.FollowerRefs = []EntityRef{{Kind: "player", ID: "b"}, {Kind: "npc", ID: "guard-id"}}
	s.Players["a"] = leader
	s.ActiveNPCIDs = []string{"guard-id"}

	next, result, err := s.DirectionalStep(in, nil, nil, nil)
	if err != nil || len(result.FollowerOrder) != 2 || result.FollowerOrder[0] != (EntityRef{Kind: "player", ID: "b"}) || result.FollowerOrder[1] != (EntityRef{Kind: "npc", ID: "guard-id"}) {
		t.Fatalf("mixed order=%v result=%+v err=%v", result.FollowerOrder, result, err)
	}
	if len(result.Followers) != 1 || result.Followers[0].ActorID != "b" || result.NPCMonsterFollowers == nil || len(result.NPCMonsterFollowers.Moves) != 1 {
		t.Fatalf("mixed result=%+v", result)
	}
	if next.Players["b"].Body.RoomID != 2 || next.NPCs["guard-id"].Body.RoomID != 2 || !reflect.DeepEqual(next.Rooms[2].PlayerIDs, []string{"a", "b"}) || !reflect.DeepEqual(next.Rooms[2].NPCIDs, []string{"guard-id"}) {
		t.Fatalf("mixed movement rooms=%+v players=%+v npcs=%+v", next.Rooms, next.Players, next.NPCs)
	}
}
