package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func playerLookupFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1, Name: "광장"}},
				PlayerIDs: []string{"a", "b"},
			},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}},
		},
		Players: map[string]PlayerState{
			"a": {Body: LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Class: 4, Race: 5, Level: 3}, Online: true},
			"b": {Body: LegacyMonster{Name: "Bob", RoomID: 1, Type: 0, Class: 4, Race: 5, Level: 7}, Online: true},
			"c": {Body: LegacyMonster{Name: "Carol", RoomID: 2, Type: 0, Class: 4, Race: 5, Level: 9}, Online: false},
		},
	}
}

func setPlayerLookupFlag(body *LegacyMonster, bit int) {
	body.Flags[bit/8] |= 1 << (bit % 8)
}

func TestPlayerSearchUsesExactOnlineCanonicalNameAndVisibilityGates(t *testing.T) {
	s := playerLookupFixture()
	original := s
	text, err := s.PlayerSearch("a", "Bob")
	want := "사용자: Bob\r\n레벨: 7\r\n직업: 검사\r\n종족: 인간족\r\n"
	if err != nil || text != want {
		t.Fatalf("search=%q err=%v want=%q", text, err, want)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("player search mutated state")
	}
	for _, target := range []string{"Bo", "Carol", "missing"} {
		if _, err := s.PlayerSearch("a", target); !errors.Is(err, ErrPlayerLookupUnavailable) {
			t.Fatalf("target %q err=%v want unavailable", target, err)
		}
	}

	tests := []struct {
		name      string
		actorBit  int
		targetBit int
	}{
		{name: "target invisible", targetBit: playerLookupInvisibleFlag},
		{name: "target dm invisible", targetBit: playerLookupDMInvisibleFlag},
		{name: "actor blind", actorBit: playerLookupBlindFlag},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := playerLookupFixture()
			if tc.actorBit != 0 {
				p := s.Players["a"]
				setPlayerLookupFlag(&p.Body, tc.actorBit)
				s.Players["a"] = p
			}
			if tc.targetBit != 0 {
				p := s.Players["b"]
				setPlayerLookupFlag(&p.Body, tc.targetBit)
				s.Players["b"] = p
			}
			if _, err := s.PlayerSearch("a", "Bob"); !errors.Is(err, ErrPlayerLookupUnavailable) {
				t.Fatalf("err=%v want unavailable", err)
			}
		})
	}

	s = playerLookupFixture()
	actor := s.Players["a"]
	setPlayerLookupFlag(&actor.Body, playerLookupDetectFlag)
	s.Players["a"] = actor
	target := s.Players["b"]
	setPlayerLookupFlag(&target.Body, playerLookupInvisibleFlag)
	s.Players["b"] = target
	if text, err := s.PlayerSearch("a", "Bob"); err != nil || !strings.Contains(text, "Bob") {
		t.Fatalf("detect-invisible search=%q err=%v", text, err)
	}
}

func TestPlayerInformationIsCanonicalOnlineOnlyAndFailsClosed(t *testing.T) {
	s := playerLookupFixture()
	original := s
	text, err := s.PlayerInformation("a", "Bob")
	want := "사용자: Bob\r\n종족: 인간족\r\n직업: 검사\r\n현재 접속 중 입니다.\r\n"
	if err != nil || text != want {
		t.Fatalf("info=%q err=%v want=%q", text, err, want)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("player information mutated state")
	}
	if _, err := s.PlayerInformation("a", "Carol"); !errors.Is(err, ErrPlayerInfoCanonicalOnly) {
		t.Fatalf("offline info err=%v want canonical-only", err)
	}

	target := s.Players["b"]
	setPlayerLookupFlag(&target.Body, playerLookupDMInvisibleFlag)
	s.Players["b"] = target
	if _, err := s.PlayerInformation("a", "Bob"); !errors.Is(err, ErrPlayerInfoForbidden) {
		t.Fatalf("dm-invisible info err=%v want forbidden", err)
	}

	s = playerLookupFixture()
	target = s.Players["b"]
	target.Body.Class = byte(len(playerLookupClassNames))
	s.Players["b"] = target
	if _, err := s.PlayerInformation("a", "Bob"); !errors.Is(err, ErrPlayerInfoCanonicalOnly) {
		t.Fatalf("unsupported canonical info err=%v want canonical-only", err)
	}
}

func TestPlayerLookupRejectsAmbiguousCanonicalNames(t *testing.T) {
	s := playerLookupFixture()
	s.Players["duplicate"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 2, Type: 0, Class: 4, Race: 5}, Online: true}
	r := s.Rooms[2]
	r.PlayerIDs = []string{"duplicate"}
	s.Rooms[2] = r
	if _, err := s.PlayerSearch("a", "Bob"); !errors.Is(err, ErrPlayerLookupUnavailable) {
		t.Fatalf("ambiguous search err=%v want unavailable", err)
	}
	if _, err := s.PlayerInformation("a", "Bob"); !errors.Is(err, ErrPlayerInfoCanonicalOnly) {
		t.Fatalf("ambiguous info err=%v want canonical-only", err)
	}
}
