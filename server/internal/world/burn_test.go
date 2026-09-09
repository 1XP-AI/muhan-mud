package world

import (
	"reflect"
	"strings"
	"testing"
)

func burnStateFixture(t *testing.T) State {
	t.Helper()
	actor := LegacyMonster{
		Name: "Alice", Type: 0, Class: 4, RoomID: 1, Gold: 10, Experience: 20,
	}
	setSettingFlag(&actor, playerHiddenStateFlag, true)
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"actor"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body: actor, Online: true,
				Items: &ItemCollection{
					Items: map[string]Item{
						"coin-1": {Object: LegacyObject{Name: "동전"}},
						"coin-2": {Object: LegacyObject{Name: "동전"}},
						"bag":    {Object: LegacyObject{Name: "가방"}, Contents: []string{"gem"}},
						"gem":    {Object: LegacyObject{Name: "보석"}},
						"sword":  {Object: LegacyObject{Name: "검"}},
					},
					Inventory: []string{"coin-1", "coin-2", "bag"},
					Ready:     [20]string{19: "sword"},
				},
			},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func burnObject(s State, id string) Item {
	return s.Players["actor"].Items.Items[id]
}

func TestPlanAndApplyBurnUsesOrderedDirectRootAndAwardsExactlyOnce(t *testing.T) {
	before := burnStateFixture(t)
	original := before
	calls := 0
	proposal, err := before.PlanBurn("actor", "동전", 1, 100, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 42
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !proposal.Burned || !proposal.ClearHidden || !proposal.Broadcast || proposal.ItemID != "coin-1" || proposal.ItemOccurrence != 1 || proposal.JackpotRoll != 42 {
		t.Fatalf("proposal=%+v calls=%d", proposal, calls)
	}

	next, result, err := before.ApplyBurn(proposal)
	if err != nil {
		t.Fatal(err)
	}
	actor := next.Players["actor"]
	if result.Action != "burn" || !result.Burned || !result.Broadcast || result.GoldAward != 4 || result.ExperienceAward != 1 || result.ItemID != "coin-1" || result.Event == nil {
		t.Fatalf("result=%+v", result)
	}
	if actor.Body.Gold != 14 || actor.Body.Experience != 21 || flag(actor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("actor=%+v", actor.Body)
	}
	if actor.Body.Timers[burnTimerIndex] != (LegacyTimer{LastTime: 100, Interval: 2}) {
		t.Fatalf("burn timer=%+v", actor.Body.Timers[burnTimerIndex])
	}
	if _, ok := actor.Items.Items["coin-1"]; ok || !containsString(actor.Items.Inventory, "coin-2") || !containsString(actor.Items.Inventory, "bag") || !containsString(actor.Items.Items["bag"].Contents, "gem") {
		t.Fatalf("inventory after burn=%+v", actor.Items)
	}
	if result.Event.RoomID != 1 || result.Event.ActorID != "actor" || result.Event.ExcludeActorID != "actor" || result.Event.ItemID != "coin-1" || !strings.Contains(result.Event.Text, "Alice") {
		t.Fatalf("event=%+v", result.Event)
	}
	if !reflect.DeepEqual(before, original) {
		t.Fatal("planning mutated source snapshot")
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestBurnCooldownUsesInjectedClockAndSkipsHiddenClearAndRandom(t *testing.T) {
	s := burnStateFixture(t)
	actor := s.Players["actor"]
	actor.Body.Timers[burnTimerIndex] = LegacyTimer{LastTime: 100, Interval: 2}
	s.Players["actor"] = actor
	proposal, err := s.PlanBurn("actor", "동전", 1, 101, func(int, int) int {
		t.Fatal("cooldown consumed jackpot random")
		return 1
	})
	if err != nil || !proposal.Cooldown || proposal.WaitSeconds != 1 || proposal.ClearHidden || proposal.Burned || proposal.Response != "1초만 기다리세요.\r\n" {
		t.Fatalf("proposal=%+v err=%v", proposal, err)
	}
	next, result, err := s.ApplyBurn(proposal)
	if err != nil || !reflect.DeepEqual(next, s) || result.Action != "burn-rejected" || !result.Cooldown || result.Changed {
		t.Fatalf("next/result=%+v/%+v err=%v", next, result, err)
	}

	proposal, err = s.PlanBurn("actor", "동전", 1, 102, nil)
	if err != nil || proposal.Cooldown {
		t.Fatalf("boundary proposal=%+v err=%v", proposal, err)
	}
	if _, result, err = s.ApplyBurn(proposal); err != nil || !result.Burned {
		t.Fatalf("boundary result=%+v err=%v", result, err)
	}
}

func TestBurnRejectsProtectedItemsButClearsHiddenAndAllowsAdminQuest(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*Item)
		admin      bool
		wantAction string
	}{
		{name: "no-burn", configure: func(item *Item) { setObjectFlag(&item.Object.Flags, burnNoBurnFlag, true) }, wantAction: "burn-rejected"},
		{name: "quest", configure: func(item *Item) { item.Object.Quest = 1; item.Object.ShotsCurrent = 1 }, wantAction: "burn-rejected"},
		{name: "event", configure: func(item *Item) {
			item.Object.Flags[objectOneWevFlag/8] |= 1 << (objectOneWevFlag % 8)
			item.Object.ShotsCurrent = 1
		}, wantAction: "burn-rejected"},
		{name: "admin-quest", configure: func(item *Item) { item.Object.Quest = 1; item.Object.ShotsCurrent = 1 }, admin: true, wantAction: "burn"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := burnStateFixture(t)
			actor := s.Players["actor"]
			if tc.admin {
				actor.Body.Class = burnSubDMClass
			}
			item := actor.Items.Items["coin-1"]
			tc.configure(&item)
			actor.Items.Items["coin-1"] = item
			s.Players["actor"] = actor
			proposal, err := s.PlanBurn("actor", "동전", 1, 100, func(int, int) int {
				return 1
			})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyBurn(proposal)
			if err != nil {
				t.Fatal(err)
			}
			nextActor := next.Players["actor"]
			if result.Action != tc.wantAction || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
				t.Fatalf("result=%+v actor=%+v", result, nextActor.Body)
			}
			if tc.admin {
				if _, ok := nextActor.Items.Items["coin-1"]; ok || nextActor.Body.Gold != 14 || nextActor.Body.Experience != 21 {
					t.Fatalf("admin quest was not burned: %+v", nextActor)
				}
			} else if _, ok := nextActor.Items.Items["coin-1"]; !ok || nextActor.Body.Gold != 10 || nextActor.Body.Experience != 20 {
				t.Fatalf("protected item changed: %+v", nextActor)
			}
		})
	}
}

