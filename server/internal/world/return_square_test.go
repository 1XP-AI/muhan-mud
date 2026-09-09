package world

import (
	"reflect"
	"strings"
	"testing"
)

func returnSquareRoom(id int16, players ...string) RoomState {
	return RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: id}},
		PlayerIDs: append([]string(nil), players...),
	}
}

func returnSquareFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			7:    returnSquareRoom(7, "actor"),
			1001: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1001}, BeenHere: 4}, PlayerIDs: []string{"early", "late"}},
			3307: returnSquareRoom(3307),
		},
		Players: map[string]PlayerState{
			"actor": {Body: LegacyMonster{Name: "Mina", Type: 0, Level: 10, Class: 4, MPCurrent: 17, RoomID: 7}, Online: true},
			"early": {Body: LegacyMonster{Name: "Alpha", Type: 0, RoomID: 1001}, Online: true},
			"late":  {Body: LegacyMonster{Name: "Zed", Type: 0, RoomID: 1001}, Online: true},
		},
	}
}

func TestPlanReturnSquareMovesMembershipAtomicallyAndOrdersEntry(t *testing.T) {
	state := returnSquareFixture()
	before := state
	next, result, err := state.PlanReturnSquare("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved || result.SourceRoomID != 7 || result.DestinationRoomID != 1001 || result.MPDrained || !result.Broadcast {
		t.Fatalf("result=%+v", result)
	}
	if got := result.Response; got != "당신이 \"귀환!\"이라고 외치자 이상한 힘에 의해 어딘가로 빨려들어갑니다.\r\n" {
		t.Fatalf("response=%q", got)
	}
	if got := next.Rooms[7].PlayerIDs; len(got) != 0 {
		t.Fatalf("source membership=%v", got)
	}
	if got, want := next.Rooms[1001].PlayerIDs, []string{"early", "actor", "late"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("destination membership=%v want=%v", got, want)
	}
	if next.Rooms[1001].Resource.BeenHere != 5 || next.Players["actor"].Body.RoomID != 1001 {
		t.Fatalf("entry state=%+v actor=%+v", next.Rooms[1001], next.Players["actor"])
	}
	if result.Event == nil || result.Event.ExcludeActorID != "actor" || result.Event.SourceRoomID != 7 || result.Event.DestinationRoomID != 1001 || !strings.Contains(result.Event.SourceText, "Mina님이 갑자기 사라집니다!") || !strings.Contains(result.Event.DestinationText, "Mina님이 갑자기 자욱한 연기와 함께 나타났습니다!") {
		t.Fatalf("event=%+v", result.Event)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("return-square mutated its input snapshot")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlanReturnSquareUsesFamilyRoomAndDrainsNonInvincibleMP(t *testing.T) {
	state := returnSquareFixture()
	actor := state.Players["actor"]
	actor.Body.Level = 21
	actor.Body.Class = 8
	actor.Body.MPCurrent = 33
	actor.Body.Flags[returnSquareFamilyReturnFlag/8] |= 1 << (returnSquareFamilyReturnFlag % 8)
	actor.Body.Daily[returnSquareExpansionDailySlot].Max = 7
	state.Players["actor"] = actor
	next, result, err := state.PlanReturnSquare("actor")
	if err != nil {
		t.Fatal(err)
	}
	if result.DestinationRoomID != 3307 || next.Players["actor"].Body.RoomID != 3307 || next.Players["actor"].Body.MPCurrent != 0 || !result.MPDrained {
		t.Fatalf("result=%+v actor=%+v", result, next.Players["actor"])
	}
	if !strings.HasPrefix(result.Response, "당신이 귀환하려하자 흑암의 세력이 당신의 도력을 뺏습니다.\r\n") {
		t.Fatalf("response=%q", result.Response)
	}

	actor = state.Players["actor"]
	actor.Body.Class = returnSquareInvincibleClass
	state.Players["actor"] = actor
	next, result, err = state.PlanReturnSquare("actor")
	if err != nil || result.MPDrained || next.Players["actor"].Body.MPCurrent != 33 {
		t.Fatalf("invincible result=%+v actor=%+v err=%v", result, next.Players["actor"], err)
	}
}

func TestPlanReturnSquareDenialsFollowSourceOrder(t *testing.T) {
	state := returnSquareFixture()
	state.Rooms[7] = RoomState{
		Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 7}},
		PlayerIDs: []string{"actor"},
		NPCIDs:    []string{"wolf"},
	}
	state.NPCs = map[string]NPCState{
		"wolf": {Body: LegacyMonster{Name: "Wolf", Type: 1, RoomID: 7}, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: 0}}},
	}
	before := state
	next, result, err := state.PlanReturnSquare("actor")
	if err != nil || result.Moved || !strings.Contains(result.Response, "싸우고") || !reflect.DeepEqual(next, state) {
		t.Fatalf("combat result=%+v err=%v next=%+v", result, err, next)
	}

	state = returnSquareFixture()
	actor := state.Players["actor"]
	actor.Body.RoomID = 1001
	state.Players["actor"] = actor
	state.Rooms[7] = returnSquareRoom(7)
	room := state.Rooms[1001]
	room.PlayerIDs = []string{"actor", "early", "late"}
	state.Rooms[1001] = room
	next, result, err = state.PlanReturnSquare("actor")
	if err != nil || result.Moved || !strings.Contains(result.Response, "이미 광장") || !reflect.DeepEqual(next, state) {
		t.Fatalf("square result=%+v err=%v next=%+v", result, err, next)
	}

	state = returnSquareFixture()
	leader := state.Players["early"]
	leader.FollowerIDs = []string{"actor"}
	state.Players["early"] = leader
	actor = state.Players["actor"]
	actor.FollowingID = "early"
	state.Players["actor"] = actor
	next, result, err = state.PlanReturnSquare("actor")
	if err != nil || result.Moved || !strings.Contains(result.Response, "먼저 그룹") || !reflect.DeepEqual(next, state) {
		t.Fatalf("follower result=%+v err=%v next=%+v", result, err, next)
	}

	// The combat branch must run before the square denial, matching C's
	// ply_is_attacking check even when the actor is already in room 1001.
	state = returnSquareFixture()
	actor = state.Players["actor"]
	actor.Body.RoomID = 1001
	state.Players["actor"] = actor
	state.Rooms[7] = returnSquareRoom(7)
	room = state.Rooms[1001]
	room.PlayerIDs = []string{"actor", "early", "late"}
	room.NPCIDs = []string{"wolf"}
	state.Rooms[1001] = room
	state.NPCs = map[string]NPCState{
		"wolf": {Body: LegacyMonster{Name: "Wolf", Type: 1, RoomID: 1001}, Enemies: []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "actor"}, Damage: 1}}},
	}
	_, result, err = state.PlanReturnSquare("actor")
	if err != nil || !strings.Contains(result.Response, "싸우고") {
		t.Fatalf("ordering result=%+v err=%v", result, err)
	}
	_ = before
}

