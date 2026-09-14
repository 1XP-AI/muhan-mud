package world

import (
	"errors"
	"strings"
	"testing"
)

func TestCurrentSceneUsesCanonicalItemsAndViewer(t *testing.T) {
	s := playerDeathFixture()
	r := s.Rooms[1]
	r.Resource.Name = "광장"
	r.Items = &ItemCollection{Items: map[string]Item{"floor": {Object: LegacyObject{Name: "검"}}}, Inventory: []string{"floor"}}
	s.Rooms[1] = r
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "광장") || !strings.Contains(text, "검") || strings.Contains(text, "Alice님") {
		t.Fatalf("%q %v", text, err)
	}
	p := s.Players["a"]
	p.Body.Flags[5] |= 4
	s.Players["a"] = p
	text, err = s.CurrentScene("a", 12)
	if err != nil || strings.Contains(text, "검") {
		t.Fatalf("blind %q %v", text, err)
	}
}

func TestCurrentSceneResolvesOnlyNecessaryLighting(t *testing.T) {
	for _, mode := range []string{"daylight", "blind", "own-light", "other-light", "unknown-dark"} {
		t.Run(mode, func(t *testing.T) {
			s := playerDeathFixture()
			room := s.Rooms[1]
			room.PlayerIDs = append(room.PlayerIDs, "unknown", "lit")
			if mode != "daylight" {
				room.Resource.Flags[1] |= 1
			}
			s.Players["unknown"] = PlayerState{Body: LegacyMonster{Name: "Unknown", RoomID: 1}, Online: true}
			s.Players["lit"] = PlayerState{Body: LegacyMonster{Name: "Lit", RoomID: 1}, Online: true}
			p := s.Players["a"]
			switch mode {
			case "blind":
				p.Body.Flags[5] |= 4
			case "own-light":
				p.Body.Flags[2] |= 2
			case "other-light":
				other := s.Players["lit"]
				other.Body.Flags[2] |= 2
				s.Players["lit"] = other
			}
			s.Players["a"], s.Rooms[1] = p, room
			_, err := s.CurrentScene("a", 12)
			if (err != nil) != (mode == "unknown-dark") {
				t.Fatalf("%s lighting: %v", mode, err)
			}
		})
	}
}

func combatNoticeSceneFixture() State {
	s := playerDeathFixture()
	room := s.Rooms[1]
	room.Resource.Name = "광장"
	room.PlayerIDs = []string{"a", "b"}
	room.NPCIDs = []string{"npc-wolf", "npc-goblin"}
	s.Rooms[1] = room
	s.Players["b"] = PlayerState{Body: LegacyMonster{Name: "Bob", RoomID: 1}, Online: true, Items: &ItemCollection{Items: map[string]Item{}}}
	s.NPCs = map[string]NPCState{
		"npc-wolf":   {Body: LegacyMonster{Name: "늑대", Description: "회색 늑대다.", Type: 1, RoomID: 1}, Enemies: []NPCEnemy{}},
		"npc-goblin": {Body: LegacyMonster{Name: "고블린", Description: "고블린이 서 있습니다.", Type: 1, RoomID: 1}, Enemies: []NPCEnemy{}},
	}
	return s
}

func TestCurrentSceneCombatNoticeMatchesDisplayRomAgainstViewer(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	original := s
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "회색 늑대다.\n") || !strings.HasSuffix(text, "늑대가 당신과 싸우고 있습니다.\n") {
		t.Fatalf("scene=%q err=%v", text, err)
	}
	if strings.Contains(text, "고블린이 당신과") || strings.Contains(text, "Bob님과") {
		t.Fatalf("peaceful goblin leaked combat: %q", text)
	}
	if !reflectDeepEqualCombatState(s, original) {
		t.Fatal("CurrentScene mutated combat state")
	}
}

func TestCurrentSceneCombatNoticeMatchesDisplayRomAgainstOtherPlayer(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	goblin := s.NPCs["npc-goblin"]
	goblin.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 3}}
	s.NPCs["npc-goblin"] = goblin
	text, err := s.CurrentScene("a", 12)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "늑대가 Bob님과 싸우고 있습니다.\n") || !strings.Contains(text, "고블린이 당신과 싸우고 있습니다.\n") {
		t.Fatalf("scene=%q", text)
	}
	wolfIdx := strings.Index(text, "늑대가 Bob님과 싸우고 있습니다.\n")
	goblinIdx := strings.Index(text, "고블린이 당신과 싸우고 있습니다.\n")
	if wolfIdx < 0 || goblinIdx < wolfIdx {
		t.Fatalf("first_mon order lost: %q", text)
	}
}

