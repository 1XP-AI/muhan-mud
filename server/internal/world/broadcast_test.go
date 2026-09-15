package world

import (
	"strings"
	"testing"
)

func broadcastFixture(class, level byte) State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a"},
				Items:     &ItemCollection{Items: map[string]Item{}},
			},
		},
		Players: map[string]PlayerState{
			"a": {
				Body:   LegacyMonster{Name: "Alice", RoomID: 1, Class: class, Level: level, HPMax: 100, HPCurrent: 100},
				Online: true,
				Items:  &ItemCollection{Items: map[string]Item{}},
			},
		},
	}
}

func TestPlanBroadcastChargesDurableDailyAndHPAndBuildsGlobalEvent(t *testing.T) {
	s := broadcastFixture(4, 20)
	actor := s.Players["a"]
	actor.Body.Flags[broadcastLimitFlag/8] |= 1 << (broadcastLimitFlag % 8)
	actor.Body.Daily[broadcastDailyIndex] = LegacyDaily{Max: 2, Current: 2, LastTime: 1000}
	s.Players["a"] = actor

	next, result, err := s.PlanBroadcast("a", BroadcastChat, "안녕하세요", BroadcastOptions{Now: 2000, LastAt: 0, GlobalAt: 0})
	if err != nil || !result.Broadcast || result.Event == nil || result.Discount != 2 || !result.DailyUsed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(result.Event.Text, "Alice> 안녕하세요") || result.Response != result.Event.Text {
		t.Fatalf("event/response=%q/%q", result.Event.Text, result.Response)
	}
	if next.Players["a"].Body.HPCurrent != 98 || next.Players["a"].Body.Daily[0].Current != 1 {
		t.Fatalf("next actor=%+v", next.Players["a"].Body)
	}
	if s.Players["a"].Body.HPCurrent != 100 || s.Players["a"].Body.Daily[0].Current != 2 {
		t.Fatal("input snapshot mutated")
	}
}

func TestPlanBroadcastHonorsLegacyGatesAndPreservesDailyChargeOnLaterFailure(t *testing.T) {
	cases := []struct {
		name      string
		kind      BroadcastKind
		mutate    func(*LegacyMonster)
		want      string
		wantDaily byte
	}{
		{name: "silent chat", kind: BroadcastChat, mutate: func(p *LegacyMonster) { p.Flags[broadcastSilentFlag/8] |= 1 << (broadcastSilentFlag % 8) }, want: "목소리가 너무 작아 잡담", wantDaily: 1},
		{name: "low level cheer", kind: BroadcastCheer, mutate: func(p *LegacyMonster) { p.Level = 19 }, want: "레벨로는 환호", wantDaily: 1},
		{name: "empty daily", kind: BroadcastChat, mutate: func(p *LegacyMonster) { p.Daily[0].Current = 0 }, want: "오늘 잡담의 한계를", wantDaily: 0},
		{name: "low hp", kind: BroadcastCheer, mutate: func(p *LegacyMonster) { p.HPCurrent = 2 }, want: "목숨이 위태로워 환호", wantDaily: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := broadcastFixture(4, 20)
			actor := s.Players["a"]
			actor.Body.Flags[broadcastLimitFlag/8] |= 1 << (broadcastLimitFlag % 8)
			actor.Body.Daily[0] = LegacyDaily{Max: 2, Current: 2, LastTime: 1000}
			tc.mutate(&actor.Body)
			s.Players["a"] = actor
			next, result, err := s.PlanBroadcast("a", tc.kind, "hello", BroadcastOptions{Now: 2000, LastAt: 0, GlobalAt: 0})
			if err != nil || result.Broadcast || !strings.Contains(result.Response, tc.want) {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if next.Players["a"].Body.Daily[0].Current != tc.wantDaily {
				t.Fatalf("daily=%+v want=%d", next.Players["a"].Body.Daily[0], tc.wantDaily)
			}
		})
	}
}

func TestPlanBroadcastUsesCheerOptOutForActorResponse(t *testing.T) {
	s := broadcastFixture(4, 20)
	actor := s.Players["a"]
	actor.Body.Flags[broadcastNoCheerFlag/8] |= 1 << (broadcastNoCheerFlag % 8)
	s.Players["a"] = actor
	next, result, err := s.PlanBroadcast("a", BroadcastCheer, "힘내", BroadcastOptions{Now: 100, LastAt: 0})
	if err != nil || !result.Broadcast || result.Response != "" || result.Event == nil || next.Players["a"].Body.HPCurrent != 98 {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"].Body, result, err)
	}
}

func TestPlanBroadcastSkipsDailyQuotaDuringGlobalCooldown(t *testing.T) {
	s := broadcastFixture(4, 20)
	actor := s.Players["a"]
	actor.Body.Flags[broadcastLimitFlag/8] |= 1 << (broadcastLimitFlag % 8)
	actor.Body.Daily[0] = LegacyDaily{Max: 2, Current: 2, LastTime: 1000}
	s.Players["a"] = actor
	next, result, err := s.PlanBroadcast("a", BroadcastChat, "welcome", BroadcastOptions{Now: 1005, LastAt: 0, GlobalAt: 1000})
	if err != nil || !result.Broadcast || result.DailyUsed || next.Players["a"].Body.Daily[0].Current != 2 {
		t.Fatalf("next=%+v result=%+v err=%v", next.Players["a"].Body, result, err)
	}
}

func TestPlanBroadcastRejectsControlAndOversizedText(t *testing.T) {
	s := broadcastFixture(4, 20)
	for _, text := range []string{"bad\ntext", strings.Repeat("가", MaxBroadcastTextBytes/3+1)} {
		if _, _, err := s.PlanBroadcast("a", BroadcastChat, text, BroadcastOptions{}); err == nil {
			t.Fatalf("accepted invalid text %q", text)
		}
	}
}