func TestPlanReturnSquareSuppressesDMInvisibleEventButStillMoves(t *testing.T) {
	state := returnSquareFixture()
	actor := state.Players["actor"]
	actor.Body.Flags[returnSquareDMInvisibleFlag/8] |= 1 << (returnSquareDMInvisibleFlag % 8)
	state.Players["actor"] = actor
	next, result, err := state.PlanReturnSquare("actor")
	if err != nil || !result.Moved || result.Broadcast || result.Event != nil || next.Players["actor"].Body.RoomID != 1001 {
		t.Fatalf("result=%+v next=%+v err=%v", result, next, err)
	}
}

func TestPlanReturnSquareFailsClosedForUnresolvedDestinationAndCombat(t *testing.T) {
	state := returnSquareFixture()
	actor := state.Players["actor"]
	actor.Body.Flags[returnSquareFamilyReturnFlag/8] |= 1 << (returnSquareFamilyReturnFlag % 8)
	actor.Body.Daily[returnSquareExpansionDailySlot].Max = 8
	state.Players["actor"] = actor
	before := state
	if _, _, err := state.PlanReturnSquare("actor"); err == nil {
		t.Fatal("missing family room accepted")
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatal("invalid destination mutated state")
	}

	state = returnSquareFixture()
	room := state.Rooms[7]
	room.Resource.Monsters = []LegacyMonster{{Name: "legacy wolf", Type: 1}}
	state.Rooms[7] = room
	if _, _, err := state.PlanReturnSquare("actor"); err == nil {
		t.Fatal("unresolved legacy enemy state accepted")
	}

	state = returnSquareFixture()
	room = state.Rooms[1001]
	room.Resource.ID = 1002
	state.Rooms[1001] = room
	if _, _, err := state.PlanReturnSquare("actor"); err == nil {
		t.Fatal("invalid square room identity accepted")
	}
}
