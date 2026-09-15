package world

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func enemyStatusFixture() State {
	return State{
		Version: 1,
		Rooms: map[int16]RoomState{
			1: {
				Resource:  LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 1}},
				PlayerIDs: []string{"actor"},
				NPCIDs:    []string{"wolf", "guard"},
			},
			2: {
				Resource: LegacyRoom{LegacyRoomHeader: LegacyRoomHeader{ID: 2}},
				NPCIDs:   []string{"remote"},
			},
		},
		Players: map[string]PlayerState{
			"actor": {
				Body:   LegacyMonster{Name: "Alice", Type: 0, RoomID: 1},
				Online: true,
			},
		},
		NPCs: map[string]NPCState{
			"wolf":   {Body: LegacyMonster{Name: "늑대", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 100}},
			"guard":  {Body: LegacyMonster{Name: "경비", Type: 1, RoomID: 1, HPMax: 100, HPCurrent: 50}},
			"remote": {Body: LegacyMonster{Name: "원격", Type: 1, RoomID: 2, HPMax: 100, HPCurrent: 100}},
		},
	}
}

func TestEnemyStatusProjectsCanonicalNPCWithDeterministicBar(t *testing.T) {
	s := enemyStatusFixture()
	original := s
	projection, err := s.ProjectEnemyStatus("actor", "경비")
	if err != nil {
		t.Fatal(err)
	}
	if projection.TargetID != "guard" || projection.TargetName != "경비" || projection.HPCurrent != 50 || projection.HPMax != 100 || projection.Filled != 8 || projection.Width != EnemyStatusBarWidth {
		t.Fatalf("projection=%+v", projection)
	}
	if projection.Bar != "[========       ]" || projection.Response != "경비 : [========       ]\r\n" {
		t.Fatalf("projection=%+v", projection)
	}
	text, err := s.EnemyStatus("actor", "경비")
	if err != nil || text != projection.Response {
		t.Fatalf("text=%q projection=%q err=%v", text, projection.Response, err)
	}
	if !reflect.DeepEqual(s, original) {
		t.Fatal("enemy status mutated the source snapshot")
	}
}

func TestEnemyStatusBarMatchesDisplayStatusCellProjection(t *testing.T) {
	tests := []struct {
		name     string
		current  int16
		maximum  int16
		want     string
		wantFill int
	}{
		{name: "dead", current: 0, maximum: 100, want: "[               ]", wantFill: 0},
		{name: "one percent rounds up", current: 1, maximum: 100, want: "[=              ]", wantFill: 1},
		{name: "third", current: 1, maximum: 3, want: "[=====          ]", wantFill: 5},
		{name: "half rounds up", current: 50, maximum: 100, want: "[========       ]", wantFill: 8},
		{name: "full", current: 100, maximum: 100, want: "[===============]", wantFill: 15},
		{name: "over max clamps", current: 101, maximum: 100, want: "[===============]", wantFill: 15},
		{name: "negative current is empty", current: -1, maximum: 100, want: "[               ]", wantFill: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bar, err := EnemyStatusBar(tc.current, tc.maximum)
			if err != nil || bar != tc.want || strings.Count(bar, "=") != tc.wantFill {
				t.Fatalf("bar=%q fills=%d err=%v", bar, strings.Count(bar, "="), err)
			}
			if len([]rune(bar)) != EnemyStatusBarWidth+2 {
				t.Fatalf("bar width=%d want=%d", len([]rune(bar)), EnemyStatusBarWidth+2)
			}
		})
	}
}

func TestEnemyStatusUsesCanonicalRoomOrderForDuplicateNames(t *testing.T) {
	s := enemyStatusFixture()
	second := s.NPCs["guard"]
	second.Body.Name = "늑대"
	second.Body.HPCurrent = 1
	s.NPCs["guard"] = second

	projection, err := s.ProjectEnemyStatus("actor", "늑대")
	if err != nil {
		t.Fatal(err)
	}
	if projection.TargetID != "wolf" || projection.HPCurrent != 100 {
		t.Fatalf("projection=%+v; want first room.NPCIDs entry", projection)
	}
}

func TestEnemyStatusRequiresExactSameRoomNPCIdentity(t *testing.T) {
	s := enemyStatusFixture()
	for _, target := range []string{"늑", "원격", "Alice", "없는괴물"} {
		if _, err := s.EnemyStatus("actor", target); !errors.Is(err, ErrEnemyStatusTargetAbsent) {
			t.Fatalf("target=%q err=%v", target, err)
		}
	}
	if _, err := s.EnemyStatus("actor", ""); !errors.Is(err, ErrEnemyStatusTargetRequired) {
		t.Fatalf("empty target err=%v", err)
	}
}

func TestEnemyStatusBlindGateDoesNotResolveTarget(t *testing.T) {
	s := enemyStatusFixture()
	actor := s.Players["actor"]
	actor.Body.Flags[enemyStatusBlindFlag/8] |= 1 << (enemyStatusBlindFlag % 8)
	s.Players["actor"] = actor
	projection, err := s.ProjectEnemyStatus("actor", "없는괴물")
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Blind || projection.TargetID != "" || projection.Response != EnemyStatusBlindResponse {
		t.Fatalf("projection=%+v", projection)
	}
	if text, err := s.EnemyStatus("actor", "늑대"); err != nil || text != EnemyStatusBlindResponse {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestEnemyStatusRejectsUnresolvedCanonicalAndVitalsBoundaries(t *testing.T) {
	s := enemyStatusFixture()
	legacy := s
	legacy.NPCs = nil
	room := legacy.Rooms[1]
	room.NPCIDs = nil
	legacy.Rooms[1] = room
	room = legacy.Rooms[2]
	room.NPCIDs = nil
	legacy.Rooms[2] = room
	if _, err := legacy.EnemyStatus("actor", "늑대"); !errors.Is(err, ErrEnemyStatusCanonicalOnly) {
		t.Fatalf("legacy NPC state err=%v", err)
	}
	invalid := enemyStatusFixture()
	npc := invalid.NPCs["wolf"]
	npc.Body.HPMax = 0
	invalid.NPCs["wolf"] = npc
	if _, err := invalid.EnemyStatus("actor", "늑대"); !errors.Is(err, ErrEnemyStatusVitalsUnavailable) {
		t.Fatalf("invalid HP maximum err=%v", err)
	}
	if _, err := EnemyStatusBar(1, 0); !errors.Is(err, ErrEnemyStatusVitalsUnavailable) {
		t.Fatalf("zero maximum bar err=%v", err)
	}
}

func TestEnemyStatusVisibilityFollowsCanonicalCreatureLookup(t *testing.T) {
	s := enemyStatusFixture()
	npc := s.NPCs["wolf"]
	npc.Body.Flags[enemyStatusInvisibleFlag/8] |= 1 << (enemyStatusInvisibleFlag % 8)
	s.NPCs["wolf"] = npc
	if _, err := s.EnemyStatus("actor", "늑대"); !errors.Is(err, ErrEnemyStatusTargetAbsent) {
		t.Fatalf("invisible NPC was exposed: %v", err)
	}
	actor := s.Players["actor"]
	actor.Body.Flags[enemyStatusDetectInvisibleFlag/8] |= 1 << (enemyStatusDetectInvisibleFlag % 8)
	s.Players["actor"] = actor
	if text, err := s.EnemyStatus("actor", "늑대"); err != nil || !strings.Contains(text, "늑대 : [===============]") {
		t.Fatalf("detected NPC text=%q err=%v", text, err)
	}
}