func TestBurnNestedEquippedAndUnresolvedInventoryFailClosed(t *testing.T) {
	s := burnStateFixture(t)
	for _, tc := range []struct {
		name     string
		itemName string
		wantID   string
	}{
		{name: "nested", itemName: "보석"},
		{name: "equipped", itemName: "검"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proposal, err := s.PlanBurn("actor", tc.itemName, 1, 100, func(int, int) int {
				t.Fatal("unselectable burn consumed jackpot random")
				return 1
			})
			if err != nil {
				t.Fatal(err)
			}
			next, result, err := s.ApplyBurn(proposal)
			nextActor := next.Players["actor"]
			if err != nil || result.Burned || result.Broadcast || result.ItemID != "" || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
				t.Fatalf("next/result=%+v/%+v err=%v", next, result, err)
			}
			if _, ok := next.Players["actor"].Items.Items["bag"]; !ok {
				t.Fatal("nested root disappeared")
			}
			if _, ok := next.Players["actor"].Items.Items["sword"]; !ok {
				t.Fatal("equipped root disappeared")
			}
		})
	}

	legacy := s.clone()
	legacyActor := legacy.Players["actor"]
	legacyActor.Items = nil
	legacyActor.Body.Inventory = []LegacyObject{{Name: "동전"}}
	legacy.Players["actor"] = legacyActor
	if _, err := legacy.PlanBurn("actor", "동전", 1, 100, nil); err == nil {
		t.Fatal("unresolved legacy inventory accepted")
	}
}

func TestBurnMatchesFindObjectVisibilityGate(t *testing.T) {
	s := burnStateFixture(t)
	actor := s.Players["actor"]
	item := actor.Items.Items["coin-1"]
	item.Object.Name = "투명동전"
	setObjectFlag(&item.Object.Flags, objectInvisibleFlag, true)
	actor.Items.Items["coin-1"] = item
	s.Players["actor"] = actor
	proposal, err := s.PlanBurn("actor", "투명동전", 1, 100, func(int, int) int {
		t.Fatal("undetected invisible burn consumed jackpot random")
		return 1
	})
	if err != nil {
		t.Fatal(err)
	}
	next, result, err := s.ApplyBurn(proposal)
	nextActor := next.Players["actor"]
	if err != nil || result.Burned || flag(nextActor.Body.Flags[:], playerHiddenStateFlag) {
		t.Fatalf("undetected invisible result=%+v err=%v", result, err)
	}
	if _, ok := next.Players["actor"].Items.Items["coin-1"]; !ok {
		t.Fatal("undetected invisible item was deleted")
	}

	actor = s.Players["actor"]
	actor.Body.Flags[playerDetectInvisibleFlag/8] |= 1 << (playerDetectInvisibleFlag % 8)
	s.Players["actor"] = actor
	proposal, err = s.PlanBurn("actor", "투명동전", 1, 100, nil)
	if err != nil || !proposal.Burned {
		t.Fatalf("detected invisible proposal=%+v err=%v", proposal, err)
	}
}

func TestApplyBurnRejectsStaleActorAndTamperedReceiptProjection(t *testing.T) {
	s := burnStateFixture(t)
	proposal, err := s.PlanBurn("actor", "동전", 1, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	changed := s.clone()
	changedActor := changed.Players["actor"]
	changedActor.Body.Gold++
	changed.Players["actor"] = changedActor
	if _, _, err := changed.ApplyBurn(proposal); err == nil {
		t.Fatal("stale actor proposal accepted")
	}

	tampered := proposal
	tampered.Response = "tampered"
	if _, _, err := s.ApplyBurn(tampered); err == nil {
		t.Fatal("tampered response accepted")
	}
}

func TestBurnJackpotRecordsInjectedRollWithoutRerunningItOnApply(t *testing.T) {
	s := burnStateFixture(t)
	calls := 0
	proposal, err := s.PlanBurn("actor", "동전", 1, 8, func(low, high int) int {
		calls++
		if low != 1 || high != 100 {
			t.Fatalf("roll range=%d..%d", low, high)
		}
		return 1
	})
	if err != nil || !proposal.Jackpot || proposal.JackpotRoll != 1 || calls != 1 {
		t.Fatalf("proposal=%+v calls=%d err=%v", proposal, calls, err)
	}
	next, result, err := s.ApplyBurn(proposal)
	if err != nil || !result.Jackpot || result.JackpotRoll != 1 || result.GoldAward != 100004 || result.ExperienceAward != 11 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if next.Players["actor"].Body.Gold != 100014 || next.Players["actor"].Body.Experience != 31 {
		t.Fatalf("jackpot actor=%+v", next.Players["actor"].Body)
	}
}
