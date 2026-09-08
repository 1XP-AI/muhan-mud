package world

import (
	"strings"
	"testing"
)

func TestPlayerWhoUsesDeterministicOrderAndVisibility(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Name = "Alice"
	s.Players["a"] = p
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1, Level: 2, Class: 4}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	b := s.Players["b"]
	b.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	s.Players["b"] = b
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	s.Rooms[1] = r
	text, err := s.PlayerWho("a")
	if err != nil || !strings.Contains(text, "Alice") || strings.Contains(text, "Bob") {
		t.Fatalf("who=%q err=%v", text, err)
	}
	p = s.Players["a"]
	p.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	s.Players["a"] = p
	text, err = s.PlayerWho("a")
	if err != nil || !strings.Contains(text, "Bob") {
		t.Fatalf("detected who=%q err=%v", text, err)
	}
}

func TestPlayerGroupUsesMixedFollowerOrder(t *testing.T) {
	s := stateFixture()
	p := s.Players["a"]
	p.Body.Name = "Alice"
	p.FollowerRefs = []EntityRef{{Kind: "player", ID: "b"}, {Kind: "npc", ID: "wolf"}}
	p.FollowerIDs = []string{"b"}
	p.NPCFollowerIDs = []string{"wolf"}
	s.Players["a"] = p
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1, HPCurrent: 20, MPCurrent: 5}, Online: true, FollowingID: "a", Items: &ItemCollection{Items: map[string]Item{}}}
	r := s.Rooms[1]
	r.PlayerIDs = append(r.PlayerIDs, "b")
	r.NPCIDs = append(r.NPCIDs, "wolf")
	s.Rooms[1] = r
	s.NPCs = map[string]NPCState{"wolf": {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPCurrent: 9, MPCurrent: 1}, FollowingPlayerID: "a"}}
	text, err := s.PlayerGroup("a")
	if err != nil || strings.Index(text, "Bob") > strings.Index(text, "늑대") || !strings.Contains(text, "Alice") {
		t.Fatalf("group=%q err=%v", text, err)
	}
}
