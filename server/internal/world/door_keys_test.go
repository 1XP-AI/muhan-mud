package world

import "testing"

func doorKeyFixture(canonical bool) State {
	s := doorCommandFixture()
	room := s.Rooms[1]
	room.Resource.Exits = []LegacyExit{{Name: "북", Flags: [4]byte{0x3c}, Key: 7}}
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Class = doorThiefClass
	actor.Body.Level = 4
	actor.Body.Stats[1] = 20
	key := LegacyObject{Name: "열쇠", Type: doorKeyObjectType, DiceCount: 7, ShotsCurrent: 2}
	if canonical {
		actor.Items = &ItemCollection{Items: map[string]Item{"key-1": {Object: key}}, Inventory: []string{"key-1"}}
	} else {
		actor.Body.Inventory = []LegacyObject{key}
	}
	s.Players["a"] = actor
	return s
}

func TestDoorKeyUnlockAndLockUseSourceOrdering(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		s := doorKeyFixture(canonical)
		unlock, err := s.PlanDoorKey("a", DoorUnlock, "북", "열쇠", 10, nil)
		if err != nil || !unlock.Changed || !unlock.Broadcast || unlock.Response != "## 찰칵 ##\r\n" {
			t.Fatalf("canonical=%v unlock=%+v err=%v", canonical, unlock, err)
		}
		next, result, err := s.ApplyDoorKey(unlock)
		if err != nil || !result.Broadcast || result.Action != DoorUnlock || flag(next.Rooms[1].Resource.Exits[0].Flags[:], doorLockedFlag) || next.Rooms[1].Resource.Exits[0].LastTime != 10 || settingBitSet(next.Players["a"].Body, playerHiddenStateFlag) {
			t.Fatalf("canonical=%v next=%+v result=%+v err=%v", canonical, next, result, err)
		}
		if canonical {
			if got := next.Players["a"].Items.Items["key-1"].Object.ShotsCurrent; got != 1 {
				t.Fatalf("canonical unlock did not consume one charge: %d", got)
			}
		} else if got := next.Players["a"].Body.Inventory[0].ShotsCurrent; got != 1 {
			t.Fatalf("legacy unlock did not consume one charge: %d", got)
		}

		lock, err := next.PlanDoorKey("a", DoorLock, "북", "열쇠", 11, nil)
		if err != nil || !lock.Changed || lock.Response != "## 찰칵 ##\r\n" {
			t.Fatalf("canonical=%v lock=%+v err=%v", canonical, lock, err)
		}
		next, result, err = next.ApplyDoorKey(lock)
		if err != nil || !result.Broadcast || result.Action != DoorLock || !flag(next.Rooms[1].Resource.Exits[0].Flags[:], doorLockedFlag) {
			t.Fatalf("canonical=%v locked next=%+v result=%+v err=%v", canonical, next, result, err)
		}
		if _, _, err := next.ApplyDoorKey(unlock); err == nil {
			t.Fatalf("canonical=%v accepted stale unlock", canonical)
		}
	}
}