func TestCurrentSceneCombatNoticeSkipsUnresolvableFirstEnemy(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	room := s.Rooms[1]
	room.PlayerIDs = []string{"a"}
	s.Rooms[1] = room
	other := s.Players["b"]
	other.Body.RoomID = 1008
	s.Players["b"] = other
	away := s.Rooms[1008]
	away.PlayerIDs = append(away.PlayerIDs, "b")
	s.Rooms[1008] = away
	text, err := s.CurrentScene("a", 12)
	if err != nil || strings.Contains(text, "싸우고") {
		t.Fatalf("absent first_ply still printed: %q err=%v", text, err)
	}

	s = combatNoticeSceneFixture()
	wolf = s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "npc", ID: "npc-goblin"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	text, err = s.CurrentScene("a", 12)
	if err != nil || strings.Contains(text, "싸우고") {
		t.Fatalf("npc first_enm searched first_ply: %q err=%v", text, err)
	}

	s = combatNoticeSceneFixture()
	other = s.Players["b"]
	other.Body.Flags[combatInvisibleFlag/8] |= 1 << (combatInvisibleFlag % 8)
	s.Players["b"] = other
	wolf = s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "b"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	text, err = s.CurrentScene("a", 12)
	if err != nil || strings.Contains(text, "싸우고") {
		t.Fatalf("MINVIS without PDINVI: %q err=%v", text, err)
	}
	actor := s.Players["a"]
	actor.Body.Flags[21/8] |= 1 << (21 % 8)
	s.Players["a"] = actor
	text, err = s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "늑대가 Bob(*)님과 싸우고 있습니다.\n") {
		t.Fatalf("detected invisible target=%q err=%v", text, err)
	}
}

func TestCurrentSceneCombatNoticeFailsClosedWhenUnmigrated(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.CurrentScene("a", 12); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("nil Enemies: %v", err)
	}

	s = combatNoticeSceneFixture()
	wolf = s.NPCs["npc-wolf"]
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: -1}}
	s.NPCs["npc-wolf"] = wolf
	if _, err := s.CurrentScene("a", 12); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("negative damage: %v", err)
	}

	s = playerDeathFixture()
	room := s.Rooms[1]
	room.Resource.Name = "광장"
	room.Resource.Monsters = []LegacyMonster{{Name: "늑대", Description: "회색 늑대다.", Type: 1}}
	s.Rooms[1] = room
	if _, err := s.CurrentScene("a", 12); !errors.Is(err, ErrRoomCombatUnmigrated) {
		t.Fatalf("legacy monsters: %v", err)
	}
}

func TestCurrentSceneCombatNoticeDoesNotScanDarkRoom(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Enemies = nil
	s.NPCs["npc-wolf"] = wolf
	room := s.Rooms[1]
	room.Resource.Flags[1] |= 1
	s.Rooms[1] = room
	actor := s.Players["a"]
	actor.Body.Flags[5] |= 4
	s.Players["a"] = actor
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "너무 어두워서") || strings.Contains(text, "싸우고") {
		t.Fatalf("dark combat scan=%q err=%v", text, err)
	}
}

func TestCurrentSceneCombatNoticeAppendsMagicSuffixParticle(t *testing.T) {
	s := combatNoticeSceneFixture()
	wolf := s.NPCs["npc-wolf"]
	wolf.Body.Flags[combatMonsterMagicFlag/8] |= 1 << (combatMonsterMagicFlag % 8)
	wolf.Enemies = []NPCEnemy{{Target: EntityRef{Kind: "player", ID: "a"}, Damage: 0}}
	s.NPCs["npc-wolf"] = wolf
	actor := s.Players["a"]
	actor.Body.Flags[20/8] |= 1 << (20 % 8)
	s.Players["a"] = actor
	text, err := s.CurrentScene("a", 12)
	if err != nil || !strings.Contains(text, "늑대(주문)가 당신과 싸우고 있습니다.\n") {
		t.Fatalf("magic crt_str=%q err=%v", text, err)
	}
}

func reflectDeepEqualCombatState(got, want State) bool {
	if len(got.NPCs) != len(want.NPCs) {
		return false
	}
	for id, npc := range want.NPCs {
		other, ok := got.NPCs[id]
		if !ok || other.Body.Name != npc.Body.Name || len(other.Enemies) != len(npc.Enemies) {
			return false
		}
		for i := range npc.Enemies {
			if other.Enemies[i] != npc.Enemies[i] {
				return false
			}
		}
	}
	return got.Players["a"].Body.RoomID == want.Players["a"].Body.RoomID
}
