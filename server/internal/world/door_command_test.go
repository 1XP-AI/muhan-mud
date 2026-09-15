package world

import "testing"

func doorCommandFixture() State {
	s := settingsFixture()
	room := s.Rooms[1]
	room.Resource.Exits = []LegacyExit{{Name: "북", Flags: [4]byte{40}, LastTime: 5}}
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	s.Players["a"] = actor
	return s
}

func TestDoorCommandOpenClosePersistsExitAndRevealsActor(t *testing.T) {
	s := doorCommandFixture()
	open, err := s.PlanDoor("a", DoorOpen, "북", 10)
	if err != nil || !open.Changed || open.ExitIndex != 0 || open.Response != "당신은 북쪽 출구를 열었습니다.\r\n" {
		t.Fatalf("open proposal=%+v err=%v", open, err)
	}
	next, result, err := s.ApplyDoor(open)
	if err != nil || !result.Broadcast || result.Action != DoorOpen || settingBitSet(next.Players["a"].Body, playerHiddenStateFlag) || exitFlag(next.Rooms[1].Resource.Exits[0], 3) {
		t.Fatalf("open next=%+v result=%+v err=%v", next, result, err)
	}
	close, err := next.PlanDoor("a", DoorClose, "북", 11)
	if err != nil || !close.Changed {
		t.Fatalf("close proposal=%+v err=%v", close, err)
	}
	next, result, err = next.ApplyDoor(close)
	if err != nil || !result.Broadcast || !flag(next.Rooms[1].Resource.Exits[0].Flags[:], 3) {
		t.Fatalf("close next=%+v result=%+v err=%v", next, result, err)
	}
	if _, _, err := next.ApplyDoor(open); err == nil {
		t.Fatal("stale door proposal was accepted")
	}
}

func TestDoorCommandRejectsLockedAndMissingTargetsWithoutMutation(t *testing.T) {
	s := doorCommandFixture()
	locked := s.Rooms[1]
	locked.Resource.Exits[0].Flags[0] |= 4
	s.Rooms[1] = locked
	proposal, err := s.PlanDoor("a", DoorOpen, "북", 10)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyDoor(proposal)
	if err != nil || result.Broadcast || result.Response != "그것은 잠겨져 있습니다.\r\n" || next.Rooms[1].Resource.Exits[0] != s.Rooms[1].Resource.Exits[0] || !settingBitSet(next.Players["a"].Body, playerHiddenStateFlag) {
		t.Fatalf("locked next=%+v result=%+v err=%v", next, result, err)
	}
	missing, err := s.PlanDoor("a", DoorOpen, "없는출구", 10)
	if err != nil {
		t.Fatal(err)
	}
	next, result, err = s.ApplyDoor(missing)
	if err != nil || result.Response != "그런 출구는 없습니다.\r\n" || result.Broadcast || next.Rooms[1].Resource.Exits[0] != s.Rooms[1].Resource.Exits[0] {
		t.Fatalf("missing next=%+v result=%+v err=%v", next, result, err)
	}
	appeared := s
	appearedRoom := appeared.Rooms[1]
	appearedRoom.Resource.Exits = append(appearedRoom.Resource.Exits, LegacyExit{Name: "없는출구", Flags: [4]byte{40}})
	appeared.Rooms[1] = appearedRoom
	if _, _, err := appeared.ApplyDoor(missing); err == nil {
		t.Fatal("stale missing-door proposal was accepted")
	}
	noTarget, err := s.PlanDoor("a", DoorClose, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err = s.ApplyDoor(noTarget)
	if err != nil || result.Response != "무엇을 닫고 싶으세요?\r\n" {
		t.Fatalf("no target result=%+v err=%v", result, err)
	}
}

func exitFlag(e LegacyExit, bit uint) bool {
	return e.Flags[bit/8]&(1<<(bit%8)) != 0
}
