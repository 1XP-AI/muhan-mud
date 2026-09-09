package world

import (
	"reflect"
	"testing"
)

func directMessageFixture(t *testing.T) State {
	t.Helper()
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b", "c"},
			},
		},
		Players: map[string]PlayerState{
			"a":       {Body: LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4}, Online: true},
			"b":       {Body: LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4}, Online: true},
			"c":       {Body: LegacyMonster{Name: "Bobby", RoomID: 1, Type: 0, Class: 4}, Online: true},
			"offline": {Body: LegacyMonster{Name: "Offline", RoomID: 1, Type: 0, Class: 4}, Online: false},
		},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlanAndApplyDirectMessageResolvesExactPrefixAndProjectsEvent(t *testing.T) {
	s := directMessageFixture(t)
	proposal, err := s.PlanDirectMessage("a", "Bob", "안녕하세요")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.TargetID != "b" || proposal.TargetName != "Bob" || !proposal.Delivered {
		t.Fatalf("proposal=%+v", proposal)
	}
	next, result, err := s.ApplyDirectMessage(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(next, s) {
		t.Fatal("direct message changed canonical state")
	}
	if result.Response != "Bob님에게 말을 전달하였습니다.\r\n" || result.Event == nil || result.Event.TargetID != "b" {
		t.Fatalf("result=%+v", result)
	}
	if result.Event.Text != "\nAlice님이 당신에게 \"안녕하세요\"라고 이야기합니다.\r\n" {
		t.Fatalf("event=%+v", result.Event)
	}
	event, ok, err := s.DirectMessageEvent(proposal)
	if err != nil || !ok || !reflect.DeepEqual(event, *result.Event) {
		t.Fatalf("event projection=%+v ok=%v err=%v", event, ok, err)
	}

	prefix, err := s.PlanDirectMessage("a", "Bo", "prefix")
	if err != nil || prefix.TargetID != "b" {
		t.Fatalf("prefix proposal=%+v err=%v", prefix, err)
	}

	echoState := s
	actor := echoState.Players["a"]
	actor.Body.Flags[playerLocalEchoFlag/8] |= 1 << (playerLocalEchoFlag % 8)
	echoState.Players["a"] = actor
	echo, err := echoState.PlanDirectMessage("a", "Bob", "echo")
	if err != nil || echo.Response != "당신은 Bob님에게 \"echo\"라고 이야기합니다.\r\n" {
		t.Fatalf("echo proposal=%+v err=%v", echo, err)
	}
}

func TestPlanDirectMessageAppliesVisibilityAndListenBoundaries(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*State)
		wantError  bool
		wantReject bool
		wantDM     bool
	}{
		{
			name: "ordinary invisible target requires detect",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
				s.Players["b"] = target
			},
			wantError: true,
		},
		{
			name: "detect sees ordinary invisible target",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
				s.Players["b"] = target
				sender := s.Players["a"]
				sender.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
				s.Players["a"] = sender
			},
			wantDM: true,
		},
		{
			name: "DM invisible target hidden from ordinary sender",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
				s.Players["b"] = target
				other := s.Players["c"]
				other.Online = false
				s.Players["c"] = other
				room := s.Rooms[1]
				room.PlayerIDs = []string{"a", "b"}
				s.Rooms[1] = room
			},
			wantError: true,
		},
		{
			name: "DM sender bypasses DM invisibility",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
				s.Players["b"] = target
				sender := s.Players["a"]
				sender.Body.Class = playerDMClass
				s.Players["a"] = sender
			},
			wantDM: true,
		},
		{
			name: "receiver listen rejection is a no-op receipt",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerIgnoreAllSendFlag/8] |= 1 << (playerIgnoreAllSendFlag % 8)
				s.Players["b"] = target
			},
			wantReject: true,
		},
		{
			name: "DM sender bypasses receiver listen rejection",
			mutate: func(s *State) {
				target := s.Players["b"]
				target.Body.Flags[playerIgnoreAllSendFlag/8] |= 1 << (playerIgnoreAllSendFlag % 8)
				s.Players["b"] = target
				sender := s.Players["a"]
				sender.Body.Class = playerDMClass
				s.Players["a"] = sender
			},
			wantDM: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := directMessageFixture(t)
			tc.mutate(&s)
			if err := s.Validate(); err != nil {
				t.Fatal(err)
			}
			proposal, err := s.PlanDirectMessage("a", "Bob", "hello")
			if tc.wantError {
				if err == nil {
					t.Fatalf("proposal=%+v", proposal)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if proposal.Delivered != tc.wantDM {
				t.Fatalf("proposal=%+v want delivered=%v", proposal, tc.wantDM)
			}
			if tc.wantReject && proposal.Response != "Bob님은 이야기 듣기 거부 상태입니다.\r\n" {
				t.Fatalf("reject response=%q", proposal.Response)
			}
			next, result, err := s.ApplyDirectMessage(proposal)
			if err != nil || !reflect.DeepEqual(next, s) {
				t.Fatalf("next=%+v result=%+v err=%v", next, result, err)
			}
			if tc.wantReject && result.Event != nil {
				t.Fatal("listen rejection produced an event")
			}
		})
	}
}

func TestPlanDirectMessageSilenceEmptyAndStaleProposalFailClosed(t *testing.T) {
	s := directMessageFixture(t)
	empty, err := s.PlanDirectMessage("a", "Bob", "   ")
	if err != nil || empty.Delivered || empty.Response != "무슨 말을 전하시려구요?\r\n" {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	silentState := s
	silent := silentState.Players["a"]
	silent.Body.Flags[playerSilentStateFlag/8] |= 1 << (playerSilentStateFlag % 8)
	silentState.Players["a"] = silent
	silenced, err := silentState.PlanDirectMessage("a", "Bob", "hello")
	if err != nil || silenced.Delivered || silenced.Response != "당신은 말을 할 수 없습니다.\r\n" {
		t.Fatalf("silenced=%+v err=%v", silenced, err)
	}

	proposal, err := s.PlanDirectMessage("a", "Bob", "stale")
	if err != nil {
		t.Fatal(err)
	}
	changed := s
	target := changed.Players["b"]
	target.Body.Flags[playerIgnoreAllSendFlag/8] |= 1 << (playerIgnoreAllSendFlag % 8)
	changed.Players["b"] = target
	if _, _, err := changed.ApplyDirectMessage(proposal); err == nil {
		t.Fatal("stale listener state was accepted")
	}

	offline := s
	player := offline.Players["b"]
	player.Online = false
	offline.Players["b"] = player
	room := offline.Rooms[1]
	room.PlayerIDs = []string{"a", "c"}
	offline.Rooms[1] = room
	if err := offline.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := offline.PlanDirectMessage("a", "Offline", "hello"); err == nil {
		t.Fatal("offline target was accepted")
	}
}