func TestDoorKeyFailuresDoNotMutate(t *testing.T) {
	s := doorKeyFixture(true)
	for _, tc := range []struct {
		lineAction string
		target     string
		key        string
		want       string
	}{
		{DoorUnlock, "북", "", "뭘 가지고 열려구요?\r\n"},
		{DoorUnlock, "북", "없는열쇠", "당신은 그런것을 갖고 있지 않습니다.\r\n"},
		{DoorUnlock, "북", "열쇠", "그것은 잠궈져 있지 않습니다.\r\n"},
	} {
		if tc.lineAction == DoorUnlock && tc.want == "그것은 잠궈져 있지 않습니다.\r\n" {
			room := s.Rooms[1]
			room.Resource.Exits[0].Flags[0] &^= 1 << doorLockedFlag
			s.Rooms[1] = room
		}
		p, err := s.PlanDoorKey("a", tc.lineAction, tc.target, tc.key, 10, nil)
		if err != nil {
			t.Fatal(err)
		}
		next, result, err := s.ApplyDoorKey(p)
		if err != nil || result.Response != tc.want || result.Broadcast || next.Rooms[1].Resource.Exits[0] != s.Rooms[1].Resource.Exits[0] {
			t.Fatalf("case=%+v p=%+v next=%+v result=%+v err=%v", tc, p, next, result, err)
		}
		// Restore the locked fixture after the deliberately unlocked branch.
		room := s.Rooms[1]
		room.Resource.Exits[0].Flags[0] |= 1 << doorLockedFlag
		s.Rooms[1] = room
	}

	wrongType := s
	actor := wrongType.Players["a"]
	item := actor.Items.Items["key-1"]
	item.Object.Type = 1
	actor.Items.Items["key-1"] = item
	wrongType.Players["a"] = actor
	p, err := wrongType.PlanDoorKey("a", DoorUnlock, "북", "열쇠", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := wrongType.ApplyDoorKey(p)
	if err != nil || result.Response != "그것은 열쇠가 아닙니다.\r\n" || result.Broadcast {
		t.Fatalf("wrong type result=%+v err=%v", result, err)
	}
}

func TestDoorKeyPickSuccessFailureCooldownAndUnpickable(t *testing.T) {
	s := doorKeyFixture(true)
	s.Players["a"] = func() PlayerState {
		p := s.Players["a"]
		p.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
		return p
	}()
	pick, err := s.PlanDoorKey("a", DoorPick, "북", "", 100, func(_, _ int) int { return 1 })
	if err != nil || !pick.Attempt || !pick.Succeeded || pick.Chance != 16 {
		t.Fatalf("pick success proposal=%+v err=%v", pick, err)
	}
	next, result, err := s.ApplyDoorKey(pick)
	if err != nil || !result.Broadcast || !result.Attempt || !result.Succeeded || flag(next.Rooms[1].Resource.Exits[0].Flags[:], doorLockedFlag) || settingBitSet(next.Players["a"].Body, playerHiddenStateFlag) || next.Players["a"].Body.Timers[doorPickTimer] != (LegacyTimer{LastTime: 100, Interval: 10}) {
		t.Fatalf("pick success next=%+v result=%+v err=%v", next, result, err)
	}

	failureState := doorKeyFixture(true)
	failure, err := failureState.PlanDoorKey("a", DoorPick, "북", "", 100, func(_, _ int) int { return 100 })
	if err != nil || !failure.Attempt || failure.Succeeded || failure.Response != "실패하였습니다!\r\n" {
		t.Fatalf("pick failure proposal=%+v err=%v", failure, err)
	}
	next, result, err = failureState.ApplyDoorKey(failure)
	if err != nil || !result.Broadcast || !result.Attempt || result.Succeeded || !flag(next.Rooms[1].Resource.Exits[0].Flags[:], doorLockedFlag) || next.Players["a"].Body.Timers[doorPickTimer].Interval != 10 {
		t.Fatalf("pick failure next=%+v result=%+v err=%v", next, result, err)
	}

	cooldownState := doorKeyFixture(true)
	actor := cooldownState.Players["a"]
	actor.Body.Timers[doorPickTimer] = LegacyTimer{LastTime: 100, Interval: 10}
	actor.Body.Flags[playerHiddenStateFlag/8] |= 1 << (playerHiddenStateFlag % 8)
	cooldownState.Players["a"] = actor
	cooldown, err := cooldownState.PlanDoorKey("a", DoorPick, "북", "", 105, func(_, _ int) int { t.Fatal("cooldown consumed RNG"); return 1 })
	if err != nil || !cooldown.Cooldown || cooldown.Response != "5초동안 기다리세요.\r\n" {
		t.Fatalf("cooldown proposal=%+v err=%v", cooldown, err)
	}
	next, result, err = cooldownState.ApplyDoorKey(cooldown)
	if err != nil || result.Broadcast || settingBitSet(next.Players["a"].Body, playerHiddenStateFlag) || next.Players["a"].Body.Timers[doorPickTimer] != actor.Body.Timers[doorPickTimer] {
		t.Fatalf("cooldown next=%+v result=%+v err=%v", next, result, err)
	}

	unpickable := doorKeyFixture(true)
	room := unpickable.Rooms[1]
	room.Resource.Exits[0].Flags[0] |= 1 << doorUnpickable
	unpickable.Rooms[1] = room
	called := false
	pick, err = unpickable.PlanDoorKey("a", DoorPick, "북", "", 100, func(low, high int) int {
		called = true
		if low != 1 || high != 100 {
			t.Fatalf("unexpected RNG range %d..%d", low, high)
		}
		return 1
	})
	if err != nil || !called || pick.Succeeded || pick.Chance != 0 {
		t.Fatalf("unpickable proposal=%+v called=%v err=%v", pick, called, err)
	}
}

func TestDoorKeyRejectsStaleInventoryAndRandomValues(t *testing.T) {
	s := doorKeyFixture(true)
	pick, err := s.PlanDoorKey("a", DoorPick, "북", "", 100, func(_, _ int) int { return 1 })
	if err != nil {
		t.Fatal(err)
	}
	changed := s
	room := changed.Rooms[1]
	room.Resource.Exits[0].Flags[0] &^= 1 << doorLockedFlag
	changed.Rooms[1] = room
	if _, _, err := changed.ApplyDoorKey(pick); err == nil {
		t.Fatal("stale pick proposal was accepted")
	}
	if _, err := doorKeyFixture(true).PlanDoorKey("a", DoorPick, "북", "", 100, func(_, _ int) int { return 0 }); err == nil {
		t.Fatal("out-of-range random result was accepted")
	}
}
