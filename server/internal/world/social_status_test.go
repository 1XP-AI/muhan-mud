package world

import (
	"reflect"
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

func whoThresholdState(total int) State {
	rooms := map[int16]RoomState{
		1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor"}},
	}
	players := map[string]PlayerState{
		"actor": {Body: LegacyMonster{Name: "Alice", RoomID: 1, Type: 0, Level: 7, Class: 4, Race: 5}, Online: true, Title: "무명의 수호자"},
	}
	for i := 1; i < total; i++ {
		id := "p" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		roomID := int16(1)
		if i%2 == 0 {
			roomID = 2
		}
		if _, ok := rooms[roomID]; !ok {
			rooms[roomID] = RoomState{Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: roomID}}}
		}
		players[id] = PlayerState{Body: LegacyMonster{Name: id, RoomID: roomID, Type: 0, Level: byte(i), Class: byte(i % len(legacyInfoClassNames)), Race: byte(i % len(legacyInfoRaceNames))}, Online: true}
		room := rooms[roomID]
		room.PlayerIDs = append(room.PlayerIDs, id)
		rooms[roomID] = room
	}
	return State{Version: 1, Rooms: rooms, Players: players}
}

func TestPlayerWhoUsesCThresholdAndCanonicalLongFields(t *testing.T) {
	shortState := whoThresholdState(30)
	short := shortState.Players["actor"]
	short.Body.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
	short.Body.Daily[FamilyDailySlot].Max = 2
	shortState.Players["actor"] = short
	shortState.Family = &FamilyState{Members: map[int16][]FamilyMember{2: {{ID: "actor", Name: "Alice", Class: 4}}}}
	long, err := shortState.PlayerWho("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(long, "종족") || !strings.Contains(long, "패거리") || !strings.Contains(long, "칭호") || !strings.Contains(long, "검사") || !strings.Contains(long, "인간족") || !strings.Contains(long, "무명의 수호자") {
		t.Fatalf("30-player who did not use detailed projection: %q", long)
	}
	if strings.Contains(long, "마지막 명령") || strings.ContainsRune(long, '\x1b') {
		t.Fatalf("who invented unavailable descriptor/ANSI data: %q", long)
	}
	if !strings.Contains(long, "총 30명의 사용자가") {
		t.Fatalf("30-player summary missing: %q", long)
	}

	compactState := whoThresholdState(31)
	compact, err := compactState.PlayerWho("actor")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compact, "[레벨 직업]") || strings.Contains(compact, "종족") || strings.Contains(compact, "무명의 수호자") {
		t.Fatalf("31-player bare who did not use compact projection: %q", compact)
	}
	detailed, err := compactState.PlayerWho("actor", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detailed, "종족") || !strings.Contains(detailed, "인간족") || !strings.Contains(detailed, "칭호") || !strings.Contains(detailed, "무명의 수호자") {
		t.Fatalf("31-player explicit long who did not use detailed projection: %q", detailed)
	}
}

func TestPlayerWhoKeepsRoomFirstResidualOrderAndReadOnly(t *testing.T) {
	s := State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}}, PlayerIDs: []string{"actor", "z-room"}},
			2: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}}, PlayerIDs: []string{"a-room"}},
			3: {Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 3}}},
		},
		Players: map[string]PlayerState{
			"actor":  {Body: LegacyMonster{Name: "Actor", RoomID: 1, Type: 0, Class: 4}, Online: true},
			"z-room": {Body: LegacyMonster{Name: "Zed", RoomID: 1, Type: 0, Class: 4}, Online: true},
			"a-room": {Body: LegacyMonster{Name: "Alpha", RoomID: 2, Type: 0, Class: 4}, Online: true},
			"m-away": {Body: LegacyMonster{Name: "Mike", RoomID: 3, Type: 0, Class: 4}, Online: false},
			"b-away": {Body: LegacyMonster{Name: "Bravo", RoomID: 2, Type: 0, Class: 4}, Online: true},
		},
	}
	room := s.Rooms[2]
	room.PlayerIDs = append(room.PlayerIDs, "b-away")
	s.Rooms[2] = room
	before := s
	text, err := s.PlayerWho("actor", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, "Zed") > strings.Index(text, "Alpha") || strings.Index(text, "Alpha") > strings.Index(text, "Bravo") {
		t.Fatalf("order=%q", text)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("who mutated canonical state")
	}
	if strings.Contains(strings.ReplaceAll(text, "\r\n", ""), "\n") {
		t.Fatalf("who output contains bare LF: %q", text)
	}
}

func TestPlayerWhoPreservesVisibilitySelfBlindAndDMGates(t *testing.T) {
	s := whoThresholdState(4)
	viewer := s.Players["actor"]
	viewer.Body.Flags[playerBlindFlag/8] |= 1 << (playerBlindFlag % 8)
	s.Players["actor"] = viewer
	blind, err := s.PlayerWho("actor")
	if err != nil || blind != "당신은 눈이 멀어 있습니다!\r\n" {
		t.Fatalf("blind=%q err=%v", blind, err)
	}
	viewer.Body.Flags[playerBlindFlag/8] &^= 1 << (playerBlindFlag % 8)
	s.Players["actor"] = viewer
	targetID := "pb0"
	target := s.Players[targetID]
	target.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	s.Players[targetID] = target
	plain, err := s.PlayerWho("actor", true)
	if err != nil || strings.Contains(plain, target.Body.Name) {
		t.Fatalf("plain invisible=%q err=%v", plain, err)
	}
	viewer = s.Players["actor"]
	viewer.Body.Flags[playerDetectFlag/8] |= 1 << (playerDetectFlag % 8)
	s.Players["actor"] = viewer
	detected, err := s.PlayerWho("actor", true)
	if err != nil || !strings.Contains(detected, target.Body.Name) {
		t.Fatalf("detected invisible=%q err=%v", detected, err)
	}
	target.Body.Flags[playerInvisibleFlag/8] &^= 1 << (playerInvisibleFlag % 8)
	target.Body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
	target.Body.Class = playerDMClass
	s.Players[targetID] = target
	viewer = s.Players["actor"]
	viewer.Body.Flags[playerDetectFlag/8] &^= 1 << (playerDetectFlag % 8)
	s.Players["actor"] = viewer
	dmHidden, err := s.PlayerWho("actor", true)
	if err != nil || strings.Contains(dmHidden, target.Body.Name) {
		t.Fatalf("ordinary DM-invisible=%q err=%v", dmHidden, err)
	}
	viewer = s.Players["actor"]
	viewer.Body.Class = playerSubDMClass
	s.Players["actor"] = viewer
	dmVisible, err := s.PlayerWho("actor", true)
	if err != nil || !strings.Contains(dmVisible, target.Body.Name) {
		t.Fatalf("sub-DM DM-invisible=%q err=%v", dmVisible, err)
	}
	self := s.Players["actor"]
	self.Body.Flags[playerInvisibleFlag/8] |= 1 << (playerInvisibleFlag % 8)
	self.Body.Flags[playerDMInvisibleFlag/8] |= 1 << (playerDMInvisibleFlag % 8)
	s.Players["actor"] = self
	selfVisible, err := s.PlayerWho("actor", true)
	if err != nil || !strings.Contains(selfVisible, self.Body.Name) {
		t.Fatalf("self visibility=%q err=%v", selfVisible, err)
	}
}

func TestPlayerWhoFailsClosedForInvalidCanonicalIdentity(t *testing.T) {
	for _, mutate := range []func(*State){
		func(s *State) {
			p := s.Players["actor"]
			p.Body.Class = byte(len(legacyInfoClassNames))
			s.Players["actor"] = p
		},
		func(s *State) {
			p := s.Players["actor"]
			p.Body.Race = byte(len(legacyInfoRaceNames))
			s.Players["actor"] = p
		},
		func(s *State) {
			p := s.Players["actor"]
			p.Body.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
			p.Body.Daily[FamilyDailySlot].Max = byte(FamilyMaxID + 1)
			s.Players["actor"] = p
		},
	} {
		s := whoThresholdState(1)
		mutate(&s)
		if _, err := s.PlayerWho("actor"); err == nil {
			t.Fatal("invalid who identity accepted")
		}
	}
	if _, err := whoThresholdState(1).PlayerWho("missing"); err == nil {
		t.Fatal("missing viewer accepted")
	}
}

func TestPlayerWhoResolvesOnlyProvidedFamilyCatalog(t *testing.T) {
	s := whoThresholdState(1)
	p := s.Players["actor"]
	p.Body.Flags[FamilyMemberFlag/8] |= 1 << (FamilyMemberFlag % 8)
	p.Body.Daily[FamilyDailySlot].Max = 2
	s.Players["actor"] = p
	withoutCatalog, err := s.PlayerWho("actor", true)
	if err != nil || strings.Contains(withoutCatalog, "청룡") {
		t.Fatalf("unimported family leaked or failed: %q err=%v", withoutCatalog, err)
	}
	withCatalog, err := s.PlayerWhoWithOptions("actor", PlayerWhoOptions{Long: true, FamilyCatalog: FamilyCatalog{Families: map[int16]FamilyDefinition{2: {ID: 2, Name: "청룡", Boss: "문주"}}}})
	if err != nil || !strings.Contains(withCatalog, "청룡") {
		t.Fatalf("resolved family missing: %q err=%v", withCatalog, err)
	}
}
